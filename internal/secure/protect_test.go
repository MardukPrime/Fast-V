package secure

import (
	"encoding/base64"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// TestSupportedMatchesPlatform guards the platform contract of the package: only
// Windows ships a DPAPI backend.
func TestSupportedMatchesPlatform(t *testing.T) {
	want := runtime.GOOS == "windows"
	if got := Supported(); got != want {
		t.Fatalf("Supported() = %v, want %v on %s", got, want, runtime.GOOS)
	}
}

func TestIsEncrypted(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "legacy plaintext", value: "p@ssw0rd", want: false},
		{name: "chinese plaintext", value: "中文密码", want: false},
		{name: "prefix only", value: Prefix, want: true},
		{name: "protected", value: Prefix + "QUJD", want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsEncrypted(testCase.value); got != testCase.want {
				t.Fatalf("IsEncrypted(%q) = %v, want %v", testCase.value, got, testCase.want)
			}
		})
	}
}

// TestProtectStringRoundTrip exercises the real DPAPI backend. It is skipped on
// platforms without DPAPI (the fallback behavior is covered by
// TestUnsupportedPlatformBehavior).
func TestProtectStringRoundTrip(t *testing.T) {
	if !Supported() {
		t.Skipf("DPAPI is only available on Windows (running on %s)", runtime.GOOS)
	}
	tests := []struct {
		name   string
		secret string
	}{
		{name: "ascii", secret: "webdav-p@ssw0rd"},
		{name: "chinese", secret: "中文密码-测试-🔐"},
		{name: "long", secret: strings.Repeat("长密码", 2048)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			protected, err := ProtectString(testCase.secret)
			if err != nil {
				t.Fatalf("ProtectString failed: %v", err)
			}
			if !IsEncrypted(protected) {
				t.Fatalf("ProtectString(%q) = %q, want the %q prefix", testCase.secret, protected, Prefix)
			}
			if protected == testCase.secret {
				t.Fatalf("ProtectString returned the plaintext unchanged")
			}
			if strings.Contains(protected, testCase.secret) {
				t.Fatalf("ProtectString output leaks the plaintext")
			}
			plain, err := UnprotectString(protected)
			if err != nil {
				t.Fatalf("UnprotectString failed: %v", err)
			}
			if plain != testCase.secret {
				t.Fatalf("round trip mismatch: got %q, want %q", plain, testCase.secret)
			}
		})
	}
}

// TestProtectStringUsesFreshCiphertext verifies that two protections of the same
// plaintext differ while both decrypt back to it (DPAPI adds a random salt; a
// deterministic cipher would let backup files be compared for equal passwords).
func TestProtectStringUsesFreshCiphertext(t *testing.T) {
	if !Supported() {
		t.Skipf("DPAPI is only available on Windows (running on %s)", runtime.GOOS)
	}
	first, err := ProtectString("same-secret")
	if err != nil {
		t.Fatalf("first ProtectString failed: %v", err)
	}
	second, err := ProtectString("same-secret")
	if err != nil {
		t.Fatalf("second ProtectString failed: %v", err)
	}
	if first == second {
		t.Fatalf("expected DPAPI to produce a fresh salt per call, got identical blobs")
	}
	for _, protected := range []string{first, second} {
		plain, err := UnprotectString(protected)
		if err != nil || plain != "same-secret" {
			t.Fatalf("UnprotectString(%q) = %q, %v", protected, plain, err)
		}
	}
}

// TestProtectStringEmpty verifies the empty shortcut: it is a cross-platform
// no-op so the app never encrypts "no password configured".
func TestProtectStringEmpty(t *testing.T) {
	protected, err := ProtectString("")
	if err != nil {
		t.Fatalf("ProtectString(\"\") error = %v", err)
	}
	if protected != "" {
		t.Fatalf("ProtectString(\"\") = %q, want an empty string", protected)
	}
	plain, err := UnprotectString("")
	if err != nil {
		t.Fatalf("UnprotectString(\"\") error = %v", err)
	}
	if plain != "" {
		t.Fatalf("UnprotectString(\"\") = %q, want an empty string", plain)
	}
}

// TestProtectStringAlreadyEncrypted verifies idempotency: a double save must not
// wrap an existing blob again.
func TestProtectStringAlreadyEncrypted(t *testing.T) {
	value := Prefix + "QUJDREVG"
	got, err := ProtectString(value)
	if err != nil {
		t.Fatalf("ProtectString(protected) error = %v", err)
	}
	if got != value {
		t.Fatalf("ProtectString(%q) = %q, want the value unchanged", value, got)
	}
}

// TestUnprotectStringLegacyPlaintext verifies backward compatibility with data
// files written before encryption existed: plaintext values pass through.
func TestUnprotectStringLegacyPlaintext(t *testing.T) {
	for _, value := range []string{"p@ssw0rd", "p@ss word", "中文密码", "a" + Prefix + "b"} {
		got, err := UnprotectString(value)
		if err != nil {
			t.Fatalf("UnprotectString(%q) error = %v, want plaintext pass-through", value, err)
		}
		if got != value {
			t.Fatalf("UnprotectString(%q) = %q, want %q", value, got, value)
		}
	}
}

// TestUnprotectStringInvalidBase64 verifies that a malformed payload is rejected
// with a Chinese error before any backend is called.
func TestUnprotectStringInvalidBase64(t *testing.T) {
	for _, value := range []string{Prefix + "not base64!!", Prefix + "@@@", Prefix + "a"} {
		_, err := UnprotectString(value)
		if err == nil {
			t.Fatalf("UnprotectString(%q) succeeded, want an error", value)
		}
		if !strings.Contains(err.Error(), "密文格式无效") {
			t.Fatalf("UnprotectString(%q) error = %q, want a format error", value, err)
		}
	}
}

// TestUnprotectStringCorruptData verifies that a valid base64 value that is not
// a usable DPAPI blob fails with the Chinese decryption error.
func TestUnprotectStringCorruptData(t *testing.T) {
	if !Supported() {
		t.Skipf("DPAPI is only available on Windows (running on %s)", runtime.GOOS)
	}
	corrupt := Prefix + base64.StdEncoding.EncodeToString([]byte("definitely not a dpapi blob"))
	_, err := UnprotectString(corrupt)
	if err == nil {
		t.Fatalf("UnprotectString(corrupt) succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "解密失败") {
		t.Fatalf("UnprotectString(corrupt) error = %q, want a decryption error", err)
	}
}

// TestUnsupportedPlatformBehavior verifies the non-Windows contract: protection
// fails with ErrUnsupported while plaintext compatibility still works.
func TestUnsupportedPlatformBehavior(t *testing.T) {
	if Supported() {
		t.Skip("only meaningful on platforms without a protection backend")
	}
	if got, err := ProtectString("secret"); !errors.Is(err, ErrUnsupported) || got != "" {
		t.Fatalf("ProtectString = (%q, %v), want (\"\", ErrUnsupported)", got, err)
	}
	if got, err := UnprotectString("secret"); err != nil || got != "secret" {
		t.Fatalf("UnprotectString(plaintext) = (%q, %v), want the plaintext", got, err)
	}
	if got, err := UnprotectString(Prefix + "QUJD"); !errors.Is(err, ErrUnsupported) || got != "" {
		t.Fatalf("UnprotectString(protected) = (%q, %v), want (\"\", ErrUnsupported)", got, err)
	}
}
