package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/store"
)

type CoreInfoProvider interface {
	Info() model.CoreInfo
}

// Manager owns the Xray process and the local proxy state.
type Manager struct {
	mu    sync.RWMutex
	store *store.Store
	core  CoreInfoProvider
	cmd   *exec.Cmd
	done  chan struct{}
	job   *processJob
	// embedded holds the in-process Xray-core instance when this build runs the
	// core as a Go library (Android / embeddedcore); cmd stays nil in that mode.
	embedded           embeddedInstance
	embeddedDone       chan struct{}
	state              model.ProxyState
	onStateChange      func(model.ProxyState)
	sampler            *trafficSampler
	sbSampler          *singboxTrafficSampler
	onTraffic          func(model.TrafficStats)
	tunCleanup         func() error
	systemProxyCleanup func() error
	logsMu             sync.Mutex
	logs               []model.LogEntry
	logSeq             int64
	onLog              func(model.LogEntry)

	// extraRulesProvider supplies routing rules generated outside the store
	// (for example the rules expanded from remote rule sets). It is invoked
	// when a configuration is generated and may be nil.
	extraRulesProvider func() []model.RoutingRule

	// Core watchdog state. All fields are guarded by mu.
	//
	// userRequestedStop records that Stop was called, so the monitor can tell an
	// intentional exit from a crash. A manual Start clears it again.
	userRequestedStop bool
	// startInProgress serializes concurrent starts, including automatic restarts
	// triggered by the core watchdog.
	startInProgress bool
	// restartAttempts counts the consecutive automatic restarts performed since
	// the core last ran stably. A manual Start resets it; the watchdog also resets
	// it once the core ran for coreStableRunThreshold without exiting.
	restartAttempts int
	// runningSince is when the current (or most recently running) core reached
	// the running state.
	runningSince time.Time
	// pendingRestart is the scheduled or in-flight automatic restart, if any.
	pendingRestart *coreRestartPlan
}

func NewManager(st *store.Store, coreProvider CoreInfoProvider) *Manager {
	return &Manager{
		store: st,
		core:  coreProvider,
		state: model.ProxyState{Status: "stopped"},
	}
}

func (m *Manager) SetOnStateChange(callback func(model.ProxyState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStateChange = callback
}

// SetOnTraffic registers the callback invoked with every live traffic sample.
// The callback runs on the sampler goroutine and must not call back into the
// manager in a blocking way.
func (m *Manager) SetOnTraffic(callback func(model.TrafficStats)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onTraffic = callback
}

// Traffic returns the latest live traffic snapshot. Once the proxy is stopped
// (or if it was never running) a zero value with Running=false is returned.
func (m *Manager) Traffic() model.TrafficStats {
	m.mu.RLock()
	sampler := m.sampler
	sbSampler := m.sbSampler
	running := (m.cmd != nil || m.embedded != nil) && m.state.Status == "running"
	m.mu.RUnlock()
	if !running {
		return model.TrafficStats{}
	}
	if sbSampler != nil {
		return sbSampler.stats()
	}
	if sampler == nil {
		return model.TrafficStats{}
	}
	return sampler.stats()
}

// startTrafficSampler starts polling the Xray stats API. It does nothing for an
// invalid API port, when the proxy is no longer running, or if a sampler is
// already active.
func (m *Manager) startTrafficSampler(corePath string, apiPort int) {
	if corePath == "" || apiPort <= 0 {
		return
	}
	m.mu.Lock()
	if m.sampler != nil || m.cmd == nil || m.state.Status != "running" {
		m.mu.Unlock()
		return
	}
	sampler := newTrafficSampler(m.emitTraffic)
	m.sampler = sampler
	m.mu.Unlock()
	sampler.start(context.Background(), corePath, apiPort)
}

// startSingBoxTrafficSampler streams per-second rates from the sing-box Clash
// API. It is a no-op when the proxy is not running, the API port is invalid or
// the sampler is already active.
func (m *Manager) startSingBoxTrafficSampler(apiPort int) {
	if apiPort <= 0 {
		return
	}
	m.mu.Lock()
	if m.sbSampler != nil || m.embedded != nil || m.cmd == nil || m.state.Status != "running" {
		m.mu.Unlock()
		return
	}
	sampler := newSingBoxTrafficSampler(m.emitTraffic)
	m.sbSampler = sampler
	m.mu.Unlock()
	sampler.start(context.Background(), apiPort)
}

// stopTrafficSampler cancels the sampling goroutines and clears the runtime
// traffic state. Concurrent callers (Stop, monitor, fail) are harmless.
func (m *Manager) stopTrafficSampler() {
	m.mu.Lock()
	sampler := m.sampler
	sbSampler := m.sbSampler
	m.sampler = nil
	m.sbSampler = nil
	m.mu.Unlock()
	if sampler != nil {
		sampler.stop()
	}
	if sbSampler != nil {
		sbSampler.stop()
	}
	if sampler != nil || sbSampler != nil {
		m.emitTraffic(model.TrafficStats{})
	}
}

func (m *Manager) emitTraffic(stats model.TrafficStats) {
	m.mu.RLock()
	callback := m.onTraffic
	m.mu.RUnlock()
	if callback != nil {
		callback(stats)
	}
}

func (m *Manager) State() model.ProxyState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enrichNodeState(m.state)
}

