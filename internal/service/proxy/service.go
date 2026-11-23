package proxy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/coremode"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	proxyruntime "github.com/MardukPrime/Fast-V/internal/proxy"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// proxyOperationTimeout bounds one start/restart operation triggered by the
	// UI or by the automatic routing scheduler.
	proxyOperationTimeout = 45 * time.Second
	// proxySchedulerTick is how often the automatic routing scheduler wakes up.
	// Settings are read on every tick, so changes apply from the next round on.
	proxySchedulerTick = time.Second
	// proxyFailoverCheckInterval is the health-check period while failover is
	// enabled and the proxy is running.
	proxyFailoverCheckInterval = 30 * time.Second
	// proxyFailoverFailureThreshold is the number of consecutive failed health
	// checks after which the active node is replaced.
	proxyFailoverFailureThreshold = 2
	// proxyURLTestBudget bounds a URL latency round triggered by the UI.
	proxyURLTestBudget = 35 * time.Second
	// trafficHistoryFlushInterval is how often accumulated live traffic deltas are
	// persisted into the daily traffic history. The sampler delivers a sample
	// every two seconds, so writing each delta straight to the store would
	// rewrite the data file thirty times a minute; batching keeps the cost
	// negligible while still recording every byte.
	trafficHistoryFlushInterval = 60 * time.Second
)

// ProxyService is the Wails adapter for local proxy start, stop and testing.
type ProxyService struct {
	store   *store.Store
	manager *proxyruntime.Manager
	core    proxyruntime.CoreInfoProvider

	// operationMu serializes proxy lifecycle changes triggered by the UI and by
	// the automatic routing scheduler.
	operationMu sync.Mutex

	// urlTestMu keeps URL latency rounds single-flight: a scheduled round and a
	// UI-triggered round never spawn competing temporary cores.
	urlTestMu sync.Mutex

	lifecycleMu     sync.Mutex
	schedulerCancel context.CancelFunc
	schedulerDone   chan struct{}

	resultsMu      sync.RWMutex
	urlTestResults []model.NodeLatency

	// traffic batches live traffic samples into the store's daily history so the
	// data file is only rewritten once per flush interval (plus proxy stop and
	// service shutdown) instead of every sampling tick.
	traffic *trafficRecorder

	// logSinkMu guards logSink, the optional persistent log sink wired by the
	// bootstrap layer. It is independent from the Wails event bus so the proxy
	// pipeline keeps working (and logging) without an application instance.
	logSinkMu sync.RWMutex
	logSink   func(model.LogEntry)
}

func New(st *store.Store, coreProvider proxyruntime.CoreInfoProvider) *ProxyService {
	manager := proxyruntime.NewManager(st, coreProvider)
	service := &ProxyService{
		store:   st,
		manager: manager,
		core:    coreProvider,
		traffic: newTrafficRecorder(st, trafficHistoryFlushInterval),
	}
	manager.SetOnStateChange(service.emit)
	manager.SetOnLog(service.emitLog)
	manager.SetOnTraffic(service.emitTraffic)
	manager.SetOnDegrade(service.handleAutoDegrade)
	return service
}

// handleAutoDegrade restarts the proxy so the fallback profile takes effect. The
// callback arrives on the probing goroutine, so the restart runs in its own
// goroutine and a failure is reported as a log line instead of being lost.
func (s *ProxyService) handleAutoDegrade(reason string) {
	if s == nil || s.manager == nil {
		return
	}
	go func() {
		if _, err := s.Restart(); err != nil {
			s.manager.AppendLog("warning", "antidpi", fmt.Sprintf("抗审查降级重启失败: %v", err))
		}
	}()
}

func (s *ProxyService) ServiceName() string {
	return "ProxyService"
}

// SetExtraRulesProvider registers a callback that supplies additional routing
// rules (for example enabled rule-set subscriptions) at configuration build
// time. The provider is called without holding manager locks.
func (s *ProxyService) SetExtraRulesProvider(provider func() []model.RoutingRule) {
	if s == nil || s.manager == nil {
		return
	}
	s.manager.SetExtraRulesProvider(provider)
}

