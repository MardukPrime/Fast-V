// Package rescue implements the one-click network rescue used by the
// diagnostics page.
//
// It detects the most common "no internet after using FastV" causes (leftover
// system proxy, leftover WinHTTP proxy, poisoned DNS cache, a busy local
// inbound port, a broken core) and repairs the safe subset of them. Every
// operating-system effect goes through the injected Platform interface and
// every proxy lifecycle effect goes through ProxyController, so the detection
// and repair logic is fully testable without touching the real registry, the
// real network stack or the real core.
package rescue

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// serviceName identifies the service in the Wails bindings.
	serviceName = "RescueService"

	severityOK      = "ok"
	severityWarning = "warning"
	severityError   = "error"

	proxyStatusRunning  = "running"
	proxyStatusStarting = "starting"
	proxyStatusError    = "error"

	// defaultLocalPort mirrors store.DefaultSettings().XrayLocalPort and is only
	// used when neither the proxy state nor the store yields a usable port.
	defaultLocalPort = 10808

	// Issue identifiers. They are stable values used by the frontend to group or
	// filter findings; the human-readable text is Title/Detail.
	issueNetworkOK           = "network-ok"
	issueSystemProxySkipped  = "system-proxy-skipped"
	issueSystemProxyOrphan   = "system-proxy-orphan"
	issueSystemProxyMismatch = "system-proxy-mismatch"
	issuePortSkipped         = "port-check-skipped"
	issuePortInUse           = "port-in-use"
	issueWinHTTPSkipped      = "winhttp-proxy-skipped"
	issueWinHTTPProxy        = "winhttp-proxy"
	issueCoreError           = "core-error"
	issueProxyStarting       = "proxy-starting"
)

// Platform abstracts the operating-system rescue primitives. defaultPlatform
// wraps the package-level platform functions; tests inject a fake.
type Platform interface {
	ReadSystemProxyState() (platform.SystemProxyState, error)
	ReadWinHTTPProxy() (string, error)
	ResetSystemProxy() error
	FlushDNS() error
	PortInUse(port int) (bool, error)
}

// ProxyController is the slice of ProxyService the rescue service depends on.
// ProxyService implements it directly (State + Restart).
type ProxyController interface {
	State() model.ProxyState
	Restart() (model.ProxyState, error)
}

// RescueIssue is one detected network problem, or a positive/skipped check.
type RescueIssue struct {
	ID       string `json:"id"`
	Severity string `json:"severity"` // ok | warning | error
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Fixable  bool   `json:"fixable"`
}

// RescueReport is the outcome of one Inspect run.
type RescueReport struct {
	Issues             []RescueIssue `json:"issues"`
	SystemProxyEnabled bool          `json:"systemProxyEnabled"`
	SystemProxyServer  string        `json:"systemProxyServer"`
	ProxyRunning       bool          `json:"proxyRunning"`
	CheckedAt          string        `json:"checkedAt"`
}

// RescueResult is the outcome of one Repair run. Fixed and Failed contain the
// stable Chinese operation names ("重置系统代理", "刷新 DNS 缓存",
// "重启本地代理").
type RescueResult struct {
	Fixed   []string     `json:"fixed"`
	Failed  []string     `json:"failed"`
	Message string       `json:"message"`
	Report  RescueReport `json:"report"`
}

// Service is the Wails adapter for the one-click network rescue.
type Service struct {
	store *store.Store
	proxy ProxyController
	ops   Platform
	now   func() time.Time
}

// New creates the rescue service backed by the real platform primitives and
// the running proxy service.
func New(st *store.Store, proxyController ProxyController) *Service {
	return NewWithPlatform(st, proxyController, defaultPlatform{})
}

// NewWithPlatform is New with an injected platform implementation. It exists so
// tests can replace the OS operations; production code uses New.
func NewWithPlatform(st *store.Store, proxyController ProxyController, ops Platform) *Service {
	return &Service{store: st, proxy: proxyController, ops: ops, now: time.Now}
}

// ServiceName identifies the service in the Wails bindings.
func (s *Service) ServiceName() string {
	return serviceName
}

