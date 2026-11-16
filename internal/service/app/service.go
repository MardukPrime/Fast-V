package app

import (
	"context"
	"runtime"
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

// updateEventName is the event broadcast when an update check completes.
const updateEventName = "app:update"

// updateChecker is the subset of update.Checker used by AppService. It is an
// interface so tests can replace the network call with a fake.
type updateChecker interface {
	Check(ctx context.Context, currentVersion string) (model.UpdateInfo, error)
}

// AppService exposes application metadata to the frontend.
type AppService struct {
	store   *store.Store
	checker updateChecker

	// emitEvent publishes an event on the Wails bus. It is nil in production
	// (emit falls back to application.Get) and is swapped out by tests so the
	// broadcast can be verified without starting an application.
	emitEvent func(name string, data any)
}

func New(st *store.Store) *AppService {
	return &AppService{store: st, checker: update.New()}
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
