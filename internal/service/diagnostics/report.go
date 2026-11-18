package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
)

// Report export limits.
const (
	// reportLogLimit is how many of the most recent proxy log lines are kept.
	reportLogLimit = 200
	// reportSelfCheckTimeout bounds the optional self-check run so exporting a
	// report can never block for longer than the regular self-check budget.
	reportSelfCheckTimeout = 45 * time.Second
	// reportFilePrefix is the archive name prefix returned to the frontend.
	reportFilePrefix = "diagnostics-report-"
)

// reportEntry is one file written into the diagnostics archive.
type reportEntry struct {
	name string
	data []byte
}

// ReportStoreProvider is the slice of *store.Store the diagnostic report needs.
// *store.Store satisfies it directly, which keeps this package free of a store
// import cycle and makes the export testable with a fake.
type ReportStoreProvider interface {
	Settings() model.Settings
	DataDir() string
	ListSubscriptions() []model.Subscription
	ListNodes() []model.Node
	ListRoutingRules() []model.RoutingRule
	ListRuleSets() []model.RuleSet
	ListNodeGroups() []model.NodeGroup
}

// ReportSources wires the optional data providers used by ExportReport. Only
// the local proxy state (given to New) is mandatory; every field here enriches
// the archive when it is available.
type ReportSources struct {
	// Store supplies the redacted settings summary and the record counts.
	Store ReportStoreProvider
	// CoreInfo returns the installed core description (installed flag, version
	// and binary path). The optional provider is used by TunAudit; when it is
	// nil the core and wintun.dll checks are reported as skipped.
	CoreInfo func() model.CoreInfo
	// AppInfo overrides the build information embedded in summary.json.
	AppInfo func() model.AppInfo
	// Logs returns the recent proxy log entries for logs.txt. When nil, the
	// proxy state provider is asked for Logs() if it implements that method.
	Logs func() []model.LogEntry
	// SelfCheck runs the diagnostics self-check for selfcheck.json. When nil
	// the archive simply omits that entry.
	SelfCheck func(ctx context.Context) []model.CheckResult
}

// SelfCheckFunc adapts a no-argument self-check runner (for example
// *selfcheckservice.Service.Run) to the ReportSources.SelfCheck signature:
//
//	diagnosticsservice.ReportSources{SelfCheck: diagnosticsservice.SelfCheckFunc(selfcheckSvc.Run)}
func SelfCheckFunc(run func() []model.CheckResult) func(context.Context) []model.CheckResult {
	if run == nil {
		return nil
	}
	return func(context.Context) []model.CheckResult {
		return run()
	}
}

// ExportReport builds a redacted diagnostics archive in the application data
// directory and returns the path of the created ZIP file. The archive contains:
//
//   - summary.json: build/platform information, a redacted settings summary,
//     record counts, the proxy state and a compact self-check summary;
//   - logs.txt: the most recent proxy log lines with URLs, credentials and
//     UUIDs redacted;
//   - selfcheck.json: the full self-check results (also redacted), written only
//     when a self-check provider was wired in.
func (s *DiagnosticsService) ExportReport() (string, error) {
	now := time.Now()
	dir := s.reportDataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("无法创建诊断报告目录: %w", err)
	}

	selfCheck := s.collectSelfCheck()
	entries := []reportEntry{
		{name: "summary.json", data: s.buildSummary(now, selfCheck)},
		{name: "logs.txt", data: []byte(s.buildLogs())},
	}
	if selfCheck != nil {
		entries = append(entries, reportEntry{name: "selfcheck.json", data: buildSelfCheckJSON(selfCheck)})
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, entry := range entries {
		writer, err := archive.Create(entry.name)
		if err != nil {
			_ = archive.Close()
			return "", fmt.Errorf("无法生成诊断报告条目 %s: %w", entry.name, err)
		}
		if _, err := writer.Write(entry.data); err != nil {
			_ = archive.Close()
			return "", fmt.Errorf("无法写入诊断报告条目 %s: %w", entry.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return "", fmt.Errorf("无法完成诊断报告压缩: %w", err)
	}

	fileName := reportFilePrefix + now.Format("20060102-150405") +
		fmt.Sprintf("-%03d", now.Nanosecond()/int(time.Millisecond)) + ".zip"
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("无法写入诊断报告: %w", err)
	}
	return path, nil
}

// reportDataDir returns the directory the archive is written to: the store data
// directory when a store is wired in, otherwise the platform default data
// directory (the same candidates store.NewStore would use).
func (s *DiagnosticsService) reportDataDir() string {
	if s.report.Store != nil {
		if dir := strings.TrimSpace(s.report.Store.DataDir()); dir != "" {
			return dir
		}
	}
	if dir := strings.TrimSpace(os.Getenv("FASTV_DATA_DIR")); dir != "" {
		return dir
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configDir, "FastV")
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cacheDir, "FastV")
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, ".fastv-data")
	}
	return os.TempDir()
}

