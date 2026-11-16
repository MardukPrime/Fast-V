package selfcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// expectedCheckIDs is the stable execution order contract of Run.
var expectedCheckIDs = []string{
	CheckCoreInstalled,
	CheckCoreConfig,
	CheckLocalPort,
	CheckAPIPort,
	CheckTunPrivilege,
	CheckSystemProxy,
	CheckNetworkTestURL,
	CheckGeoData,
	CheckDataWritable,
	CheckSubscriptions,
}

type fakeStore struct {
	settings      model.Settings
	node          model.Node
	hasNode       bool
	subscriptions []model.Subscription
	rules         []model.RoutingRule
	dataDir       string
}

func (f *fakeStore) Settings() model.Settings                { return f.settings }
func (f *fakeStore) ActiveNode() (model.Node, bool)          { return f.node, f.hasNode }
func (f *fakeStore) ListSubscriptions() []model.Subscription { return f.subscriptions }
func (f *fakeStore) ListRoutingRules() []model.RoutingRule   { return f.rules }
func (f *fakeStore) DataDir() string                         { return f.dataDir }

type fakeCore struct {
	info model.CoreInfo
}

func (f *fakeCore) Info() model.CoreInfo { return f.info }

type fakeProxy struct {
	state model.ProxyState
}

func (f *fakeProxy) State() model.ProxyState { return f.state }

// panicSettingsProvider simulates a broken external dependency that panics.
type panicSettingsProvider struct{}

func (panicSettingsProvider) Settings() model.Settings { panic("settings provider exploded") }

// roundTripFunc adapts a function to http.RoundTripper so the network check can
// be driven without sockets.
type roundTripFunc func(request *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// testResponse builds a minimal HTTP response for the fake transport.
func testResponse(request *http.Request, statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}
}

// fixture bundles a fully wired but faked checker plus the mutable fakes so each
// test can flip exactly one dependency.
type fixture struct {
	checker  *Checker
	store    *fakeStore
	core     *fakeCore
	proxy    *fakeProxy
	geoDir   string
	dataDir  string
	corePath string
}

func testSettings() model.Settings {
	return model.Settings{
		CoreType:                  "xray",
		ProxyMode:                 "rule",
		XrayLocalPort:             10808,
		XrayLogLevel:              "warning",
		XrayMuxConcurrency:        8,
		XraySniffing:              true,
		XrayDNSStrategy:           "UseIP",
		XrayDNSServers:            "1.1.1.1,8.8.8.8",
		XrayDomainStrategy:        "IPIfNonMatch",
		XrayTunMTU:                1500,
		TestURL:                   "https://www.gstatic.com/generate_204",
		SystemProxyMode:           "system",
		StatsEnabled:              true,
		XrayAPIEnabled:            false,
		XrayAPIPort:               10085,
		AutoSelectIntervalMinutes: 10,
		AutoSelectToleranceMS:     80,
		LoadBalanceMode:           "leastPing",
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	directory := t.TempDir()
	geoDir := filepath.Join(directory, "geo")
	if err := os.MkdirAll(geoDir, 0o700); err != nil {
		t.Fatalf("create geo dir: %v", err)
	}
	for _, name := range []string{geoIPFileName, geoSiteFileName} {
		if err := os.WriteFile(filepath.Join(geoDir, name), []byte("geo-data-"+name), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	corePath := filepath.Join(directory, "cores", "xray", "xray.exe")
	if err := os.MkdirAll(filepath.Dir(corePath), 0o700); err != nil {
		t.Fatalf("create core dir: %v", err)
	}
	// A plain text file is enough: tests never execute the real core.
	if err := os.WriteFile(corePath, []byte("fake xray binary"), 0o600); err != nil {
		t.Fatalf("write fake core: %v", err)
	}

	store := &fakeStore{
		settings: testSettings(),
		node: model.Node{
			ID:       "node-1",
			Name:     "测试节点",
			Protocol: "trojan",
			Address:  "example.com",
			Port:     443,
			Enabled:  true,
			Raw:      "trojan://password@example.com:443#%E6%B5%8B%E8%AF%95%E8%8A%82%E7%82%B9",
		},
		hasNode: true,
		dataDir: filepath.Join(directory, "data"),
	}

	checker := &Checker{
		Store:         store,
		Core:          &fakeCore{info: model.CoreInfo{Installed: true, Version: "26.11.9", Status: "ready", Path: corePath}},
		Proxy:         &fakeProxy{state: model.ProxyState{Status: "running", LocalAddr: "127.0.0.1:10808"}},
		Nodes:         store,
		Subscriptions: store,
		Rules:         store,
		Elevated:      func() bool { return true },
		DataDir:       store.dataDir,
		GeoDir:        geoDir,
		TestConfig:    func(ctx context.Context) error { return nil },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return testResponse(request, http.StatusNoContent), nil
		})},
		Now: func() time.Time {
			return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
		},
	}

	return &fixture{
		checker:  checker,
		store:    store,
		core:     checker.Core.(*fakeCore),
		proxy:    checker.Proxy.(*fakeProxy),
		geoDir:   geoDir,
		dataDir:  store.dataDir,
		corePath: corePath,
	}
}

