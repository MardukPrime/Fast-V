package proxy

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

// ---------------------------------------------------------------------------
// Pure watchdog policy
// ---------------------------------------------------------------------------

func TestRestartBackoffDelay(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 8 * time.Second},
		{10, 8 * time.Second},
		{0, time.Second},
		{-3, time.Second},
	}
	for _, tc := range cases {
		if got := restartBackoffDelay(tc.attempt); got != tc.want {
			t.Errorf("restartBackoffDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestNextRestartDelay(t *testing.T) {
	allowed := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
	}
	for _, tc := range allowed {
		delay, ok := nextRestartDelay(tc.attempt)
		if !ok {
			t.Errorf("nextRestartDelay(%d) is not allowed, want %v", tc.attempt, tc.want)
			continue
		}
		if delay != tc.want {
			t.Errorf("nextRestartDelay(%d) delay = %v, want %v", tc.attempt, delay, tc.want)
		}
	}
	for _, attempt := range []int{-1, 0, 4, 5} {
		if delay, ok := nextRestartDelay(attempt); ok || delay != 0 {
			t.Errorf("nextRestartDelay(%d) = (%v, %v), want (0, false) past the retry limit", attempt, delay, ok)
		}
	}
}

func TestShouldAutoRestartCore(t *testing.T) {
	cases := []struct {
		name       string
		enabled    bool
		userStop   bool
		prevStatus string
		want       bool
	}{
		{"unexpected exit while running", true, false, "running", true},
		{"user stop while running", true, true, "running", false},
		{"user stop during shutdown", true, true, "stopping", false},
		{"auto restart disabled", false, false, "running", false},
		{"exit before the core was running", true, false, "starting", false},
		{"already stopped", true, false, "stopped", false},
		{"already failed", true, false, "error", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoRestartCore(tc.enabled, tc.userStop, tc.prevStatus); got != tc.want {
				t.Fatalf("shouldAutoRestartCore(%v, %v, %q) = %v, want %v",
					tc.enabled, tc.userStop, tc.prevStatus, got, tc.want)
			}
		})
	}
}

