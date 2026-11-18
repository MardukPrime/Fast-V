package rescue

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// fakePlatform records every OS-level call so tests can assert what Repair did
// without touching the real registry, netsh or ipconfig.
type fakePlatform struct {
	systemProxy    platform.SystemProxyState
	systemProxyErr error

	winHTTPProxy string
	winHTTPErr   error

	resetErr error
	flushErr error

	portInUse map[int]bool
	portErr   error
	portCalls []int

	resetCalls int
	flushCalls int
}

func (f *fakePlatform) ReadSystemProxyState() (platform.SystemProxyState, error) {
	return f.systemProxy, f.systemProxyErr
}

func (f *fakePlatform) ReadWinHTTPProxy() (string, error) {
	return f.winHTTPProxy, f.winHTTPErr
}

func (f *fakePlatform) ResetSystemProxy() error {
	f.resetCalls++
	if f.resetErr != nil {
		return f.resetErr
	}
	f.systemProxy = platform.SystemProxyState{}
	return nil
}

func (f *fakePlatform) FlushDNS() error {
	f.flushCalls++
	return f.flushErr
}

func (f *fakePlatform) PortInUse(port int) (bool, error) {
	f.portCalls = append(f.portCalls, port)
	if f.portErr != nil {
		return false, f.portErr
	}
	return f.portInUse[port], nil
}

// fakeProxy is a ProxyController with controllable running state and restart
// outcome.
type fakeProxy struct {
	state      model.ProxyState
	restartErr error

	restartCalls int
}

func (f *fakeProxy) State() model.ProxyState {
	return f.state
}

func (f *fakeProxy) Restart() (model.ProxyState, error) {
	f.restartCalls++
	if f.restartErr != nil {
		return f.state, f.restartErr
	}
	if f.state.Status == proxyStatusRunning {
		f.state.StartedAt = "restarted"
	}
	return f.state, nil
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	return st
}

// newTestService builds a service with fake primitives and a deterministic
// clock so CheckedAt is assertable.
func newTestService(t *testing.T, plat *fakePlatform, proxy *fakeProxy) *Service {
	t.Helper()
	service := NewWithPlatform(newTestStore(t), proxy, plat)
	service.now = func() time.Time {
		return time.Date(2026, time.November, 18, 12, 0, 0, 0, time.UTC)
	}
	return service
}

func findIssue(report RescueReport, id string) (RescueIssue, bool) {
	for _, issue := range report.Issues {
		if issue.ID == id {
			return issue, true
		}
	}
	return RescueIssue{}, false
}

func requireIssue(t *testing.T, report RescueReport, id string) RescueIssue {
	t.Helper()
	issue, ok := findIssue(report, id)
	if !ok {
		t.Fatalf("expected issue %q in %+v", id, report.Issues)
	}
	return issue
}

func requireNoIssue(t *testing.T, report RescueReport, id string) {
	t.Helper()
	if _, ok := findIssue(report, id); ok {
		t.Fatalf("did not expect issue %q in %+v", id, report.Issues)
	}
}

func requireStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func requireInts(t *testing.T, got, want []int) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestServiceName(t *testing.T) {
	if name := New(nil, nil).ServiceName(); name != "RescueService" {
		t.Fatalf("unexpected service name: %q", name)
	}
}

func TestInspectReportsHealthyEnvironment(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"},
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("a healthy environment must report exactly one ok issue: %+v", report.Issues)
	}
	issue := report.Issues[0]
	if issue.ID != issueNetworkOK || issue.Severity != severityOK || issue.Fixable {
		t.Fatalf("unexpected healthy issue: %+v", issue)
	}
	if !report.SystemProxyEnabled || report.SystemProxyServer != "127.0.0.1:10808" || !report.ProxyRunning {
		t.Fatalf("unexpected report state: %+v", report)
	}
	if want := "2026-11-18T12:00:00Z"; report.CheckedAt != want {
		t.Fatalf("checkedAt = %q, want %q", report.CheckedAt, want)
	}
	// The port is owned by the running core, so it must not be probed.
	if len(plat.portCalls) != 0 {
		t.Fatalf("port must not be probed while the proxy is running: %v", plat.portCalls)
	}
}

func TestInspectAcceptsPerProtocolProxyList(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{
			Enabled: true,
			Server:  "http=127.0.0.1:10808;https=127.0.0.1:10808",
		},
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	requireNoIssue(t, report, issueSystemProxyMismatch)
	if _, ok := findIssue(report, issueNetworkOK); !ok {
		t.Fatalf("expected network-ok, got %+v", report.Issues)
	}
}