func resultByID(t *testing.T, results []model.CheckResult, id string) model.CheckResult {
	t.Helper()
	for _, result := range results {
		if result.ID == id {
			return result
		}
	}
	t.Fatalf("result %q not found in %d results", id, len(results))
	return model.CheckResult{}
}

func assertStatus(t *testing.T, results []model.CheckResult, id string, want string) model.CheckResult {
	t.Helper()
	result := resultByID(t, results, id)
	if result.Status != want {
		t.Fatalf("check %s: status = %q, want %q (detail=%q hint=%q)", id, result.Status, want, result.Detail, result.Hint)
	}
	return result
}

func assertResultShape(t *testing.T, results []model.CheckResult) {
	t.Helper()
	if len(results) != len(expectedCheckIDs) {
		t.Fatalf("got %d results, want %d", len(results), len(expectedCheckIDs))
	}
	for index, result := range results {
		if result.ID != expectedCheckIDs[index] {
			t.Fatalf("result %d: id = %q, want %q", index, result.ID, expectedCheckIDs[index])
		}
		if result.Name == "" {
			t.Fatalf("result %q has no name", result.ID)
		}
		switch result.Status {
		case StatusOK, StatusWarn, StatusError:
		default:
			t.Fatalf("result %q has invalid status %q", result.ID, result.Status)
		}
	}
}

func TestRunAllChecksOK(t *testing.T) {
	instance := newFixture(t)

	results := instance.checker.Run(context.Background())

	assertResultShape(t, results)
	for _, result := range results {
		if result.Status != StatusOK {
			t.Fatalf("check %s: status = %q, want ok (detail=%q hint=%q)", result.ID, result.Status, result.Detail, result.Hint)
		}
		if result.Detail == "" {
			t.Fatalf("check %s: ok result has no detail", result.ID)
		}
	}
}

func TestRunWhenCoreNotInstalled(t *testing.T) {
	instance := newFixture(t)
	instance.core.info = model.CoreInfo{Installed: false, Status: "not_installed", Message: "尚未安装 Xray-core"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreInstalled, StatusError)
	if !strings.Contains(result.Detail, "未检测到") {
		t.Fatalf("detail does not mention the missing core: %q", result.Detail)
	}
	if !strings.Contains(result.Hint, "设置") {
		t.Fatalf("hint does not point to settings: %q", result.Hint)
	}
}

func TestRunWhenCoreStatusIsError(t *testing.T) {
	instance := newFixture(t)
	instance.core.info = model.CoreInfo{Installed: true, Status: "error", Message: "failed to read version"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreInstalled, StatusError)
	if !strings.Contains(result.Detail, "failed to read version") {
		t.Fatalf("detail does not include the core message: %q", result.Detail)
	}
}

func TestRunConfigWithoutActiveNode(t *testing.T) {
	instance := newFixture(t)
	instance.store.hasNode = false

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreConfig, StatusWarn)
	if !strings.Contains(result.Detail, "未选择激活节点") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the missing active node")
	}
}

func TestRunConfigWithInjectedFailure(t *testing.T) {
	instance := newFixture(t)
	instance.checker.TestConfig = func(ctx context.Context) error {
		return errors.New("invalid config: unknown outbound tag")
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreConfig, StatusError)
	if !strings.Contains(result.Detail, "invalid config: unknown outbound tag") {
		t.Fatalf("detail does not include the validation error: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the failed validation")
	}
}