func TestPlanCoreRestart(t *testing.T) {
	cases := []struct {
		name        string
		enabled     bool
		userStop    bool
		prevStatus  string
		consecutive int
		ranFor      time.Duration
		wantAttempt int
		wantDelay   time.Duration
		wantRestart bool
	}{
		{
			name: "first crash", enabled: true, prevStatus: "running", consecutive: 0, ranFor: 3 * time.Second,
			wantAttempt: 1, wantDelay: time.Second, wantRestart: true,
		},
		{
			name: "second consecutive crash", enabled: true, prevStatus: "running", consecutive: 1, ranFor: 3 * time.Second,
			wantAttempt: 2, wantDelay: 2 * time.Second, wantRestart: true,
		},
		{
			name: "third consecutive crash", enabled: true, prevStatus: "running", consecutive: 2, ranFor: 3 * time.Second,
			wantAttempt: 3, wantDelay: 4 * time.Second, wantRestart: true,
		},
		{
			name: "retry limit reached", enabled: true, prevStatus: "running", consecutive: 3, ranFor: 3 * time.Second,
			wantRestart: false,
		},
		{
			name: "stable run resets the chain", enabled: true, prevStatus: "running", consecutive: 3, ranFor: 61 * time.Second,
			wantAttempt: 1, wantDelay: time.Second, wantRestart: true,
		},
		{
			name: "stable threshold resets the chain", enabled: true, prevStatus: "running", consecutive: 2, ranFor: coreStableRunThreshold,
			wantAttempt: 1, wantDelay: time.Second, wantRestart: true,
		},
		{
			name: "short run keeps the chain", enabled: true, prevStatus: "running", consecutive: 1, ranFor: coreStableRunThreshold - time.Millisecond,
			wantAttempt: 2, wantDelay: 2 * time.Second, wantRestart: true,
		},
		{
			name: "negative consecutive count is clamped", enabled: true, prevStatus: "running", consecutive: -4, ranFor: time.Second,
			wantAttempt: 1, wantDelay: time.Second, wantRestart: true,
		},
		{
			name: "user stop is never restarted", enabled: true, userStop: true, prevStatus: "running", ranFor: time.Second,
			wantRestart: false,
		},
		{
			name: "shutdown status is never restarted", enabled: true, userStop: true, prevStatus: "stopping", ranFor: time.Second,
			wantRestart: false,
		},
		{
			name: "disabled setting", enabled: false, prevStatus: "running", ranFor: time.Second,
			wantRestart: false,
		},
		{
			name: "crash while starting is not a watchdog restart", enabled: true, prevStatus: "starting", ranFor: time.Second,
			wantRestart: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempt, delay, restart := planCoreRestart(
				tc.enabled, tc.userStop, tc.prevStatus, tc.consecutive, tc.ranFor,
			)
			if restart != tc.wantRestart {
				t.Fatalf("planCoreRestart() restart = %v, want %v", restart, tc.wantRestart)
			}
			if !tc.wantRestart {
				if attempt != 0 || delay != 0 {
					t.Fatalf("planCoreRestart() = (%d, %v) when no restart is allowed, want (0, 0)", attempt, delay)
				}
				return
			}
			if attempt != tc.wantAttempt || delay != tc.wantDelay {
				t.Fatalf("planCoreRestart() = (%d, %v), want (%d, %v)", attempt, delay, tc.wantAttempt, tc.wantDelay)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Real-core integration tests (skipped when the local Xray binary is missing)
// ---------------------------------------------------------------------------

// watchdogTestBinary returns the local Xray binary used by the integration tests
// and skips the test when it is unavailable (for example in CI).
func watchdogTestBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	return binary
}

func watchdogFreeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

type watchdogCoreProvider struct {
	info model.CoreInfo
}

func (p watchdogCoreProvider) Info() model.CoreInfo {
	return p.info
}

// newWatchdogTestManager stores one node and returns a manager wired to the
// local Xray binary, with the given automatic restart setting.
func newWatchdogTestManager(t *testing.T, autoRestart bool) (*Manager, *store.Store) {
	t.Helper()
	binary := watchdogTestBinary(t)
	port := watchdogFreeTCPPort(t)
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("NewStoreAt() error = %v", err)
	}
	settings := store.DefaultSettings()
	settings.CoreType = "xray"
	settings.SystemProxyMode = "off"
	settings.XrayLocalPort = port
	settings.XrayAPIEnabled = false
	settings.StatsEnabled = false
	settings.XrayLogLevel = "warning"
	settings.ProxyMode = "rule"
	settings.AutoRestartCore = autoRestart
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings() error = %v", err)
	}
	nodes := subdomain.BuildNodes("sub-1", []subdomain.ParsedNode{{
		Name: "Watchdog Test", Protocol: "vless", Address: "example.com", Port: 443,
		Raw: "vless://00000000-0000-0000-0000-000000000031@example.com:443?security=tls#Watchdog",
	}})
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Watchdog"}, nodes); err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}
	manager := NewManager(st, watchdogCoreProvider{info: model.CoreInfo{
		Installed: true,
		Path:      binary,
		Status:    "ready",
	}})
	return manager, st
}

func startWatchdogTestManager(t *testing.T, manager *Manager) model.ProxyState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	state, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v logs=%+v", err, manager.Logs())
	}
	if state.Status != "running" || state.PID == 0 {
		t.Fatalf("unexpected start state: %+v", state)
	}
	return state
}

// killCoreProcess terminates the running core the way a crash would.
func killCoreProcess(t *testing.T, pid int) {
	t.Helper()
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("os.FindProcess(%d) error = %v", pid, err)
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("failed to kill the Xray-core process: %v", err)
	}
}

// waitForWatchdogState polls the manager state until match is satisfied.
func waitForWatchdogState(t *testing.T, manager *Manager, timeout time.Duration, match func(model.ProxyState) bool) model.ProxyState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := model.ProxyState{}
	for time.Now().Before(deadline) {
		last = manager.State()
		if match(last) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the expected proxy state, last=%+v logs=%+v", last, manager.Logs())
	return last
}

// countLogFragments counts the manager log entries whose message contains the
// fragment.
func countLogFragments(manager *Manager, fragment string) int {
	count := 0
	for _, entry := range manager.Logs() {
		if strings.Contains(entry.Message, fragment) {
			count++
		}
	}
	return count
}