// SetLogSink registers an optional persistent sink that receives every proxy log
// entry in addition to the live Wails event. The bootstrap layer wires it to the
// file logger. A nil sink is ignored so the default in-memory behavior stays
// unchanged. The sink is stored thread-safely because proxy log entries may be
// produced from the manager's background goroutines.
func (s *ProxyService) SetLogSink(sink func(model.LogEntry)) {
	if s == nil || sink == nil {
		return
	}
	s.logSinkMu.Lock()
	s.logSink = sink
	s.logSinkMu.Unlock()
}

// ServiceStartup starts the automatic routing scheduler. The scheduler exits when
// the lifecycle context is cancelled (ServiceShutdown) and never outlives it.
func (s *ProxyService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.schedulerCancel != nil {
		return nil
	}
	schedulerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.schedulerCancel = cancel
	s.schedulerDone = done
	go func() {
		defer close(done)
		s.schedulerLoop(schedulerCtx)
	}()
	return nil
}

func (s *ProxyService) ServiceShutdown() error {
	s.lifecycleMu.Lock()
	cancel, done := s.schedulerCancel, s.schedulerDone
	s.schedulerCancel, s.schedulerDone = nil, nil
	s.lifecycleMu.Unlock()

	// Stop the scheduler first so it cannot start a new round while the proxy is
	// being shut down, then wait for the goroutine to finish.
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	_, err := s.manager.Stop()
	// Persist the traffic deltas accumulated since the last periodic flush before
	// the service goes away.
	s.flushTrafficHistory()
	return err
}

func (s *ProxyService) Start() (model.ProxyState, error) {
	if runtime.GOOS == "android" && !coremode.Enabled() {
		// Android preview: the desktop design runs Xray as a child process and
		// routes traffic through the OS proxy, neither of which exists on a
		// phone. A VpnService + embedded core is required and tracked for a
		// later release; fail with a clear message instead of a core crash.
		return model.ProxyState{}, errors.New("Android 预览版暂不支持本机代理内核；请使用桌面版，移动端 VPN 支持将在后续版本提供")
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.startLocked(context.Background())
}

func (s *ProxyService) startLocked(ctx context.Context) (model.ProxyState, error) {
	if err := ctx.Err(); err != nil {
		return s.manager.State(), err
	}
	relaunching, err := s.ensureAdminForTun()
	if err != nil {
		return s.manager.State(), err
	}
	if relaunching {
		return s.manager.State(), nil
	}
	startCtx, cancel := context.WithTimeout(ctx, proxyOperationTimeout)
	defer cancel()
	return s.manager.Start(startCtx)
}

func (s *ProxyService) ensureAdminForTun() (bool, error) {
	if !s.store.Settings().TunMode || platform.IsElevated() {
		return false, nil
	}
	if err := platform.LaunchElevatedRestart(); err != nil {
		return false, err
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		if app := application.Get(); app != nil {
			app.Quit()
		}
	}()
	return true, nil
}

// SwitchNode persists the active node and restarts a running proxy so the new
// node takes effect. Concurrent UI requests and scheduled switches are
// serialized by operationMu.
func (s *ProxyService) SwitchNode(id string) (model.ProxyState, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.switchNodeLocked(context.Background(), id)
}

func (s *ProxyService) switchNodeLocked(ctx context.Context, id string) (model.ProxyState, error) {
	if err := ctx.Err(); err != nil {
		return s.manager.State(), err
	}
	if _, err := s.store.SetActiveNode(id); err != nil {
		return s.manager.State(), err
	}
	// Load balancing routes through every enabled node, so switching the active
	// node only updates the persisted record; the restart keeps the lifecycle
	// semantics identical to single-node mode.
	if s.store.Settings().LoadBalanceEnabled {
		s.manager.AppendLog("info", "loadbalance", "负载均衡模式：当前节点记录已更新，正在重启代理")
	}
	state := s.manager.State()
	if state.Status != "running" && state.Status != "starting" {
		return state, nil
	}
	if _, err := s.manager.Stop(); err != nil {
		return s.manager.State(), err
	}
	startCtx, cancel := context.WithTimeout(ctx, proxyOperationTimeout)
	defer cancel()
	return s.manager.Start(startCtx)
}

// switchNode is the internal switch entry point used by the scheduler.
func (s *ProxyService) switchNode(ctx context.Context, id string) (model.ProxyState, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.switchNodeLocked(ctx, id)
}

func (s *ProxyService) Restart() (model.ProxyState, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	state := s.manager.State()
	if state.Status != "running" && state.Status != "starting" {
		return state, nil
	}
	if _, err := s.manager.Stop(); err != nil {
		return s.manager.State(), err
	}
	ctx, cancel := context.WithTimeout(context.Background(), proxyOperationTimeout)
	defer cancel()
	return s.manager.Start(ctx)
}

func (s *ProxyService) Stop() (model.ProxyState, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	state, err := s.manager.Stop()
	// The traffic sampler stops with the proxy; write the remaining deltas now
	// instead of waiting for a flush interval that may never come.
	s.flushTrafficHistory()
	return state, err
}

func (s *ProxyService) State() model.ProxyState {
	return s.manager.State()
}

// Traffic returns the latest live traffic snapshot for the running proxy.
func (s *ProxyService) Traffic() model.TrafficStats {
	return s.manager.Traffic()
}

func (s *ProxyService) Logs() []model.LogEntry {
	return s.manager.Logs()
}

func (s *ProxyService) ClearLogs() {
	s.manager.ClearLogs()
}

func (s *ProxyService) Test() (model.ProxyTestResult, error) {
	return s.manager.Test(s.store.Settings())
}

// TestURL measures the real proxy-chain latency of the requested nodes. An empty
// ids list tests every enabled node; unknown or disabled nodes are reported
// individually in Error. It reuses the NodeService batch engine and feeds the
// failover selector with fresh latency data.
func (s *ProxyService) TestURL(ids []string) []model.NodeLatency {
	s.urlTestMu.Lock()
	defer s.urlTestMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), proxyURLTestBudget)
	defer cancel()
	results := proxyruntime.URLTestNodes(ctx, s.core, s.store.ListNodes(), s.store.Settings(), ids, s.urlTestTempDir())
	if len(ids) == 0 {
		s.setURLTestResults(results)
	}
	return results
}