func (m *Manager) enrichNodeState(state model.ProxyState) model.ProxyState {
	// Load balancing mode sets a descriptive proxy-wide node name; never replace
	// it with the active node record.
	if state.NodeName != "" {
		return state
	}
	node, ok := m.store.ActiveNode()
	if !ok {
		return state
	}
	state.NodeID = node.ID
	state.NodeName = node.Name
	state.NodeProtocol = node.Protocol
	state.NodeAddress = node.Address
	state.NodePort = node.Port
	if sub, err := m.store.FindSubscription(node.SubscriptionID); err == nil {
		state.NodeSubscription = sub.Name
	}
	state.ProxyMode = m.store.Settings().ProxyMode
	return state
}
func (m *Manager) SetOnLog(callback func(model.LogEntry)) {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	m.onLog = callback
}

// SetExtraRulesProvider registers the callback that supplies additional routing
// rules (for example the rules expanded from remote rule sets). The rules are
// appended after the stored user rules when a configuration is generated, so
// user rules keep their priority. Passing nil removes the provider.
func (m *Manager) SetExtraRulesProvider(provider func() []model.RoutingRule) {
	m.mu.Lock()
	m.extraRulesProvider = provider
	m.mu.Unlock()
}

// extraRules snapshots the registered extra rules provider under the manager
// lock and invokes it without holding the lock, so the provider may read the
// store or take its own locks without risking a deadlock. A nil provider
// contributes no rules.
func (m *Manager) extraRules() []model.RoutingRule {
	m.mu.RLock()
	provider := m.extraRulesProvider
	m.mu.RUnlock()
	if provider == nil {
		return nil
	}
	return provider()
}

// mergeRoutingRules appends the provider-supplied rules after the stored user
// rules, preserving the user rule order and priority.
func mergeRoutingRules(userRules, extraRules []model.RoutingRule) []model.RoutingRule {
	if len(extraRules) == 0 {
		return userRules
	}
	merged := make([]model.RoutingRule, 0, len(userRules)+len(extraRules))
	merged = append(merged, userRules...)
	merged = append(merged, extraRules...)
	return merged
}

func (m *Manager) Logs() []model.LogEntry {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	return append([]model.LogEntry(nil), m.logs...)
}

func (m *Manager) ClearLogs() {
	m.logsMu.Lock()
	m.logs = nil
	m.logsMu.Unlock()
}

func (m *Manager) appendLog(level string, source string, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	m.logsMu.Lock()
	m.logSeq++
	entry := model.LogEntry{
		ID:        strconv.FormatInt(m.logSeq, 10),
		Timestamp: model.NowRFC3339(),
		Level:     level,
		Source:    source,
		Message:   message,
	}
	m.logs = append(m.logs, entry)
	if len(m.logs) > 1500 {
		m.logs = append([]model.LogEntry(nil), m.logs[len(m.logs)-1500:]...)
	}
	callback := m.onLog
	m.logsMu.Unlock()
	if callback != nil {
		callback(entry)
	}
}

func (m *Manager) captureLogStream(stream io.Reader, source string) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		m.appendLog(logLevel(line), source, line)
	}
}

func logLevel(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "[debug]"):
		return "debug"
	case strings.Contains(lower, "[warning]"), strings.Contains(lower, "[warn]"):
		return "warning"
	case strings.Contains(lower, "[error]"):
		return "error"
	case strings.Contains(lower, "failed"), strings.Contains(lower, "fatal"):
		return "error"
	default:
		return "info"
	}
}

// Core watchdog policy. The watchdog reacts only to an unexpected exit of a core
// that had reached the running state: it never fights a user-requested Stop and
// gives up after a bounded number of consecutive restart attempts.
const (
	// coreRestartBaseDelay is the backoff before the first automatic restart;
	// every further consecutive attempt doubles it (1s, 2s, 4s, 8s).
	coreRestartBaseDelay = time.Second
	// coreRestartMaxDelay caps the exponential backoff ladder.
	coreRestartMaxDelay = 8 * time.Second
	// coreMaxConsecutiveRestarts is how many automatic restarts are attempted in
	// one crash chain before the watchdog gives up and reports an error state.
	coreMaxConsecutiveRestarts = 3
	// coreStableRunThreshold resets the consecutive restart counter once the core
	// has been running for this long.
	coreStableRunThreshold = 60 * time.Second
	// coreRestartStartTimeout bounds one automatic restart so a wedged core can
	// never keep the watchdog busy forever.
	coreRestartStartTimeout = 45 * time.Second
	// coreRestartCancelWait bounds how long Start/Stop wait for a cancelled
	// automatic restart to unwind.
	coreRestartCancelWait = 6 * time.Second
)

// coreRestartPlan identifies one scheduled automatic restart. cancel aborts the
// backoff wait or the in-flight start, and done is closed once the watchdog
// goroutine has finished with it.
type coreRestartPlan struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// errCoreRestartCanceled reports that an automatic core restart was abandoned
// because the user stopped the proxy while the restart was starting.
var errCoreRestartCanceled = errors.New("automatic core restart canceled")