// TestManagerAutoRestartsCoreAfterCrash kills a running core and verifies that
// the watchdog restarts it with the documented backoff, logs the event and
// emits the starting/running states.
func TestManagerAutoRestartsCoreAfterCrash(t *testing.T) {
	manager, _ := newWatchdogTestManager(t, true)
	t.Cleanup(func() { _, _ = manager.Stop() })

	var eventsMu sync.Mutex
	events := make([]model.ProxyState, 0, 32)
	manager.SetOnStateChange(func(state model.ProxyState) {
		eventsMu.Lock()
		events = append(events, state)
		eventsMu.Unlock()
	})

	state := startWatchdogTestManager(t, manager)
	originalPID := state.PID

	killCoreProcess(t, originalPID)

	restarted := waitForWatchdogState(t, manager, 30*time.Second, func(s model.ProxyState) bool {
		return s.Status == "running" && s.PID != 0 && s.PID != originalPID
	})
	if restarted.LocalAddr != state.LocalAddr {
		t.Fatalf("restarted proxy changed the local address: %+v", restarted)
	}
	if got := countLogFragments(manager, "核心异常退出，正在自动重启（第 1 次）"); got != 1 {
		t.Fatalf("crash restart was not logged exactly once (got %d): %+v", got, manager.Logs())
	}
	if got := countLogFragments(manager, "核心自动重启成功（第 1 次）"); got != 1 {
		t.Fatalf("successful restart was not logged exactly once (got %d): %+v", got, manager.Logs())
	}

	// The restarted core must serve the local inbound again.
	conn, err := net.DialTimeout("tcp", restarted.LocalAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("local proxy port is not reachable after the restart: %v", err)
	}
	_ = conn.Close()

	// The state stream must show a "starting" state after the crashed core had
	// reached "running", before the replacement core became ready.
	eventsMu.Lock()
	snapshot := append([]model.ProxyState(nil), events...)
	eventsMu.Unlock()
	runningIndex := -1
	for i, event := range snapshot {
		if event.Status == "running" && event.PID == originalPID {
			runningIndex = i
		}
	}
	sawStartingAfterCrash := false
	if runningIndex >= 0 {
		for _, event := range snapshot[runningIndex+1:] {
			if event.Status == "starting" && event.PID == 0 {
				sawStartingAfterCrash = true
				break
			}
		}
	}
	if !sawStartingAfterCrash {
		t.Fatalf("no starting state was emitted between the crash and the restart: %+v", snapshot)
	}

	// A user-requested stop must win over the watchdog: no further restart.
	if _, err := manager.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if final := manager.State(); final.Status != "stopped" {
		t.Fatalf("proxy restarted after a user stop: %+v", final)
	}
	if got := countLogFragments(manager, "核心异常退出，正在自动重启"); got != 1 {
		t.Fatalf("user stop triggered another crash restart (got %d entries): %+v", got, manager.Logs())
	}
}

// TestManagerStopCancelsPendingAutoRestart crashes the core and stops the proxy
// while the watchdog is still waiting for its backoff: no replacement core may be
// launched and the stopped state must win.
func TestManagerStopCancelsPendingAutoRestart(t *testing.T) {
	manager, _ := newWatchdogTestManager(t, true)
	t.Cleanup(func() { _, _ = manager.Stop() })

	state := startWatchdogTestManager(t, manager)
	restartScheduled := make(chan struct{}, 1)
	manager.SetOnStateChange(func(next model.ProxyState) {
		if next.Status == "starting" && next.PID == 0 {
			select {
			case restartScheduled <- struct{}{}:
			default:
			}
		}
	})
	killCoreProcess(t, state.PID)
	select {
	case <-restartScheduled:
	case <-time.After(5 * time.Second):
		t.Fatalf("watchdog did not schedule a restart: %+v logs=%+v", manager.State(), manager.Logs())
	}

	if stopped, err := manager.Stop(); err != nil || stopped.Status != "stopped" {
		t.Fatalf("Stop() = (%+v, %v), want the stopped state", stopped, err)
	}
	if got := countLogFragments(manager, "核心自动重启已取消"); got != 1 {
		t.Fatalf("cancelled restart was not logged exactly once (got %d): %+v", got, manager.Logs())
	}
	// Wait past the first backoff step (1s) to prove no core was launched.
	time.Sleep(1500 * time.Millisecond)
	if final := manager.State(); final.Status != "stopped" {
		t.Fatalf("watchdog restarted the core after a user stop: %+v", final)
	}
	if got := countLogFragments(manager, "核心自动重启成功"); got != 0 {
		t.Fatalf("a core was restarted after a user stop (got %d entries): %+v", got, manager.Logs())
	}
}

