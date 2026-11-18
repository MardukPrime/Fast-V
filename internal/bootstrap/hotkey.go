package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/hotkey"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	proxyservice "github.com/MardukPrime/Fast-V/internal/service/proxy"
	settingsservice "github.com/MardukPrime/Fast-V/internal/service/settings"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// startGlobalHotkeys wires the configurable global keyboard shortcuts to the
// proxy service and keeps the Win32 registrations in sync with the settings.
//
// The three combinations live in the settings (HotkeyToggleProxy,
// HotkeyCycleMode, HotkeyNextNode); an empty value disables that action and
// HotkeysEnabled turns every shortcut off. Registration is skipped while the
// shortcuts are disabled. Every successful settings save broadcasts
// "settings:changed", and the manager re-registers the shortcuts when (and only
// when) the effective combination table changed, so editing a shortcut works
// without an application restart. Registration failures, for example when
// another program already owns the shortcut, are written to the in-app log
// instead of aborting the startup.
func startGlobalHotkeys(
	st *store.Store,
	settingsSvc *settingsservice.SettingsService,
	proxySvc *proxyservice.ProxyService,
) {
	if st == nil || settingsSvc == nil || proxySvc == nil {
		return
	}

	manager := newGlobalHotkeyManager(st, settingsSvc, proxySvc)

	// The event callback only queues the snapshot: the manager worker owns the
	// Win32 registration lifecycle and never blocks the Wails event thread.
	if app := application.Get(); app != nil {
		app.Event.On("settings:changed", func(event *application.CustomEvent) {
			settings, ok := event.Data.(model.Settings)
			if !ok {
				return
			}
			manager.update(settings)
		})
		app.OnShutdown(manager.stop)
	}
	manager.update(st.Settings())
}

// globalHotkeyManager owns the lifecycle of the global shortcut registration.
//
// A single worker goroutine performs every registration change. Wails event
// callbacks submit settings snapshots through update; the worker coalesces them
// and, before registering a new table, cancels the current message loop and
// waits for it to exit so the old shortcuts are released before the
// re-registration starts (RegisterHotKey would otherwise fail with "already in
// use").
type globalHotkeyManager struct {
	st          *store.Store
	settingsSvc *settingsservice.SettingsService
	proxySvc    *proxyservice.ProxyService

	updates  chan model.Settings
	stopCh   chan struct{}
	stopOnce sync.Once
}

func newGlobalHotkeyManager(
	st *store.Store,
	settingsSvc *settingsservice.SettingsService,
	proxySvc *proxyservice.ProxyService,
) *globalHotkeyManager {
	manager := &globalHotkeyManager{
		st:          st,
		settingsSvc: settingsSvc,
		proxySvc:    proxySvc,
		updates:     make(chan model.Settings, 8),
		stopCh:      make(chan struct{}),
	}
	go manager.run()
	return manager
}

// update queues a settings snapshot without blocking the caller (the Wails
// event thread or the startup path). Only the newest pending snapshot matters.
func (m *globalHotkeyManager) update(settings model.Settings) {
	if m == nil {
		return
	}
	select {
	case m.updates <- settings:
		return
	default:
	}
	// The queue is full: drop the stale pending snapshot and try once more.
	select {
	case <-m.updates:
	default:
	}
	select {
	case m.updates <- settings:
	default:
	}
}

// stop ends the worker and releases the current registration. It is wired to
// the application shutdown and is safe to call more than once.
func (m *globalHotkeyManager) stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
}

