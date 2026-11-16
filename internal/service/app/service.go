package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/store"
	"github.com/MardukPrime/Fast-V/internal/update"
)

// updateCheckTimeout bounds one application update check, both for the manual
// CheckUpdate call and for the automatic check performed at startup.
const updateCheckTimeout = 30 * time.Second

// updateDownloadTimeout bounds one update package download and extraction. The
// context passed to the installer is what actually cancels a stalled transfer.
const updateDownloadTimeout = 10 * time.Minute

// updateEventName is the event broadcast when an update check completes.
const updateEventName = "app:update"

// noUpdateAssetMessage is returned by DownloadUpdate when the latest release
// does not publish a usable Windows package.
const noUpdateAssetMessage = "该版本没有可用的 Windows 更新包"

// updateChecker is the subset of update.Checker used by AppService. It is an
// interface so tests can replace the network call with a fake.
type updateChecker interface {
	Check(ctx context.Context, currentVersion string) (model.UpdateInfo, error)
}

// AppService exposes application metadata to the frontend.
type AppService struct {
	store   *store.Store
	checker updateChecker

	// download fetches and extracts an update package into destDir and returns
	// the executable to install. New wires it to update.DownloadAndExtract.
	download func(ctx context.Context, info model.UpdateInfo, destDir string) (string, error)
	// launchScript starts the generated replacement script. It is a field so
	// tests can observe ApplyUpdate without spawning cmd.exe.
	launchScript func(scriptPath string) error
	// quitApp asks the application to exit. It is a field so tests can observe
	// ApplyUpdate without terminating the test process.
	quitApp func() error
	// executable resolves the running executable path (os.Executable in
	// production).
	executable func() (string, error)
	// newScriptPath returns where the replacement script is written.
	newScriptPath func() (string, error)
	// writeScript renders and writes the replacement script.
	writeScript func(currentExe, newExe, scriptPath string) error

	// emitEvent publishes an event on the Wails bus. It is nil in production
	// (emit falls back to application.Get) and is swapped out by tests so the
	// broadcast can be verified without starting an application.
	emitEvent func(name string, data any)

	// mu guards downloaded, the result of the last successful DownloadUpdate
	// that ApplyUpdate installs.
	mu         sync.Mutex
	downloaded model.UpdateInfo
}

func New(st *store.Store) *AppService {
	return &AppService{
		store:   st,
		checker: update.New(),
		download: func(ctx context.Context, info model.UpdateInfo, destDir string) (string, error) {
			return update.DownloadAndExtract(ctx, nil, info, destDir)
		},
		launchScript:  update.StartReplaceScript,
		quitApp:       defaultQuitApplication,
		executable:    os.Executable,
		newScriptPath: update.NewReplaceScriptPath,
		writeScript:   update.WriteReplaceScript,
	}
}

func (s *AppService) ServiceName() string {
	return "AppService"
}

// ServiceStartup only probes the current autostart state. Startup must never
// rewrite HKCU\...\Run: the registry value is changed exclusively by an explicit
// SetAutostart call from the user.
//
// When settings.AutoCheckUpdate is enabled a single update check runs in the
// background; its result is broadcast as "app:update" (model.UpdateInfo). The
// request is bound to the service context, so it is cancelled on shutdown and
// never publishes a result after the application started closing.
func (s *AppService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	_, _ = platform.AutostartEnabled()
	if !s.autoCheckUpdateEnabled() {
		return nil
	}
	go s.autoCheckUpdate(ctx)
	return nil
}

// autoCheckUpdateEnabled reports whether the persisted settings ask for an
// automatic update check at startup.
func (s *AppService) autoCheckUpdateEnabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	return s.store.Settings().AutoCheckUpdate
}

// autoCheckUpdate performs the startup check and broadcasts its result.
func (s *AppService) autoCheckUpdate(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, updateCheckTimeout)
	defer cancel()

	info, _ := s.runCheck(checkCtx)
	if ctx.Err() != nil {
		// The application is shutting down: the result is stale.
		return
	}
	s.emitUpdate(info)
}

