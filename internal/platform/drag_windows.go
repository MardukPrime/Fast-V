//go:build windows

package platform

import "syscall"

var (
	user32DLL          = syscall.NewLazyDLL("user32.dll")
	releaseCaptureProc = user32DLL.NewProc("ReleaseCapture")
	sendMessageWProc   = user32DLL.NewProc("SendMessageW")
)

const (
	wmNCLButtonDown = 0x00A1
	htCaption       = 2
)

// StartWindowDrag hands the window over to the native move loop, which gives the
// floating ball the same smooth dragging behaviour as a title bar.
func StartWindowDrag(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	releaseCaptureProc.Call()
	sendMessageWProc.Call(hwnd, wmNCLButtonDown, htCaption, 0)
}
