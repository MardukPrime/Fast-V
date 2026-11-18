package proxy

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildPACMatchesProxyAndDirectRules asserts that the generated PAC script
// contains the proxy directive and every local/private bypass rule required by
// the PAC system-proxy mode.
func TestBuildPACMatchesProxyAndDirectRules(t *testing.T) {
	pac := BuildPAC("127.0.0.1:10808")
	if pac == "" {
		t.Fatal("BuildPAC returned an empty script for a valid proxy address")
	}
	required := []string{
		"function FindProxyForURL(url, host)",
		`var FASTV_PROXY = "PROXY 127.0.0.1:10808";`,
		`return "DIRECT";`,
		"return FASTV_PROXY;",
		`h === "localhost"`,
		`h === "127.0.0.1"`,
		`dnsDomainIs(h, ".local")`,
		`shExpMatch(h, "127.*")`,
		`shExpMatch(h, "10.*")`,
		`shExpMatch(h, "192.168.*")`,
		`shExpMatch(h, "172.1[6-9].*")`,
		`shExpMatch(h, "172.2[0-9].*")`,
		`shExpMatch(h, "172.3[01].*")`,
	}
	for _, fragment := range required {
		if !strings.Contains(pac, fragment) {
			t.Errorf("generated PAC script is missing %q\nscript:\n%s", fragment, pac)
		}
	}
	if strings.Contains(pac, "SOCKS") || strings.Contains(pac, "https") {
		t.Errorf("generated PAC script contains an unexpected proxy type:\n%s", pac)
	}
}

// TestBuildPACIsDeterministic guards against a generated timestamp or other
// random content, which would make the file change on every start.
func TestBuildPACIsDeterministic(t *testing.T) {
	first := BuildPAC("127.0.0.1:10808")
	second := BuildPAC("127.0.0.1:10808")
	if first != second {
		t.Fatal("BuildPAC output is not deterministic")
	}
}

func TestBuildPACAcceptsValidProxyAddresses(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "127.0.0.1:10808", want: `var FASTV_PROXY = "PROXY 127.0.0.1:10808";`},
		{input: "localhost:1080", want: `var FASTV_PROXY = "PROXY localhost:1080";`},
		{input: "  127.0.0.1:10808  ", want: `var FASTV_PROXY = "PROXY 127.0.0.1:10808";`},
		{input: "proxy-host.local:7890", want: `var FASTV_PROXY = "PROXY proxy-host.local:7890";`},
	}
	for _, test := range tests {
		pac := BuildPAC(test.input)
		if !strings.Contains(pac, test.want) {
			t.Errorf("BuildPAC(%q) does not contain %q:\n%s", test.input, test.want, pac)
		}
	}
}

// TestBuildPACRejectsInvalidProxyAddress documents the empty-string policy: an
// unusable proxy address must never be embedded in a PAC script served to the
// whole system.
func TestBuildPACRejectsInvalidProxyAddress(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"127.0.0.1",              // no port
		"127.0.0.1:",             // empty port
		":10808",                 // no host
		"127.0.0.1:0",            // port not in 1-65535
		"127.0.0.1:70000",        // port not in 1-65535
		"127.0.0.1:abc",          // non-numeric port
		"127.0.0.1:10808;DIRECT", // injection attempt
		`127.0.0.1:10808"`,       // quote injection attempt
		"http://127.0.0.1:10808", // scheme is not part of host:port
		"[::1]:10808",            // IPv6 literals are not supported
	}
	for _, address := range invalid {
		if pac := BuildPAC(address); pac != "" {
			t.Errorf("BuildPAC(%q) = %q, want an empty script", address, pac)
		}
	}
}

func TestPACFilePath(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "pac", "fastv.pac")
	if got := pacFilePath(dir); got != want {
		t.Fatalf("pacFilePath(%q) = %q, want %q", dir, got, want)
	}
}

// TestWritePACFile covers the updater helper used by the manager: the script
// must land at <data dir>/pac/fastv.pac, be complete, and be owner-readable
// only (on platforms with Unix permission bits).
func TestWritePACFile(t *testing.T) {
	dir := t.TempDir()
	path, err := writePACFile(dir, "127.0.0.1:10808")
	if err != nil {
		t.Fatalf("writePACFile failed: %v", err)
	}
	if path != pacFilePath(dir) {
		t.Fatalf("writePACFile returned %q, want %q", path, pacFilePath(dir))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the generated PAC file failed: %v", err)
	}
	pac := string(data)
	if !strings.Contains(pac, "function FindProxyForURL(url, host)") {
		t.Fatalf("generated PAC file is incomplete:\n%s", pac)
	}
	if !strings.Contains(pac, `var FASTV_PROXY = "PROXY 127.0.0.1:10808";`) {
		t.Fatalf("generated PAC file does not point at the local proxy:\n%s", pac)
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat PAC file: %v", statErr)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("PAC file permissions = %o, want 600", perm)
		}
	}
}

// TestWritePACFileOverwritesStaleScript covers a restart with a different local
// port: the previous script must be replaced, not appended to.
func TestWritePACFileOverwritesStaleScript(t *testing.T) {
	dir := t.TempDir()
	if _, err := writePACFile(dir, "127.0.0.1:10808"); err != nil {
		t.Fatalf("first writePACFile failed: %v", err)
	}
	path, err := writePACFile(dir, "127.0.0.1:20808")
	if err != nil {
		t.Fatalf("second writePACFile failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the generated PAC file failed: %v", err)
	}
	if strings.Contains(string(data), "PROXY 127.0.0.1:10808") {
		t.Fatalf("stale PAC content was not replaced:\n%s", data)
	}
	if !strings.Contains(string(data), `var FASTV_PROXY = "PROXY 127.0.0.1:20808";`) {
		t.Fatalf("PAC file does not contain the new proxy address:\n%s", data)
	}
}

func TestWritePACFileRejectsInvalidAddress(t *testing.T) {
	for _, address := range []string{"", "not-a-proxy", "127.0.0.1:70000"} {
		if _, err := writePACFile(t.TempDir(), address); err == nil {
			t.Errorf("writePACFile(%q) returned no error", address)
		}
	}
}

func TestRemovePACFileIsBestEffort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastv.pac")
	if err := os.WriteFile(path, []byte("script"), 0o600); err != nil {
		t.Fatal(err)
	}
	removePACFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("PAC file still exists after removePACFile: err=%v", err)
	}
	// Removing a missing file and an empty path must not panic or fail loudly.
	removePACFile(path)
	removePACFile("")
}
