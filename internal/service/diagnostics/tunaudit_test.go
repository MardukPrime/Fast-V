package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// --- test helpers ----------------------------------------------------------

// tunTestFileInfo is the minimal os.FileInfo returned by the injected stat
// function so the wintun.dll probe can be driven with plain values.
type tunTestFileInfo struct {
	name  string
	isDir bool
}

func (f tunTestFileInfo) Name() string       { return f.name }
func (f tunTestFileInfo) Size() int64        { return 1 }
func (f tunTestFileInfo) Mode() os.FileMode  { return 0o644 }
func (f tunTestFileInfo) ModTime() time.Time { return time.Time{} }
func (f tunTestFileInfo) IsDir() bool        { return f.isDir }
func (f tunTestFileInfo) Sys() any           { return nil }

// tunAuditHooks overrides the platform/elevation/file probes of TunAudit.
type tunAuditHooks struct {
	goos     string
	elevated bool
	stat     func(string) (os.FileInfo, error)
}

// installTunAuditHooks swaps the audit indirections for one test and restores
// them afterwards. Tests are single-threaded in this package, so the package
// variables are safe to override.
func installTunAuditHooks(t *testing.T, hooks tunAuditHooks) {
	t.Helper()
	previousGOOS := tunGOOSFunc
	previousElevated := tunIsElevatedFunc
	previousStat := tunStatFunc
	if hooks.goos != "" {
		goos := hooks.goos
		tunGOOSFunc = func() string { return goos }
	}
	tunIsElevatedFunc = func() bool { return hooks.elevated }
	if hooks.stat != nil {
		tunStatFunc = hooks.stat
	}
	t.Cleanup(func() {
		tunGOOSFunc = previousGOOS
		tunIsElevatedFunc = previousElevated
		tunStatFunc = previousStat
	})
}

// tunTestCorePath is the fake core binary path used by the audit tests.
func tunTestCorePath() string {
	return filepath.Join("C:", "FastV", "cores", "xray", "xray.exe")
}

// tunInstalledCoreProvider returns an injected provider reporting an installed
// core with a version.
func tunInstalledCoreProvider() func() model.CoreInfo {
	return func() model.CoreInfo {
		return model.CoreInfo{
			Installed: true,
			Version:   "26.11.9",
			Path:      tunTestCorePath(),
			Status:    "ready",
		}
	}
}

// tunPresentStat reports every probed file as an existing regular file.
func tunPresentStat(string) (os.FileInfo, error) {
	return tunTestFileInfo{name: tunWintunDLLName}, nil
}

// newTunAuditStore creates an isolated store whose settings carry the given TUN
// switch state.
func newTunAuditStore(t *testing.T, tunMode bool) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	settings := st.Settings()
	settings.TunMode = tunMode
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	return st
}

// tunCheckByID returns one check from the audit result.
func tunCheckByID(t *testing.T, result TunAuditResult, id string) TunCheckItem {
	t.Helper()
	for _, check := range result.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("TUN audit has no check %q; checks = %+v", id, result.Checks)
	return TunCheckItem{}
}

// requireTunCheck asserts the status of one check and returns it.
func requireTunCheck(t *testing.T, result TunAuditResult, id, status string) TunCheckItem {
	t.Helper()
	check := tunCheckByID(t, result, id)
	if check.Status != status {
		t.Fatalf("check %q status = %q, want %q (detail: %s)", id, check.Status, status, check.Detail)
	}
	if strings.TrimSpace(check.Title) == "" {
		t.Fatalf("check %q has an empty title", id)
	}
	if strings.TrimSpace(check.Detail) == "" {
		t.Fatalf("check %q has an empty detail", id)
	}
	return check
}

// requireTunCheckCount asserts the audit always reports the full checklist.
func requireTunCheckCount(t *testing.T, result TunAuditResult) {
	t.Helper()
	const want = 6
	if len(result.Checks) != want {
		t.Fatalf("TUN audit returned %d checks, want %d: %+v", len(result.Checks), want, result.Checks)
	}
}

// --- TunAudit tests --------------------------------------------------------

// TestTunAuditSkipsNonWindowsPlatform covers the non-Windows branch: TUN is
// unsupported, so the platform-dependent checks are skipped and no fallback is
// offered.
func TestTunAuditSkipsNonWindowsPlatform(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "linux", elevated: false, stat: tunPresentStat})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)

	platform := requireTunCheck(t, result, tunCheckPlatform, tunStatusSkip)
	if !strings.Contains(platform.Detail, "当前平台不支持 TUN") {
		t.Fatalf("unexpected platform detail: %q", platform.Detail)
	}
	for _, id := range []string{tunCheckElevation, tunCheckCore, tunCheckWintun, tunCheckProxy} {
		requireTunCheck(t, result, id, tunStatusSkip)
	}
	// The TUN switch itself is still readable and reported.
	requireTunCheck(t, result, tunCheckSetting, tunStatusOK)

	if result.CanFallback || result.FallbackHint != "" {
		t.Fatalf("non-Windows audit must not offer a fallback: %+v", result)
	}
	if result.CheckedAt == "" {
		t.Fatal("CheckedAt is empty")
	}
	if _, err := time.Parse(time.RFC3339, result.CheckedAt); err != nil {
		t.Fatalf("CheckedAt is not RFC3339: %q (%v)", result.CheckedAt, err)
	}
}