// restartBackoffDelay returns the exponential backoff before the given one-based
// restart attempt (1s, 2s, 4s, 8s, ...), capped at coreRestartMaxDelay.
func restartBackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := coreRestartBaseDelay
	for i := 1; i < attempt && delay < coreRestartMaxDelay; i++ {
		delay *= 2
	}
	if delay > coreRestartMaxDelay {
		delay = coreRestartMaxDelay
	}
	return delay
}

// nextRestartDelay returns the delay before the given one-based automatic
// restart attempt and whether such an attempt is allowed at all.
func nextRestartDelay(attempt int) (time.Duration, bool) {
	if attempt < 1 || attempt > coreMaxConsecutiveRestarts {
		return 0, false
	}
	return restartBackoffDelay(attempt), true
}

// shouldAutoRestartCore decides whether an observed core exit is a crash the
// watchdog must handle. A user-requested stop is never restarted, and neither is
// an exit that happened before the proxy reached the running state (the initial
// start reports those through its normal error path).
func shouldAutoRestartCore(autoRestartEnabled bool, userRequestedStop bool, previousStatus string) bool {
	if !autoRestartEnabled || userRequestedStop {
		return false
	}
	return previousStatus == "running"
}

// planCoreRestart decides how the watchdog reacts to an unexpected core exit. It
// returns the one-based restart attempt, the backoff delay before it and whether
// a restart is allowed at all. The consecutive restart chain is reset when the
// exited core had been running stably for at least coreStableRunThreshold, so
// only quick crash loops are limited.
func planCoreRestart(autoRestartEnabled bool, userRequestedStop bool, previousStatus string, consecutiveRestarts int, ranFor time.Duration) (attempt int, delay time.Duration, ok bool) {
	if !shouldAutoRestartCore(autoRestartEnabled, userRequestedStop, previousStatus) {
		return 0, 0, false
	}
	if consecutiveRestarts < 0 {
		consecutiveRestarts = 0
	}
	if ranFor >= coreStableRunThreshold {
		consecutiveRestarts = 0
	}
	attempt = consecutiveRestarts + 1
	delay, ok = nextRestartDelay(attempt)
	if !ok {
		return 0, 0, false
	}
	return attempt, delay, true
}

func (m *Manager) Start(ctx context.Context) (model.ProxyState, error) {
	// A manual start supersedes a scheduled watchdog restart. Flag the pending
	// restart as user-cancelled while it unwinds so an in-flight restart reports
	// "cancelled" instead of leaving an error state behind, then start normally.
	m.mu.Lock()
	restartPending := m.pendingRestart != nil
	if restartPending {
		m.userRequestedStop = true
	}
	m.mu.Unlock()
	m.cancelPendingRestart()
	m.mu.Lock()
	m.userRequestedStop = false
	if m.cmd == nil {
		m.restartAttempts = 0
		m.runningSince = time.Time{}
	}
	m.mu.Unlock()
	return m.startCore(ctx, false)
}