// Inspect reports every detected network problem. It is best-effort: an
// unavailable platform capability (for example the WinHTTP query on a system
// where netsh is missing) becomes an "ok" issue explaining the skip instead of
// failing the whole inspection.
func (s *Service) Inspect() (RescueReport, error) {
	report := RescueReport{
		Issues:    []RescueIssue{},
		CheckedAt: s.clock().Format(time.RFC3339),
	}
	add := func(id, severity, title, detail string, fixable bool) {
		report.Issues = append(report.Issues, RescueIssue{
			ID:       id,
			Severity: severity,
			Title:    title,
			Detail:   detail,
			Fixable:  fixable,
		})
	}

	state := model.ProxyState{}
	if s.proxy != nil {
		state = s.proxy.State()
	}
	running := state.Status == proxyStatusRunning
	starting := state.Status == proxyStatusStarting
	report.ProxyRunning = running

	port := localInboundPort(s.store, state)
	localAddr := localInboundAddr(state, port)
	ops := s.platform()

	// a) A system proxy that points at a local inbound that is not running —
	// the classic "FastV exited but Windows still routes through the dead
	// proxy" failure.
	systemProxy, proxyErr := ops.ReadSystemProxyState()
	if proxyErr != nil {
		add(issueSystemProxySkipped, severityOK, "系统代理检查已跳过",
			"无法读取系统代理设置："+proxyErr.Error(), false)
	} else {
		report.SystemProxyEnabled = systemProxy.Enabled
		report.SystemProxyServer = systemProxy.Server
		switch {
		case systemProxy.Enabled && !running && !starting:
			detail := fmt.Sprintf("系统代理指向 %s，但 FastV 本地代理未运行，可能导致无法上网。",
				proxyServerText(systemProxy.Server))
			add(issueSystemProxyOrphan, severityWarning, "系统代理残留", detail, true)
		case systemProxy.Enabled && running && !systemProxyMatchesInbound(systemProxy.Server, localAddr):
			// b) The proxy is running but Windows points somewhere else (another
			// port, or an address left over from a previous port setting).
			detail := fmt.Sprintf("系统代理地址为 %s，与本地入站地址 %s 不一致，部分程序可能无法联网。",
				proxyServerText(systemProxy.Server), localAddr)
			add(issueSystemProxyMismatch, severityWarning, "系统代理地址不一致", detail, false)
		}
	}

	// c) The local inbound port is only inspected while the proxy is not using
	// it; otherwise a busy port is expected and must not be reported.
	if !running && !starting {
		if inUse, err := ops.PortInUse(port); err != nil {
			add(issuePortSkipped, severityOK, "端口占用检查已跳过",
				fmt.Sprintf("无法检测本地入站端口 %d：%v", port, err), false)
		} else if inUse {
			detail := fmt.Sprintf("端口 %d 已被其他程序占用，代理无法启动；请关闭占用该端口的程序或在设置中更换本地端口。", port)
			add(issuePortInUse, severityError, "本地入站端口被占用", detail, false)
		}
	}

	// d) WinHTTP keeps a machine-wide proxy separate from WinINet; stale values
	// break Windows Update and other system components even when the browser
	// proxy is already fixed.
	winHTTPProxy, winHTTPErr := ops.ReadWinHTTPProxy()
	switch {
	case winHTTPErr != nil:
		add(issueWinHTTPSkipped, severityOK, "WinHTTP 代理检查已跳过",
			"无法查询 WinHTTP 代理设置："+winHTTPErr.Error(), false)
	case strings.TrimSpace(winHTTPProxy) != "":
		detail := fmt.Sprintf("WinHTTP 代理仍指向 %s，系统更新和部分应用可能无法联网。",
			strings.TrimSpace(winHTTPProxy))
		add(issueWinHTTPProxy, severityWarning, "WinHTTP 代理残留", detail, !running && !starting)
	}

	// e) Core state reported by the proxy service itself.
	switch {
	case starting:
		add(issueProxyStarting, severityOK, "本地代理正在启动",
			"本地代理尚未就绪，端口占用和核心状态检查将在启动完成后生效。", false)
	case running:
		switch {
		case strings.TrimSpace(state.Error) != "":
			add(issueCoreError, severityError, "本地代理核心异常",
				"本地代理虽在运行，但核心报告错误："+state.Error+"。", true)
		case state.LocalAddr == "":
			add(issueCoreError, severityError, "本地代理核心状态异常",
				"本地代理状态缺少本地入站地址，可能无法转发流量。", true)
		}
	case state.Status == proxyStatusError:
		reason := strings.TrimSpace(state.Error)
		if reason == "" {
			reason = "代理核心未能启动"
		}
		add(issueCoreError, severityError, "本地代理核心异常",
			"本地代理核心处于异常状态："+reason+"。", false)
	}

	// f) Nothing wrong: return exactly one positive issue.
	if !hasProblem(report.Issues) {
		add(issueNetworkOK, severityOK, "网络环境正常",
			"未检测到系统代理残留、端口占用或核心异常。", false)
	}
	return report, nil
}