func TestRunConfigWithBuildFailure(t *testing.T) {
	instance := newFixture(t)
	instance.checker.TestConfig = nil
	instance.store.node = model.Node{ID: "bad-node", Name: "坏节点", Protocol: "vmess", Raw: "this-is-not-a-share-link"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreConfig, StatusError)
	if !strings.Contains(result.Detail, "生成 Xray 配置失败") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunConfigWithMissingCoreBinary(t *testing.T) {
	instance := newFixture(t)
	instance.checker.TestConfig = nil
	instance.core.info.Path = filepath.Join(t.TempDir(), "missing", "xray.exe")

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreConfig, StatusError)
	if !strings.Contains(result.Detail, "可执行文件") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

// TestRunConfigWithExecFailure feeds a non-executable file to the real default
// validation implementation. The external command fails, but the check must
// report an error result instead of panicking or aborting the run.
func TestRunConfigWithExecFailure(t *testing.T) {
	instance := newFixture(t)
	instance.checker.TestConfig = nil

	results := instance.checker.Run(context.Background())

	assertResultShape(t, results)
	result := assertStatus(t, results, CheckCoreConfig, StatusError)
	if !strings.Contains(result.Detail, "Xray 配置校验失败") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the failed core validation")
	}
}

// TestRunConfigWithRealXray validates the real default implementation end to end
// with the Xray-core binary shipped in the workspace. It is skipped on machines
// without one, so the unit test suite stays hermetic.
func TestRunConfigWithRealXray(t *testing.T) {
	corePath, geoDir := findRealXray(t)

	instance := newFixture(t)
	instance.checker.TestConfig = nil
	instance.core.info = model.CoreInfo{Installed: true, Status: "ready", Path: corePath}
	instance.checker.GeoDir = geoDir
	instance.checker.DataDir = filepath.Join(t.TempDir(), "data")

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckCoreConfig, StatusOK)
	if !strings.Contains(result.Detail, "校验通过") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

// findRealXray locates a real Xray-core binary for the optional integration
// check. FASTV_SELFCHECK_XRAY_BIN takes precedence over the workspace copies.
func findRealXray(t *testing.T) (string, string) {
	t.Helper()

	if configured := strings.TrimSpace(os.Getenv("FASTV_SELFCHECK_XRAY_BIN")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, filepath.Dir(configured)
		}
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Skipf("cannot resolve repository root: %v", err)
	}
	candidates := []string{
		filepath.Join(root, ".fastv-data", "cores", "xray", "xray.exe"),
		filepath.Join(root, ".fastv-data", "cores", "xray", "xray"),
		filepath.Join(root, ".tools", "xray", "xray.exe"),
		filepath.Join(root, ".tools", "xray", "xray"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, filepath.Dir(candidate)
		}
	}
	t.Skip("no real Xray-core binary available")
	return "", ""
}

func TestRunLocalPortAvailable(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.StatsEnabled = false
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	instance.checker.PortAvailable = func(port int) error {
		if port != instance.store.settings.XrayLocalPort {
			t.Fatalf("probed unexpected port %d", port)
		}
		return nil
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckLocalPort, StatusOK)
	if !strings.Contains(result.Detail, "可用") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunLocalPortOccupied(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.StatsEnabled = false
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	instance.checker.PortAvailable = func(port int) error {
		return errors.New("address already in use")
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckLocalPort, StatusWarn)
	if !strings.Contains(result.Detail, "不可用") || !strings.Contains(result.Detail, "address already in use") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the occupied port")
	}
}

