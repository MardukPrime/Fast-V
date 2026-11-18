package bootstrap

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/deeplink"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/service/node"
	"github.com/MardukPrime/Fast-V/internal/service/subscription"
)

const (
	// deepLinkImportedEvent is emitted after a deep link was imported
	// successfully; its payload is a deepLinkEvent.
	deepLinkImportedEvent = "deeplink:imported"
	// deepLinkFailedEvent is emitted when a deep link could not be imported.
	deepLinkFailedEvent = "deeplink:failed"
	// deepLinkStartupDelay gives the Wails services and the frontend time to
	// finish starting before a launch deep link is imported, so the emitted
	// events reach a ready UI.
	deepLinkStartupDelay = time.Second
	// deepLinkLogSource labels deep link diagnostics in the in-app log view.
	deepLinkLogSource = "deeplink"
	// subscriptionsChangedEvent is the existing event the subscriptions page
	// listens to for reloading its list.
	subscriptionsChangedEvent = "subscriptions:changed"
)

// deepLinkEvent is the payload of the deeplink:imported and deeplink:failed
// events consumed by the frontend.
type deepLinkEvent struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

// deepLinkOutcome describes what one imported deep link produced.
type deepLinkOutcome struct {
	Name    string
	Message string
}

// deepLinkImporter applies one parsed deep link. The production implementation
// wraps the subscription and node services; unit tests inject a fake.
type deepLinkImporter interface {
	Import(request deeplink.Request) (deepLinkOutcome, error)
}

// serviceDeepLinkImporter connects the deep link handler to the real services.
type serviceDeepLinkImporter struct {
	subs  *subscription.Service
	nodes *node.NodeService
}

// Import applies one parsed deep link through the subscription or node service.
func (i serviceDeepLinkImporter) Import(request deeplink.Request) (deepLinkOutcome, error) {
	switch request.Kind {
	case deeplink.KindSubscription:
		if i.subs == nil {
			return deepLinkOutcome{}, errors.New("订阅服务未就绪")
		}
		view, err := i.subs.Add(model.SubscriptionInput{URL: request.URL})
		if err != nil {
			return deepLinkOutcome{}, err
		}
		return deepLinkOutcome{
			Name:    view.Name,
			Message: importMessage(request.Kind, view.Name),
		}, nil

	case deeplink.KindNode:
		if i.nodes == nil {
			return deepLinkOutcome{}, errors.New("节点服务未就绪")
		}
		result, err := i.nodes.ImportLinks(request.URL)
		if err != nil {
			return deepLinkOutcome{}, err
		}
		if result.Imported > 0 {
			name := ""
			if len(result.Nodes) > 0 {
				name = result.Nodes[0].Name
			}
			return deepLinkOutcome{
				Name:    name,
				Message: importMessage(request.Kind, name),
			}, nil
		}
		if result.Skipped > 0 {
			return deepLinkOutcome{Message: "节点已存在，已跳过重复导入"}, nil
		}
		if len(result.Errors) > 0 {
			return deepLinkOutcome{}, errors.New(strings.Join(result.Errors, "；"))
		}
		return deepLinkOutcome{}, errors.New("分享链接未解析出可用节点")

	default:
		return deepLinkOutcome{}, fmt.Errorf("不支持的深链接类型: %s", request.Kind)
	}
}

// importMessage builds the success message shown for one imported item.
func importMessage(kind string, name string) string {
	label := "项"
	switch kind {
	case deeplink.KindSubscription:
		label = "订阅"
	case deeplink.KindNode:
		label = "节点"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return label + "已导入"
	}
	return label + "「" + name + "」已导入"
}

// HandleDeepLink parses one fastv:// deep link and applies it to the
// subscription or node service. Success and failure are broadcast to the
// frontend as deeplink:imported / deeplink:failed events. It never panics: event
// emission is skipped when the Wails application is not available yet (for
// example in unit tests), and a nil service is reported as an error.
func HandleDeepLink(raw string, subs *subscription.Service, nodes *node.NodeService) error {
	return handleDeepLink(raw, serviceDeepLinkImporter{subs: subs, nodes: nodes})
}

// handleDeepLink is the testable core of HandleDeepLink: it takes any
// deepLinkImporter so tests can avoid the real services.
func handleDeepLink(raw string, importer deepLinkImporter) error {
	request, err := deeplink.Parse(raw)
	if err != nil {
		return failDeepLink("", err)
	}
	return applyDeepLink(request, importer)
}

// applyDeepLink imports one parsed request and emits the result events.
func applyDeepLink(request deeplink.Request, importer deepLinkImporter) error {
	if importer == nil {
		return failDeepLink(request.Kind, errors.New("深链接导入器未就绪"))
	}
	outcome, err := importer.Import(request)
	if err != nil {
		return failDeepLink(request.Kind, err)
	}
	emitEvent(deepLinkImportedEvent, deepLinkEvent{
		Kind:    request.Kind,
		Name:    outcome.Name,
		Message: outcome.Message,
	})
	emitEvent("proxy:log", model.LogEntry{
		ID:        model.NewID(),
		Timestamp: model.NowRFC3339(),
		Level:     "info",
		Source:    deepLinkLogSource,
		Message:   outcome.Message,
	})
	if request.Kind == deeplink.KindSubscription {
		// The subscriptions page reloads its list when this existing event
		// arrives, so a deep link import is visible without a page refresh.
		emitEvent(subscriptionsChangedEvent)
	}
	return nil
}

