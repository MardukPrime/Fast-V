//go:build windows

package platform

import "testing"

func TestURLSchemeKeyPath(t *testing.T) {
	if got, want := urlSchemeKeyPath("fastv"), `Software\Classes\fastv`; got != want {
		t.Fatalf("urlSchemeKeyPath(%q) = %q, want %q", "fastv", got, want)
	}
}

func TestURLSchemeCommand(t *testing.T) {
	exePath := `C:\Program Files\FastV\FastV.exe`
	want := `"C:\Program Files\FastV\FastV.exe" "%1"`
	if got := urlSchemeCommand(exePath); got != want {
		t.Fatalf("urlSchemeCommand(%q) = %q, want %q", exePath, got, want)
	}
}

func TestValidRegistryScheme(t *testing.T) {
	for _, scheme := range []string{"fastv", "fastv-test", "a+b.c", "x1"} {
		if !validRegistryScheme(scheme) {
			t.Errorf("validRegistryScheme(%q) = false, want true", scheme)
		}
	}
	for _, scheme := range []string{"", "1fastv", "fast v", `fast\v`, "fast_v", "-fastv", "快速"} {
		if validRegistryScheme(scheme) {
			t.Errorf("validRegistryScheme(%q) = true, want false", scheme)
		}
	}
}

// TestRegisterURLSchemeRejectsInvalidInput covers the validation path, which
// never touches the registry: a malformed input must fail before any key is
// created.
func TestRegisterURLSchemeRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		scheme  string
		exePath string
	}{
		{name: "empty scheme", scheme: "", exePath: `C:\Program Files\FastV\FastV.exe`},
		{name: "invalid scheme", scheme: "bad scheme", exePath: `C:\Program Files\FastV\FastV.exe`},
		{name: "empty executable path", scheme: "fastv", exePath: ""},
		{name: "quoted executable path", scheme: "fastv", exePath: `C:\Fast"V.exe`},
		{name: "multiline executable path", scheme: "fastv", exePath: "C:\\Fast\r\nV.exe"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := RegisterURLScheme(tc.scheme, tc.exePath); err == nil {
				t.Fatalf("RegisterURLScheme(%q, %q) returned no error", tc.scheme, tc.exePath)
			}
		})
	}
}
