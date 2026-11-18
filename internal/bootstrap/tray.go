package bootstrap

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/MardukPrime/Fast-V/internal/hotkey"
	"github.com/MardukPrime/Fast-V/internal/model"
	appservice "github.com/MardukPrime/Fast-V/internal/service/app"
	proxyservice "github.com/MardukPrime/Fast-V/internal/service/proxy"
	settingsservice "github.com/MardukPrime/Fast-V/internal/service/settings"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// trayTooltipStopped is the tooltip shown while the proxy is not running.
	trayTooltipStopped = "FastV - 代理未启动"
	// maxTrayNodeEntries is how many enabled nodes the tray node submenu lists.
	maxTrayNodeEntries = 10
	// trayLabelWidth bounds dynamically built menu label text (in runes) so a
	// long node name cannot stretch the native tray menu.
	trayLabelWidth = 30
)

// trayController owns the system tray icon and menu plus the dynamic updates
// driven by proxy state and subscription events.
//
// All menu mutations after construction run on the UI thread (see apply):
// Wails menu items update the native Win32/AppKit menu directly.
type trayController struct {
	app         *application.App
	store       *store.Store
	window      *application.WebviewWindow
	proxy       *proxyservice.ProxyService
	settingsSvc *settingsservice.SettingsService
	appSvc      *appservice.AppService

	tray *application.SystemTray
	menu *application.Menu

	startItem   *application.MenuItem
	stopItem    *application.MenuItem
	restartItem *application.MenuItem
	modeMenu    *application.Menu
	modeItems   map[string]*application.MenuItem
	nodesItem   *application.MenuItem
	nodesMenu   *application.Menu
	checkItem   *application.MenuItem

	// nodesSignature detects changes to the enabled node list so the submenu is
	// only rebuilt when its contents actually changed.
	nodesSignature string

	// lastState keeps the latest proxy state snapshot so the tooltip can be
	// re-rendered when the settings change without a new proxy event.
	lastState model.ProxyState
	// lastIconStatus caches the status whose color is currently shown in the
	// tray icon so repeated state events do not re-encode and re-apply the icon.
	lastIconStatus string
}

// newTray creates the system tray, its menu and the close/minimize-to-tray
// behavior. It must run after the main window has been created and before
// app.Run; Wails creates the tray icon itself once the application starts.
func newTray(
	app *application.App,
	st *store.Store,
	window *application.WebviewWindow,
	proxy *proxyservice.ProxyService,
	settingsSvc *settingsservice.SettingsService,
	appSvc *appservice.AppService,
) *trayController {
	if app == nil || window == nil {
		return nil
	}

	tray := &trayController{
		app:         app,
		store:       st,
		window:      window,
		proxy:       proxy,
		settingsSvc: settingsSvc,
		appSvc:      appSvc,
	}
	tray.tray = app.SystemTray.New()
	tray.tray.SetIcon(trayIconForStatus(""))
	tray.lastIconStatus = "stopped"
	tray.tray.SetTooltip(trayTooltipStopped)
	tray.buildMenu()
	tray.refreshMenu()
	tray.tray.OnClick(tray.showWindow)
	tray.installWindowBehavior()
	tray.listen()
	return tray
}

// buildMenu assembles the static tray menu. Dynamic labels and the node/mode
// submenu contents are refreshed later by the event listeners.
func (t *trayController) buildMenu() {
	menu := application.NewMenu()

	menu.Add("显示主窗口").OnClick(func(*application.Context) { t.showWindow() })
	menu.Add("隐藏主窗口").OnClick(func(*application.Context) { t.hideWindow() })

	menu.AddSeparator()

	t.startItem = menu.Add("启动代理")
	t.startItem.OnClick(func(*application.Context) { t.startProxy() })

	t.stopItem = menu.Add("停止代理")
	t.stopItem.OnClick(func(*application.Context) { t.stopProxy() })

	t.restartItem = menu.Add("重启代理")
	t.restartItem.OnClick(func(*application.Context) { t.restartProxy() })

	t.modeMenu = menu.AddSubmenu("代理模式")
	t.modeItems = make(map[string]*application.MenuItem, len(hotkey.ProxyModes()))
	for _, mode := range hotkey.ProxyModes() {
		item := t.modeMenu.AddRadio(hotkey.ProxyModeLabel(mode), false)
		item.OnClick(func(*application.Context) { t.setProxyMode(mode) })
		t.modeItems[mode] = item
	}

	menu.AddSeparator()

	t.nodesMenu = menu.AddSubmenu("节点")
	t.nodesItem = menu.FindByLabel("节点")

	menu.AddSeparator()

	t.checkItem = menu.Add("检查更新")
	t.checkItem.OnClick(func(*application.Context) { t.checkForUpdates() })

	menu.Add("退出").OnClick(func(*application.Context) { t.app.Quit() })

	t.menu = menu
	t.applyIdleState()
	t.applySettings(t.storeSettings())
	t.syncNodes(t.activeNodeID())
}