// collectSelfCheck runs the optional self-check provider. A nil result means
// the selfcheck.json entry is skipped entirely.
func (s *DiagnosticsService) collectSelfCheck() []model.CheckResult {
	if s.report.SelfCheck == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), reportSelfCheckTimeout)
	defer cancel()
	results := s.report.SelfCheck(ctx)
	if results == nil {
		return []model.CheckResult{}
	}
	return results
}

// --- summary.json ----------------------------------------------------------

// reportSummary is the machine-readable overview written to summary.json.
type reportSummary struct {
	GeneratedAt string           `json:"generatedAt"`
	OS          string           `json:"os"`
	Arch        string           `json:"arch"`
	App         model.AppInfo    `json:"app"`
	Settings    reportSettings   `json:"settings"`
	Counts      reportCounts     `json:"counts"`
	Proxy       reportProxyState `json:"proxy"`
	SelfCheck   reportSelfCheck  `json:"selfCheck"`
}

// reportSettings is the whitelisted, redacted settings summary. Sensitive
// free-form values (WebDAV URL/password, inbound password, ConfigOverride, DNS
// overrides, hotkey bindings) are never exported; only their presence is
// reported through booleans. Everything else is a non-sensitive enum-like
// string, a number or a boolean.
type reportSettings struct {
	Theme              string `json:"theme"`
	Language           string `json:"language"`
	AccentColor        string `json:"accentColor"`
	LayoutMode         string `json:"layoutMode"`
	ProxyMode          string `json:"proxyMode"`
	CoreType           string `json:"coreType"`
	XrayLogLevel       string `json:"xrayLogLevel"`
	XrayDNSStrategy    string `json:"xrayDnsStrategy"`
	XrayDomainStrategy string `json:"xrayDomainStrategy"`
	SystemProxyMode    string `json:"systemProxyMode"`
	LoadBalanceMode    string `json:"loadBalanceMode"`

	DockTransparency          int `json:"dockTransparency"`
	UpdateIntervalMinutes     int `json:"updateIntervalMinutes"`
	XrayLocalPort             int `json:"xrayLocalPort"`
	XrayMuxConcurrency        int `json:"xrayMuxConcurrency"`
	XrayTunMTU                int `json:"xrayTunMtu"`
	XraySockoptKeepIdle       int `json:"xraySockoptKeepIdle"`
	XraySockoptKeepIntvl      int `json:"xraySockoptKeepIntvl"`
	XraySockoptUserTimeout    int `json:"xraySockoptUserTimeout"`
	AutoSelectIntervalMinutes int `json:"autoSelectIntervalMinutes"`
	AutoSelectToleranceMS     int `json:"autoSelectToleranceMs"`
	XrayAPIPort               int `json:"xrayApiPort"`

	AutoUpdate                 bool `json:"autoUpdate"`
	CoreAutoUpdate             bool `json:"coreAutoUpdate"`
	XrayMuxEnabled             bool `json:"xrayMuxEnabled"`
	XraySniffing               bool `json:"xraySniffing"`
	XraySockoptTFO             bool `json:"xraySockoptTfo"`
	XraySockoptMPTCP           bool `json:"xraySockoptMptcp"`
	TunMode                    bool `json:"tunMode"`
	AllowLan                   bool `json:"allowLan"`
	InboundAuthEnabled         bool `json:"inboundAuthEnabled"`
	CloseToTray                bool `json:"closeToTray"`
	AutoRestartCore            bool `json:"autoRestartCore"`
	SubscriptionUpdateViaProxy bool `json:"subscriptionUpdateViaProxy"`
	MinimizeToTray             bool `json:"minimizeToTray"`
	HotkeysEnabled             bool `json:"hotkeysEnabled"`
	AutoCheckUpdate            bool `json:"autoCheckUpdate"`
	StatsEnabled               bool `json:"statsEnabled"`
	AutoSelectEnabled          bool `json:"autoSelectEnabled"`
	FailoverEnabled            bool `json:"failoverEnabled"`
	LoadBalanceEnabled         bool `json:"loadBalanceEnabled"`
	XrayAPIEnabled             bool `json:"xrayApiEnabled"`

	// Presence flags stand in for the sensitive values that must not leak.
	WebDavConfigured    bool `json:"webdavConfigured"`
	WebDavPasswordSet   bool `json:"webdavPasswordSet"`
	WebDavPathSet       bool `json:"webdavPathSet"`
	InboundUsernameSet  bool `json:"inboundUsernameSet"`
	InboundPasswordSet  bool `json:"inboundPasswordSet"`
	ConfigOverrideSet   bool `json:"configOverrideSet"`
	CustomDNSServersSet bool `json:"customDnsServersSet"`
	HotkeysConfigured   bool `json:"hotkeysConfigured"`
}