// Repair applies the safe subset of fixes:
//
//   - proxy not running: reset the system/WinHTTP proxy and flush DNS;
//   - proxy running: flush DNS and restart the core (the system proxy is left
//     untouched because it still points at a valid local inbound).
//
// Repair is best-effort and never returns an error for individual failures;
// they are recorded in Failed so the UI can show what still needs attention.
func (s *Service) Repair() (RescueResult, error) {
	report, _ := s.Inspect()
	fixed := []string{}
	failed := []string{}

	status := ""
	if s.proxy != nil {
		status = s.proxy.State().Status
	}
	if status == proxyStatusStarting {
		return RescueResult{
			Fixed:   fixed,
			Failed:  failed,
			Message: "本地代理正在启动，请稍后重试网络救援。",
			Report:  report,
		}, nil
	}

	ops := s.platform()
	if report.ProxyRunning {
		if err := ops.FlushDNS(); err != nil {
			failed = append(failed, "刷新 DNS 缓存")
		} else {
			fixed = append(fixed, "刷新 DNS 缓存")
		}
		if s.proxy == nil {
			failed = append(failed, "重启本地代理")
		} else if state, err := s.proxy.Restart(); err != nil || state.Status != proxyStatusRunning {
			failed = append(failed, "重启本地代理")
		} else {
			fixed = append(fixed, "重启本地代理")
		}
	} else {
		if err := ops.ResetSystemProxy(); err != nil {
			failed = append(failed, "重置系统代理")
		} else {
			fixed = append(fixed, "重置系统代理")
			report.SystemProxyEnabled = false
			report.SystemProxyServer = ""
		}
		if err := ops.FlushDNS(); err != nil {
			failed = append(failed, "刷新 DNS 缓存")
		} else {
			fixed = append(fixed, "刷新 DNS 缓存")
		}
	}

	// Re-inspect so the returned report describes the post-repair state.
	if refreshed, err := s.Inspect(); err == nil {
		report = refreshed
	}
	return RescueResult{
		Fixed:   fixed,
		Failed:  failed,
		Message: repairMessage(fixed, failed),
		Report:  report,
	}, nil
}

// defaultPlatform calls the real platform package.
type defaultPlatform struct{}

func (defaultPlatform) ReadSystemProxyState() (platform.SystemProxyState, error) {
	return platform.ReadSystemProxyState()
}

func (defaultPlatform) ReadWinHTTPProxy() (string, error) {
	return platform.ReadWinHTTPProxy()
}

func (defaultPlatform) ResetSystemProxy() error {
	return platform.ResetSystemProxy()
}

func (defaultPlatform) FlushDNS() error {
	return platform.FlushDNS()
}

func (defaultPlatform) PortInUse(port int) (bool, error) {
	return platform.PortInUse(port)
}

func (s *Service) platform() Platform {
	if s.ops != nil {
		return s.ops
	}
	return defaultPlatform{}
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// localInboundPort resolves the local SOCKS/HTTP inbound port from the running
// proxy state first and from the persisted settings second.
func localInboundPort(st *store.Store, state model.ProxyState) int {
	if _, port, err := net.SplitHostPort(strings.TrimSpace(state.LocalAddr)); err == nil {
		if parsed, convErr := strconv.Atoi(port); convErr == nil && parsed > 0 && parsed <= 65535 {
			return parsed
		}
	}
	if st != nil {
		if port := st.Settings().XrayLocalPort; port > 0 && port <= 65535 {
			return port
		}
	}
	return defaultLocalPort
}

// localInboundAddr returns the local inbound address to compare against the
// system proxy, falling back to 127.0.0.1:<port> when the proxy is not running.
func localInboundAddr(state model.ProxyState, port int) string {
	if addr := strings.TrimSpace(state.LocalAddr); addr != "" {
		return addr
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// systemProxyMatchesInbound reports whether any address in a WinINet
// ProxyServer value points at the local inbound (loopback host + same port).
// ProxyServer may be a single host:port or a per-protocol list such as
// "http=127.0.0.1:10808;https=127.0.0.1:10808".
func systemProxyMatchesInbound(server, localAddr string) bool {
	_, expectedPort, err := net.SplitHostPort(strings.TrimSpace(localAddr))
	if err != nil {
		return false
	}
	for _, candidate := range proxyServerAddresses(server) {
		host, port, splitErr := net.SplitHostPort(candidate)
		if splitErr != nil || port != expectedPort {
			continue
		}
		if isLoopbackHost(host) {
			return true
		}
	}
	return false
}

// proxyServerAddresses splits a WinINet ProxyServer value into individual
// host:port candidates, dropping protocol keys and URL schemes.
func proxyServerAddresses(server string) []string {
	fields := strings.FieldsFunc(server, func(r rune) bool {
		return r == ';' || r == ',' || unicode.IsSpace(r)
	})
	addresses := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if idx := strings.Index(field, "="); idx >= 0 {
			field = strings.TrimSpace(field[idx+1:])
		}
		for _, scheme := range []string{"http://", "https://", "socks5://", "socks://"} {
			if strings.HasPrefix(strings.ToLower(field), scheme) {
				field = field[len(scheme):]
			}
		}
		field = strings.TrimSuffix(strings.TrimSpace(field), "/")
		if field != "" {
			addresses = append(addresses, field)
		}
	}
	return addresses
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func proxyServerText(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		return "（空地址）"
	}
	return server
}

func hasProblem(issues []RescueIssue) bool {
	for _, issue := range issues {
		if issue.Severity != severityOK {
			return true
		}
	}
	return false
}

func repairMessage(fixed, failed []string) string {
	switch {
	case len(fixed) == 0 && len(failed) == 0:
		return "未执行任何修复操作。"
	case len(failed) == 0:
		return fmt.Sprintf("网络救援完成，已修复 %d 项。", len(fixed))
	case len(fixed) == 0:
		return fmt.Sprintf("网络救援未完成，%d 项失败。", len(failed))
	default:
		return fmt.Sprintf("网络救援完成，已修复 %d 项，%d 项失败。", len(fixed), len(failed))
	}
}