// TestManagerAutoRestartStopsAfterConsecutiveCrashes kills the core four times
// in a row and verifies that the watchdog performs exactly three consecutive
// restarts and then gives up with an error state.
func TestManagerAutoRestartStopsAfterConsecutiveCrashes(t *testing.T) {
	manager, _ := newWatchdogTestManager(t, true)
	t.Cleanup(func() { _, _ = manager.Stop() })

	state := startWatchdogTestManager(t, manager)
	lastPID := state.PID
	for attempt := 1; attempt <= coreMaxConsecutiveRestarts; attempt++ {
		killCoreProcess(t, lastPID)
		restarted := waitForWatchdogState(t, manager, 30*time.Second, func(s model.ProxyState) bool {
			return s.Status == "running" && s.PID != 0 && s.PID != lastPID
		})
		lastPID = restarted.PID
	}
	if got := countLogFragments(manager, "核心异常退出，正在自动重启"); got != coreMaxConsecutiveRestarts {
		t.Fatalf("expected %d automatic restarts, got %d: %+v", coreMaxConsecutiveRestarts, got, manager.Logs())
	}

	// The fourth consecutive crash must exhaust the retry limit instead of
	// restarting the core again.
	killCoreProcess(t, lastPID)
	failed := waitForWatchdogState(t, manager, 15*time.Second, func(s model.ProxyState) bool {
		return s.Status == "error"
	})
	if strings.TrimSpace(failed.Error) == "" {
		t.Fatalf("exhausted watchdog has no error text: %+v", failed)
	}
	if got := countLogFragments(manager, "自动重启已达上限"); got != 1 {
		t.Fatalf("retry limit was not reported exactly once (got %d): %+v", got, manager.Logs())
	}
	if got := countLogFragments(manager, "核心异常退出，正在自动重启"); got != coreMaxConsecutiveRestarts {
		t.Fatalf("watchdog restarted past the limit (got %d entries): %+v", got, manager.Logs())
	}
}

// TestManagerCoreCrashWithoutAutoRestartStopsWithError verifies that disabling
// AutoRestartCore keeps the old behaviour: the crash is reported as an error
// state and the core is not restarted.
func TestManagerCoreCrashWithoutAutoRestartStopsWithError(t *testing.T) {
	manager, _ := newWatchdogTestManager(t, false)
	t.Cleanup(func() { _, _ = manager.Stop() })

	state := startWatchdogTestManager(t, manager)
	killCoreProcess(t, state.PID)

	failed := waitForWatchdogState(t, manager, 15*time.Second, func(s model.ProxyState) bool {
		return s.Status == "error"
	})
	if strings.TrimSpace(failed.Error) == "" {
		t.Fatalf("crash error state has no error text: %+v", failed)
	}
	if got := countLogFragments(manager, "核心异常退出，正在自动重启"); got != 0 {
		t.Fatalf("auto restart ran although it is disabled (got %d entries): %+v", got, manager.Logs())
	}
	if _, err := manager.Stop(); err != nil {
		t.Fatalf("stop after crash failed: %v", err)
	}
}

// TestManagerAutoRestartFailureStopsWithError forces the restart attempt to
// fail (no nodes left to build a configuration) and verifies that the watchdog
// gives up with an error state instead of retrying forever.
func TestManagerAutoRestartFailureStopsWithError(t *testing.T) {
	manager, st := newWatchdogTestManager(t, true)
	t.Cleanup(func() { _, _ = manager.Stop() })

	state := startWatchdogTestManager(t, manager)
	if err := st.DeleteSubscription("sub-1"); err != nil {
		t.Fatalf("DeleteSubscription() error = %v", err)
	}

	killCoreProcess(t, state.PID)

	failed := waitForWatchdogState(t, manager, 20*time.Second, func(s model.ProxyState) bool {
		return s.Status == "error"
	})
	if !strings.Contains(failed.Error, "no active node") {
		t.Fatalf("unexpected restart failure error: %+v", failed)
	}
	if got := countLogFragments(manager, "核心自动重启失败（第 1 次）"); got != 1 {
		t.Fatalf("restart failure was not logged exactly once (got %d): %+v", got, manager.Logs())
	}
	time.Sleep(1200 * time.Millisecond)
	if final := manager.State(); final.Status != "error" {
		t.Fatalf("a failed automatic restart must not be retried: %+v", final)
	}
}