// reportCounts is the record inventory of the local data file.
type reportCounts struct {
	Subscriptions        int `json:"subscriptions"`
	EnabledSubscriptions int `json:"enabledSubscriptions"`
	Nodes                int `json:"nodes"`
	EnabledNodes         int `json:"enabledNodes"`
	ManualNodes          int `json:"manualNodes"`
	RoutingRules         int `json:"routingRules"`
	EnabledRoutingRules  int `json:"enabledRoutingRules"`
	RuleSets             int `json:"ruleSets"`
	EnabledRuleSets      int `json:"enabledRuleSets"`
	NodeGroups           int `json:"nodeGroups"`
}

// reportProxyState is the redacted proxy state. Node identifiers, server
// addresses and subscription names are intentionally omitted.
type reportProxyState struct {
	Status    string `json:"status"`
	ProxyMode string `json:"proxyMode"`
	Inbound   string `json:"inbound"`
	LocalAddr string `json:"localAddr"`
	StartedAt string `json:"startedAt"`
	PID       int    `json:"pid"`
	Error     string `json:"error,omitempty"`
}

// reportSelfCheck summarises the latest self-check in summary.json. The full
// per-check detail lives in selfcheck.json.
type reportSelfCheck struct {
	Available bool     `json:"available"`
	Total     int      `json:"total"`
	OK        int      `json:"ok"`
	Warn      int      `json:"warn"`
	Error     int      `json:"error"`
	Failed    []string `json:"failedChecks,omitempty"`
}

func (s *DiagnosticsService) buildSummary(now time.Time, selfCheck []model.CheckResult) []byte {
	summary := reportSummary{
		GeneratedAt: now.Format(time.RFC3339),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		App:         s.reportAppInfo(),
		SelfCheck:   summarizeSelfCheck(selfCheck),
	}
	if s.proxy != nil {
		summary.Proxy = reportProxyStateFrom(s.proxy.State())
	}
	if provider := s.report.Store; provider != nil {
		summary.Settings = reportSettingsFrom(provider.Settings())
		summary.Counts = reportCountsFrom(provider)
	}
	// json.MarshalIndent cannot fail for this struct; a failure would mean a
	// programming error, so fall back to an empty-but-valid JSON object.
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return []byte("{}")
	}
	return append(encoded, '\n')
}