// applyIdleState resets the menu state shown before the first proxy event.
func (t *trayController) applyIdleState() {
	if t.startItem != nil {
		t.startItem.SetEnabled(true)
	}
	if t.stopItem != nil {
		t.stopItem.SetEnabled(false)
	}
	if t.restartItem != nil {
		t.restartItem.SetEnabled(false)
	}
}

// listen subscribes to the events that drive the dynamic tray state.
func (t *trayController) listen() {
	if t.app == nil {
		return
	}
	t.app.Event.On("proxy:state", func(event *application.CustomEvent) {
		state, ok := event.Data.(model.ProxyState)
		if !ok {
			return
		}
		t.apply(func() { t.applyProxyState(state) })
	})
	t.app.Event.On("subscriptions:changed", func(*application.CustomEvent) {
		activeID := t.activeNodeID()
		t.apply(func() {
			t.syncNodes(activeID)
			t.refreshMenu()
		})
	})
	// Any surface can save the settings (settings page, tray mode items, global
	// hotkeys). Following the broadcast keeps the mode check marks and the
	// tooltip correct without re-reading the store on a timer.
	t.app.Event.On("settings:changed", func(event *application.CustomEvent) {
		settings, ok := event.Data.(model.Settings)
		if !ok {
			return
		}
		t.apply(func() { t.applySettings(settings) })
	})
}

// apply runs fn on the UI thread, where the native menus and tray must be
// mutated.
func (t *trayController) apply(fn func()) {
	if t == nil || t.app == nil || fn == nil {
		return
	}
	application.InvokeAsync(fn)
}

// applyProxyState refreshes the tooltip and menu items from a proxy state
// snapshot.
func (t *trayController) applyProxyState(state model.ProxyState) {
	t.lastState = state
	t.updateIcon(state.Status)

	// The mode radio items follow the persisted setting, while the tooltip uses
	// the mode the running core reports when it has one.
	settings := t.storeSettings()
	t.applyModeChecks(settings.ProxyMode)
	t.refreshTooltip(settings)

	busy := state.Status == "starting" || state.Status == "running" || state.Status == "stopping"
	if t.startItem != nil {
		t.startItem.SetEnabled(!busy)
	}
	if t.stopItem != nil {
		t.stopItem.SetEnabled(state.Status == "running" || state.Status == "starting")
	}
	if t.restartItem != nil {
		// "重启代理" is only meaningful once the proxy is actually running.
		t.restartItem.SetEnabled(state.Status == "running")
	}

	activeNodeID := strings.TrimSpace(state.NodeID)
	nodeName := strings.TrimSpace(state.NodeName)
	if activeNodeID == "" {
		if node, ok := t.storeActiveNode(); ok {
			activeNodeID = node.ID
			if nodeName == "" {
				nodeName = node.Name
			}
		}
	}
	if t.nodesItem != nil {
		if nodeName != "" {
			t.nodesItem.SetLabel("节点（当前：" + truncateLabel(nodeName, trayLabelWidth) + "）")
		} else {
			t.nodesItem.SetLabel("节点")
		}
	}

	t.syncNodes(activeNodeID)
	t.refreshMenu()
}

// refreshMenu publishes the current menu state to the tray. Calling SetMenu
// again is the cross-platform refresh path: Windows rebuilds its native popup,
// macOS rebuilds the NSMenu and Linux rebuilds the DBus menu.
func (t *trayController) refreshMenu() {
	if t == nil || t.tray == nil || t.menu == nil {
		return
	}
	t.tray.SetMenu(t.menu)
}

// syncNodes replaces the node submenu with the first enabled nodes and marks
// the active one. It reports whether the submenu changed; callers refresh the
// tray menu afterwards.
func (t *trayController) syncNodes(activeNodeID string) bool {
	if t.nodesMenu == nil {
		return false
	}
	nodes := trayNodes(t.store)
	signature := activeNodeID
	for _, node := range nodes {
		signature += "|" + node.ID
	}
	if signature == t.nodesSignature {
		return false
	}
	t.nodesSignature = signature

	t.nodesMenu.Clear()
	if len(nodes) == 0 {
		empty := t.nodesMenu.Add("暂无启用的节点")
		empty.SetEnabled(false)
	} else {
		for _, node := range nodes {
			item := node
			label := strings.TrimSpace(item.Name)
			if label == "" {
				label = item.Address
			}
			if item.ID == activeNodeID {
				label = "● " + label
			}
			t.nodesMenu.Add(truncateLabel(label, trayLabelWidth)).OnClick(func(*application.Context) {
				t.switchNode(item.ID)
			})
		}
	}
	return true
}