// urlTestTempDir keeps the temporary measurement config next to the normal
// runtime config so it never depends on a writable OS temp directory.
func (s *ProxyService) urlTestTempDir() string {
	return filepath.Join(s.store.DataDir(), "runtime")
}

// schedulerLoop drives automatic node selection and failover health checks. It
// owns all of its timers, reads settings on every tick (so settings changes apply
// from the next round on) and exits as soon as the lifecycle context is
// cancelled.
func (s *ProxyService) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(proxySchedulerTick)
	defer ticker.Stop()

	var nextAutoSelect time.Time
	var lastHealthCheck time.Time
	consecutiveFailures := 0
	loadBalanceNotified := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		settings := s.store.Settings()
		state := s.manager.State()
		now := time.Now()

		// Load balancing delegates node selection to Xray's balancer/observatory,
		// so the app-level automatic selection and failover stay idle.
		if settings.LoadBalanceEnabled {
			if !loadBalanceNotified {
				s.manager.AppendLog("info", "loadbalance", "负载均衡已启用，自动选路与故障切换暂不执行（由 Xray observatory 调度节点）")
				loadBalanceNotified = true
			}
			nextAutoSelect = time.Time{}
			lastHealthCheck = time.Time{}
			consecutiveFailures = 0
			continue
		}
		loadBalanceNotified = false

		// Automatic node selection. TUN mode captures the temporary URL-test core's
		// direct dials as tunnel traffic, so a probe would measure the tunnel
		// instead of the candidate nodes; keep auto-select idle while TUN is on.
		if !settings.AutoSelectEnabled || settings.TunMode {
			nextAutoSelect = time.Time{}
		} else if state.Status == "running" && (nextAutoSelect.IsZero() || !now.Before(nextAutoSelect)) {
			// TryLock keeps a UI-triggered round from being delayed by the
			// scheduler: when the engine is busy the round is retried on a later
			// tick without consuming the interval.
			if s.urlTestMu.TryLock() {
				switched := s.runAutoSelect(ctx, settings)
				s.urlTestMu.Unlock()
				nextAutoSelect = time.Now().Add(autoSelectInterval(settings))
				if switched {
					continue
				}
			}
		}

		// Health checks. The same probe feeds two features: failover switching and
		// the anti-DPI fallback, so it has to run when either one is enabled —
		// otherwise the fallback would never see a failure unless the user also
		// turned failover on.
		if !settings.FailoverEnabled && !settings.AutoAntiDpiFallback {
			lastHealthCheck = time.Time{}
			consecutiveFailures = 0
			continue
		}
		if s.manager.State().Status != "running" {
			consecutiveFailures = 0
			continue
		}
		if !lastHealthCheck.IsZero() && time.Since(lastHealthCheck) < proxyFailoverCheckInterval {
			continue
		}
		lastHealthCheck = time.Now()
		if s.checkProxyHealth(consecutiveFailures + 1) {
			if consecutiveFailures > 0 {
				s.manager.AppendLog("info", "failover", "代理健康检查恢复，故障切换计数已清零")
			}
			consecutiveFailures = 0
			continue
		}
		// The fallback arms itself inside the probe; failover switching stays
		// gated on its own setting.
		if !settings.FailoverEnabled {
			continue
		}
		consecutiveFailures++
		if consecutiveFailures < proxyFailoverFailureThreshold {
			continue
		}
		consecutiveFailures = 0
		if s.runFailover(ctx) {
			lastHealthCheck = time.Now()
		}
	}
}

