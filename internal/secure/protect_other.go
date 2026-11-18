//go:build !windows

package secure

// supported reports that the DPAPI backend only exists on Windows.
func supported() bool { return false }

// protectBytes is unsupported outside Windows: callers keep the plaintext form
// because the value could never be decrypted on this platform anyway.
func protectBytes([]byte) ([]byte, error) { return nil, ErrUnsupported }

// unprotectBytes is unsupported outside Windows: prefixed values are reported as
// unsupported and left untouched by the caller.
func unprotectBytes([]byte) ([]byte, error) { return nil, ErrUnsupported }