func TestRunLocalPortInvalid(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.StatsEnabled = false
	instance.store.settings.XrayLocalPort = 0
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	instance.checker.PortAvailable = func(port int) error {
		t.Fatalf("should not probe an invalid port %d", port)
		return nil
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckLocalPort, StatusWarn)
	if !strings.Contains(result.Detail, "无效") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunLocalPortSkippedWhileProxyRunning(t *testing.T) {
	instance := newFixture(t)
	probed := false
	instance.checker.PortAvailable = func(port int) error {
		probed = true
		return nil
	}

	assertStatus(t, instance.checker.Run(context.Background()), CheckLocalPort, StatusOK)
	if probed {
		t.Fatal("port must not be probed while the proxy is running")
	}
}

func TestRunLocalPortSkippedWhileProxyStarting(t *testing.T) {
	instance := newFixture(t)
	instance.proxy.state = model.ProxyState{Status: "starting"}
	probed := false
	instance.checker.PortAvailable = func(port int) error {
		probed = true
		return nil
	}

	assertStatus(t, instance.checker.Run(context.Background()), CheckLocalPort, StatusOK)
	if probed {
		t.Fatal("port must not be probed while the proxy is starting")
	}
}

func TestRunAPIPortDisabled(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.StatsEnabled = false
	instance.store.settings.XrayAPIEnabled = false
	instance.checker.PortAvailable = func(port int) error {
		t.Fatalf("should not probe the disabled API port %d", port)
		return nil
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckAPIPort, StatusOK)
	if !strings.Contains(result.Detail, "未启用") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunAPIPortAvailable(t *testing.T) {
	instance := newFixture(t)
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	probedAPI := false
	instance.checker.PortAvailable = func(port int) error {
		if port == instance.store.settings.XrayLocalPort {
			return nil
		}
		if port != instance.store.settings.XrayAPIPort {
			t.Fatalf("probed unexpected port %d", port)
		}
		probedAPI = true
		return nil
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckAPIPort, StatusOK)
	if !probedAPI {
		t.Fatal("API port was not probed")
	}
	if !strings.Contains(result.Detail, "可用") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunAPIPortOccupied(t *testing.T) {
	instance := newFixture(t)
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	instance.checker.PortAvailable = func(port int) error {
		if port == instance.store.settings.XrayLocalPort {
			return nil
		}
		return errors.New("permission denied")
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckAPIPort, StatusWarn)
	if !strings.Contains(result.Detail, "permission denied") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunAPIPortSameAsLocalPort(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.XrayAPIPort = instance.store.settings.XrayLocalPort
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	probes := make([]int, 0, 2)
	instance.checker.PortAvailable = func(port int) error {
		probes = append(probes, port)
		return nil
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckAPIPort, StatusWarn)
	if !strings.Contains(result.Detail, "相同") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if len(probes) != 1 || probes[0] != instance.store.settings.XrayLocalPort {
		t.Fatalf("expected only the local port to be probed, got %v", probes)
	}
}

func TestRunAPIPortInvalid(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.XrayAPIPort = 0
	instance.proxy.state = model.ProxyState{Status: "stopped"}
	probes := make([]int, 0, 2)
	instance.checker.PortAvailable = func(port int) error {
		probes = append(probes, port)
		return nil
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckAPIPort, StatusWarn)
	if !strings.Contains(result.Detail, "无效") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if len(probes) != 1 || probes[0] != instance.store.settings.XrayLocalPort {
		t.Fatalf("expected only the local port to be probed, got %v", probes)
	}
}

func TestRunTunDisabledIsOK(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.TunMode = false

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckTunPrivilege, StatusOK)
	if !strings.Contains(result.Detail, "未启用") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunTunWarnsWithoutElevation(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.TunMode = true
	instance.checker.Elevated = func() bool { return false }

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckTunPrivilege, StatusWarn)
	if !strings.Contains(result.Detail, "没有管理员权限") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if !strings.Contains(result.Hint, "管理员") {
		t.Fatalf("unexpected hint: %q", result.Hint)
	}
}

func TestRunTunOKWithElevation(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.TunMode = true
	instance.checker.Elevated = func() bool { return true }

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckTunPrivilege, StatusOK)
	if !strings.Contains(result.Detail, "权限正常") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunTunWarnsWhenElevationUnknown(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.TunMode = true
	instance.checker.Elevated = nil

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckTunPrivilege, StatusWarn)
	if !strings.Contains(result.Detail, "无法确认") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSystemProxyRunning(t *testing.T) {
	instance := newFixture(t)

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSystemProxy, StatusOK)
	if !strings.Contains(result.Detail, "127.0.0.1:10808") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSystemProxyOff(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.SystemProxyMode = "off"
	instance.proxy.state = model.ProxyState{Status: "stopped"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSystemProxy, StatusOK)
	if !strings.Contains(result.Detail, "已关闭") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSystemProxyStoppedWarns(t *testing.T) {
	instance := newFixture(t)
	instance.proxy.state = model.ProxyState{Status: "stopped"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSystemProxy, StatusWarn)
	if !strings.Contains(result.Detail, "未运行") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the stopped proxy")
	}
}

func TestRunSystemProxyErrorStateWarns(t *testing.T) {
	instance := newFixture(t)
	instance.proxy.state = model.ProxyState{Status: "error", Error: "Xray-core did not become ready"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSystemProxy, StatusWarn)
	if !strings.Contains(result.Detail, "Xray-core did not become ready") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSystemProxyUnknownModeWarns(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.SystemProxyMode = "bogus"

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSystemProxy, StatusWarn)
	if !strings.Contains(result.Detail, "未知") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSystemProxySkippedInTunMode(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.SystemProxyMode = "system"
	instance.store.settings.TunMode = true
	instance.proxy.state = model.ProxyState{Status: "stopped"}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSystemProxy, StatusOK)
	if !strings.Contains(result.Detail, "TUN") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunNetworkTestURLReachable(t *testing.T) {
	instance := newFixture(t)
	methods := make([]string, 0, 2)
	instance.checker.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		methods = append(methods, request.Method)
		return testResponse(request, http.StatusNoContent), nil
	})}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckNetworkTestURL, StatusOK)
	if !strings.Contains(result.Detail, "204") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if len(methods) == 0 || methods[0] != http.MethodHead {
		t.Fatalf("expected a HEAD request first, got %v", methods)
	}
}

