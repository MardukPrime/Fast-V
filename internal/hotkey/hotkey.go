// Package hotkey defines FastV's global keyboard shortcuts, the actions they
// trigger and the pure state helpers behind those actions.
//
// The platform package owns the Win32 registration and the message loop; this
// package owns the binding table, the labels shown to the user and the
// validation applied before a registration is attempted.
package hotkey

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
)

// Action IDs delivered to a global hotkey handler. Every binding needs a
// stable, non-zero ID because the ID is what the Win32 message loop reports
// back when the shortcut fires.
const (
	// ActionToggleProxy starts the local proxy when it is stopped and stops it
	// otherwise (Ctrl+Shift+F).
	ActionToggleProxy = 1
	// ActionCycleMode rotates the proxy mode rule → global → direct
	// (Ctrl+Shift+M).
	ActionCycleMode = 2
	// ActionNextNode activates the next enabled node (Ctrl+Shift+N).
	ActionNextNode = 3
)

// virtualKeyF / virtualKeyM / virtualKeyN are the Windows virtual-key codes of
// the letter keys used by the default bindings (virtual-key codes of letters
// equal their uppercase ASCII value).
const (
	virtualKeyF uint32 = 0x46 // 'F'
	virtualKeyM uint32 = 0x4D // 'M'
	virtualKeyN uint32 = 0x4E // 'N'
)

// proxyModeOrder is the order Ctrl+Shift+M cycles through. It mirrors the
// Settings.ProxyMode values accepted by the settings service.
var proxyModeOrder = []string{"rule", "global", "direct"}

// DefaultBindings returns the global shortcuts registered at application
// startup. The returned slice is a fresh copy on every call.
func DefaultBindings() []platform.HotkeyBinding {
	return []platform.HotkeyBinding{
		{ID: ActionToggleProxy, Modifiers: platform.ModControl | platform.ModShift, Key: virtualKeyF},
		{ID: ActionCycleMode, Modifiers: platform.ModControl | platform.ModShift, Key: virtualKeyM},
		{ID: ActionNextNode, Modifiers: platform.ModControl | platform.ModShift, Key: virtualKeyN},
	}
}

// Labels returns the Chinese description of every hotkey action, keyed by the
// action ID.
func Labels() map[int]string {
	return map[int]string{
		ActionToggleProxy: "切换代理启停",
		ActionCycleMode:   "循环代理模式",
		ActionNextNode:    "切换到下一个启用节点",
	}
}

// ValidateBindings checks the binding table before registration: every binding
// needs a non-zero, unique ID, at least one modifier and a key.
func ValidateBindings(bindings []platform.HotkeyBinding) error {
	if len(bindings) == 0 {
		return errors.New("没有可注册的全局快捷键")
	}
	seen := make(map[int]struct{}, len(bindings))
	for _, binding := range bindings {
		if binding.ID <= 0 {
			return errors.New("全局快捷键 ID 不能为空")
		}
		if _, exists := seen[binding.ID]; exists {
			return fmt.Errorf("全局快捷键 ID 重复：%d", binding.ID)
		}
		seen[binding.ID] = struct{}{}
		if binding.Modifiers == 0 {
			return fmt.Errorf("全局快捷键 ID %d 缺少修饰键", binding.ID)
		}
		if binding.Key == 0 {
			return fmt.Errorf("全局快捷键 ID %d 缺少按键", binding.ID)
		}
	}
	return nil
}

// Run registers the bindings on the current platform and blocks while the
// shortcut message loop is active. Invalid bindings are rejected locally, so a
// broken table never reaches the Win32 registration.
func Run(ctx context.Context, bindings []platform.HotkeyBinding, handler func(id int)) error {
	if err := ValidateBindings(bindings); err != nil {
		return err
	}
	if handler == nil {
		return errors.New("快捷键处理函数不能为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return platform.RegisterGlobalHotkeys(ctx, bindings, handler)
}

// ProxyModes returns the supported proxy modes in cycling order. The returned
// slice is a copy.
func ProxyModes() []string {
	return append([]string(nil), proxyModeOrder...)
}

// NextProxyMode returns the mode after current in the rule → global → direct
// cycle. Unknown or empty values fall back to the first mode.
func NextProxyMode(current string) string {
	current = strings.TrimSpace(current)
	for index, mode := range proxyModeOrder {
		if mode == current {
			return proxyModeOrder[(index+1)%len(proxyModeOrder)]
		}
	}
	return proxyModeOrder[0]
}

// ProxyModeLabel returns the Chinese label of a proxy mode.
func ProxyModeLabel(mode string) string {
	switch strings.TrimSpace(mode) {
	case "rule":
		return "规则分流"
	case "global":
		return "全局代理"
	case "direct":
		return "全部直连"
	default:
		return "规则分流"
	}
}

// NextEnabledNode returns the enabled node that follows activeID in the user
// ordered node list, wrapping around at the end. Disabled nodes are skipped and
// an unknown or disabled activeID falls back to the first enabled node. The
// boolean result reports whether any enabled node exists.
func NextEnabledNode(nodes []model.Node, activeID string) (model.Node, bool) {
	enabled := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			enabled = append(enabled, node)
		}
	}
	if len(enabled) == 0 {
		return model.Node{}, false
	}
	for index, node := range enabled {
		if node.ID == activeID {
			return enabled[(index+1)%len(enabled)], true
		}
	}
	return enabled[0], true
}
