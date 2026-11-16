package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return st
}

// fakeChecker records how the service calls the update checker.
type fakeChecker struct {
	info model.UpdateInfo
	err  error

	// waitForContext makes Check block until the request context is cancelled,
	// simulating an in-flight network request during application shutdown.
	waitForContext bool
	finished       chan struct{}

	mu             sync.Mutex
	calls          int
	currentVersion string
	called         chan struct{}
}

func newFakeChecker(info model.UpdateInfo, err error) *fakeChecker {
	return &fakeChecker{info: info, err: err, called: make(chan struct{}, 4)}
}

func (f *fakeChecker) Check(ctx context.Context, currentVersion string) (model.UpdateInfo, error) {
	f.mu.Lock()
	f.calls++
	f.currentVersion = currentVersion
	f.mu.Unlock()
	select {
	case f.called <- struct{}{}:
	default:
	}
	if f.waitForContext {
		<-ctx.Done()
	}
	if f.finished != nil {
		close(f.finished)
	}
	return f.info, f.err
}

func (f *fakeChecker) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeChecker) version() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.currentVersion
}

// captureUpdates installs an emitEvent hook that records "app:update" payloads.
func captureUpdates(t *testing.T, svc *AppService) func() []model.UpdateInfo {
	t.Helper()
	var mu sync.Mutex
	var updates []model.UpdateInfo
	svc.emitEvent = func(name string, data any) {
		if name != updateEventName {
			t.Errorf("emitted event %q, want %q", name, updateEventName)
			return
		}
		info, ok := data.(model.UpdateInfo)
		if !ok {
			t.Errorf("app:update payload type = %T, want model.UpdateInfo", data)
			return
		}
		mu.Lock()
		updates = append(updates, info)
		mu.Unlock()
	}
	return func() []model.UpdateInfo {
		mu.Lock()
		defer mu.Unlock()
		return append([]model.UpdateInfo(nil), updates...)
	}
}

func TestCheckUpdateUsesBuildVersionAndBroadcasts(t *testing.T) {
	svc := New(newTestStore(t))
	checker := newFakeChecker(model.UpdateInfo{
		CurrentVersion: buildinfo.Version,
		LatestVersion:  "9.9.9",
		Available:      true,
		Notes:          "release notes",
		URL:            "https://example.com/release",
	}, nil)
	svc.checker = checker
	updates := captureUpdates(t, svc)

	info, err := svc.CheckUpdate()
	if err != nil {
		t.Fatalf("CheckUpdate() error = %v, want nil", err)
	}
	if got := checker.version(); got != buildinfo.Version {
		t.Errorf("checker received version %q, want buildinfo.Version %q", got, buildinfo.Version)
	}
	if info.LatestVersion != "9.9.9" || !info.Available {
		t.Errorf("CheckUpdate() info = %+v, want the fake result", info)
	}
	broadcasts := updates()
	if len(broadcasts) != 1 {
		t.Fatalf("broadcast count = %d, want 1", len(broadcasts))
	}
	if broadcasts[0].LatestVersion != "9.9.9" || !broadcasts[0].Available {
		t.Errorf("app:update payload = %+v, want the check result", broadcasts[0])
	}
}

func TestCheckUpdatePropagatesFailure(t *testing.T) {
	svc := New(newTestStore(t))
	checkFailed := errors.New("check update: GitHub returned HTTP 503")
	svc.checker = newFakeChecker(model.UpdateInfo{
		CurrentVersion: buildinfo.Version,
		Error:          checkFailed.Error(),
	}, checkFailed)
	updates := captureUpdates(t, svc)

	info, err := svc.CheckUpdate()
	if !errors.Is(err, checkFailed) {
		t.Fatalf("CheckUpdate() error = %v, want %v", err, checkFailed)
	}
	if info.Error == "" {
		t.Error("UpdateInfo.Error is empty, want the failure message")
	}
	// A failed check is still broadcast so the UI can report it.
	if len(updates()) != 1 {
		t.Fatalf("broadcast count = %d, want 1", len(updates()))
	}
}