// startCore implements Start. autoRestart is true when the call comes from the
// core watchdog: in that mode a user Stop that races with the start cancels the
// start instead of leaving an error state behind.
func (m *Manager) startCore(ctx context.Context, autoRestart bool) (model.ProxyState, error) {
	// Snapshot the extra rule provider before taking the manager lock: the
	// provider is invoked without holding the lock so it can never deadlock
	// against code that reads manager state.
	extraRules := m.extraRules()
	m.mu.Lock()
	if m.startInProgress {
		// Another Start (manual or watchdog-driven) is already running; report the
		// current state instead of launching a second core.
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		return state, nil
	}
	if m.cmd != nil || m.embedded != nil {
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		return state, nil
	}
	if autoRestart && (m.userRequestedStop || ctx.Err() != nil) {
		m.state = model.ProxyState{Status: "stopped"}
		m.runningSince = time.Time{}
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		m.emit(state)
		return state, errCoreRestartCanceled
	}
	m.startInProgress = true
	defer func() {
		m.mu.Lock()
		m.startInProgress = false
		m.mu.Unlock()
	}()
	settings := m.store.Settings()
	singboxMode := settings.CoreType == "singbox"
	if embeddedCoreEnabled() && singboxMode {
		m.appendLog("warning", "core", "内嵌内核仅支持 Xray 配置，已忽略 sing-box 选择")
		settings.CoreType = "xray"
		singboxMode = false
	}
	if settings.CoreType != "xray" && !singboxMode {
		m.mu.Unlock()
		return m.fail("only Xray-core and sing-box are currently supported")
	}
	coreInfo := m.core.Info()
	if !coreInfo.Installed || coreInfo.Path == "" {
		m.mu.Unlock()
		return m.fail("Xray-core is not installed")
	}

	// Load balancing routes every enabled node through the balancer. It needs at
	// least two enabled nodes; TUN mode is excluded because the existing TUN route
	// setup only pins one node server IP to the physical interface. Both cases fall
	// back to the single active node and are reported once in the log.
	enabledNodes := enabledProxyNodes(m.store.ListNodes())
	useLoadBalance := settings.LoadBalanceEnabled && !settings.TunMode && len(enabledNodes) >= 2
	var warnings []string
	if singboxMode && settings.LoadBalanceEnabled {
		warnings = append(warnings, "sing-box 暂不支持负载均衡，已回退单节点")
		useLoadBalance = false
	}
	if settings.LoadBalanceEnabled {
		switch {
		case singboxMode:
			// already reported above
		case settings.TunMode:
			warnings = append(warnings, "TUN 模式下暂不支持负载均衡，已回退单节点")
		case len(enabledNodes) < 2:
			warnings = append(warnings, "负载均衡需要至少 2 个启用节点")
		}
	}
	var node model.Node

	// The stats/management API shares the settings API port. An invalid or
	// conflicting port would make Xray fail to bind the inbound, so keep the
	// proxy usable: an explicitly requested API fails fast, otherwise the
	// best-effort traffic statistics are skipped for this run.
	statsSkippedForPort := false
	if settings.StatsEnabled || settings.XrayAPIEnabled {
		if settings.XrayAPIPort <= 0 || settings.XrayAPIPort == settings.XrayLocalPort {
			if settings.XrayAPIEnabled {
				m.mu.Unlock()
				return m.fail("Xray API 端口无效或与本地代理端口相同，请在设置中修改")
			}
			settings.StatsEnabled = false
			statsSkippedForPort = true
		}
	}
	if settings.TunMode && singboxMode {
		if err := preflightSingBoxTun(); err != nil {
			m.mu.Unlock()
			return m.fail(err.Error())
		}
	}
	if settings.TunMode && !singboxMode {
		if err := preflightTun(coreInfo.Path); err != nil {
			m.mu.Unlock()
			return m.fail(err.Error())
		}
	}

	customRules := mergeRoutingRules(m.store.ListRoutingRules(), extraRules)
	var (
		config   []byte
		buildErr error
	)
	if singboxMode {
		active, ok := m.store.ActiveNode()
		if !ok {
			m.mu.Unlock()
			return m.fail("no active node selected")
		}
		node = active
		config, buildErr = BuildSingBoxConfigWithGeoAssets(node, settings, filepath.Join(m.store.DataDir(), "cores", "singbox"), customRules...)
	} else if useLoadBalance {
		config, buildErr = BuildLoadBalanceConfig(enabledNodes, settings, customRules...)
		if buildErr != nil {
			// A single broken node must not take the whole proxy down: keep the
			// active node working in single-node mode and surface the reason.
			warnings = append(warnings, "负载均衡配置生成失败，已回退单节点: "+buildErr.Error())
			useLoadBalance = false
		}
	}
	if !singboxMode && !useLoadBalance {
		if node.ID == "" {
			active, ok := m.store.ActiveNode()
			if !ok {
				m.mu.Unlock()
				return m.fail("no active node selected")
			}
			node = active
		}
		config, buildErr = BuildConfig(node, settings, customRules...)
	}
	if buildErr != nil {
		m.mu.Unlock()
		return m.fail(buildErr.Error())
	}
	if singboxMode && bytes.Contains(config, []byte(`"rule_set"`)) {
		m.appendLog("info", "core", "sing-box 路由包含 GeoIP/GeoSite 规则，首次启动会通过代理下载规则集")
	}
	runtimeDir := filepath.Join(m.store.DataDir(), "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		m.mu.Unlock()
		return m.fail(err.Error())
	}
	configFileName := "xray-config.json"
	if singboxMode {
		configFileName = "sing-box-config.json"
	}
	configPath := filepath.Join(runtimeDir, configFileName)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		m.mu.Unlock()
		return m.fail(err.Error())
	}

	subscriptionName := ""
	resolvedIP := ""
	if !useLoadBalance {
		if sub, err := m.store.FindSubscription(node.SubscriptionID); err == nil {
			subscriptionName = sub.Name
		}
		resolvedIP = resolveNodeHost(ctx, node.Address)
		if settings.TunMode && !singboxMode && resolvedIP == "" {
			m.mu.Unlock()
			return m.fail("TUN 模式要求节点服务器地址能够解析为 IP")
		}
	}
	inbound := "SOCKS5 / HTTP"
	if settings.TunMode {
		inbound = "TUN + SOCKS5 / HTTP"
	}
	state := model.ProxyState{
		Status:    "starting",
		ProxyMode: settings.ProxyMode,
		Inbound:   inbound,
		LocalAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.XrayLocalPort)),
	}
	if useLoadBalance {
		// Load balancing has no single active node; the balancer picks one of the
		// enabled nodes for every connection.
		state.NodeName = loadBalanceNodeName(len(enabledNodes))
	} else {
		state.NodeID = node.ID
		state.NodeName = node.Name
		state.NodeProtocol = node.Protocol
		state.NodeAddress = node.Address
		state.NodePort = node.Port
		state.NodeSubscription = subscriptionName
		state.ResolvedIP = resolvedIP
	}
	m.state = state
	m.mu.Unlock()
	m.emit(state)
	for _, warning := range warnings {
		m.appendLog("warning", "loadbalance", warning)
	}
	if useLoadBalance {
		m.appendLog("info", "loadbalance", fmt.Sprintf("负载均衡已启用：%d 个节点，策略 %s", len(enabledNodes), LoadBalanceStrategy(settings.LoadBalanceMode)))
	}
	if statsSkippedForPort {
		m.appendLog("warning", "stats", fmt.Sprintf("Xray API 端口 %d 与本地代理端口相同，已跳过实时流量统计", settings.XrayAPIPort))
	}

	if embeddedCoreEnabled() {
		return m.startEmbeddedCore(ctx, config)
	}

	coreName := "Xray"
	testArgs := []string{"run", "-test", "-config", configPath}
	runArgs := []string{"run", "-config", configPath}
	if singboxMode {
		coreName = "sing-box"
		testArgs = []string{"check", "-c", configPath}
		runArgs = []string{"run", "-c", configPath}
	}
	m.appendLog("info", "core", "正在校验 "+coreName+" 配置")
	testCmd := exec.CommandContext(ctx, coreInfo.Path, testArgs...)
	hideWindow(testCmd)
	if !singboxMode {
		testCmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(coreInfo.Path))
	}
	if output, err := testCmd.CombinedOutput(); err != nil {
		if autoRestart && m.userStopRequested() {
			return m.abortAutoRestart(nil, nil, nil)
		}
		return m.fail(fmt.Sprintf("%s 配置校验失败: %s", coreName, strings.TrimSpace(string(output))))
	}
	m.appendLog("info", "core", coreName+" 配置校验通过")
	if autoRestart && m.userStopRequested() {
		return m.abortAutoRestart(nil, nil, nil)
	}

	cmd := exec.Command(coreInfo.Path, runArgs...)
	hideWindow(cmd)
	if !singboxMode {
		cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(coreInfo.Path))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return m.fail(fmt.Sprintf("failed to open %s stdout: %v", coreName, err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return m.fail(fmt.Sprintf("failed to open %s stderr: %v", coreName, err))
	}
	if err := cmd.Start(); err != nil {
		return m.fail(fmt.Sprintf("failed to start %s: %v", coreInfo.Version, err))
	}
	go m.captureLogStream(stdout, "stdout")
	go m.captureLogStream(stderr, "stderr")
	m.appendLog("info", "core", fmt.Sprintf("%s 已启动，PID %d", coreName, cmd.Process.Pid))
	job, err := newProcessJob()
	if err != nil {
		_ = cmd.Process.Kill()
		return m.fail(fmt.Sprintf("failed to create process job: %v", err))
	}
	if err := job.Assign(cmd.Process); err != nil {
		_ = job.Close()
		_ = cmd.Process.Kill()
		return m.fail(fmt.Sprintf("failed to assign process job: %v", err))
	}

	done := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.done = done
	m.job = job
	m.state.PID = cmd.Process.Pid
	m.state.StartedAt = model.NowRFC3339()
	m.mu.Unlock()

	go m.monitor(cmd, done, job)

	if err := waitForPort(ctx, state.LocalAddr, 12*time.Second); err != nil {
		if autoRestart && m.userStopRequested() {
			return m.abortAutoRestart(cmd, done, job)
		}
		_ = cmd.Process.Kill()
		<-done
		_ = job.Close()
		return m.fail(fmt.Sprintf("Xray-core did not become ready: %v", err))
	}

	m.appendLog("info", "core", "本地入站已就绪: "+state.LocalAddr)
	if autoRestart && m.userStopRequested() {
		return m.abortAutoRestart(cmd, done, job)
	}
	if settings.TunMode {
		if settings.SystemProxyMode == "system" || settings.SystemProxyMode == "pac" {
			m.appendLog("info", "system", "TUN 模式已接管系统流量，跳过 Windows 系统代理")
		}
	} else {
		switch settings.SystemProxyMode {
		case "system":
			m.enableSystemProxy(state.LocalAddr)
		case "pac":
			m.enablePACSystemProxy(state.LocalAddr)
		}
	}
	if settings.TunMode && !singboxMode {
		cleanup, routeErr := setupTunRoutes(tunInterfaceName, resolvedIP)
		if routeErr != nil {
			_ = cmd.Process.Kill()
			<-done
			_ = job.Close()
			if autoRestart && m.userStopRequested() {
				return m.abortAutoRestart(cmd, done, job)
			}
			return m.fail(fmt.Sprintf("TUN 路由配置失败: %v", routeErr))
		}
		m.mu.Lock()
		m.tunCleanup = cleanup
		m.mu.Unlock()
		m.appendLog("info", "tun", "TUN 适配器与系统路由已配置")
	}

	m.mu.Lock()
	stoppedDuringStart := autoRestart && m.userRequestedStop
	if !stoppedDuringStart {
		m.state.Status = "running"
		m.runningSince = time.Now()
	}
	state = m.state
	m.mu.Unlock()
	if stoppedDuringStart {
		return m.abortAutoRestart(cmd, done, job)
	}
	m.appendLog("info", "core", "代理已进入运行状态")
	m.emit(state)
	if !singboxMode && (settings.StatsEnabled || settings.XrayAPIEnabled) {
		m.appendLog("info", "stats", fmt.Sprintf("实时流量统计已启用（采样间隔 %s）", trafficSampleInterval))
		m.startTrafficSampler(coreInfo.Path, settings.XrayAPIPort)
	}
	if singboxMode && (settings.StatsEnabled || settings.XrayAPIEnabled) {
		m.appendLog("info", "stats", "sing-box 实时流量统计已启用（Clash API）")
		m.startSingBoxTrafficSampler(settings.XrayAPIPort)
	}
	return state, nil
}