func (s *DiagnosticsService) reportAppInfo() model.AppInfo {
	if s.report.AppInfo != nil {
		return s.report.AppInfo()
	}
	return model.AppInfo{
		Name:         buildinfo.ProductName,
		Version:      buildinfo.Version,
		Commit:       buildinfo.Commit,
		BuildDate:    buildinfo.BuildDate,
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		WailsVersion: buildinfo.WailsVersion,
	}
}

func reportSettingsFrom(settings model.Settings) reportSettings {
	return reportSettings{
		Theme:              settings.Theme,
		Language:           settings.Language,
		AccentColor:        settings.AccentColor,
		LayoutMode:         settings.LayoutMode,
		ProxyMode:          settings.ProxyMode,
		CoreType:           settings.CoreType,
		XrayLogLevel:       settings.XrayLogLevel,
		XrayDNSStrategy:    settings.XrayDNSStrategy,
		XrayDomainStrategy: settings.XrayDomainStrategy,
		SystemProxyMode:    settings.SystemProxyMode,
		LoadBalanceMode:    settings.LoadBalanceMode,

		DockTransparency:          settings.DockTransparency,
		UpdateIntervalMinutes:     settings.UpdateIntervalMinutes,
		XrayLocalPort:             settings.XrayLocalPort,
		XrayMuxConcurrency:        settings.XrayMuxConcurrency,
		XrayTunMTU:                settings.XrayTunMTU,
		XraySockoptKeepIdle:       settings.XraySockoptKeepIdle,
		XraySockoptKeepIntvl:      settings.XraySockoptKeepIntvl,
		XraySockoptUserTimeout:    settings.XraySockoptUserTimeout,
		AutoSelectIntervalMinutes: settings.AutoSelectIntervalMinutes,
		AutoSelectToleranceMS:     settings.AutoSelectToleranceMS,
		XrayAPIPort:               settings.XrayAPIPort,

		AutoUpdate:                 settings.AutoUpdate,
		CoreAutoUpdate:             settings.CoreAutoUpdate,
		XrayMuxEnabled:             settings.XrayMuxEnabled,
		XraySniffing:               settings.XraySniffing,
		XraySockoptTFO:             settings.XraySockoptTFO,
		XraySockoptMPTCP:           settings.XraySockoptMPTCP,
		TunMode:                    settings.TunMode,
		AllowLan:                   settings.AllowLan,
		InboundAuthEnabled:         settings.InboundAuthEnabled,
		CloseToTray:                settings.CloseToTray,
		AutoRestartCore:            settings.AutoRestartCore,
		SubscriptionUpdateViaProxy: settings.SubscriptionUpdateViaProxy,
		MinimizeToTray:             settings.MinimizeToTray,
		HotkeysEnabled:             settings.HotkeysEnabled,
		AutoCheckUpdate:            settings.AutoCheckUpdate,
		StatsEnabled:               settings.StatsEnabled,
		AutoSelectEnabled:          settings.AutoSelectEnabled,
		FailoverEnabled:            settings.FailoverEnabled,
		LoadBalanceEnabled:         settings.LoadBalanceEnabled,
		XrayAPIEnabled:             settings.XrayAPIEnabled,

		WebDavConfigured:    settings.WebDavURL != "" || settings.WebDavUsername != "" || settings.WebDavPassword != "",
		WebDavPasswordSet:   settings.WebDavPassword != "",
		WebDavPathSet:       settings.WebDavPath != "",
		InboundUsernameSet:  settings.InboundUsername != "",
		InboundPasswordSet:  settings.InboundPassword != "",
		ConfigOverrideSet:   settings.ConfigOverride != "",
		CustomDNSServersSet: settings.XrayDNSServers != "",
		HotkeysConfigured:   settings.HotkeyToggleProxy != "" || settings.HotkeyCycleMode != "" || settings.HotkeyNextNode != "",
	}
}

