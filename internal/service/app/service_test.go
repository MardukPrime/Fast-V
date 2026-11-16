package app

import (
	"context"
	"errors"
	"path/filepath"
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