// showWindow brings the main window back: it restores a minimized window and
// re-shows one hidden to the tray.
func (t *trayController) showWindow() {
	if t == nil || t.window == nil {
		return
	}
	t.window.Show()
	t.window.Focus()
}

// hideWindow hides the main window; the tray icon keeps the application
// running.
func (t *trayController) hideWindow() {
	if t == nil || t.window == nil {
		return
	}
	t.window.Hide()
}

// startProxy starts the local proxy from the tray menu. State changes are
// broadcast by ProxyService as "proxy:state" and update the menu again.
func (t *trayController) startProxy() {
	if t == nil || t.proxy == nil {
		return
	}
	if _, err := t.proxy.Start(); err != nil {
		t.emitLog("error", "从托盘启动代理失败："+err.Error())
	}
}

// stopProxy stops the local proxy from the tray menu.
func (t *trayController) stopProxy() {
	if t == nil || t.proxy == nil {
		return
	}
	if _, err := t.proxy.Stop(); err != nil {
		t.emitLog("error", "从托盘停止代理失败："+err.Error())
	}
}

// switchNode activates the selected node. A running proxy is restarted by
// ProxyService so the new node takes effect immediately.
func (t *trayController) switchNode(id string) {
	if t == nil || t.proxy == nil || strings.TrimSpace(id) == "" {
		return
	}
	state, err := t.proxy.SwitchNode(id)
	if err != nil {
		t.emitLog("error", "从托盘切换节点失败："+err.Error())
		return
	}
	// A switch while the proxy is stopped changes only the persisted active
	// node and emits no proxy:state event, so refresh the menu from the
	// returned snapshot as well.
	t.apply(func() { t.applyProxyState(state) })
}

// restartProxy restarts a running proxy from the tray menu. The menu item is
// disabled while the proxy is stopped, so clicking it only happens while a
// restart is meaningful.
func (t *trayController) restartProxy() {
	if t == nil || t.proxy == nil {
		return
	}
	state, err := t.proxy.Restart()
	if err != nil {
		t.emitLog("error", "从托盘重启代理失败："+err.Error())
		return
	}
	// Restart emits proxy:state while the proxy is running; applying the
	// snapshot here also covers the (disabled) stopped case.
	t.apply(func() { t.applyProxyState(state) })
}

// setProxyMode persists the requested proxy mode and restarts a running proxy
// so the new routing mode takes effect immediately. The settings service
// validates the payload and broadcasts "settings:changed", which refreshes the
// check marks and the tooltip through the listener installed in listen.
func (t *trayController) setProxyMode(mode string) {
	if t == nil || t.store == nil || t.settingsSvc == nil {
		return
	}
	if _, known := t.modeItems[mode]; !known {
		return
	}
	settings := t.store.Settings()
	if settings.ProxyMode == mode {
		t.apply(func() { t.applySettings(settings) })
		return
	}
	settings.ProxyMode = mode
	saved, err := t.settingsSvc.Save(settings)
	if err != nil {
		t.emitLog("error", "从托盘切换代理模式失败："+err.Error())
		// A rejected save leaves the persisted mode untouched; re-sync the
		// check marks with it.
		t.apply(func() { t.applySettings(t.storeSettings()) })
		return
	}
	t.emitLog("info", "代理模式已切换为"+hotkey.ProxyModeLabel(saved.ProxyMode))
	t.apply(func() { t.applySettings(saved) })

	if t.proxy == nil {
		return
	}
	state := t.proxy.State()
	if state.Status != "running" && state.Status != "starting" {
		return
	}
	if _, err := t.proxy.Restart(); err != nil {
		t.emitLog("error", "切换代理模式后重启代理失败："+err.Error())
	}
}

// storeSettings returns the persisted settings, tolerating a nil store during
// construction.
func (t *trayController) storeSettings() model.Settings {
	if t == nil || t.store == nil {
		return model.Settings{}
	}
	return t.store.Settings()
}

// applySettings refreshes everything that depends on the persisted settings:
// the checked proxy mode radio item and the tooltip.
func (t *trayController) applySettings(settings model.Settings) {
	if t == nil {
		return
	}
	t.applyModeChecks(settings.ProxyMode)
	t.refreshTooltip(settings)
}