func reportCountsFrom(provider ReportStoreProvider) reportCounts {
	counts := reportCounts{}
	for _, sub := range provider.ListSubscriptions() {
		counts.Subscriptions++
		if sub.Enabled {
			counts.EnabledSubscriptions++
		}
	}
	for _, node := range provider.ListNodes() {
		counts.Nodes++
		if node.Enabled {
			counts.EnabledNodes++
		}
		if node.SubscriptionID == "" {
			counts.ManualNodes++
		}
	}
	for _, rule := range provider.ListRoutingRules() {
		counts.RoutingRules++
		if rule.Enabled {
			counts.EnabledRoutingRules++
		}
	}
	for _, set := range provider.ListRuleSets() {
		counts.RuleSets++
		if set.Enabled {
			counts.EnabledRuleSets++
		}
	}
	counts.NodeGroups = len(provider.ListNodeGroups())
	return counts
}

func reportProxyStateFrom(state model.ProxyState) reportProxyState {
	return reportProxyState{
		Status:    state.Status,
		ProxyMode: state.ProxyMode,
		Inbound:   state.Inbound,
		LocalAddr: state.LocalAddr,
		StartedAt: state.StartedAt,
		PID:       state.PID,
		Error:     redactReportText(state.Error),
	}
}

func summarizeSelfCheck(results []model.CheckResult) reportSelfCheck {
	summary := reportSelfCheck{Available: results != nil, Total: len(results)}
	for _, result := range results {
		switch result.Status {
		case "ok":
			summary.OK++
		case "warn":
			summary.Warn++
		default:
			summary.Error++
		}
		if result.Status != "ok" && result.ID != "" {
			summary.Failed = append(summary.Failed, result.ID)
		}
	}
	return summary
}

// --- logs.txt --------------------------------------------------------------

func (s *DiagnosticsService) buildLogs() string {
	entries := s.collectLogs()
	if len(entries) > reportLogLimit {
		entries = entries[len(entries)-reportLogLimit:]
	}
	var builder strings.Builder
	for _, entry := range entries {
		builder.WriteString("[")
		builder.WriteString(entry.Timestamp)
		builder.WriteString("] [")
		builder.WriteString(entry.Level)
		builder.WriteString("] [")
		builder.WriteString(entry.Source)
		builder.WriteString("] ")
		builder.WriteString(redactReportText(entry.Message))
		builder.WriteByte('\n')
	}
	return builder.String()
}

// collectLogs prefers the explicitly injected log provider and otherwise asks
// the proxy state provider (the production ProxyService implements Logs()).
func (s *DiagnosticsService) collectLogs() []model.LogEntry {
	if s.report.Logs != nil {
		return s.report.Logs()
	}
	if provider, ok := s.proxy.(interface{ Logs() []model.LogEntry }); ok {
		return provider.Logs()
	}
	return nil
}

// --- selfcheck.json --------------------------------------------------------

func buildSelfCheckJSON(results []model.CheckResult) []byte {
	redacted := make([]model.CheckResult, 0, len(results))
	for _, result := range results {
		result.Detail = redactReportText(result.Detail)
		result.Hint = redactReportText(result.Hint)
		redacted = append(redacted, result)
	}
	encoded, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return []byte("[]")
	}
	return append(encoded, '\n')
}

// --- redaction -------------------------------------------------------------

var (
	// reportSensitiveAssignment redacts "token=...", "password=..." and similar
	// key/value pairs wherever they appear.
	reportSensitiveAssignment = regexp.MustCompile(`(?i)\b(token|password|passwd|pwd|secret|uuid|apikey|api_key|access_key|auth|key)=([^&\s"'<>]+)`)
	// reportURLPattern drops every absolute URL, which is where subscription
	// links and provider tokens usually appear.
	reportURLPattern = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>]+`)
	// reportUUIDPattern hides bare UUIDs (node credentials) that are not part
	// of an assignment.
	reportUUIDPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

// redactReportText removes URLs, credential assignments and bare UUIDs from a
// value before it can reach the exported archive.
func redactReportText(value string) string {
	if value == "" {
		return value
	}
	redacted := reportSensitiveAssignment.ReplaceAllString(value, "${1}=***")
	redacted = reportURLPattern.ReplaceAllString(redacted, "[redacted-url]")
	redacted = reportUUIDPattern.ReplaceAllString(redacted, "[redacted-uuid]")
	return redacted
}