// TestTunAuditWarnsWhenTunDisabled covers TUN mode switched off: the audit
// reports a warning, skips the proxy-state check and never offers a fallback.
func TestTunAuditWarnsWhenTunDisabled(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	st := newTunAuditStore(t, false)
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)
	requireTunCheck(t, result, tunCheckPlatform, tunStatusOK)
	setting := requireTunCheck(t, result, tunCheckSetting, tunStatusWarning)
	if !strings.Contains(setting.Detail, "TUN 模式未开启") {
		t.Fatalf("unexpected TUN setting detail: %q", setting.Detail)
	}
	if setting.Remedy == "" {
		t.Fatal("disabled TUN mode should carry a remedy")
	}
	requireTunCheck(t, result, tunCheckElevation, tunStatusOK)
	requireTunCheck(t, result, tunCheckCore, tunStatusOK)
	requireTunCheck(t, result, tunCheckWintun, tunStatusOK)
	requireTunCheck(t, result, tunCheckProxy, tunStatusSkip)

	if result.CanFallback || result.FallbackHint != "" {
		t.Fatalf("TUN disabled must not offer a fallback: %+v", result)
	}
}

// TestTunAuditRequiresElevation covers a non-elevated process: an error with a
// concrete remedy, and the fallback is offered because TUN is enabled.
func TestTunAuditRequiresElevation(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: false, stat: tunPresentStat})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)

	elevation := requireTunCheck(t, result, tunCheckElevation, tunStatusError)
	if elevation.Remedy != "以管理员身份启动 FastV" {
		t.Fatalf("unexpected elevation remedy: %q", elevation.Remedy)
	}
	requireTunCheck(t, result, tunCheckProxy, tunStatusOK)

	if !result.CanFallback {
		t.Fatal("TUN enabled with an error must offer the fallback")
	}
	requireTunFallbackHint(t, result.FallbackHint)
}

// TestTunAuditReportsMissingCore covers a missing core installation.
func TestTunAuditReportsMissingCore(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	st := newTunAuditStore(t, true)
	core := func() model.CoreInfo {
		return model.CoreInfo{Installed: false, Path: tunTestCorePath(), Status: "not_installed"}
	}
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: core,
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)

	coreCheck := requireTunCheck(t, result, tunCheckCore, tunStatusError)
	if coreCheck.Remedy != "先在设置中下载 Xray 内核" {
		t.Fatalf("unexpected core remedy: %q", coreCheck.Remedy)
	}
	requireTunCheck(t, result, tunCheckWintun, tunStatusSkip)
	if !result.CanFallback {
		t.Fatal("missing core must offer the fallback when TUN is enabled")
	}
	requireTunFallbackHint(t, result.FallbackHint)
}

// TestTunAuditReportsMissingWintun covers a core installed without wintun.dll.
func TestTunAuditReportsMissingWintun(t *testing.T) {
	var statPath string
	installTunAuditHooks(t, tunAuditHooks{
		goos:     "windows",
		elevated: true,
		stat: func(path string) (os.FileInfo, error) {
			statPath = path
			return nil, os.ErrNotExist
		},
	})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)

	wintun := requireTunCheck(t, result, tunCheckWintun, tunStatusError)
	if !strings.Contains(wintun.Remedy, "重新安装/更新 Xray-core") {
		t.Fatalf("unexpected wintun remedy: %q", wintun.Remedy)
	}
	wantPath := filepath.Join(filepath.Dir(tunTestCorePath()), tunWintunDLLName)
	if statPath != wantPath {
		t.Fatalf("statFunc probed %q, want %q", statPath, wantPath)
	}
	requireTunCheck(t, result, tunCheckCore, tunStatusOK)
	if !result.CanFallback {
		t.Fatal("missing wintun.dll must offer the fallback when TUN is enabled")
	}
	requireTunFallbackHint(t, result.FallbackHint)
}

// TestTunAuditTreatsWintunDirectoryAsMissing guards against a directory named
// wintun.dll being mistaken for the driver file.
func TestTunAuditTreatsWintunDirectoryAsMissing(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{
		goos:     "windows",
		elevated: true,
		stat: func(string) (os.FileInfo, error) {
			return tunTestFileInfo{name: tunWintunDLLName, isDir: true}, nil
		},
	})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheck(t, result, tunCheckWintun, tunStatusError)
}

// TestTunAuditSkipsUnavailableProviders covers both optional providers being
// absent: the audit must not panic and must report skipped checks.
func TestTunAuditSkipsUnavailableProviders(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)

	requireTunCheck(t, result, tunCheckPlatform, tunStatusOK)
	requireTunCheck(t, result, tunCheckSetting, tunStatusSkip)
	requireTunCheck(t, result, tunCheckElevation, tunStatusOK)
	requireTunCheck(t, result, tunCheckCore, tunStatusSkip)
	requireTunCheck(t, result, tunCheckWintun, tunStatusSkip)
	requireTunCheck(t, result, tunCheckProxy, tunStatusSkip)
	if result.CanFallback || result.FallbackHint != "" {
		t.Fatalf("unreadable settings must not offer a fallback: %+v", result)
	}
}