// runAutoSelect measures every enabled node and switches to a meaningfully
// faster one. The caller must hold urlTestMu. It reports whether the active node
// was changed.
func (s *ProxyService) runAutoSelect(ctx context.Context, settings model.Settings) bool {
	nodes := enabledNodes(s.store.ListNodes())
	if len(nodes) == 0 {
		return false
	}
	results := proxyruntime.URLTest(ctx, proxyruntime.URLTestRequest{
		CorePath: s.corePath(),
		Nodes:    nodes,
		Settings: settings,
		TempDir:  s.urlTestTempDir(),
	})
	s.setURLTestResults(results)

	currentID := s.manager.State().NodeID
	if currentID == "" {
		if active, ok := s.store.ActiveNode(); ok {
			currentID = active.ID
		}
	}
	targetID := proxyruntime.ChooseBestNode(results, currentID, latencyForNode(results, currentID), settings.AutoSelectToleranceMS)
	if targetID == "" {
		return false
	}
	s.manager.AppendLog("info", "autoselect",
		fmt.Sprintf("自动选路：切换到 %s (%dms)", nodeDisplayName(nodes, targetID), latencyForNode(results, targetID)))
	if _, err := s.switchNode(ctx, targetID); err != nil {
		s.manager.AppendLog("error", "autoselect", "自动选路切换失败: "+err.Error())
		return false
	}
	return true
}

// runFailover switches to the fastest known alternative after repeated health
// check failures, falling back to the next enabled node. It reports whether the
// active node was changed.
func (s *ProxyService) runFailover(ctx context.Context) bool {
	nodes := enabledNodes(s.store.ListNodes())
	currentID := s.manager.State().NodeID
	targetID := proxyruntime.ChooseFailoverNode(s.snapshotURLTestResults(), nodes, currentID)
	if targetID == "" {
		s.manager.AppendLog("warning", "failover", "故障切换：当前节点连续失败，但没有可用的备选节点")
		return false
	}
	s.manager.AppendLog("warning", "failover",
		fmt.Sprintf("故障切换：当前节点连续失败，切换到 %s", nodeDisplayName(nodes, targetID)))
	if _, err := s.switchNode(ctx, targetID); err != nil {
		s.manager.AppendLog("error", "failover", "故障切换失败: "+err.Error())
		return false
	}
	return true
}