// failDeepLink broadcasts and logs an import failure, then returns err so the
// caller can decide whether to surface it.
func failDeepLink(kind string, err error) error {
	message := "深链接导入失败"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	} else {
		err = errors.New(message)
	}
	emitEvent(deepLinkFailedEvent, deepLinkEvent{Kind: kind, Message: message})
	emitEvent("proxy:log", model.LogEntry{
		ID:        model.NewID(),
		Timestamp: model.NowRFC3339(),
		Level:     "error",
		Source:    deepLinkLogSource,
		Message:   "深链接导入失败：" + message,
	})
	return err
}

// emitEvent broadcasts a Wails event when an application object exists. Deep
// links can be handled very early in the startup path, so a missing application
// must be a no-op instead of a panic.
func emitEvent(name string, data ...any) {
	app := application.Get()
	if app == nil || app.Event == nil {
		return
	}
	app.Event.Emit(name, data...)
}

// deepLinkIntegration wires deep links into the application lifecycle: URL
// protocol registration, deep links passed on the first launch and links
// forwarded by a second instance.
type deepLinkIntegration struct {
	subs  *subscription.Service
	nodes *node.NodeService

	// window is stored atomically because the single instance callback runs on
	// the Wails event goroutine while the main goroutine creates the window.
	window atomic.Pointer[application.WebviewWindow]
}

// newDeepLinkIntegration creates the deep link lifecycle helper for one
// application run.
func newDeepLinkIntegration(subs *subscription.Service, nodes *node.NodeService) *deepLinkIntegration {
	return &deepLinkIntegration{subs: subs, nodes: nodes}
}

// registerURLScheme best-effort registers the fastv:// URL protocol for the
// current executable. Registration is Windows-only and a failure is logged
// instead of aborting startup: deep links may still be imported from the
// command line or a second instance launch.
func (d *deepLinkIntegration) registerURLScheme() {
	if d == nil || runtime.GOOS != "windows" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		log.Printf("deeplink: 获取程序路径失败: %v", err)
		return
	}
	if err := platform.RegisterURLScheme(deeplink.Scheme, executable); err != nil {
		log.Printf("deeplink: 注册 %s:// 协议失败: %v", deeplink.Scheme, err)
	}
}

// setWindow records the main window so a later second instance launch can
// surface it.
func (d *deepLinkIntegration) setWindow(window *application.WebviewWindow) {
	if d == nil {
		return
	}
	d.window.Store(window)
}

// handleStartupArgs imports the deep links passed on the first launch (for
// example when the operating system opened fastv:// for an app that was not
// running). The work is deferred so the Wails services and the frontend finish
// starting before the network import runs.
func (d *deepLinkIntegration) handleStartupArgs() {
	if d == nil {
		return
	}
	requests := parseDeepLinkArgs(os.Args[1:])
	if len(requests) == 0 {
		return
	}
	go func() {
		time.Sleep(deepLinkStartupDelay)
		d.applyAll(requests)
	}()
}

// onSecondInstanceLaunch is wired to SingleInstance.OnSecondInstanceLaunch. It
// surfaces the main window and imports the links forwarded by the second
// instance.
func (d *deepLinkIntegration) onSecondInstanceLaunch(data application.SecondInstanceData) {
	if d == nil {
		return
	}
	d.showWindow()
	requests := parseDeepLinkArgs(data.Args)
	if len(requests) == 0 {
		return
	}
	go d.applyAll(requests)
}

// applyAll imports every request in order. Failures are already broadcast to
// the frontend and logged; the next request is still attempted.
func (d *deepLinkIntegration) applyAll(requests []deeplink.Request) {
	importer := serviceDeepLinkImporter{subs: d.subs, nodes: d.nodes}
	for _, request := range requests {
		if err := applyDeepLink(request, importer); err != nil {
			log.Printf("deeplink: 导入失败: %v", err)
		}
	}
}

// showWindow brings the main window to the front, if it has been created yet.
func (d *deepLinkIntegration) showWindow() {
	window := d.window.Load()
	if window == nil {
		return
	}
	window.Show()
	window.Focus()
}

// parseDeepLinkArgs parses every command line argument that is a deep link.
// Arguments that are not deep links (the executable path, flags, file paths)
// are ignored; malformed arguments that still look like FastV links are logged
// so a broken handler registration is diagnosable. Duplicate links are only
// imported once.
func parseDeepLinkArgs(args []string) []deeplink.Request {
	requests := make([]deeplink.Request, 0, len(args))
	seen := make(map[string]struct{}, len(args))
	for _, arg := range args {
		request, err := deeplink.Parse(arg)
		if err != nil {
			if looksLikeFastVLink(arg) {
				log.Printf("deeplink: 忽略无效参数 %q: %v", arg, err)
			}
			continue
		}
		key := request.Kind + "\x00" + request.URL
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		requests = append(requests, request)
	}
	return requests
}

// looksLikeFastVLink reports whether an unparsable argument was intended to be
// a FastV deep link.
func looksLikeFastVLink(value string) bool {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"'`)
	return strings.HasPrefix(strings.ToLower(trimmed), deeplink.Scheme+":")
}