func TestServiceStartupAutoChecksWhenEnabled(t *testing.T) {
	st := newTestStore(t)
	if settings := st.Settings(); !settings.AutoCheckUpdate {
		t.Fatal("store defaults must enable AutoCheckUpdate for this test")
	}
	svc := New(st)
	checker := newFakeChecker(model.UpdateInfo{
		CurrentVersion: buildinfo.Version,
		LatestVersion:  "1.0.0",
		Available:      true,
	}, nil)
	svc.checker = checker
	updates := captureUpdates(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v, want nil", err)
	}

	select {
	case <-checker.called:
	case <-time.After(3 * time.Second):
		t.Fatal("startup auto check did not run")
	}

	deadline := time.After(3 * time.Second)
	for {
		if len(updates()) == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("broadcast count = %d, want 1", len(updates()))
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestServiceStartupSkipsAutoCheckWhenDisabled(t *testing.T) {
	st := newTestStore(t)
	settings := st.Settings()
	settings.AutoCheckUpdate = false
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	svc := New(st)
	checker := newFakeChecker(model.UpdateInfo{}, nil)
	svc.checker = checker

	if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v, want nil", err)
	}
	select {
	case <-checker.called:
		t.Fatal("auto check ran although AutoCheckUpdate is disabled")
	case <-time.After(150 * time.Millisecond):
	}
	if got := checker.callCount(); got != 0 {
		t.Fatalf("checker calls = %d, want 0", got)
	}
}

func TestServiceStartupWithoutStoreSkipsAutoCheck(t *testing.T) {
	svc := New(nil)
	if svc.autoCheckUpdateEnabled() {
		t.Fatal("autoCheckUpdateEnabled() = true without a store")
	}
	svc.checker = newFakeChecker(model.UpdateInfo{}, nil)
	if err := svc.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v, want nil", err)
	}
}

func TestServiceStartupCancelledContextDoesNotBroadcast(t *testing.T) {
	st := newTestStore(t)
	svc := New(st)
	checker := newFakeChecker(model.UpdateInfo{LatestVersion: "1.0.0", Available: true}, nil)
	checker.waitForContext = true
	checker.finished = make(chan struct{})
	svc.checker = checker
	updates := captureUpdates(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	if err := svc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v, want nil", err)
	}
	// Cancel while the request is in flight: the stale-result guard must stop
	// the broadcast even though the checker returned a usable result.
	cancel()

	select {
	case <-checker.called:
	case <-time.After(3 * time.Second):
		t.Fatal("startup auto check did not run")
	}
	select {
	case <-checker.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("checker was not cancelled")
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(updates()); got != 0 {
		t.Fatalf("broadcast count = %d, want 0 after shutdown", got)
	}
}

// fakeDownload records how DownloadUpdate calls the installer.
type fakeDownload struct {
	mu      sync.Mutex
	calls   int
	destDir string
	info    model.UpdateInfo
	exePath string
	err     error
}

func (f *fakeDownload) download(_ context.Context, info model.UpdateInfo, destDir string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.info = info
	f.destDir = destDir
	if f.err != nil {
		return "", f.err
	}
	return f.exePath, nil
}

func (f *fakeDownload) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeDownload) lastDestDir() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.destDir
}

// downloadedUpdateInfo is a release with a usable Windows asset.
func downloadedUpdateInfo() model.UpdateInfo {
	return model.UpdateInfo{
		CurrentVersion: buildinfo.Version,
		LatestVersion:  "0.3.0",
		Available:      true,
		URL:            "https://example.com/release",
		AssetName:      "FastV-windows-amd64.zip",
		AssetURL:       "https://example.com/FastV-windows-amd64.zip",
		AssetSize:      1024,
	}
}