func TestInspectWarnsOrphanSystemProxy(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"},
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(report.Issues) != 1 {
		t.Fatalf("expected exactly one warning: %+v", report.Issues)
	}
	issue := requireIssue(t, report, issueSystemProxyOrphan)
	if issue.Severity != severityWarning {
		t.Fatalf("unexpected severity: %+v", issue)
	}
	if !issue.Fixable {
		t.Fatalf("orphan system proxy must be fixable: %+v", issue)
	}
	if !strings.Contains(issue.Detail, "127.0.0.1:10808") || !strings.Contains(issue.Detail, "未运行") {
		t.Fatalf("detail must mention the server and stopped proxy: %q", issue.Detail)
	}
	if !report.SystemProxyEnabled || report.SystemProxyServer != "127.0.0.1:10808" || report.ProxyRunning {
		t.Fatalf("unexpected report state: %+v", report)
	}
}

func TestInspectWarnsMismatchedSystemProxy(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10809"},
	}
	proxy := &fakeProxy{state: model.ProxyState{
		Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808", NodeName: "demo",
	}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	issue := requireIssue(t, report, issueSystemProxyMismatch)
	if issue.Severity != severityWarning {
		t.Fatalf("unexpected severity: %+v", issue)
	}
	if !strings.Contains(issue.Detail, "127.0.0.1:10809") || !strings.Contains(issue.Detail, "127.0.0.1:10808") {
		t.Fatalf("detail must mention both addresses: %q", issue.Detail)
	}
	if !report.ProxyRunning {
		t.Fatalf("expected running proxy in report: %+v", report)
	}
}

func TestInspectReportsPortInUse(t *testing.T) {
	plat := &fakePlatform{portInUse: map[int]bool{10808: true}}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	issue := requireIssue(t, report, issuePortInUse)
	if issue.Severity != severityError {
		t.Fatalf("unexpected severity: %+v", issue)
	}
	if !strings.Contains(issue.Detail, "10808") {
		t.Fatalf("detail must mention the port: %q", issue.Detail)
	}
	requireInts(t, plat.portCalls, []int{10808})
}

func TestInspectUsesConfiguredLocalPort(t *testing.T) {
	st := newTestStore(t)
	settings := st.Settings()
	settings.XrayLocalPort = 20808
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	plat := &fakePlatform{portInUse: map[int]bool{20808: true}}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := NewWithPlatform(st, proxy, plat)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	requireIssue(t, report, issuePortInUse)
	requireInts(t, plat.portCalls, []int{20808})
}

func TestInspectSkipsPortCheckOnError(t *testing.T) {
	plat := &fakePlatform{portErr: errors.New("probe unavailable")}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	issue := requireIssue(t, report, issuePortSkipped)
	if issue.Severity != severityOK {
		t.Fatalf("skipped check must be ok-severity: %+v", issue)
	}
	requireNoIssue(t, report, issuePortInUse)
}

