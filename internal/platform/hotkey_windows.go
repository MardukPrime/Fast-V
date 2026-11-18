//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Global hotkey modifier flags, matching the Win32 RegisterHotKey fsModifiers
// values (MOD_ALT / MOD_CONTROL / MOD_SHIFT / MOD_WIN).
const (
	ModAlt     uint32 = 0x0001
	ModControl uint32 = 0x0002
	ModShift   uint32 = 0x0004
	ModWin     uint32 = 0x0008
)

// HotkeyBindingSpec describes the action-independent part of a shortcut: the
// modifier flags plus the main key. The hotkey package parses the user
// configured combinations into this type before an action ID is attached to
// each of them.
type HotkeyBindingSpec struct {
	// Modifiers is a combination of the ModAlt / ModControl / ModShift / ModWin
	// flags.
	Modifiers uint32
	// Key is a Windows virtual-key code such as 'F' (0x46) or VK_F1 (0x70).
	Key uint32
}

// HotkeyBinding describes one system-wide keyboard shortcut.
type HotkeyBinding struct {
	// ID identifies the shortcut in the handler callback. It must be unique
	// among the registered bindings and belong to the application-defined range
	// 1..0xBFFF.
	ID int
	// Modifiers is a combination of the ModAlt / ModControl / ModShift / ModWin
	// flags.
	Modifiers uint32
	// Key is a Windows virtual-key code such as 'F' (0x46) or VK_F1 (0x70).
	Key uint32
}

const (
	// wmHotkey is posted to the registering thread when its shortcut fires.
	wmHotkey = 0x0312 // WM_HOTKEY
	// wmQuit terminates the message loop; the cancellation watcher posts it.
	wmQuit = 0x0012 // WM_QUIT
	// modNoRepeat is added to every registration so holding the keys down emits
	// a single WM_HOTKEY instead of an autorepeat storm.
	modNoRepeat = 0x4000 // MOD_NOREPEAT
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

// win32Msg mirrors the native MSG structure. The field order and the implicit
// padding keep the Go layout identical to the 64-bit Windows definition (48
// bytes).
type win32Msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct {
		x int32
		y int32
	}
}

// RegisterGlobalHotkeys registers every binding and runs a Win32 message loop
// on a dedicated OS thread until ctx is cancelled.
//
// The function blocks: it returns nil once ctx cancellation has stopped the
// loop and every registration has been removed, or a Chinese error when a
// shortcut cannot be registered (a shortcut owned by another process is the
// common reason).
func RegisterGlobalHotkeys(ctx context.Context, bindings []HotkeyBinding, handler func(id int)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if handler == nil {
		return errors.New("快捷键处理函数不能为空")
	}
	if len(bindings) == 0 {
		return errors.New("没有可注册的全局快捷键")
	}

	// RegisterHotKey attaches the shortcut to the message queue of the calling
	// thread, so registration, the GetMessage loop and the cleanup all have to
	// run on one dedicated OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// GetMessage only starts dispatching once the thread owns a message queue;
	// PeekMessage (with PM_NOREMOVE) creates it.
	var msg win32Msg
	peekMessage(&msg)

	registered := make([]HotkeyBinding, 0, len(bindings))
	for _, binding := range bindings {
		if err := registerHotkey(binding); err != nil {
			// Do not leak the shortcuts registered before the failing one.
			unregisterHotkeys(registered)
			return err
		}
		registered = append(registered, binding)
	}

	threadID := uint32(mustGetCurrentThreadID())

	// The watcher turns context cancellation into WM_QUIT for the thread that
	// owns the message loop. RegisterHotKey/PeekMessage above already created
	// the queue, so PostThreadMessage cannot race with queue creation.
	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		select {
		case <-ctx.Done():
			postThreadMessage(threadID, wmQuit)
		case <-stopWatcher:
		}
	}()

	loopErr := runHotkeyMessageLoop(&msg, handler)
	unregisterHotkeys(registered)
	return loopErr
}

// runHotkeyMessageLoop dispatches WM_HOTKEY notifications to handler until the
// thread receives WM_QUIT.
func runHotkeyMessageLoop(msg *win32Msg, handler func(id int)) error {
	for {
		result, _, callErr := procGetMessageW.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0)
		switch int32(result) {
		case -1:
			return fmt.Errorf("读取全局快捷键消息失败（%s）", win32ErrorText(callErr))
		case 0:
			// WM_QUIT: a normal shutdown triggered by context cancellation.
			return nil
		}
		if msg.message == wmHotkey {
			handler(int(msg.wParam))
		}
	}
}

// registerHotkey adds one shortcut to the calling thread's message queue.
func registerHotkey(binding HotkeyBinding) error {
	result, _, callErr := procRegisterHotKey.Call(
		0,
		uintptr(binding.ID),
		uintptr(binding.Modifiers|modNoRepeat),
		uintptr(binding.Key),
	)
	if result == 0 {
		return fmt.Errorf("注册全局快捷键失败：%s 可能已被其他程序占用（%s）", bindingName(binding), win32ErrorText(callErr))
	}
	return nil
}

// unregisterHotkeys releases every registration; cleanup is best effort
// because the process is shutting the shortcuts down anyway.
func unregisterHotkeys(bindings []HotkeyBinding) {
	for _, binding := range bindings {
		_, _, _ = procUnregisterHotKey.Call(0, uintptr(binding.ID))
	}
}

func peekMessage(msg *win32Msg) {
	// PM_NOREMOVE (0): only force the queue into existence.
	_, _, _ = procPeekMessageW.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0, 0)
}

func postThreadMessage(threadID uint32, message uint32) {
	_, _, _ = procPostThreadMessageW.Call(uintptr(threadID), uintptr(message), 0, 0)
}

func mustGetCurrentThreadID() uintptr {
	id, _, _ := procGetCurrentThreadId.Call()
	return id
}

// win32ErrorText renders the GetLastError value returned by a Win32 call. The
// Go syscall wrapper always returns a non-nil error, including Errno(0) on
// success, so the zero value is reported as an unknown error here.
func win32ErrorText(callErr error) string {
	var errno syscall.Errno
	if errors.As(callErr, &errno) && errno != 0 {
		return fmt.Sprintf("错误码 %d：%s", uint32(errno), errno.Error())
	}
	return "未知错误"
}

// bindingName renders a binding like "Ctrl+Shift+F" for error messages.
func bindingName(binding HotkeyBinding) string {
	parts := make([]string, 0, 4)
	if binding.Modifiers&ModControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if binding.Modifiers&ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if binding.Modifiers&ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if binding.Modifiers&ModWin != 0 {
		parts = append(parts, "Win")
	}
	if key := virtualKeyName(binding.Key); key != "" {
		parts = append(parts, key)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("ID %d", binding.ID)
	}
	return strings.Join(parts, "+")
}

// virtualKeyName names the common virtual-key codes used by FastV bindings.
func virtualKeyName(key uint32) string {
	switch {
	case key >= 'A' && key <= 'Z':
		return string(rune(key))
	case key >= '0' && key <= '9':
		return string(rune(key))
	case key >= 0x70 && key <= 0x87:
		return fmt.Sprintf("F%d", key-0x6F)
	default:
		return fmt.Sprintf("VK(0x%02X)", key)
	}
}