func TestNewInitializesUpdateHooks(t *testing.T) {
	svc := New(nil)
	if svc.download == nil || svc.launchScript == nil || svc.quitApp == nil ||
		svc.executable == nil || svc.newScriptPath == nil || svc.writeScript == nil {
		t.Fatal("New() did not install the production update hooks")
	}
}

func TestDownloadUpdateDownloadsAssetIntoDataDir(t *testing.T) {
	svc := New(newTestStore(t))
	svc.checker = newFakeChecker(downloadedUpdateInfo(), nil)
	downloader := &fakeDownload{exePath: filepath.Join(t.TempDir(), "extracted", "FastV.exe")}
	svc.download = downloader.download

	info, err := svc.DownloadUpdate()
	if err != nil {
		t.Fatalf("DownloadUpdate() error = %v, want nil", err)
	}
	if info.DownloadedPath != downloader.exePath {
		t.Errorf("DownloadedPath = %q, want %q", info.DownloadedPath, downloader.exePath)
	}
	if info.AssetName != "FastV-windows-amd64.zip" || info.AssetURL == "" {
		t.Errorf("DownloadUpdate() info = %+v, want the release asset fields preserved", info)
	}
	if info.Error != "" {
		t.Errorf("UpdateInfo.Error = %q, want empty", info.Error)
	}
	if got := downloader.callCount(); got != 1 {
		t.Fatalf("installer calls = %d, want 1", got)
	}
	wantDir := filepath.Join(svc.store.DataDir(), "updates", "0.3.0")
	if got := downloader.lastDestDir(); got != wantDir {
		t.Errorf("installer destDir = %q, want %q", got, wantDir)
	}
	pending, ok := svc.pendingDownload()
	if !ok || pending.DownloadedPath != downloader.exePath {
		t.Errorf("pendingDownload() = (%+v, %v), want the downloaded update", pending, ok)
	}
}

func TestDownloadUpdateSanitizesVersionDirectory(t *testing.T) {
	svc := New(newTestStore(t))
	info := downloadedUpdateInfo()
	info.LatestVersion = "../../evil/0.3.0"
	svc.checker = newFakeChecker(info, nil)
	downloader := &fakeDownload{exePath: filepath.Join(t.TempDir(), "FastV.exe")}
	svc.download = downloader.download

	if _, err := svc.DownloadUpdate(); err != nil {
		t.Fatalf("DownloadUpdate() error = %v, want nil", err)
	}
	destDir := downloader.lastDestDir()
	relative, err := filepath.Rel(filepath.Join(svc.store.DataDir(), "updates"), destDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Errorf("installer destDir %q escapes the updates directory (rel = %q, err = %v)", destDir, relative, err)
	}
}

func TestDownloadUpdateWithoutAssetReturnsChineseError(t *testing.T) {
	svc := New(newTestStore(t))
	info := downloadedUpdateInfo()
	info.AssetName = ""
	info.AssetURL = ""
	svc.checker = newFakeChecker(info, nil)
	downloader := &fakeDownload{exePath: "unused"}
	svc.download = downloader.download

	result, err := svc.DownloadUpdate()
	if err == nil {
		t.Fatal("DownloadUpdate() error = nil, want the no-asset error")
	}
	if err.Error() != noUpdateAssetMessage {
		t.Errorf("DownloadUpdate() error = %q, want %q", err, noUpdateAssetMessage)
	}
	if result.Error != noUpdateAssetMessage {
		t.Errorf("UpdateInfo.Error = %q, want %q", result.Error, noUpdateAssetMessage)
	}
	if got := downloader.callCount(); got != 0 {
		t.Errorf("installer calls = %d, want 0 without an asset", got)
	}
}

