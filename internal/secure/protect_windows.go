//go:build windows

package secure

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// applicationEntropy binds every protected value to FastV in addition to the
// Windows user account: a generic DPAPI call made by another application running
// as the same user cannot decrypt the data without knowing these bytes. The
// value is part of the storage format and must never change, otherwise existing
// data.json files become undecryptable.
var applicationEntropy = []byte("FastV settings secret v1")

// protectionDescription is the human readable DPAPI description stored inside
// every blob; it helps diagnostics tools identify the source of a blob.
const protectionDescription = "FastV settings secret"

// supported reports that the Windows DPAPI backend is available.
func supported() bool { return true }

// protectBytes encrypts plain with DPAPI for the current Windows user.
func protectBytes(plain []byte) ([]byte, error) {
	var out windows.DataBlob
	name := windows.StringToUTF16Ptr(protectionDescription)
	err := windows.CryptProtectData(
		dataBlob(plain),
		name,
		dataBlob(applicationEntropy),
		0,
		nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN,
		&out,
	)
	if err != nil {
		return nil, fmt.Errorf("敏感配置加密失败: %w", err)
	}
	return copyAndFreeBlob(&out), nil
}

// unprotectBytes decrypts a blob produced by protectBytes. A blob encrypted by
// another Windows user or machine fails here: DPAPI keys never leave the user
// profile, so the data cannot be recovered on a different account.
func unprotectBytes(ciphertext []byte) ([]byte, error) {
	var out windows.DataBlob
	err := windows.CryptUnprotectData(
		dataBlob(ciphertext),
		nil,
		dataBlob(applicationEntropy),
		0,
		nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN,
		&out,
	)
	if err != nil {
		return nil, fmt.Errorf("敏感配置解密失败（数据可能由其他 Windows 用户或机器加密）: %w", err)
	}
	return copyAndFreeBlob(&out), nil
}

// dataBlob wraps data in the DPAPI blob shape. DPAPI only reads the buffer for
// the duration of the call, so no copy is needed.
func dataBlob(data []byte) *windows.DataBlob {
	if len(data) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

// copyAndFreeBlob copies a DPAPI output blob into Go memory and releases it.
// DPAPI allocates the output with LocalAlloc, so it must be freed with LocalFree
// exactly once; the copy is required because the returned slice would otherwise
// point at freed memory after the function returns.
func copyAndFreeBlob(blob *windows.DataBlob) []byte {
	if blob.Data == nil || blob.Size == 0 {
		return []byte{}
	}
	out := append([]byte(nil), unsafe.Slice(blob.Data, blob.Size)...)
	_, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(blob.Data)))
	blob.Data = nil
	blob.Size = 0
	return out
}