// startEmbeddedCore runs the in-process Xray-core path. The generated config is
// reused as-is except for GeoIP/GeoSite conditions, which are stripped until
// Android ships the geo datasets. Traffic statistics via the CLI API are not
// available in-process yet, so the dashboard falls back to zero live rates while
// the session totals keep working.
func (m *Manager) startEmbeddedCore(ctx context.Context, config []byte) (model.ProxyState, error) {
	settings := m.store.Settings()
	if settings.TunMode {
		return m.fail("内嵌内核暂不支持 TUN 模式")
	}
	cleanConfig, removed, err := sanitizeEmbeddedConfig(config)
	if err != nil {
		return m.fail(err.Error())
	}
	if removed > 0 {
		m.appendLog("warning", "core", fmt.Sprintf("内嵌内核缺少 GeoIP/GeoSite 数据集，已跳过 %d 条 Geo 路由条件", removed))
	}
	instance, err := embeddedStart(cleanConfig)
	if err != nil {
		return m.fail(fmt.Sprintf("内嵌 Xray-core 启动失败: %v", err))
	}
	done := make(chan struct{})
	m.mu.Lock()
	m.embedded = instance
	m.embeddedDone = done
	m.state.PID = os.Getpid()
	m.state.StartedAt = model.NowRFC3339()
	m.mu.Unlock()
	m.appendLog("info", "core", fmt.Sprintf("内嵌 Xray-core 已启动（进程内，PID %d）", os.Getpid()))

	starting := m.State()
	if err := waitForPort(ctx, starting.LocalAddr, 12*time.Second); err != nil {
		_ = instance.Close()
		m.mu.Lock()
		m.embedded = nil
		m.embeddedDone = nil
		m.mu.Unlock()
		close(done)
		return m.fail(fmt.Sprintf("内嵌 Xray-core 未能就绪: %v", err))
	}
	m.appendLog("info", "core", "本地入站已就绪: "+starting.LocalAddr)

	m.mu.Lock()
	stoppedDuringStart := m.userRequestedStop
	if !stoppedDuringStart {
		m.state.Status = "running"
		m.runningSince = time.Now()
	} else {
		m.embedded = nil
		m.embeddedDone = nil
	}
	m.mu.Unlock()
	if stoppedDuringStart {
		_ = instance.Close()
		close(done)
		return m.State(), nil
	}
	m.appendLog("info", "core", "代理已进入运行状态（内嵌内核）")
	state := m.State()
	m.emit(state)
	if settings.SystemProxyMode != "off" {
		m.appendLog("info", "system", "内嵌内核模式不设置系统代理，本地代理端口: "+state.LocalAddr)
	}
	if settings.StatsEnabled || settings.XrayAPIEnabled {
		m.appendLog("warning", "stats", "内嵌内核模式下暂不支持实时流量统计")
	}
	return state, nil
}