// CheckUpdate checks for a newer FastV release, broadcasts the result as
// "app:update" (model.UpdateInfo) and returns it. Network and protocol failures
// are returned both as the error and in UpdateInfo.Error.
func (s *AppService) CheckUpdate() (model.UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	info, err := s.runCheck(ctx)
	s.emitUpdate(info)
	return info, err
}

func (s *AppService) runCheck(ctx context.Context) (model.UpdateInfo, error) {
	checker := s.checker
	if checker == nil {
		checker = update.New()
	}
	return checker.Check(ctx, buildinfo.Version)
}

// DownloadUpdate checks for a newer release and, when the release publishes a
// Windows package, downloads and extracts it into
// <data dir>/updates/<latest version>/. The returned UpdateInfo carries the
// extracted executable path in DownloadedPath; ApplyUpdate then installs it.
//
// A release without a usable Windows asset is reported as an error so the UI
// can tell the user instead of silently doing nothing.
func (s *AppService) DownloadUpdate() (model.UpdateInfo, error) {
	if s == nil {
		return model.UpdateInfo{}, errors.New("应用服务不可用")
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateDownloadTimeout)
	defer cancel()

	info, err := s.runCheck(ctx)
	if err != nil {
		return info, err
	}
	if strings.TrimSpace(info.AssetURL) == "" || strings.TrimSpace(info.AssetName) == "" {
		err := errors.New(noUpdateAssetMessage)
		info.Error = err.Error()
		return info, err
	}
	if s.store == nil {
		err := errors.New("应用数据目录不可用，无法保存更新包")
		info.Error = err.Error()
		return info, err
	}

	destDir := filepath.Join(s.store.DataDir(), "updates", versionDirName(info.LatestVersion))
	executablePath, err := s.downloadPackage(ctx, info, destDir)
	if err != nil {
		info.Error = err.Error()
		return info, err
	}
	info.DownloadedPath = executablePath
	s.setDownloaded(info)
	return info, nil
}

// ApplyUpdate installs the package downloaded by DownloadUpdate: it generates a
// replacement script, starts it detached and asks the application to quit. The
// script waits for this process to exit, overwrites the running executable and
// starts the new version.
//
// A missing download is reported with a Chinese error instead of guessing.
func (s *AppService) ApplyUpdate() error {
	if s == nil {
		return errors.New("应用服务不可用")
	}

	info, ok := s.pendingDownload()
	if !ok {
		return errors.New("没有已下载的更新包，请先下载更新")
	}
	newExe := strings.TrimSpace(info.DownloadedPath)
	if newExe == "" {
		return errors.New("没有已下载的更新包，请先下载更新")
	}
	stat, err := os.Stat(newExe)
	if err != nil {
		return fmt.Errorf("更新包文件不存在: %s", newExe)
	}
	if stat.IsDir() {
		return fmt.Errorf("更新包路径不是文件: %s", newExe)
	}

	currentExe, err := s.currentExecutable()
	if err != nil {
		return err
	}
	absoluteCurrent, err := filepath.Abs(currentExe)
	if err != nil {
		return fmt.Errorf("解析当前程序路径失败: %w", err)
	}
	absoluteNew, err := filepath.Abs(newExe)
	if err != nil {
		return fmt.Errorf("解析更新包路径失败: %w", err)
	}
	if strings.EqualFold(filepath.Clean(absoluteCurrent), filepath.Clean(absoluteNew)) {
		return errors.New("更新包路径与当前程序相同，无法替换")
	}

	scriptPath, err := s.replacementScriptPath()
	if err != nil {
		return err
	}
	if err := s.writeReplacementScript(absoluteCurrent, absoluteNew, scriptPath); err != nil {
		return err
	}
	if err := s.launchReplacementScript(scriptPath); err != nil {
		return err
	}
	return s.quitApplication()
}

// downloadPackage runs the injected downloader, falling back to the real
// implementation for services built without New.
func (s *AppService) downloadPackage(ctx context.Context, info model.UpdateInfo, destDir string) (string, error) {
	download := s.download
	if download == nil {
		download = func(ctx context.Context, info model.UpdateInfo, destDir string) (string, error) {
			return update.DownloadAndExtract(ctx, nil, info, destDir)
		}
	}
	return download(ctx, info, destDir)
}