// applyModeChecks marks the radio item matching mode as checked and every other
// mode item as unchecked.
func (t *trayController) applyModeChecks(mode string) {
	for itemMode, item := range t.modeItems {
		if item == nil {
			continue
		}
		item.SetChecked(itemMode == mode)
	}
}

// refreshTooltip renders the tooltip from the last proxy state plus the current
// settings. The running state's own mode wins while the proxy is active because
// it describes what the core is actually doing.
func (t *trayController) refreshTooltip(settings model.Settings) {
	if t == nil || t.tray == nil {
		return
	}
	mode := strings.TrimSpace(t.lastState.ProxyMode)
	if mode == "" {
		mode = settings.ProxyMode
	}
	t.tray.SetTooltip(proxyTooltip(t.lastState, mode))
}

// checkForUpdates runs the application update check from the tray menu. The
// result is broadcast by AppService as "app:update" (model.UpdateInfo); the main
// window is shown so the user can see the result.
func (t *trayController) checkForUpdates() {
	if t == nil || t.appSvc == nil {
		return
	}
	t.apply(func() {
		if t.checkItem != nil {
			t.checkItem.SetEnabled(false)
			t.checkItem.SetLabel("检查更新中…")
		}
		t.refreshMenu()
	})

	info, err := t.appSvc.CheckUpdate()

	t.apply(func() {
		if t.checkItem != nil {
			t.checkItem.SetEnabled(true)
			if info.Available {
				t.checkItem.SetLabel("检查更新（发现新版本 v" + info.LatestVersion + "）")
			} else {
				t.checkItem.SetLabel("检查更新")
			}
		}
		t.refreshMenu()
	})

	switch {
	case err != nil:
		t.emitLog("error", "检查应用更新失败："+err.Error())
	case info.Available:
		t.emitLog("info", "发现 FastV 新版本 v"+info.LatestVersion)
	default:
		t.emitLog("info", "FastV 已是最新版本")
	}
	t.showWindow()
}

// installWindowBehavior wires close-to-tray and minimize-to-tray.
//
// Wails v3.1.0 exposes synchronous, cancellable window event hooks
// (WebviewWindow.RegisterHook): cancelling WindowClosing prevents the
// framework's own close listener from destroying the window, which is exactly
// what close-to-tray needs (the framework tests this behavior in
// webview_window_close_test.go).
//
// There is no cancellable minimize event: the platforms emit WindowMinimise
// after the native window is already minimized, so a hook cannot veto it.
// Minimize-to-tray is therefore best effort: the window is hidden as soon as
// the (asynchronous) minimize notification arrives.
func (t *trayController) installWindowBehavior() {
	if t == nil || t.window == nil || t.store == nil {
		return
	}
	t.window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !t.store.Settings().CloseToTray {
			return
		}
		event.Cancel()
		t.window.Hide()
	})
	t.window.OnWindowEvent(events.Common.WindowMinimise, func(*application.WindowEvent) {
		if t.store.Settings().MinimizeToTray {
			t.window.Hide()
		}
	})
}

// emitLog publishes a proxy log entry so tray-triggered failures show up in the
// in-app log view.
func (t *trayController) emitLog(level, message string) {
	if t == nil || t.app == nil {
		return
	}
	t.app.Event.Emit("proxy:log", model.LogEntry{
		ID:        model.NewID(),
		Timestamp: model.NowRFC3339(),
		Level:     level,
		Source:    "tray",
		Message:   message,
	})
}

// activeNodeID returns the persisted active node id, if any.
func (t *trayController) activeNodeID() string {
	if node, ok := t.storeActiveNode(); ok {
		return node.ID
	}
	return ""
}

func (t *trayController) storeActiveNode() (model.Node, bool) {
	if t == nil || t.store == nil {
		return model.Node{}, false
	}
	return t.store.ActiveNode()
}

// trayNodes returns the first enabled nodes in the user-defined order.
func trayNodes(st *store.Store) []model.Node {
	if st == nil {
		return nil
	}
	nodes := st.ListNodes()
	enabled := make([]model.Node, 0, min(len(nodes), maxTrayNodeEntries))
	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		enabled = append(enabled, node)
		if len(enabled) == maxTrayNodeEntries {
			break
		}
	}
	return enabled
}