func (m *Manager) Stop() (model.ProxyState, error) {
	m.mu.Lock()
	m.userRequestedStop = true
	m.mu.Unlock()
	// Cancel a scheduled automatic restart before tearing the proxy down. This is
	// what makes an intentional stop win over the watchdog; the wait is bounded so
	// a stop can never hang on a wedged restart.
	m.cancelPendingRestart()
	defer m.stopSystemProxy()
	m.mu.Lock()
	if m.embedded != nil {
		instance := m.embedded
		done := m.embeddedDone
		m.embedded = nil
		m.embeddedDone = nil
		m.state.Status = "stopping"
		m.mu.Unlock()
		m.emit(m.State())
		m.appendLog("info", "core", "正在停止内嵌 Xray-core")
		m.stopTrafficSampler()
		if cleanup := m.takeTunCleanup(); cleanup != nil {
			_ = cleanup()
		}
		_ = instance.Close()
		if done != nil {
			close(done)
		}
		m.mu.Lock()
		m.state = model.ProxyState{Status: "stopped"}
		m.runningSince = time.Time{}
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		m.emit(state)
		return state, nil
	}
	m.mu.Unlock()
	m.mu.Lock()
	if m.cmd == nil {
		m.state = model.ProxyState{Status: "stopped"}
		m.runningSince = time.Time{}
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		m.stopTrafficSampler()
		if cleanup := m.takeTunCleanup(); cleanup != nil {
			_ = cleanup()
		}
		m.emit(state)
		return state, nil
	}
	cmd := m.cmd
	done := m.done
	job := m.job
	m.state.Status = "stopping"
	m.mu.Unlock()
	m.emit(m.State())
	m.appendLog("info", "core", "正在停止 Xray-core")
	m.stopTrafficSampler()
	if cleanup := m.takeTunCleanup(); cleanup != nil {
		_ = cleanup()
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	if job != nil {
		_ = job.Close()
	}
	if settings := m.store.Settings(); settings.TunMode && settings.CoreType == "singbox" {
		// sing-box owns the TUN adapter/routes; after a forced stop, remove any
		// leftover routes/addresses so the next start and the physical network
		// stay clean.
		_ = cleanupSingBoxTun()
	}

	m.mu.Lock()
	m.cmd = nil
	m.done = nil
	m.job = nil
	m.state = model.ProxyState{Status: "stopped"}
	m.runningSince = time.Time{}
	state := m.enrichNodeState(m.state)
	m.mu.Unlock()
	m.emit(state)
	return state, nil
}
func (m *Manager) Test(settings model.Settings) (model.ProxyTestResult, error) {
	state := m.State()
	if state.Status != "running" || state.LocalAddr == "" {
		return model.ProxyTestResult{}, errors.New("proxy is not running")
	}
	target := strings.TrimSpace(settings.TestURL)
	if target == "" {
		target = "https://www.gstatic.com/generate_204"
	}
	proxyURL, err := url.Parse("http://" + state.LocalAddr)
	if err != nil {
		return model.ProxyTestResult{}, err
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Get(target)
	latency := time.Since(start).Milliseconds()
	if latency == 0 {
		latency = 1
	}
	result := model.ProxyTestResult{
		TargetURL: target,
		LatencyMS: latency,
		CheckedAt: model.NowRFC3339(),
	}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !result.Success {
		result.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
		return result, errors.New(result.Error)
	}
	return result, nil
}

// monitor waits for one core process and decides what happens after it exited.
// A user-requested stop ends in the stopped state as before; an unexpected exit
// of a running core is handed to the watchdog when auto restart is enabled.
func (m *Manager) monitor(cmd *exec.Cmd, done chan struct{}, job *processJob) {
	err := cmd.Wait()
	close(done)
	_ = job.Close()
	if err != nil {
		m.appendLog("error", "core", "Xray-core 已退出: "+err.Error())
	} else {
		m.appendLog("info", "core", "Xray-core 已退出")
	}
	// Release every resource the exited core may own before deciding about a
	// restart: a restarted core configures the system proxy, TUN routes and the
	// traffic sampler again from scratch.
	m.stopSystemProxy()
	if cleanup := m.takeTunCleanup(); cleanup != nil {
		_ = cleanup()
	}
	m.stopTrafficSampler()

	m.mu.Lock()
	if m.cmd != cmd {
		m.mu.Unlock()
		return
	}
	previousStatus := m.state.Status
	ranFor := time.Duration(0)
	if !m.runningSince.IsZero() {
		ranFor = time.Since(m.runningSince)
	}
	m.cmd = nil
	m.done = nil
	m.job = nil
	settings := m.store.Settings()
	attempt, delay, restart := planCoreRestart(settings.AutoRestartCore, m.userRequestedStop, previousStatus, m.restartAttempts, ranFor)
	if restart {
		restartCtx, cancel := context.WithTimeout(context.Background(), coreRestartStartTimeout)
		plan := &coreRestartPlan{cancel: cancel, done: make(chan struct{})}
		m.pendingRestart = plan
		m.restartAttempts = attempt
		m.runningSince = time.Time{}
		m.state.Status = "starting"
		m.state.Error = ""
		m.state.PID = 0
		m.state.StartedAt = ""
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		m.appendLog("warning", "core", fmt.Sprintf("核心异常退出，正在自动重启（第 %d 次）", attempt))
		m.emit(state)
		go m.runCoreRestart(restartCtx, plan, attempt, delay)
		return
	}
	exhausted := shouldAutoRestartCore(settings.AutoRestartCore, m.userRequestedStop, previousStatus)
	if previousStatus != "stopping" {
		if err != nil {
			m.state.Status = "error"
			m.state.Error = err.Error()
		} else {
			m.state.Status = "stopped"
		}
	}
	m.runningSince = time.Time{}
	state := m.enrichNodeState(m.state)
	m.mu.Unlock()
	m.emit(state)
	if exhausted {
		m.appendLog("error", "core", fmt.Sprintf("核心连续异常退出 %d 次，自动重启已达上限，已停止自动重启", coreMaxConsecutiveRestarts))
	}
}

// userStopRequested reports whether Stop was called and no manual Start has run
// since.
func (m *Manager) userStopRequested() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.userRequestedStop
}

// cancelPendingRestart cancels a scheduled or in-flight automatic restart and
// waits (bounded) until the watchdog goroutine has unwound, so a manual Start or
// Stop never races the watchdog for the core process.
func (m *Manager) cancelPendingRestart() {
	m.mu.Lock()
	plan := m.pendingRestart
	m.pendingRestart = nil
	m.mu.Unlock()
	if plan == nil {
		return
	}
	plan.cancel()
	select {
	case <-plan.done:
	case <-time.After(coreRestartCancelWait):
	}
}

// runCoreRestart performs one watchdog restart: it waits for the backoff delay
// and then starts a new core. Only one of these goroutines exists at a time.
func (m *Manager) runCoreRestart(ctx context.Context, plan *coreRestartPlan, attempt int, delay time.Duration) {
	defer func() {
		m.mu.Lock()
		if m.pendingRestart == plan {
			m.pendingRestart = nil
		}
		m.mu.Unlock()
		plan.cancel()
		close(plan.done)
	}()

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		// A user Stop or Start cancelled the pending restart during the backoff.
		m.appendLog("info", "core", "核心自动重启已取消")
		return
	}

	m.mu.Lock()
	stale := m.pendingRestart != plan || m.userRequestedStop || m.cmd != nil
	m.mu.Unlock()
	if stale {
		return
	}

	// The crashed core's system proxy, TUN routes and traffic sampler were already
	// released by the monitor; repeat the cleanup defensively so a new process can
	// never inherit resources of the previous one.
	m.stopSystemProxy()
	if cleanup := m.takeTunCleanup(); cleanup != nil {
		_ = cleanup()
	}
	m.stopTrafficSampler()

	state, err := m.startCore(ctx, true)
	switch {
	case errors.Is(err, errCoreRestartCanceled):
		m.appendLog("info", "core", "核心自动重启已取消")
	case err != nil:
		m.appendLog("error", "core", fmt.Sprintf("核心自动重启失败（第 %d 次），已停止自动重启: %v", attempt, err))
	case state.Status == "running":
		m.appendLog("info", "core", fmt.Sprintf("核心自动重启成功（第 %d 次）", attempt))
	}
}