func TestDownloadUpdateCheckFailureSkipsDownload(t *testing.T) {
	svc := New(newTestStore(t))
	checkFailed := errors.New("check update: GitHub returned HTTP 503")
	svc.checker = newFakeChecker(model.UpdateInfo{Error: checkFailed.Error()}, checkFailed)
	downloader := &fakeDownload{exePath: "unused"}
	svc.download = downloader.download

	info, err := svc.DownloadUpdate()
	if !errors.Is(err, checkFailed) {
		t.Fatalf("DownloadUpdate() error = %v, want %v", err, checkFailed)
	}
	if info.Error == "" {
		t.Error("UpdateInfo.Error is empty, want the check failure")
	}
	if got := downloader.callCount(); got != 0 {
		t.Errorf("installer calls = %d, want 0 after a failed check", got)
	}
}

func TestDownloadUpdatePropagatesInstallFailure(t *testing.T) {
	svc := New(newTestStore(t))
	svc.checker = newFakeChecker(downloadedUpdateInfo(), nil)
	installFailed := errors.New("下载更新包失败: connection reset")
	svc.download = (&fakeDownload{err: installFailed}).download

	info, err := svc.DownloadUpdate()
	if !errors.Is(err, installFailed) {
		t.Fatalf("DownloadUpdate() error = %v, want %v", err, installFailed)
	}
	if info.Error != installFailed.Error() {
		t.Errorf("UpdateInfo.Error = %q, want %q", info.Error, installFailed.Error())
	}
	if _, ok := svc.pendingDownload(); ok {
		t.Error("pendingDownload() reports a download after a failed install")
	}
}

// preparedUpdateService returns a service with a downloaded update whose
// replacement hooks only record calls.
type updateApplyRecorder struct {
	events     []string
	scriptPath string
	quitCalls  int
	launchErr  error
}

func newApplyTestService(t *testing.T) (*AppService, *updateApplyRecorder, string) {
	t.Helper()
	svc := New(newTestStore(t))
	newExe := filepath.Join(t.TempDir(), "extracted", "FastV.exe")
	if err := os.MkdirAll(filepath.Dir(newExe), 0o755); err != nil {
		t.Fatalf("create downloaded update dir: %v", err)
	}
	if err := os.WriteFile(newExe, []byte("MZ fake"), 0o600); err != nil {
		t.Fatalf("write downloaded update: %v", err)
	}
	svc.setDownloaded(model.UpdateInfo{DownloadedPath: newExe})

	recorder := &updateApplyRecorder{scriptPath: filepath.Join(t.TempDir(), "fastv-update.cmd")}
	svc.newScriptPath = func() (string, error) { return recorder.scriptPath, nil }
	svc.executable = func() (string, error) { return `C:\Apps\FastV\FastV.exe`, nil }
	svc.launchScript = func(scriptPath string) error {
		if scriptPath != recorder.scriptPath {
			t.Errorf("launchScript(%q), want %q", scriptPath, recorder.scriptPath)
		}
		recorder.events = append(recorder.events, "launch")
		return recorder.launchErr
	}
	svc.quitApp = func() error {
		recorder.events = append(recorder.events, "quit")
		recorder.quitCalls++
		return nil
	}
	return svc, recorder, newExe
}

func TestApplyUpdateWritesScriptLaunchesAndQuits(t *testing.T) {
	svc, recorder, newExe := newApplyTestService(t)

	if err := svc.ApplyUpdate(); err != nil {
		t.Fatalf("ApplyUpdate() error = %v, want nil", err)
	}
	wantEvents := []string{"launch", "quit"}
	if len(recorder.events) != len(wantEvents) {
		t.Fatalf("hook events = %v, want %v", recorder.events, wantEvents)
	}
	for index, want := range wantEvents {
		if recorder.events[index] != want {
			t.Fatalf("hook events = %v, want launch before quit", recorder.events)
		}
	}

	content, err := os.ReadFile(recorder.scriptPath)
	if err != nil {
		t.Fatalf("read replacement script: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `"C:\Apps\FastV\FastV.exe"`) {
		t.Errorf("script does not quote the current executable:\n%s", text)
	}
	if !strings.Contains(text, `"`+filepath.Clean(newExe)+`"`) {
		t.Errorf("script does not quote the downloaded executable %q:\n%s", newExe, text)
	}
	if !strings.Contains(text, strconv.Itoa(os.Getpid())) {
		t.Errorf("script does not contain the current PID %d:\n%s", os.Getpid(), text)
	}
}

func TestApplyUpdateRequiresDownload(t *testing.T) {
	svc := New(newTestStore(t))
	svc.quitApp = func() error {
		t.Fatal("quitApp was called without a downloaded update")
		return nil
	}

	err := svc.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() error = nil, want a missing-download error")
	}
	if !strings.Contains(err.Error(), "请先下载") {
		t.Errorf("ApplyUpdate() error = %q, want it to ask for a download first", err)
	}
}

