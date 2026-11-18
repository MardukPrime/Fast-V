//go:build !windows

package platform

import (
	"context"
	"errors"
)

// Global hotkey modifier flags. The values mirror the Win32 RegisterHotKey
// fsModifiers constants so binding definitions are shared across platforms.
const (
	ModAlt     uint32 = 0x0001
	ModControl uint32 = 0x0002
	ModShift   uint32 = 0x0004
	ModWin     uint32 = 0x0008
)

// HotkeyBindingSpec describes the action-independent part of a shortcut: the
// modifier flags plus the main key.
type HotkeyBindingSpec struct {
	// Modifiers is a combination of the ModAlt / ModControl / ModShift / ModWin
	// flags.
	Modifiers uint32
	// Key is a virtual-key code; only Windows interprets it.
	Key uint32
}

// HotkeyBinding describes one system-wide keyboard shortcut.
type HotkeyBinding struct {
	// ID identifies the shortcut in the handler callback.
	ID int
	// Modifiers is a combination of the ModAlt / ModControl / ModShift / ModWin
	// flags.
	Modifiers uint32
	// Key is a virtual-key code; only Windows interprets it.
	Key uint32
}

// RegisterGlobalHotkeys is not available outside Windows.
func RegisterGlobalHotkeys(_ context.Context, _ []HotkeyBinding, _ func(id int)) error {
	return errors.New("仅支持 Windows")
}