// run applies the requested shortcut tables until the application shuts down.
func (m *globalHotkeyManager) run() {
	var (
		cancel context.CancelFunc
		done   chan struct{}
		// signature describes the shortcuts currently registered; an empty
		// signature means "nothing registered".
		signature string
		first     = true
	)

	// stopRegistration tears the current message loop down and waits until the
	// platform layer has unregistered its shortcuts.
	stopRegistration := func() {
		if cancel != nil {
			cancel()
		}
		if done != nil {
			<-done
		}
		cancel, done = nil, nil
		signature = ""
	}
	defer stopRegistration()

	for {
		var settings model.Settings
		select {
		case <-m.stopCh:
			return
		case settings = <-m.updates:
		}

		// Collapse a burst of settings changes into the newest snapshot.
		for coalescing := true; coalescing; {
			select {
			case newer := <-m.updates:
				settings = newer
			default:
				coalescing = false
			}
		}

		bindings, desired, buildErr := desiredHotkeyBindings(settings)
		invalid := buildErr != nil
		if invalid {
			emitHotkeyLog("error", "全局快捷键组合无效，已停止注册："+buildErr.Error())
			bindings, desired = nil, ""
		}
		// Unrelated settings changes (proxy mode, tray options, ...) keep the
		// same shortcut table and must not interrupt the message loop.
		if !first && desired == signature {
			continue
		}
		wasFirst := first
		first = false

		stopRegistration()
		signature = desired
		if len(bindings) == 0 {
			switch {
			case invalid:
				// The error above is the useful log entry.
			case !settings.HotkeysEnabled:
				emitHotkeyLog("info", "全局快捷键已在设置中关闭，已注销快捷键（可在设置中重新开启）")
			default:
				emitHotkeyLog("info", "没有配置任何全局快捷键，本次不注册")
			}
			continue
		}

		ctx, newCancel := context.WithCancel(context.Background())
		newDone := make(chan struct{})
		cancel, done = newCancel, newDone
		go func(bindings []platform.HotkeyBinding, ctx context.Context, done chan struct{}) {
			defer close(done)
			// A non-nil error with a live context means registration failed,
			// for example because another program owns the shortcut.
			if err := hotkey.Run(ctx, bindings, m.handle); err != nil && ctx.Err() == nil {
				emitHotkeyLog("error", "注册全局快捷键失败："+err.Error())
			}
		}(bindings, ctx, newDone)
		if !wasFirst {
			emitHotkeyLog("info", "全局快捷键已更新："+describeHotkeyBindings(bindings))
		}
	}
}

// handle dispatches one shortcut press; it runs on the platform message-loop
// thread.
func (m *globalHotkeyManager) handle(id int) {
	handleGlobalHotkey(m.st, m.settingsSvc, m.proxySvc, id)
}

// desiredHotkeyBindings resolves the shortcut table requested by the settings
// and a stable signature of it. The signature lets the manager skip
// re-registration when an unrelated setting changed. When the shortcuts are
// disabled, or every action has an empty combination, both results are empty.
func desiredHotkeyBindings(settings model.Settings) ([]platform.HotkeyBinding, string, error) {
	if !settings.HotkeysEnabled {
		return nil, "", nil
	}
	bindings, err := hotkey.BindingsFromSettings(settings)
	if err != nil {
		return nil, "", err
	}
	if len(bindings) == 0 {
		return nil, "", nil
	}
	return bindings, hotkeyBindingsSignature(bindings), nil
}

// hotkeyBindingsSignature renders the binding table in canonical form.
func hotkeyBindingsSignature(bindings []platform.HotkeyBinding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		parts = append(parts, fmt.Sprintf("%d=%s", binding.ID, bindingCombo(binding)))
	}
	return strings.Join(parts, "|")
}

// describeHotkeyBindings renders a log friendly shortcut list such as
// "切换代理启停=Ctrl+Shift+F，循环代理模式=Ctrl+Shift+M".
func describeHotkeyBindings(bindings []platform.HotkeyBinding) string {
	labels := hotkey.Labels()
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		parts = append(parts, labels[binding.ID]+"="+bindingCombo(binding))
	}
	return strings.Join(parts, "，")
}

// bindingCombo formats the key part of a registered binding.
func bindingCombo(binding platform.HotkeyBinding) string {
	return hotkey.FormatCombo(platform.HotkeyBindingSpec{Modifiers: binding.Modifiers, Key: binding.Key})
}

// handleGlobalHotkey executes one shortcut action. It re-reads the settings on
// every key press because the re-registration triggered by a settings save runs
// asynchronously, so this check keeps a just-disabled switch effective
// immediately. It also recovers from panics because a hotkey handler runs on
// the message-loop thread and must never take the process down.
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
