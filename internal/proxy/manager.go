package proxy

import (
	"bufio"
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
	mu                 sync.RWMutex
	store              *store.Store
	core               CoreInfoProvider
	cmd                *exec.Cmd
	done               chan struct{}
	job                *processJob
	state              model.ProxyState
	onStateChange      func(model.ProxyState)
	sampler            *trafficSampler
	onTraffic          func(model.TrafficStats)
	tunCleanup         func() error
	systemProxyCleanup func() error
	logsMu             sync.Mutex
	logs               []model.LogEntry
	logSeq             int64
	onLog              func(model.LogEntry)
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
	running := m.cmd != nil && m.state.Status == "running"
	m.mu.RUnlock()
	if sampler == nil || !running {
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

// stopTrafficSampler cancels the sampling goroutine and clears the runtime
// traffic state. Concurrent callers (Stop, monitor, fail) are harmless.
func (m *Manager) stopTrafficSampler() {
	m.mu.Lock()
	sampler := m.sampler
	m.sampler = nil
	m.mu.Unlock()
	if sampler == nil {
		return
	}
	sampler.stop()
	m.emitTraffic(model.TrafficStats{})
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
func (m *Manager) Start(ctx context.Context) (model.ProxyState, error) {
	m.mu.Lock()
	if m.cmd != nil {
		state := m.enrichNodeState(m.state)
		m.mu.Unlock()
		return state, nil
	}
	settings := m.store.Settings()
	if settings.CoreType != "xray" {
		m.mu.Unlock()
		return m.fail("only Xray-core is currently supported")
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
	if settings.LoadBalanceEnabled {
		switch {
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
	if settings.TunMode {
		if err := preflightTun(coreInfo.Path); err != nil {
			m.mu.Unlock()
			return m.fail(err.Error())
		}
	}

	customRules := m.store.ListRoutingRules()
	var (
		config   []byte
		buildErr error
	)
	if useLoadBalance {
		config, buildErr = BuildLoadBalanceConfig(enabledNodes, settings, customRules...)
		if buildErr != nil {
			// A single broken node must not take the whole proxy down: keep the
			// active node working in single-node mode and surface the reason.
			warnings = append(warnings, "负载均衡配置生成失败，已回退单节点: "+buildErr.Error())
			useLoadBalance = false
		}
	}
	if !useLoadBalance {
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
	runtimeDir := filepath.Join(m.store.DataDir(), "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		m.mu.Unlock()
		return m.fail(err.Error())
	}
	configPath := filepath.Join(runtimeDir, "xray-config.json")
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
		if settings.TunMode && resolvedIP == "" {
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

	m.appendLog("info", "core", "正在校验 Xray 配置")
	testCmd := exec.CommandContext(ctx, coreInfo.Path, "run", "-test", "-config", configPath)
	hideWindow(testCmd)
	testCmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(coreInfo.Path))
	if output, err := testCmd.CombinedOutput(); err != nil {
		return m.fail(fmt.Sprintf("Xray config validation failed: %s", strings.TrimSpace(string(output))))
	}
	m.appendLog("info", "core", "Xray 配置校验通过")

	cmd := exec.Command(coreInfo.Path, "run", "-config", configPath)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(coreInfo.Path))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return m.fail(fmt.Sprintf("failed to open Xray stdout: %v", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return m.fail(fmt.Sprintf("failed to open Xray stderr: %v", err))
	}
	if err := cmd.Start(); err != nil {
		return m.fail(fmt.Sprintf("failed to start Xray-core: %v", err))
	}
	go m.captureLogStream(stdout, "stdout")
	go m.captureLogStream(stderr, "stderr")
	m.appendLog("info", "core", fmt.Sprintf("Xray-core 已启动，PID %d", cmd.Process.Pid))
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
		_ = cmd.Process.Kill()
		<-done
		_ = job.Close()
		return m.fail(fmt.Sprintf("Xray-core did not become ready: %v", err))
	}

	m.appendLog("info", "core", "本地入站已就绪: "+state.LocalAddr)
	if settings.SystemProxyMode == "system" && !settings.TunMode {
		cleanup, proxyErr := platform.EnableSystemProxy(state.LocalAddr)
		if proxyErr != nil {
			m.appendLog("warning", "system", "Windows 系统代理设置失败: "+proxyErr.Error()+"；代理仍会继续运行，请手动配置系统代理或使用 TUN 模式")
		} else {
			m.mu.Lock()
			m.systemProxyCleanup = cleanup
			m.mu.Unlock()
			m.appendLog("info", "system", "Windows 系统代理已启用: "+state.LocalAddr)
		}
	} else if settings.TunMode && settings.SystemProxyMode == "system" {
		m.appendLog("info", "system", "TUN 模式已接管系统流量，跳过 Windows 系统代理")
	}
	if settings.TunMode {
		cleanup, routeErr := setupTunRoutes(tunInterfaceName, resolvedIP)
		if routeErr != nil {
			_ = cmd.Process.Kill()
			<-done
			_ = job.Close()
			return m.fail(fmt.Sprintf("TUN 路由配置失败: %v", routeErr))
		}
		m.mu.Lock()
		m.tunCleanup = cleanup
		m.mu.Unlock()
		m.appendLog("info", "tun", "TUN 适配器与系统路由已配置")
	}

	m.mu.Lock()
	m.state.Status = "running"
	state = m.state
	m.mu.Unlock()
	m.appendLog("info", "core", "代理已进入运行状态")
	m.emit(state)
	if settings.StatsEnabled || settings.XrayAPIEnabled {
		m.appendLog("info", "stats", fmt.Sprintf("实时流量统计已启用（采样间隔 %s）", trafficSampleInterval))
		m.startTrafficSampler(coreInfo.Path, settings.XrayAPIPort)
	}
	return state, nil
}
func (m *Manager) Stop() (model.ProxyState, error) {
	defer m.stopSystemProxy()
	m.mu.Lock()
	if m.cmd == nil {
		m.state = model.ProxyState{Status: "stopped"}
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

	m.mu.Lock()
	m.cmd = nil
	m.done = nil
	m.job = nil
	m.state = model.ProxyState{Status: "stopped"}
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

func (m *Manager) monitor(cmd *exec.Cmd, done chan struct{}, job *processJob) {
	err := cmd.Wait()
	close(done)
	_ = job.Close()
	if err != nil {
		m.appendLog("error", "core", "Xray-core 已退出: "+err.Error())
	} else {
		m.appendLog("info", "core", "Xray-core 已退出")
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
		if m.state.Status != "stopping" {
			if err != nil {
				m.state.Status = "error"
				m.state.Error = err.Error()
			} else {
				m.state.Status = "stopped"
			}
		}
	}
	state := m.enrichNodeState(m.state)
	m.mu.Unlock()
	m.emit(state)
}

func (m *Manager) stopSystemProxy() {
	cleanup := m.takeSystemProxyCleanup()
	if cleanup != nil {
		_ = cleanup()
		m.appendLog("info", "system", "Windows 系统代理已恢复")
	}
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
