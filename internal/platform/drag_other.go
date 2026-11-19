//go:build !windows

package platform

// StartWindowDrag is a no-op on platforms where the floating ball cannot use the
// Win32 native move loop.
func StartWindowDrag(hwnd uintptr) {}
