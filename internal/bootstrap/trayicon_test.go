package bootstrap

import (
	"bytes"
	"image/png"
	"testing"
)

// TestTrayIconForStatus verifies that every proxy status maps to a valid,
// non-empty PNG and that the colors differ between the interesting states.
func TestTrayIconForStatus(t *testing.T) {
	statuses := []string{"running", "starting", "stopping", "error", "stopped", ""}
	icons := map[string][]byte{}
	for _, status := range statuses {
		data := trayIconForStatus(status)
		if len(data) == 0 {
			t.Fatalf("trayIconForStatus(%q) returned no bytes", status)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("trayIconForStatus(%q) is not a valid PNG: %v", status, err)
		}
		if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Fatalf("trayIconForStatus(%q) size = %dx%d, want 32x32", status, img.Bounds().Dx(), img.Bounds().Dy())
		}
		icons[status] = data
	}

	// The status colors must differ: running (green), error (red), stopped (blue).
	for _, pair := range [][2]string{{"running", "error"}, {"running", "stopped"}, {"error", "stopped"}} {
		if bytes.Equal(icons[pair[0]], icons[pair[1]]) {
			t.Fatalf("tray icons for %q and %q are identical, want different colors", pair[0], pair[1])
		}
	}
	// starting/stopping share the amber color.
	if !bytes.Equal(icons["starting"], icons["stopping"]) {
		t.Fatal("starting and stopping should share the amber icon")
	}
}

// TestTrayUpdateIconSkipsNilControllers makes sure the helper tolerates a
// partially initialised tray (the window may close before the tray exists).
func TestTrayUpdateIconSkipsNilControllers(t *testing.T) {
	var controller *trayController
	controller.updateIcon("running") // must not panic
}