// setDownloaded records the last successfully downloaded update.
func (s *AppService) setDownloaded(info model.UpdateInfo) {
	s.mu.Lock()
	s.downloaded = info
	s.mu.Unlock()
}

// pendingDownload returns the last downloaded update when it has an executable
// path.
func (s *AppService) pendingDownload() (model.UpdateInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.downloaded
	return info, strings.TrimSpace(info.DownloadedPath) != ""
}

// currentExecutable resolves the path of the running executable.
func (s *AppService) currentExecutable() (string, error) {
	executable := s.executable
	if executable == nil {
		executable = os.Executable
	}
	path, err := executable()
	if err != nil {
		return "", fmt.Errorf("获取当前程序路径失败: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("获取当前程序路径失败: 路径为空")
	}
	return path, nil
}

// replacementScriptPath allocates the location of the replacement script.
func (s *AppService) replacementScriptPath() (string, error) {
	newScriptPath := s.newScriptPath
	if newScriptPath == nil {
		newScriptPath = update.NewReplaceScriptPath
	}
	path, err := newScriptPath()
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("生成更新脚本路径失败: 路径为空")
	}
	return path, nil
}

// writeReplacementScript renders the script that replaces the executable.
func (s *AppService) writeReplacementScript(currentExe, newExe, scriptPath string) error {
	writeScript := s.writeScript
	if writeScript == nil {
		writeScript = update.WriteReplaceScript
	}
	return writeScript(currentExe, newExe, scriptPath)
}

// launchReplacementScript starts the replacement script detached.
func (s *AppService) launchReplacementScript(scriptPath string) error {
	launchScript := s.launchScript
	if launchScript == nil {
		launchScript = update.StartReplaceScript
	}
	return launchScript(scriptPath)
}

// quitApplication asks Wails to terminate the application so the replacement
// script can overwrite the executable.
func (s *AppService) quitApplication() error {
	quitApp := s.quitApp
	if quitApp == nil {
		quitApp = defaultQuitApplication
	}
	return quitApp()
}

// defaultQuitApplication is the production quit hook. It reports an error when
// no Wails application is running, which keeps a failed update actionable.
func defaultQuitApplication() error {
	app := application.Get()
	if app == nil {
		log.Printf("apply update: wails application is not available")
		return errors.New("退出应用失败: Wails 应用未就绪")
	}
	app.Quit()
	return nil
}

// versionDirName turns a release tag into a safe single directory name. Release
// tags come from a remote document, so path separators and dot segments must
// never reach the file system.
func versionDirName(version string) string {
	version = strings.TrimSpace(version)
	var builder strings.Builder
	for _, r := range version {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
		if builder.Len() >= 64 {
			break
		}
	}
	name := strings.Trim(builder.String(), "._-")
	if name == "" {
		return "latest"
	}
	return name
}

func (s *AppService) emitUpdate(info model.UpdateInfo) {
	s.emit(updateEventName, info)
}

func (s *AppService) emit(name string, data any) {
	if s.emitEvent != nil {
		s.emitEvent(name, data)
		return
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

func (s *AppService) Info() model.AppInfo {
	return model.AppInfo{
		Name:         buildinfo.ProductName,
		Version:      buildinfo.Version,
		Commit:       buildinfo.Commit,
		BuildDate:    buildinfo.BuildDate,
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		WailsVersion: buildinfo.WailsVersion,
	}
}

func (s *AppService) IsElevated() bool {
	return platform.IsElevated()
}

// AutostartEnabled reports whether FastV is registered to start with Windows.
func (s *AppService) AutostartEnabled() (bool, error) {
	return platform.AutostartEnabled()
}

// SetAutostart enables or disables launching FastV when the user logs in and
// returns the state that is in effect afterwards.
func (s *AppService) SetAutostart(enabled bool) (bool, error) {
	if err := platform.SetAutostart(enabled); err != nil {
		return false, err
	}
	return platform.AutostartEnabled()
}

func (s *AppService) RestartAsAdmin() error {
	if platform.IsElevated() {
		return nil
	}
	if err := platform.LaunchElevatedRestart(); err != nil {
		return err
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		if app := application.Get(); app != nil {
			app.Quit()
		}
	}()
	return nil
}
