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

// virtualKeyF1 / virtualKeyF24 bound the Windows virtual-key codes of the
// function keys F1..F24 (VK_F1 = 0x70, VK_F24 = 0x87).
const (
	virtualKeyF1  uint32 = 0x70
	virtualKeyF24 uint32 = 0x87
)

// proxyModeOrder is the order Ctrl+Shift+M cycles through. It mirrors the
// Settings.ProxyMode values accepted by the settings service.
var proxyModeOrder = []string{"rule", "global", "direct"}

// DefaultBindings returns the factory shortcut table. The application now
// registers the combinations configured in the settings (see
// BindingsFromSettings), but the defaults remain the reference table documented
// to the user and used by tests. The returned slice is a fresh copy on every
// call.
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

// settingsHotkey describes one configurable global shortcut: the action it
// triggers and where the user configured combination lives in the settings.
type settingsHotkey struct {
	id  int
	get func(model.Settings) string
	set func(*model.Settings, string)
}

// settingsHotkeys returns the configurable shortcuts in their fixed processing
// order. The order also decides which combination is reported first when two
// actions share the same keys.
func settingsHotkeys() []settingsHotkey {
	return []settingsHotkey{
		{
			id:  ActionToggleProxy,
			get: func(settings model.Settings) string { return settings.HotkeyToggleProxy },
			set: func(settings *model.Settings, value string) { settings.HotkeyToggleProxy = value },
		},
		{
			id:  ActionCycleMode,
			get: func(settings model.Settings) string { return settings.HotkeyCycleMode },
			set: func(settings *model.Settings, value string) { settings.HotkeyCycleMode = value },
		},
		{
			id:  ActionNextNode,
			get: func(settings model.Settings) string { return settings.HotkeyNextNode },
			set: func(settings *model.Settings, value string) { settings.HotkeyNextNode = value },
		},
	}
}

// ParseCombo parses a human readable combination such as "Ctrl+Shift+F" into
// its platform binding specification.
//
// The syntax is a list of modifiers followed by exactly one main key, joined by
// "+". Modifier names are case insensitive and may appear in any order:
// Ctrl/Control, Alt, Shift and Win. The main key is a letter A-Z, a digit 0-9
// or a function key F1-F24 (also case insensitive); the result is always
// normalized to the Ctrl+Alt+Shift+Win+Key order by FormatCombo.
//
// An empty string is reported as an explicit error because "no shortcut" is a
// decision the callers make themselves (an empty settings value disables the
// action).
func ParseCombo(value string) (platform.HotkeyBindingSpec, error) {
	var spec platform.HotkeyBindingSpec
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return spec, errors.New("快捷键不能为空")
	}

	for _, rawPart := range strings.Split(trimmed, "+") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			return spec, fmt.Errorf("快捷键格式错误：%q", value)
		}
		if modifier, ok := parseModifier(part); ok {
			if spec.Modifiers&modifier != 0 {
				return spec, fmt.Errorf("快捷键修饰键重复：%s", modifierName(modifier))
			}
			spec.Modifiers |= modifier
			continue
		}
		if spec.Key != 0 {
			return spec, fmt.Errorf("快捷键只能包含一个主键：%q", value)
		}
		key, ok := parseMainKey(part)
		if !ok {
			return spec, fmt.Errorf("不支持的按键：%s", part)
		}
		spec.Key = key
	}

	if spec.Key == 0 {
		return spec, errors.New("快捷键缺少主键")
	}
	// A global hotkey without a modifier would swallow an everyday key (for
	// example every F5 press in every application), so it is rejected instead
	// of being handed to RegisterHotKey.
	if spec.Modifiers == 0 {
		return spec, errors.New("快捷键缺少修饰键（至少需要 Ctrl、Alt、Shift 或 Win 中的一个）")
	}
	return spec, nil
}

// FormatCombo renders a binding specification in its canonical form, for
// example "Ctrl+Alt+Shift+Win+F". The modifiers always appear in the same
// order and the main key is upper case, so two equivalent specifications format
// to the same string.
func FormatCombo(spec platform.HotkeyBindingSpec) string {
	parts := make([]string, 0, 5)
	if spec.Modifiers&platform.ModControl != 0 {
		parts = append(parts, "Ctrl")
	}
	if spec.Modifiers&platform.ModAlt != 0 {
		parts = append(parts, "Alt")
	}
	if spec.Modifiers&platform.ModShift != 0 {
		parts = append(parts, "Shift")
	}
	if spec.Modifiers&platform.ModWin != 0 {
		parts = append(parts, "Win")
	}
	if key := mainKeyName(spec.Key); key != "" {
		parts = append(parts, key)
	}
	return strings.Join(parts, "+")
}

// parseModifier maps one modifier name to its platform flag.
func parseModifier(part string) (uint32, bool) {
	switch strings.ToLower(part) {
	case "ctrl", "control":
		return platform.ModControl, true
	case "alt":
		return platform.ModAlt, true
	case "shift":
		return platform.ModShift, true
	case "win":
		return platform.ModWin, true
	default:
		return 0, false
	}
}

