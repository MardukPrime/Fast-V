package proxy

import (
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func TestAutoDegradeAfterConsecutiveFailures(t *testing.T) {
	settings := model.Settings{AutoAntiDpiFallback: true}
	manager := &Manager{}
	reasons := make([]string, 0, 1)
	manager.SetOnDegrade(func(reason string) { reasons = append(reasons, reason) })

	manager.noteProbeResult(settings, false)
	if manager.Degraded() {
		t.Fatal("a single failure must not degrade the session")
	}

	// A success in between resets the counter.
	manager.noteProbeResult(settings, true)
	manager.noteProbeResult(settings, false)
	if manager.Degraded() {
		t.Fatal("the failure counter was not reset by the successful probe")
	}

	manager.noteProbeResult(settings, false)
	if !manager.Degraded() {
		t.Fatal("two consecutive failures must engage the fallback")
	}
	if len(reasons) != 1 {
		t.Fatalf("expected exactly one degrade callback, got %d", len(reasons))
	}

	// The profile is applied to the config, not to the stored settings.
	effective := manager.effectiveSettings(model.Settings{})
	if !effective.XrayFragmentEnabled {
		t.Fatal("fallback must enable TLS fragmentation")
	}
	if effective.XrayFragmentPackets != "tlshello" || effective.XrayFragmentLength != "100-200" || effective.XrayFragmentDelay != "10-20" {
		t.Fatalf("unexpected fallback ranges: %+v", effective)
	}
	if effective.XrayTLSFingerprint != "randomized" {
		t.Fatalf("fallback must randomize the fingerprint, got %q", effective.XrayTLSFingerprint)
	}

	// Further failures do not re-trigger the callback.
	manager.noteProbeResult(settings, false)
	if len(reasons) != 1 {
		t.Fatalf("degrade callback fired again: %d", len(reasons))
	}
}

func TestAutoDegradeHonoursDisabledSetting(t *testing.T) {
	manager := &Manager{}
	manager.SetOnDegrade(func(string) { t.Fatal("callback must not fire when the setting is off") })
	disabled := model.Settings{AutoAntiDpiFallback: false}
	for i := 0; i < 5; i++ {
		manager.noteProbeResult(disabled, false)
	}
	if manager.Degraded() {
		t.Fatal("disabled fallback must never degrade")
	}
	if manager.effectiveSettings(disabled).XrayFragmentEnabled {
		t.Fatal("effective settings must be untouched while not degraded")
	}
}

// TestEffectiveSettingsKeepsUserRanges makes sure the fallback does not overwrite
// ranges the user already tuned.
func TestEffectiveSettingsKeepsUserRanges(t *testing.T) {
	manager := &Manager{}
	manager.degraded.Store(true)
	stored := model.Settings{
		XrayFragmentPackets: "1-3",
		XrayFragmentLength:  "1-10",
		XrayFragmentDelay:   "0",
		XrayTLSFingerprint:  "firefox",
	}
	effective := manager.effectiveSettings(stored)
	if effective.XrayFragmentPackets != "1-3" || effective.XrayFragmentLength != "1-10" || effective.XrayFragmentDelay != "0" {
		t.Fatalf("user ranges were overwritten: %+v", effective)
	}
	if effective.XrayTLSFingerprint != "randomized" {
		t.Fatalf("the fallback must still randomize the fingerprint, got %q", effective.XrayTLSFingerprint)
	}
}