// checkProxyHealth performs one request through the local inbound using the
// existing proxy test path.
func (s *ProxyService) checkProxyHealth(attempt int) bool {
	result, err := s.manager.Test(s.store.Settings())
	if err == nil && result.Success {
		return true
	}
	message := strings.TrimSpace(result.Error)
	if message == "" && err != nil {
		message = err.Error()
	}
	if message == "" {
		message = "未知错误"
	}
	s.manager.AppendLog("warning", "failover",
		fmt.Sprintf("代理健康检查失败（%d/%d）：%s", attempt, proxyFailoverFailureThreshold, message))
	return false
}

func (s *ProxyService) corePath() string {
	if s.core == nil {
		return ""
	}
	return strings.TrimSpace(s.core.Info().Path)
}

func (s *ProxyService) setURLTestResults(results []model.NodeLatency) {
	s.resultsMu.Lock()
	s.urlTestResults = append([]model.NodeLatency(nil), results...)
	s.resultsMu.Unlock()
}

func (s *ProxyService) snapshotURLTestResults() []model.NodeLatency {
	s.resultsMu.RLock()
	defer s.resultsMu.RUnlock()
	return append([]model.NodeLatency(nil), s.urlTestResults...)
}

// autoSelectInterval converts the configured interval into a duration, clamping
// hand-edited or legacy values into the supported 1-1440 minute range.
func autoSelectInterval(settings model.Settings) time.Duration {
	minutes := settings.AutoSelectIntervalMinutes
	if minutes < 1 {
		minutes = 1
	}
	if minutes > 1440 {
		minutes = 1440
	}
	return time.Duration(minutes) * time.Minute
}

func enabledNodes(nodes []model.Node) []model.Node {
	enabled := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			enabled = append(enabled, node)
		}
	}
	return enabled
}

func nodeDisplayName(nodes []model.Node, id string) string {
	for _, node := range nodes {
		if node.ID == id {
			if strings.TrimSpace(node.Name) != "" {
				return node.Name
			}
			return id
		}
	}
	return id
}

func latencyForNode(results []model.NodeLatency, id string) int64 {
	if id == "" {
		return 0
	}
	for _, result := range results {
		if result.NodeID == id && result.Error == "" {
			return result.LatencyMS
		}
	}
	return 0
}

func (s *ProxyService) emitLog(entry model.LogEntry) {
	// Broadcast first so the UI never waits for disk I/O, then persist through
	// the optional sink.
	if app := application.Get(); app != nil {
		app.Event.Emit("proxy:log", entry)
	}
	s.logSinkMu.RLock()
	sink := s.logSink
	s.logSinkMu.RUnlock()
	if sink == nil {
		return
	}
	// A misbehaving sink must never break the proxy pipeline: the log fan-out is
	// best effort by design.
	defer func() {
		_ = recover()
	}()
	sink(entry)
}

func (s *ProxyService) emit(state model.ProxyState) {
	if app := application.Get(); app != nil {
		app.Event.Emit("proxy:state", state)
	}
	// A stopped or failed proxy produces no further live samples, so persist the
	// pending traffic deltas now instead of waiting for the next interval.
	if state.Status == "stopped" || state.Status == "error" {
		s.flushTrafficHistory()
	}
}

func (s *ProxyService) emitTraffic(stats model.TrafficStats) {
	if app := application.Get(); app != nil {
		app.Event.Emit("proxy:traffic", stats)
	}
	// Accumulate the delta in memory; persistence happens once per flush interval
	// (or when the proxy stops) so the sampler's two second tick never rewrites
	// the store file.
	if err := s.observeTraffic(stats); err != nil {
		s.manager.AppendLog("warning", "stats", "保存流量历史失败: "+err.Error())
	}
}

// observeTraffic forwards one live sample to the traffic recorder. It is a small
// wrapper so the event emission above stays independent from persistence.
func (s *ProxyService) observeTraffic(stats model.TrafficStats) error {
	if s.traffic == nil {
		return nil
	}
	return s.traffic.observe(stats)
}