func TestInspectSkipsUnavailablePlatform(t *testing.T) {
	plat := &fakePlatform{
		systemProxyErr: errors.New("仅支持 Windows"),
		winHTTPErr:     errors.New("仅支持 Windows"),
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	for _, id := range []string{issueSystemProxySkipped, issueWinHTTPSkipped} {
		issue := requireIssue(t, report, id)
		if issue.Severity != severityOK {
			t.Fatalf("skipped check must be ok-severity: %+v", issue)
		}
	}
	if report.SystemProxyEnabled || report.SystemProxyServer != "" {
		t.Fatalf("unavailable platform must leave proxy fields empty: %+v", report)
	}
}

func TestInspectReportsWinHTTPResidual(t *testing.T) {
	t.Run("proxy stopped is fixable", func(t *testing.T) {
		plat := &fakePlatform{winHTTPProxy: "127.0.0.1:10809"}
		proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
		service := newTestService(t, plat, proxy)

		report, err := service.Inspect()
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		issue := requireIssue(t, report, issueWinHTTPProxy)
		if issue.Severity != severityWarning || !issue.Fixable {
			t.Fatalf("unexpected winhttp issue: %+v", issue)
		}
		if !strings.Contains(issue.Detail, "127.0.0.1:10809") {
			t.Fatalf("detail must mention the winhttp proxy: %q", issue.Detail)
		}
	})

	t.Run("proxy running leaves system proxy alone", func(t *testing.T) {
		plat := &fakePlatform{winHTTPProxy: "127.0.0.1:10809"}
		proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808"}}
		service := newTestService(t, plat, proxy)

		report, err := service.Inspect()
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		issue := requireIssue(t, report, issueWinHTTPProxy)
		if issue.Fixable {
			t.Fatalf("running-proxy repair must not claim to reset the system proxy: %+v", issue)
		}
	})
}

func TestInspectReportsCoreError(t *testing.T) {
	t.Run("running with core error", func(t *testing.T) {
		plat := &fakePlatform{}
		proxy := &fakeProxy{state: model.ProxyState{
			Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808", Error: "core crashed",
		}}
		service := newTestService(t, plat, proxy)

		report, err := service.Inspect()
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		issue := requireIssue(t, report, issueCoreError)
		if issue.Severity != severityError || !issue.Fixable {
			t.Fatalf("unexpected core issue: %+v", issue)
		}
		if !strings.Contains(issue.Detail, "core crashed") {
			t.Fatalf("detail must carry the core error: %q", issue.Detail)
		}
	})

	t.Run("stopped in error state", func(t *testing.T) {
		plat := &fakePlatform{}
		proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusError, Error: "节点不可用"}}
		service := newTestService(t, plat, proxy)

		report, err := service.Inspect()
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		issue := requireIssue(t, report, issueCoreError)
		if issue.Severity != severityError || issue.Fixable {
			t.Fatalf("a stopped core is not fixed by the safe repair: %+v", issue)
		}
	})

	t.Run("running without local address", func(t *testing.T) {
		plat := &fakePlatform{}
		proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusRunning}}
		service := newTestService(t, plat, proxy)

		report, err := service.Inspect()
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		issue := requireIssue(t, report, issueCoreError)
		if issue.Severity != severityError || !issue.Fixable {
			t.Fatalf("unexpected core issue: %+v", issue)
		}
	})
}

func TestInspectReportsStartingProxy(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"},
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusStarting, LocalAddr: "127.0.0.1:10808"}}
	service := newTestService(t, plat, proxy)

	report, err := service.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	issue := requireIssue(t, report, issueProxyStarting)
	if issue.Severity != severityOK {
		t.Fatalf("starting state is informational: %+v", issue)
	}
	requireNoIssue(t, report, issueSystemProxyOrphan)
	requireNoIssue(t, report, issuePortInUse)
	if len(plat.portCalls) != 0 {
		t.Fatalf("port must not be probed while the proxy is starting: %v", plat.portCalls)
	}
	if report.ProxyRunning {
		t.Fatalf("starting proxy is not running: %+v", report)
	}
}

func TestRepairWhenProxyStopped(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"},
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := newTestService(t, plat, proxy)

	result, err := service.Repair()
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	requireStrings(t, result.Fixed, []string{"重置系统代理", "刷新 DNS 缓存"})
	requireStrings(t, result.Failed, []string{})
	if plat.resetCalls != 1 || plat.flushCalls != 1 {
		t.Fatalf("unexpected platform calls: reset=%d flush=%d", plat.resetCalls, plat.flushCalls)
	}
	if result.Report.SystemProxyEnabled || result.Report.SystemProxyServer != "" {
		t.Fatalf("post-repair report must show the reset proxy: %+v", result.Report)
	}
	if !strings.Contains(result.Message, "已修复 2 项") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	if _, ok := findIssue(result.Report, issueNetworkOK); !ok {
		t.Fatalf("post-repair report must be healthy: %+v", result.Report.Issues)
	}
}

func TestRepairWhenProxyRunning(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"},
	}
	proxy := &fakeProxy{state: model.ProxyState{
		Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808", NodeName: "demo",
	}}
	service := newTestService(t, plat, proxy)

	result, err := service.Repair()
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	requireStrings(t, result.Fixed, []string{"刷新 DNS 缓存", "重启本地代理"})
	requireStrings(t, result.Failed, []string{})
	if plat.flushCalls != 1 || proxy.restartCalls != 1 {
		t.Fatalf("unexpected calls: flush=%d restart=%d", plat.flushCalls, proxy.restartCalls)
	}
	if plat.resetCalls != 0 {
		t.Fatalf("running-proxy repair must not reset the system proxy: reset=%d", plat.resetCalls)
	}
	if proxy.state.StartedAt != "restarted" {
		t.Fatalf("restart must reach the proxy controller: %+v", proxy.state)
	}
	if !result.Report.ProxyRunning {
		t.Fatalf("post-repair report must show the restarted proxy: %+v", result.Report)
	}
	if !strings.Contains(result.Message, "已修复 2 项") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
}

