// Package secure protects the sensitive settings values persisted by the store
// with the operating system's native secret protection.
//
// On Windows a value is encrypted with DPAPI and is therefore bound to the
// current Windows user account (and machine): another user, another machine or a
// copied data.json cannot decrypt it. On other platforms the protection backend
// is unavailable and callers fall back to the plaintext representation, because
// a non-Windows build can never decrypt a DPAPI blob anyway.
package secure

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Prefix marks a protected value in the persisted JSON document. The versioned
// marker keeps plaintext values written by older builds distinguishable from
// encrypted ones, so both can be read by the same code path and the protection
// scheme can change in a future version without guessing.
const Prefix = "enc:v1:"

// ErrUnsupported reports that the running platform has no secret protection
// backend. Callers that still have to persist the value keep the plaintext form.
var ErrUnsupported = errors.New("当前平台不支持敏感配置加密（仅 Windows 提供 DPAPI）")

// Supported reports whether values can actually be protected on this platform.
func Supported() bool { return supported() }

// IsEncrypted reports whether value carries the encrypted-value prefix.
func IsEncrypted(value string) bool { return strings.HasPrefix(value, Prefix) }

// ProtectString returns the persisted representation of a sensitive value.
//
// An empty value stays empty and a value that already carries the encrypted
// prefix is returned unchanged, so protecting an already protected value is
// safe. On Windows the plaintext is encrypted with DPAPI for the current user
// and encoded as "enc:v1:<base64>". On an unsupported platform it returns
// ErrUnsupported.
func ProtectString(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if IsEncrypted(plain) {
		return plain, nil
	}
	ciphertext, err := protectBytes([]byte(plain))
	if err != nil {
		return "", err
	}
	return Prefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// UnprotectString reverses ProtectString.
//
// Empty values and values without the encrypted prefix are returned unchanged,
// which keeps data files and backups written by older builds (plaintext)
// readable. On Windows a damaged blob or one encrypted for another Windows
// user/machine returns an error. On an unsupported platform a prefixed value
// returns ErrUnsupported, while plaintext values still pass through unchanged.
func UnprotectString(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !IsEncrypted(value) {
		return value, nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("敏感配置密文格式无效: %w", err)
	}
	plain, err := unprotectBytes(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