// abortAutoRestart rolls back an automatic restart start that a user Stop
// interrupted: the freshly started process (if it was already launched) is
// killed and every resource it may have configured is released again so the
// stopped state wins.
func (m *Manager) abortAutoRestart(cmd *exec.Cmd, done chan struct{}, job *processJob) (model.ProxyState, error) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	if job != nil {
		_ = job.Close()
	}
	m.stopSystemProxy()
	if cleanup := m.takeTunCleanup(); cleanup != nil {
		_ = cleanup()
	}
	m.stopTrafficSampler()
	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd = nil
		m.done = nil
		m.job = nil
	}
	m.state = model.ProxyState{Status: "stopped"}
	m.runningSince = time.Time{}
	state := m.enrichNodeState(m.state)
	m.mu.Unlock()
	m.emit(state)
	return state, errCoreRestartCanceled
}

func (m *Manager) stopSystemProxy() {
	cleanup := m.takeSystemProxyCleanup()
	if cleanup != nil {
		_ = cleanup()
		m.appendLog("info", "system", "Windows 系统代理已恢复")
	}
}

// enableSystemProxy applies the fixed WinINet proxy and stores the restore
// function so every stop path (manual stop, watchdog cleanup, failed start) can
// revert it.
func (m *Manager) enableSystemProxy(localAddr string) {
	cleanup, proxyErr := platform.EnableSystemProxy(localAddr)
	if proxyErr != nil {
		m.appendLog("warning", "system", "Windows 系统代理设置失败: "+proxyErr.Error()+"；代理仍会继续运行，请手动配置系统代理或使用 TUN 模式")
		return
	}
	m.mu.Lock()
	m.systemProxyCleanup = cleanup
	m.mu.Unlock()
	m.appendLog("info", "system", "Windows 系统代理已启用: "+localAddr)
}