func TestRepairRecordsFailuresWhenProxyStopped(t *testing.T) {
	plat := &fakePlatform{
		systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"},
		resetErr:    errors.New("access denied"),
		flushErr:    errors.New("flush failed"),
	}
	proxy := &fakeProxy{state: model.ProxyState{Status: "stopped"}}
	service := newTestService(t, plat, proxy)

	result, err := service.Repair()
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	requireStrings(t, result.Fixed, []string{})
	requireStrings(t, result.Failed, []string{"重置系统代理", "刷新 DNS 缓存"})
	if !strings.Contains(result.Message, "2 项失败") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	// The failed reset must leave the detected residual proxy in the report.
	if !result.Report.SystemProxyEnabled {
		t.Fatalf("post-repair report must still show the residual proxy: %+v", result.Report)
	}
}

func TestRepairRecordsRestartFailure(t *testing.T) {
	plat := &fakePlatform{}
	proxy := &fakeProxy{
		state:      model.ProxyState{Status: proxyStatusRunning, LocalAddr: "127.0.0.1:10808"},
		restartErr: errors.New("restart failed"),
	}
	service := newTestService(t, plat, proxy)

	result, err := service.Repair()
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	requireStrings(t, result.Fixed, []string{"刷新 DNS 缓存"})
	requireStrings(t, result.Failed, []string{"重启本地代理"})
}

func TestRepairWhileStartingDoesNothing(t *testing.T) {
	plat := &fakePlatform{systemProxy: platform.SystemProxyState{Enabled: true, Server: "127.0.0.1:10808"}}
	proxy := &fakeProxy{state: model.ProxyState{Status: proxyStatusStarting, LocalAddr: "127.0.0.1:10808"}}
	service := newTestService(t, plat, proxy)

	result, err := service.Repair()
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	requireStrings(t, result.Fixed, []string{})
	requireStrings(t, result.Failed, []string{})
	if !strings.Contains(result.Message, "正在启动") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
	if plat.resetCalls != 0 || plat.flushCalls != 0 || proxy.restartCalls != 0 {
		t.Fatalf("repair must not touch anything while starting: reset=%d flush=%d restart=%d",
			plat.resetCalls, plat.flushCalls, proxy.restartCalls)
	}
}

func TestSystemProxyMatchesInbound(t *testing.T) {
	cases := []struct {
		name      string
		server    string
		localAddr string
		want      bool
	}{
		{name: "plain match", server: "127.0.0.1:10808", localAddr: "127.0.0.1:10808", want: true},
		{name: "localhost", server: "localhost:10808", localAddr: "127.0.0.1:10808", want: true},
		{name: "per-protocol list", server: "http=127.0.0.1:10808;https=127.0.0.1:10808", localAddr: "127.0.0.1:10808", want: true},
		{name: "url scheme", server: "http://127.0.0.1:10808", localAddr: "127.0.0.1:10808", want: true},
		{name: "wrong port", server: "127.0.0.1:10809", localAddr: "127.0.0.1:10808", want: false},
		{name: "non loopback host", server: "192.168.1.10:10808", localAddr: "127.0.0.1:10808", want: false},
		{name: "empty server", server: "", localAddr: "127.0.0.1:10808", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := systemProxyMatchesInbound(tc.server, tc.localAddr); got != tc.want {
				t.Fatalf("systemProxyMatchesInbound(%q, %q) = %v, want %v",
					tc.server, tc.localAddr, got, tc.want)
			}
		})
	}
}

func TestRepairMessage(t *testing.T) {
	cases := []struct {
		fixed  []string
		failed []string
		want   string
	}{
		{fixed: nil, failed: nil, want: "未执行任何修复操作。"},
		{fixed: []string{"刷新 DNS 缓存"}, failed: nil, want: "网络救援完成，已修复 1 项。"},
		{fixed: nil, failed: []string{"重置系统代理"}, want: "网络救援未完成，1 项失败。"},
		{fixed: []string{"刷新 DNS 缓存"}, failed: []string{"重启本地代理"}, want: "网络救援完成，已修复 1 项，1 项失败。"},
	}
	for _, tc := range cases {
		if got := repairMessage(tc.fixed, tc.failed); got != tc.want {
			t.Fatalf("repairMessage(%v, %v) = %q, want %q", tc.fixed, tc.failed, got, tc.want)
		}
	}
}
