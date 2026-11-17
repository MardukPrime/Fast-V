package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/hotkey"
	"github.com/MardukPrime/Fast-V/internal/model"
	proxyservice "github.com/MardukPrime/Fast-V/internal/service/proxy"
	settingsservice "github.com/MardukPrime/Fast-V/internal/service/settings"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// startGlobalHotkeys wires the global keyboard shortcuts to the proxy service.
//
// Registration is skipped when the user disabled the shortcuts in the settings;
// after a successful registration the enabled switch is checked again on every
// key press, so turning the hotkeys off takes effect immediately (turning them
// back on works without a restart too, because the registration itself stays in
// place). Registration failures, for example when another program already owns
// the shortcut, are written to the in-app log instead of aborting the startup.
func startGlobalHotkeys(
	st *store.Store,
	settingsSvc *settingsservice.SettingsService,
	proxySvc *proxyservice.ProxyService,
) {
	if st == nil || settingsSvc == nil || proxySvc == nil {
		return
	}
	if !st.Settings().HotkeysEnabled {
		emitHotkeyLog("info", "全局快捷键已在设置中关闭，本次启动不注册（重新开启后需重启应用）")
		return
	}

	handler := func(id int) {
		handleGlobalHotkey(st, settingsSvc, proxySvc, id)
	}
	go func() {
		// The message loop runs for the application lifetime. Cancelling the
		// context on shutdown makes the platform layer post WM_QUIT to the hotkey
		// thread and unregister every shortcut cleanly instead of relying on
		// process exit; the deferred cancel covers the failure paths as well.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if app := application.Get(); app != nil {
			app.OnShutdown(cancel)
		}
		// Run blocks while the message loop is active; it returns only when the
		// shortcuts could not be registered or the context was cancelled.
		if err := hotkey.Run(ctx, hotkey.DefaultBindings(), handler); err != nil {
			emitHotkeyLog("error", "注册全局快捷键失败："+err.Error())
		}
	}()
}

// handleGlobalHotkey executes one shortcut action. It re-reads the settings on
// every key press so disabling the hotkeys applies without a restart, and it
// recovers from panics because a hotkey handler runs on the message-loop thread
// and must never take the process down.
func handleGlobalHotkey(
	st *store.Store,
	settingsSvc *settingsservice.SettingsService,
	proxySvc *proxyservice.ProxyService,
	id int,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			emitHotkeyLog("error", fmt.Sprintf("全局快捷键执行失败：%v", recovered))
		}
	}()

	// Re-read the live settings: the registration outlives the switch, so this
	// check is what makes "关闭热键即时失效" true.
	if !st.Settings().HotkeysEnabled {
		return
	}

	switch id {
	case hotkey.ActionToggleProxy:
		toggleProxyFromHotkey(proxySvc)
	case hotkey.ActionCycleMode:
		cycleProxyModeFromHotkey(st, settingsSvc, proxySvc)
	case hotkey.ActionNextNode:
		switchToNextNodeFromHotkey(st, proxySvc)
	}
}

// toggleProxyFromHotkey stops a running proxy and starts a stopped one.
func toggleProxyFromHotkey(proxySvc *proxyservice.ProxyService) {
	if proxySvc == nil {
		return
	}
	if proxySvc.State().Status == "running" {
		if _, err := proxySvc.Stop(); err != nil {
			emitHotkeyLog("error", "快捷键：停止代理失败："+err.Error())
			return
		}
		emitHotkeyLog("info", "快捷键：代理已停止")
		return
	}
	if _, err := proxySvc.Start(); err != nil {
		emitHotkeyLog("error", "快捷键：启动代理失败："+err.Error())
		return
	}
	emitHotkeyLog("info", "快捷键：代理已启动")
}

// cycleProxyModeFromHotkey persists the next proxy mode (rule → global →
// direct) and restarts a running proxy so the mode applies immediately.
func cycleProxyModeFromHotkey(
	st *store.Store,
	settingsSvc *settingsservice.SettingsService,
	proxySvc *proxyservice.ProxyService,
) {
	if st == nil || settingsSvc == nil || proxySvc == nil {
		return
	}
	current := st.Settings()
	next := hotkey.NextProxyMode(current.ProxyMode)
	if next == current.ProxyMode {
		return
	}
	current.ProxyMode = next
	// SettingsService.Save validates the complete settings payload and
	// broadcasts "settings:changed", which keeps the tray menu in sync.
	saved, err := settingsSvc.Save(current)
	if err != nil {
		emitHotkeyLog("error", "快捷键：保存代理模式失败："+err.Error())
		return
	}
	emitHotkeyLog("info", "快捷键：代理模式已切换为"+hotkey.ProxyModeLabel(saved.ProxyMode))

	state := proxySvc.State()
	if state.Status != "running" && state.Status != "starting" {
		return
	}
	if _, err := proxySvc.Restart(); err != nil {
		emitHotkeyLog("error", "快捷键：切换代理模式后重启代理失败："+err.Error())
	}
}

// switchToNextNodeFromHotkey activates the next enabled node in the user
// ordered list. A running proxy is restarted by ProxyService.SwitchNode.
func switchToNextNodeFromHotkey(st *store.Store, proxySvc *proxyservice.ProxyService) {
	if st == nil || proxySvc == nil {
		return
	}
	activeID := ""
	if active, ok := st.ActiveNode(); ok {
		activeID = active.ID
	}
	next, ok := hotkey.NextEnabledNode(st.ListNodes(), activeID)
	if !ok {
		emitHotkeyLog("warning", "快捷键：没有可用的启用节点")
		return
	}
	if next.ID == activeID {
		emitHotkeyLog("info", "快捷键：只有一个启用的节点，无需切换")
		return
	}
	if _, err := proxySvc.SwitchNode(next.ID); err != nil {
		emitHotkeyLog("error", "快捷键：切换节点失败："+err.Error())
		return
	}
	emitHotkeyLog("info", "快捷键：已切换到节点"+hotkeyNodeName(next))
}

// hotkeyNodeName returns the display name of a node for log messages,
// falling back to the address and then the id.
func hotkeyNodeName(node model.Node) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	if address := strings.TrimSpace(node.Address); address != "" {
		return address
	}
	return node.ID
}

// emitHotkeyLog publishes a proxy log entry so hotkey events and failures show
// up in the in-app log view (event "proxy:log", consumed by LogsPage).
func emitHotkeyLog(level, message string) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit("proxy:log", model.LogEntry{
		ID:        model.NewID(),
		Timestamp: model.NowRFC3339(),
		Level:     level,
		Source:    "hotkey",
		Message:   message,
	})
}