// enablePACSystemProxy writes the generated PAC script, points the WinINet
// automatic proxy configuration at it and stores a restore function that also
// deletes the script again. Like the fixed-proxy path, a failure is only a
// warning: the local proxy keeps running.
func (m *Manager) enablePACSystemProxy(localAddr string) {
	pacPath, err := writePACFile(m.store.DataDir(), localAddr)
	if err != nil {
		m.appendLog("warning", "system", "PAC 系统代理准备失败: "+err.Error()+"；代理仍会继续运行，请手动配置系统代理或使用 TUN 模式")
		return
	}
	cleanup, proxyErr := platform.EnableSystemProxyPAC(pacPath)
	if proxyErr != nil {
		removePACFile(pacPath)
		m.appendLog("warning", "system", "PAC 系统代理设置失败: "+proxyErr.Error()+"；代理仍会继续运行，请手动配置系统代理或使用 TUN 模式")
		return
	}
	m.mu.Lock()
	m.systemProxyCleanup = func() error {
		restoreErr := cleanup()
		removePACFile(pacPath)
		return restoreErr
	}
	m.mu.Unlock()
	m.appendLog("info", "system", "PAC 系统代理已启用: "+localAddr+"（PAC 文件: "+pacPath+"）")
}

func (m *Manager) takeSystemProxyCleanup() func() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanup := m.systemProxyCleanup
	m.systemProxyCleanup = nil
	return cleanup
}
func (m *Manager) takeTunCleanup() func() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanup := m.tunCleanup
	m.tunCleanup = nil
	return cleanup
}
func (m *Manager) fail(message string) (model.ProxyState, error) {
	m.appendLog("error", "system", message)
	m.mu.Lock()
	m.state = model.ProxyState{Status: "error", Error: message}
	state := m.enrichNodeState(m.state)
	m.mu.Unlock()
	m.stopSystemProxy()
	m.stopTrafficSampler()
	m.emit(state)
	return state, errors.New(message)
}

func (m *Manager) emit(state model.ProxyState) {
	m.mu.RLock()
	callback := m.onStateChange
	m.mu.RUnlock()
	if callback != nil {
		callback(state)
	}
}

func waitForPort(ctx context.Context, address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("timeout waiting for local proxy port")
}

func resolveNodeHost(ctx context.Context, address string) string {
	if parsed := net.ParseIP(strings.TrimSpace(address)); parsed != nil {
		return parsed.String()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(lookupCtx, address)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0]
}

// enabledProxyNodes returns the nodes the balancer is allowed to route through.
func enabledProxyNodes(nodes []model.Node) []model.Node {
	enabled := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			enabled = append(enabled, node)
		}
	}
	return enabled
}

// loadBalanceNodeName is the ProxyState node label shown while the balancer runs.
func loadBalanceNodeName(nodeCount int) string {
	return fmt.Sprintf("负载均衡（%d 个节点）", nodeCount)
}