func TestRunNetworkTestURLFallsBackToGet(t *testing.T) {
	instance := newFixture(t)
	methods := make([]string, 0, 2)
	instance.checker.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		methods = append(methods, request.Method)
		if request.Method == http.MethodHead {
			return testResponse(request, http.StatusMethodNotAllowed), nil
		}
		return testResponse(request, http.StatusOK), nil
	})}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckNetworkTestURL, StatusOK)
	if !strings.Contains(result.Detail, "200") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if len(methods) != 2 || methods[0] != http.MethodHead || methods[1] != http.MethodGet {
		t.Fatalf("expected HEAD then GET, got %v", methods)
	}
}

func TestRunNetworkTestURLHTTPErrorWarns(t *testing.T) {
	instance := newFixture(t)
	instance.checker.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return testResponse(request, http.StatusInternalServerError), nil
	})}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckNetworkTestURL, StatusWarn)
	if !strings.Contains(result.Detail, "500") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the failing test URL")
	}
}

func TestRunNetworkTestURLRequestErrorWarns(t *testing.T) {
	instance := newFixture(t)
	instance.checker.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connect: connection refused")
	})}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckNetworkTestURL, StatusWarn)
	if !strings.Contains(result.Detail, "不可达") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunNetworkTestURLNotConfiguredWarns(t *testing.T) {
	instance := newFixture(t)
	instance.store.settings.TestURL = "   "

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckNetworkTestURL, StatusWarn)
	if !strings.Contains(result.Detail, "未配置") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunGeoDataComplete(t *testing.T) {
	instance := newFixture(t)

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckGeoData, StatusOK)
	if !strings.Contains(result.Detail, geoIPFileName) || !strings.Contains(result.Detail, geoSiteFileName) {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunGeoDataMissingWarns(t *testing.T) {
	instance := newFixture(t)
	if err := os.Remove(filepath.Join(instance.geoDir, geoIPFileName)); err != nil {
		t.Fatalf("remove geoip: %v", err)
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckGeoData, StatusWarn)
	if !strings.Contains(result.Detail, geoIPFileName) {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if !strings.Contains(result.Hint, "更新 Geo") {
		t.Fatalf("unexpected hint: %q", result.Hint)
	}
}

func TestRunDataWritable(t *testing.T) {
	instance := newFixture(t)

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckDataWritable, StatusOK)
	if !strings.Contains(result.Detail, instance.dataDir) {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if _, err := os.Stat(instance.dataDir); err != nil {
		t.Fatalf("data directory was not created: %v", err)
	}
}

func TestRunDataWritableWithoutDataDirWarns(t *testing.T) {
	instance := newFixture(t)
	instance.checker.DataDir = ""

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckDataWritable, StatusWarn)
	if !strings.Contains(result.Detail, "未配置数据目录") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunDataWritableError(t *testing.T) {
	instance := newFixture(t)
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	instance.checker.DataDir = blocked

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckDataWritable, StatusError)
	if !strings.Contains(result.Detail, "数据目录不可用") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the unwritable data directory")
	}
}

