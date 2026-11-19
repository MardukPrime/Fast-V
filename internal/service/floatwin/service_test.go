package floatwin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := store.NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat data file: %v", err)
	}
	return New(st), st
}

// TestDisablePersistsAndCloses verifies the ball's own close action turns the
// persisted setting off so it does not come back on the next start.
func TestDisablePersistsAndCloses(t *testing.T) {
	svc, st := newTestService(t)
	settings := st.Settings()
	settings.FloatWindowEnabled = true
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	got, err := svc.Disable()
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if got.FloatWindowEnabled {
		t.Fatal("Disable returned settings with the floating ball still enabled")
	}
	if st.Settings().FloatWindowEnabled {
		t.Fatal("floating ball setting was not persisted as disabled")
	}

	// Disabling again is idempotent.
	if _, err := svc.Disable(); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}

func TestDisableWithoutStoreFails(t *testing.T) {
	svc := New(nil)
	if _, err := svc.Disable(); err == nil {
		t.Fatal("Disable with a nil store should return an error")
	}
}

// TestHarmlessCallsWithoutWindow ensures lifecycle calls do not panic when the
// window could not be created (unsupported session, headless tests).
func TestHarmlessCallsWithoutWindow(t *testing.T) {
	svc := New(nil)
	svc.Sync(false)
	svc.Close()
	svc.DragStart(10, 10)
	svc.DragMove(20, 20)
	svc.DragEnd()
	opened := false
	svc.OpenMainHandler = func() { opened = true }
	svc.OpenMain()
	if !opened {
		t.Fatal("OpenMain did not invoke the wired callback")
	}
	if err := svc.Show(); err == nil {
		t.Fatal("Show without a running Wails application should fail")
	}
}