// flushTrafficHistory persists accumulated live traffic deltas into the daily
// history immediately. Failures are logged and never fail the proxy operation.
func (s *ProxyService) flushTrafficHistory() {
	if s.traffic == nil {
		return
	}
	if err := s.traffic.flush(); err != nil {
		s.manager.AppendLog("warning", "stats", "保存流量历史失败: "+err.Error())
	}
}

// trafficRecorder batches live traffic deltas in memory and writes them into the
// store's daily history at most once per flush interval. Live samples arrive
// every two seconds; accumulating them until the interval elapsed (or until the
// proxy stops / the service shuts down) keeps the disk cost negligible while
// still recording every byte.
type trafficRecorder struct {
	store    *store.Store
	interval time.Duration
	// now is overridable in tests; nil means time.Now.
	now func() time.Time

	mu            sync.Mutex
	pendingUplink int64
	pendingDown   int64
	lastFlushAt   time.Time
}

func newTrafficRecorder(st *store.Store, interval time.Duration) *trafficRecorder {
	if interval <= 0 {
		interval = trafficHistoryFlushInterval
	}
	recorder := &trafficRecorder{store: st, interval: interval, now: time.Now}
	recorder.lastFlushAt = recorder.clock()
	return recorder
}

// observe adds one live sample to the pending totals and flushes them once the
// flush interval elapsed. Snapshots that report an error or a stopped proxy are
// ignored: on error the sampler repeats the last successful window's deltas, so
// counting them would double count traffic.
func (r *trafficRecorder) observe(stats model.TrafficStats) error {
	if r == nil || r.store == nil || !stats.Running || stats.Error != "" {
		return nil
	}
	uplink, downlink := stats.UplinkBytes, stats.DownlinkBytes
	if uplink < 0 {
		uplink = 0
	}
	if downlink < 0 {
		downlink = 0
	}
	r.mu.Lock()
	r.pendingUplink += uplink
	r.pendingDown += downlink
	if r.pendingUplink+r.pendingDown == 0 || !r.flushDueLocked() {
		r.mu.Unlock()
		return nil
	}
	pendingUplink, pendingDown := r.takePendingLocked()
	r.mu.Unlock()
	return r.persist(pendingUplink, pendingDown)
}

// flush writes every pending delta immediately. It is used when the proxy stops
// or the service shuts down so no traffic is lost between two intervals.
func (r *trafficRecorder) flush() error {
	if r == nil || r.store == nil {
		return nil
	}
	r.mu.Lock()
	uplink, downlink := r.takePendingLocked()
	r.mu.Unlock()
	return r.persist(uplink, downlink)
}

// pending returns the currently buffered totals. It is only used by tests.
func (r *trafficRecorder) pending() (int64, int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pendingUplink, r.pendingDown
}

// flushDueLocked reports whether the next flush is due. The caller must hold r.mu.
func (r *trafficRecorder) flushDueLocked() bool {
	return !r.clock().Before(r.lastFlushAt.Add(r.interval))
}

// takePendingLocked returns and clears the pending totals, recording the flush
// time. The caller must hold r.mu.
func (r *trafficRecorder) takePendingLocked() (int64, int64) {
	uplink, downlink := r.pendingUplink, r.pendingDown
	r.pendingUplink, r.pendingDown = 0, 0
	r.lastFlushAt = r.clock()
	return uplink, downlink
}

// persist writes one batch into the store. On failure the bytes are put back
// into the pending totals so the next sample retries instead of dropping them.
func (r *trafficRecorder) persist(uplink, downlink int64) error {
	if uplink == 0 && downlink == 0 {
		return nil
	}
	if err := r.store.AddTrafficBytes(uplink, downlink, r.clock()); err != nil {
		r.mu.Lock()
		r.pendingUplink += uplink
		r.pendingDown += downlink
		r.mu.Unlock()
		return err
	}
	return nil
}

func (r *trafficRecorder) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}