// modifierName returns the canonical display name of a modifier flag.
func modifierName(modifier uint32) string {
	switch modifier {
	case platform.ModControl:
		return "Ctrl"
	case platform.ModAlt:
		return "Alt"
	case platform.ModShift:
		return "Shift"
	case platform.ModWin:
		return "Win"
	default:
		return fmt.Sprintf("VK-MOD(0x%02X)", modifier)
	}
}

// parseMainKey maps one main key name to its Windows virtual-key code. Only the
// keys the recorder can produce are accepted: A-Z, 0-9 and F1-F24.
func parseMainKey(part string) (uint32, bool) {
	upper := strings.ToUpper(strings.TrimSpace(part))
	if len(upper) == 1 {
		character := upper[0]
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			return uint32(character), true
		}
		return 0, false
	}
	if len(upper) >= 2 && upper[0] == 'F' {
		number := 0
		for _, digit := range upper[1:] {
			if digit < '0' || digit > '9' {
				return 0, false
			}
			number = number*10 + int(digit-'0')
		}
		// Reject F0, F01 and F25 and beyond: no such virtual-key codes.
		if number < 1 || number > 24 || (len(upper) > 2 && upper[1] == '0') {
			return 0, false
		}
		return virtualKeyF1 + uint32(number-1), true
	}
	return 0, false
}

// mainKeyName returns the canonical name of a virtual-key code used by a
// combination, or an empty string for an unset key.
func mainKeyName(key uint32) string {
	switch {
	case key >= 'A' && key <= 'Z':
		return string(rune(key))
	case key >= '0' && key <= '9':
		return string(rune(key))
	case key >= virtualKeyF1 && key <= virtualKeyF24:
		return fmt.Sprintf("F%d", key-virtualKeyF1+1)
	case key == 0:
		return ""
	default:
		// Bindings collected from elsewhere may use a key outside the recorder
		// range; keep the value visible instead of silently dropping it.
		return fmt.Sprintf("VK(0x%02X)", key)
	}
}

// configuredHotkey pairs one configurable action with its parsed combination.
type configuredHotkey struct {
	action settingsHotkey
	spec   platform.HotkeyBindingSpec
}

// parseSettingsCombos is the single validation pass behind
// ValidateSettingsCombos, NormalizeSettingsCombos and BindingsFromSettings. It
// trims every configured value, parses the non-empty ones and rejects
// combinations that two actions would share.
func parseSettingsCombos(settings model.Settings) ([]configuredHotkey, error) {
	labels := Labels()
	configured := make([]configuredHotkey, 0, len(settingsHotkeys()))
	seen := make(map[string]settingsHotkey, len(settingsHotkeys()))
	for _, entry := range settingsHotkeys() {
		value := strings.TrimSpace(entry.get(settings))
		if value == "" {
			continue
		}
		spec, err := ParseCombo(value)
		if err != nil {
			return nil, fmt.Errorf("%q快捷键无效：%w", labels[entry.id], err)
		}
		combo := FormatCombo(spec)
		if previous, exists := seen[combo]; exists {
			return nil, fmt.Errorf("快捷键不能重复：%q与%q都使用了 %s", labels[previous.id], labels[entry.id], combo)
		}
		seen[combo] = entry
		configured = append(configured, configuredHotkey{action: entry, spec: spec})
	}
	return configured, nil
}

// NormalizeSettingsCombos validates every configured combination and rewrites
// it into its canonical FormatCombo form. An empty value means "this action has
// no shortcut" and stays empty. Invalid or duplicated combinations are reported
// with Chinese errors.
func NormalizeSettingsCombos(settings model.Settings) (model.Settings, error) {
	configured, err := parseSettingsCombos(settings)
	if err != nil {
		return model.Settings{}, err
	}
	for _, entry := range settingsHotkeys() {
		entry.set(&settings, "")
	}
	for _, item := range configured {
		item.action.set(&settings, FormatCombo(item.spec))
	}
	return settings, nil
}

// ValidateSettingsCombos checks the three configurable shortcuts of a settings
// payload: every non-empty value must parse and no two actions may use the same
// combination. It shares the exact rules used by BindingsFromSettings, so a
// rejected save can never store a combination that would fail at registration
// time. SettingsService.Save calls it before persisting.
func ValidateSettingsCombos(settings model.Settings) error {
	_, err := parseSettingsCombos(settings)
	return err
}

// BindingsFromSettings converts the configured shortcuts into the platform
// binding table. Empty values disable the corresponding action and are skipped;
// the returned slice is empty when the user disabled every shortcut.
func BindingsFromSettings(settings model.Settings) ([]platform.HotkeyBinding, error) {
	configured, err := parseSettingsCombos(settings)
	if err != nil {
		return nil, err
	}
	bindings := make([]platform.HotkeyBinding, 0, len(configured))
	for _, item := range configured {
		bindings = append(bindings, platform.HotkeyBinding{
			ID:        item.action.id,
			Modifiers: item.spec.Modifiers,
			Key:       item.spec.Key,
		})
	}
	return bindings, nil
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