func TestRunSubscriptionsNoEnabled(t *testing.T) {
	instance := newFixture(t)
	instance.store.subscriptions = []model.Subscription{
		{ID: "sub-1", Name: "未启用订阅", Enabled: false, LastError: "ignored"},
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSubscriptions, StatusOK)
	if !strings.Contains(result.Detail, "没有启用的订阅") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSubscriptionsAllHealthy(t *testing.T) {
	instance := newFixture(t)
	instance.store.subscriptions = []model.Subscription{
		{ID: "sub-1", Name: "机场 A", Enabled: true},
		{ID: "sub-2", Name: "机场 B", Enabled: true},
		{ID: "sub-3", Name: "机场 C", Enabled: false, LastError: "disabled failures are ignored"},
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSubscriptions, StatusOK)
	if !strings.Contains(result.Detail, "2 个") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
}

func TestRunSubscriptionsWithFailuresWarns(t *testing.T) {
	instance := newFixture(t)
	instance.store.subscriptions = []model.Subscription{
		{ID: "sub-1", Name: "机场 A", Enabled: true, LastError: "timeout"},
		{ID: "sub-2", Name: "机场 B", Enabled: true},
		{ID: "sub-3", Name: "机场 C", Enabled: true, LastError: "404"},
		{ID: "sub-4", Name: "机场 D", Enabled: false, LastError: "ignored"},
	}

	result := assertStatus(t, instance.checker.Run(context.Background()), CheckSubscriptions, StatusWarn)
	if !strings.Contains(result.Detail, "2 个") || !strings.Contains(result.Detail, "机场 A") {
		t.Fatalf("unexpected detail: %q", result.Detail)
	}
	if result.Hint == "" {
		t.Fatal("expected a hint for the failing subscriptions")
	}
}

// TestRunPanickingProviderDoesNotPanic proves that a broken external dependency
// only degrades its own checks: the run still returns the full fixed result list.
func TestRunPanickingProviderDoesNotPanic(t *testing.T) {
	instance := newFixture(t)
	instance.checker.Store = panicSettingsProvider{}

	results := instance.checker.Run(context.Background())

	assertResultShape(t, results)
	failures := 0
	for _, result := range results {
		if result.Status == StatusError && strings.Contains(result.Detail, "检查执行异常") {
			failures++
		}
	}
	if failures == 0 {
		t.Fatal("expected error results from the panicking provider")
	}
}

// TestRunNilDependenciesDoesNotPanic proves the zero-value checker is safe: the
// engine still produces exactly one result per check in the fixed order.
func TestRunNilDependenciesDoesNotPanic(t *testing.T) {
	checker := &Checker{}

	results := checker.Run(nil)

	assertResultShape(t, results)
	for _, result := range results {
		if result.Detail == "" {
			t.Fatalf("check %s has no detail", result.ID)
		}
	}
}

func TestNewCheckerWiresProductionDefaults(t *testing.T) {
	instance := newFixture(t)

	checker := NewChecker(instance.store, instance.core, instance.proxy)

	if checker.Store == nil || checker.Nodes == nil || checker.Subscriptions == nil || checker.Rules == nil {
		t.Fatal("store providers were not wired")
	}
	if checker.Core == nil || checker.Proxy == nil {
		t.Fatal("core/proxy providers were not wired")
	}
	if checker.Elevated == nil {
		t.Fatal("elevation default was not wired")
	}
	if checker.Now == nil {
		t.Fatal("clock default was not wired")
	}
	if checker.DataDir != instance.dataDir {
		t.Fatalf("data dir = %q, want %q", checker.DataDir, instance.dataDir)
	}

	// The fake core path points to the geo directory layout, but the executable
	// is not a real core: keep the run hermetic with the injected dependencies.
	checker.TestConfig = instance.checker.TestConfig
	checker.HTTPClient = instance.checker.HTTPClient
	results := checker.Run(context.Background())
	assertResultShape(t, results)
}

func TestTailOutput(t *testing.T) {
	if got := tailOutput(nil, 10); got != "" {
		t.Fatalf("empty output = %q, want empty", got)
	}

	long := strings.Repeat("x", 100) + "\nlast line"
	got := tailOutput([]byte(long), 12)
	if !strings.HasPrefix(got, "...") {
		t.Fatalf("expected truncated tail, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("output must be single line: %q", got)
	}
	if !strings.HasSuffix(got, "last line") {
		t.Fatalf("tail lost the last line: %q", got)
	}
}