// proxyTooltip renders the tray tooltip for the current proxy state, including
// the active proxy mode while the proxy is starting or running (for example
// 「FastV - 代理运行中：香港01 · 规则分流」).
func proxyTooltip(state model.ProxyState, mode string) string {
	name := strings.TrimSpace(state.NodeName)
	modeLabel := ""
	if strings.TrimSpace(mode) != "" {
		modeLabel = hotkey.ProxyModeLabel(mode)
	}
	switch state.Status {
	case "starting":
		if modeLabel != "" {
			return "FastV - 正在启动代理 · " + modeLabel
		}
		return "FastV - 正在启动代理"
	case "running":
		parts := make([]string, 0, 2)
		if name != "" {
			parts = append(parts, truncateLabel(name, trayLabelWidth))
		}
		if modeLabel != "" {
			parts = append(parts, modeLabel)
		}
		if len(parts) > 0 {
			return "FastV - 代理运行中：" + strings.Join(parts, " · ")
		}
		return "FastV - 代理运行中"
	case "stopping":
		return "FastV - 正在停止代理"
	case "error":
		message := strings.TrimSpace(state.Error)
		if message != "" {
			return "FastV - 代理异常：" + truncateLabel(message, 60)
		}
		return "FastV - 代理异常"
	default:
		return trayTooltipStopped
	}
}

// updateIcon paints the tray icon according to the proxy status so the mode and
// health of the proxy are visible at a glance: green when running, amber while
// starting/stopping, red for errors and blue when stopped.
func (t *trayController) updateIcon(status string) {
	if t == nil || t.tray == nil {
		return
	}
	if status == "" {
		status = "stopped"
	}
	if status == t.lastIconStatus {
		return
	}
	t.lastIconStatus = status
	t.tray.SetIcon(trayIconForStatus(status))
}

// trayIconForStatus returns the PNG bytes for one proxy status.
func trayIconForStatus(status string) []byte {
	var background color.NRGBA
	switch status {
	case "running":
		background = color.NRGBA{R: 0x2f, G: 0x9e, B: 0x63, A: 0xff}
	case "starting", "stopping":
		background = color.NRGBA{R: 0xc0, G: 0x8a, B: 0x2e, A: 0xff}
	case "error":
		background = color.NRGBA{R: 0xc9, G: 0x4b, B: 0x4b, A: 0xff}
	default:
		background = color.NRGBA{R: 0x2f, G: 0x6f, B: 0xe8, A: 0xff}
	}
	return trayIconPNG(background)
}

// truncateLabel shortens value to limit runes, appending an ellipsis.
func truncateLabel(value string, limit int) string {
	runes := []rune(value)
	if limit <= 0 || len(runes) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

// trayIconPNG renders the 32x32 tray icon in code, so the build needs no binary
// asset and SetIcon always receives a valid, non-empty PNG.
func trayIconPNG(background color.NRGBA) []byte {
	const size = 32
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	foreground := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	center := float64(size-1) / 2
	const radius = 15.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - center
			dy := float64(y) - center
			if dx*dx+dy*dy <= radius*radius {
				icon.SetNRGBA(x, y, background)
			}
		}
	}
	// A white "V" mark: two thick strokes meeting at the bottom.
	drawThickSegment(icon, 10, 10, 16, 22, 2.4, foreground)
	drawThickSegment(icon, 22, 10, 16, 22, 2.4, foreground)

	var encoded bytes.Buffer
	// Encoding an in-memory NRGBA image into a bytes.Buffer cannot fail; the
	// icon is built exactly once at startup.
	_ = png.Encode(&encoded, icon)
	return encoded.Bytes()
}

// drawThickSegment paints a rounded stroke from (x1,y1) to (x2,y2).
func drawThickSegment(img *image.NRGBA, x1, y1, x2, y2, halfWidth float64, c color.NRGBA) {
	minX := int(min(float64(x1), float64(x2)) - halfWidth - 1)
	maxX := int(max(float64(x1), float64(x2)) + halfWidth + 1)
	minY := int(min(float64(y1), float64(y2)) - halfWidth - 1)
	maxY := int(max(float64(y1), float64(y2)) + halfWidth + 1)

	dx := x2 - x1
	dy := y2 - y1
	lengthSquared := dx*dx + dy*dy
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
				continue
			}
			// Distance from the pixel centre to the segment.
			px := float64(x) - x1
			py := float64(y) - y1
			t := 0.0
			if lengthSquared > 0 {
				t = (px*dx + py*dy) / lengthSquared
				t = min(max(t, 0), 1)
			}
			nearestX := x1 + t*dx
			nearestY := y1 + t*dy
			offsetX := float64(x) - nearestX
			offsetY := float64(y) - nearestY
			if offsetX*offsetX+offsetY*offsetY <= halfWidth*halfWidth {
				img.SetNRGBA(x, y, c)
			}
		}
	}
}