// TestTunAuditSkipsUnavailableSettings keeps the core checks running when only
// the settings store is missing.
func TestTunAuditSkipsUnavailableSettings(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)
	requireTunCheck(t, result, tunCheckSetting, tunStatusSkip)
	requireTunCheck(t, result, tunCheckCore, tunStatusOK)
	requireTunCheck(t, result, tunCheckWintun, tunStatusOK)
	requireTunCheck(t, result, tunCheckProxy, tunStatusSkip)
	if result.CanFallback {
		t.Fatal("unreadable settings must not offer a fallback")
	}
}

// TestTunAuditWarnsWhenProxyStopped covers TUN enabled with the proxy down: a
// warning (not a fatal error) and therefore no fallback offer.
func TestTunAuditWarnsWhenProxyStopped(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	proxy := requireTunCheck(t, result, tunCheckProxy, tunStatusWarning)
	if proxy.Remedy == "" {
		t.Fatal("stopped proxy should carry a remedy")
	}
	if result.CanFallback {
		t.Fatal("a warning-only audit must not offer the fallback")
	}
}

// TestTunAuditFallbackRequiresTunEnabled covers the fallback condition: errors
// alone are not enough, TUN mode must be enabled.
func TestTunAuditFallbackRequiresTunEnabled(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: false, stat: tunPresentStat})
	st := newTunAuditStore(t, false)
	core := func() model.CoreInfo {
		return model.CoreInfo{Installed: false, Path: tunTestCorePath()}
	}
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}}, ReportSources{
		Store:    st,
		CoreInfo: core,
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheck(t, result, tunCheckElevation, tunStatusError)
	requireTunCheck(t, result, tunCheckCore, tunStatusError)
	if result.CanFallback || result.FallbackHint != "" {
		t.Fatalf("TUN disabled must not offer a fallback even with errors: %+v", result)
	}
}

// TestTunAuditAllChecksPass covers the healthy configuration.
func TestTunAuditAllChecksPass(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	requireTunCheckCount(t, result)

	seen := make(map[string]struct{}, len(result.Checks))
	for _, check := range result.Checks {
		if _, exists := seen[check.ID]; exists {
			t.Fatalf("duplicate check id %q", check.ID)
		}
		seen[check.ID] = struct{}{}
		if check.Status != tunStatusOK {
			t.Fatalf("check %q status = %q, want ok (detail: %s)", check.ID, check.Status, check.Detail)
		}
		if check.Remedy != "" {
			t.Fatalf("passing check %q should not carry a remedy: %q", check.ID, check.Remedy)
		}
	}
	core := requireTunCheck(t, result, tunCheckCore, tunStatusOK)
	if !strings.Contains(core.Detail, "26.11.9") {
		t.Fatalf("core detail should include the installed version: %q", core.Detail)
	}
	if result.CanFallback || result.FallbackHint != "" {
		t.Fatalf("healthy TUN audit must not offer a fallback: %+v", result)
	}
}

// TestTunAuditResultJSONShape pins the camelCase wire shape consumed by the
// frontend bindings.
func TestTunAuditResultJSONShape(t *testing.T) {
	installTunAuditHooks(t, tunAuditHooks{goos: "windows", elevated: true, stat: tunPresentStat})
	st := newTunAuditStore(t, true)
	service := New(fakeProxyState{state: model.ProxyState{Status: "running"}}, ReportSources{
		Store:    st,
		CoreInfo: tunInstalledCoreProvider(),
	})

	result, err := service.TunAudit()
	if err != nil {
		t.Fatalf("TunAudit: %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal audit result: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal audit result: %v", err)
	}
	for _, key := range []string{"checks", "canFallback", "fallbackHint", "checkedAt"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("marshalled result is missing key %q: %s", key, encoded)
		}
	}
	var checks []map[string]json.RawMessage
	if err := json.Unmarshal(decoded["checks"], &checks); err != nil {
		t.Fatalf("unmarshal checks: %v", err)
	}
	if len(checks) != 6 {
		t.Fatalf("marshalled checks = %d, want 6", len(checks))
	}
	for _, key := range []string{"id", "status", "title", "detail", "remedy"} {
		if _, ok := checks[0][key]; !ok {
			t.Fatalf("marshalled check is missing key %q: %s", key, encoded)
		}
	}
}

// requireTunFallbackHint asserts the fallback hint mentions the supported
// recovery path in Chinese.
func requireTunFallbackHint(t *testing.T, hint string) {
	t.Helper()
	if hint == "" {
		t.Fatal("fallback hint is empty")
	}
	for _, fragment := range []string{"系统代理", "PAC", "重启代理"} {
		if !strings.Contains(hint, fragment) {
			t.Fatalf("fallback hint %q is missing %q", hint, fragment)
		}
	}
}