func TestApplyUpdateRejectsMissingFile(t *testing.T) {
	svc := New(newTestStore(t))
	svc.setDownloaded(model.UpdateInfo{DownloadedPath: filepath.Join(t.TempDir(), "missing", "FastV.exe")})
	svc.quitApp = func() error {
		t.Fatal("quitApp was called for a missing update file")
		return nil
	}

	err := svc.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() error = nil, want a missing-file error")
	}
	if !strings.Contains(err.Error(), "更新包文件不存在") {
		t.Errorf("ApplyUpdate() error = %q, want a missing-file message", err)
	}
}

func TestApplyUpdateRejectsSamePath(t *testing.T) {
	svc, recorder, newExe := newApplyTestService(t)
	svc.executable = func() (string, error) { return newExe, nil }

	err := svc.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() error = nil, want a same-path error")
	}
	if !strings.Contains(err.Error(), "相同") {
		t.Errorf("ApplyUpdate() error = %q, want a same-path message", err)
	}
	if len(recorder.events) != 0 {
		t.Errorf("hooks were called for a rejected update: %v", recorder.events)
	}
}

func TestApplyUpdateLaunchFailureDoesNotQuit(t *testing.T) {
	svc, recorder, _ := newApplyTestService(t)
	recorder.launchErr = errors.New("启动更新脚本失败: boom")

	err := svc.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() error = nil, want the launcher error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("ApplyUpdate() error = %q, want the launcher error", err)
	}
	if recorder.quitCalls != 0 {
		t.Errorf("quitApp calls = %d, want 0 after a failed launch", recorder.quitCalls)
	}
}

func TestApplyUpdateQuitUnavailableReturnsError(t *testing.T) {
	svc, recorder, _ := newApplyTestService(t)
	// No Wails application runs in unit tests, so the production quit hook must
	// report an error instead of pretending the update was applied.
	svc.quitApp = nil

	err := svc.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() error = nil, want a quit error")
	}
	if !strings.Contains(err.Error(), "Wails") {
		t.Errorf("ApplyUpdate() error = %q, want it to mention the unavailable Wails app", err)
	}
	if len(recorder.events) != 1 || recorder.events[0] != "launch" {
		t.Errorf("hook events = %v, want the script to be launched before quitting", recorder.events)
	}
}

func TestVersionDirName(t *testing.T) {
	cases := map[string]string{
		"0.3.0":          "0.3.0",
		"v0.3.0":         "v0.3.0",
		"0.3.0/rc1":      "0.3.0_rc1",
		"../../evil":     "evil",
		"..":             "latest",
		"":               "latest",
		"中文":             "latest",
		"0.3.0+build":    "0.3.0_build",
		"1.0.0 (stable)": "1.0.0__stable",
	}
	for version, want := range cases {
		if got := versionDirName(version); got != want {
			t.Errorf("versionDirName(%q) = %q, want %q", version, got, want)
		}
	}
	long := strings.Repeat("9", 200)
	if got := versionDirName(long); len(got) > 64 {
		t.Errorf("versionDirName(long) length = %d, want at most 64", len(got))
	}
}
