package diagnostics

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

type fakeProxyState struct {
	state model.ProxyState
}

func (f fakeProxyState) State() model.ProxyState {
	return f.state
}

func TestCheckRejectsInvalidMode(t *testing.T) {
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}})
	if _, err := service.Check("invalid", false); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestCheckRejectsStoppedProxy(t *testing.T) {
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}})
	if _, err := service.Check("proxy", false); err == nil {
		t.Fatal("expected stopped proxy error")
	}
}

// --- ExportReport tests ----------------------------------------------------

// newReportTestStore creates an isolated data store for the export tests.
func newReportTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// readReportArchive opens the produced ZIP and returns entry name -> content.
func readReportArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open report archive: %v", err)
	}
	defer reader.Close()

	files := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatalf("open archive entry %s: %v", file.Name, err)
		}
		data, err := io.ReadAll(handle)
		_ = handle.Close()
		if err != nil {
			t.Fatalf("read archive entry %s: %v", file.Name, err)
		}
		files[file.Name] = string(data)
	}
	return files
}

// fakeProxyWithLogs also implements the optional Logs() accessor so the report
// can fall back to the proxy state provider for logs.txt.
type fakeProxyWithLogs struct {
	fakeProxyState
	logs []model.LogEntry
}

func (f fakeProxyWithLogs) Logs() []model.LogEntry {
	return f.logs
}

func TestExportReportWritesRedactedArchive(t *testing.T) {
	const (
		webDavPassword  = "secret123"
		inboundPassword = "inbound-secret"
		subscriptionURL = "https://sub.example.com/api/v1/client/subscribe?token=secret-token-123"
	)
	st := newReportTestStore(t)
	settings := st.Settings()
	settings.WebDavURL = "https://dav.example.com/remote.php/dav/files/dav-user"
	settings.WebDavUsername = "dav-user"
	settings.WebDavPassword = webDavPassword
	settings.WebDavPath = "fastv-backups"
	settings.InboundAuthEnabled = true
	settings.InboundUsername = "inbound-user"
	settings.InboundPassword = inboundPassword
	settings.ConfigOverride = `{"log":{"loglevel":"debug"}}`
	settings.XrayLocalPort = 10808
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	nodes := []model.Node{{
		ID:             "node-secret-1",
		SubscriptionID: "sub-secret",
		Name:           "节点一",
		Protocol:       "vless",
		Address:        "node.example.com",
		Port:           443,
		Raw:            "vless://00000000-0000-0000-0000-000000000009@node.example.com:443?security=tls#Node",
		Enabled:        true,
	}}
	if err := st.AddSubscription(model.Subscription{
		ID:      "sub-secret",
		Name:    "Secret Sub",
		URL:     subscriptionURL,
		Enabled: true,
	}, nodes); err != nil {
		t.Fatal(err)
	}
	if err := st.AddManualNodes([]model.Node{{
		ID:       "manual-node",
		Name:     "手动节点",
		Protocol: "trojan",
		Address:  "manual.example.com",
		Port:     443,
		Raw:      "trojan://manual-password@manual.example.com:443#Manual",
		Enabled:  true,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveRoutingRule(model.RoutingRule{Name: "局域网直连", Type: "ip", Value: "10.0.0.0/8", Action: "direct", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveRuleSet(model.RuleSet{Name: "广告拦截", URL: subscriptionURL, Kind: "domain", Action: "block", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveNodeGroup(model.NodeGroup{Name: "常用", Type: "manual", NodeIDs: []string{"node-secret-1"}}); err != nil {
		t.Fatal(err)
	}

	logs := []model.LogEntry{
		{Timestamp: "2026-11-18T21:00:00+09:00", Level: "info", Source: "core", Message: "本地入站已就绪: 127.0.0.1:10808"},
		{Timestamp: "2026-11-18T21:00:01+09:00", Level: "error", Source: "proxy", Message: `拉取订阅失败: Get "` + subscriptionURL + `": dial tcp timeout`},
		{Timestamp: "2026-11-18T21:00:02+09:00", Level: "warning", Source: "core", Message: "节点凭据 UUID 00000000-0000-0000-0000-000000000009"},
		{Timestamp: "2026-11-18T21:00:03+09:00", Level: "info", Source: "system", Message: "password=" + inboundPassword + " 已应用"},
	}
	selfChecks := []model.CheckResult{
		{ID: "subscriptions", Name: "订阅", Status: "warn", Detail: "订阅地址 " + subscriptionURL + " 拉取失败", Hint: "检查网络后重试"},
		{ID: "local-port", Name: "本地端口", Status: "ok", Detail: "10808 可用"},
	}
	service := New(
		fakeProxyState{state: model.ProxyState{
			Status:    "running",
			ProxyMode: "rule",
			Inbound:   "SOCKS5 / HTTP",
			LocalAddr: "127.0.0.1:10808",
			StartedAt: "2026-11-18T20:59:00+09:00",
			PID:       4242,
		}},
		ReportSources{
			Store:   st,
			AppInfo: func() model.AppInfo { return model.AppInfo{Name: "FastV", Version: "9.9.9", Commit: "abc123"} },
			Logs:    func() []model.LogEntry { return logs },
			SelfCheck: SelfCheckFunc(func() []model.CheckResult {
				return selfChecks
			}),
		},
	)

	path, err := service.ExportReport()
	if err != nil {
		t.Fatalf("export report: %v", err)
	}
	if dir := filepath.Dir(path); dir != st.DataDir() {
		t.Fatalf("expected the report in the data dir %q, got %q", st.DataDir(), dir)
	}
	if base := filepath.Base(path); !strings.HasPrefix(base, "diagnostics-report-") || !strings.HasSuffix(base, ".zip") {
		t.Fatalf("unexpected report file name %q", base)
	}

	files := readReportArchive(t, path)
	summaryJSON, ok := files["summary.json"]
	if !ok {
		t.Fatal("summary.json is missing from the report archive")
	}
	logsText, ok := files["logs.txt"]
	if !ok {
		t.Fatal("logs.txt is missing from the report archive")
	}
	selfCheckJSON, ok := files["selfcheck.json"]
	if !ok {
		t.Fatal("selfcheck.json is missing from the report archive")
	}

	// No known secret may appear verbatim in any entry.
	secrets := []string{
		webDavPassword,
		inboundPassword,
		"secret-token-123",
		"https://sub.example.com",
		"https://dav.example.com",
		"dav-user",
		"inbound-user",
		"00000000-0000-0000-0000-000000000009",
		"manual-password",
		"node.example.com",
		`"webdavPassword"`,
		`"webdavUrl"`,
		`"inboundPassword"`,
	}
	for name, content := range files {
		for _, secret := range secrets {
			if strings.Contains(content, secret) {
				t.Fatalf("entry %s leaks sensitive value %q", name, secret)
			}
		}
	}

	// The safe log line is kept while URL and password values are redacted.
	if !strings.Contains(logsText, "本地入站已就绪: 127.0.0.1:10808") {
		t.Fatalf("expected the harmless log line to be preserved, got %q", logsText)
	}
	if !strings.Contains(logsText, "[redacted-url]") {
		t.Fatalf("expected the subscription URL in logs to be redacted, got %q", logsText)
	}
	if !strings.Contains(logsText, "password=***") {
		t.Fatalf("expected the password assignment in logs to be redacted, got %q", logsText)
	}
	if !strings.Contains(logsText, "[redacted-uuid]") {
		t.Fatalf("expected the bare UUID in logs to be redacted, got %q", logsText)
	}

	var summary map[string]any
	if err := json.Unmarshal([]byte(summaryJSON), &summary); err != nil {
		t.Fatalf("summary.json is not valid JSON: %v", err)
	}
	if got := summary["os"]; got != runtime.GOOS {
		t.Fatalf("unexpected os in summary: %v", got)
	}
	app := summary["app"].(map[string]any)
	if app["version"] != "9.9.9" || app["commit"] != "abc123" {
		t.Fatalf("unexpected app info in summary: %+v", app)
	}
	counts := summary["counts"].(map[string]any)
	expectCount := func(key string, want float64) {
		t.Helper()
		if counts[key] != want {
			t.Fatalf("expected counts.%s=%v, got %v (all %+v)", key, want, counts[key], counts)
		}
	}
	expectCount("subscriptions", 1)
	expectCount("nodes", 2)
	expectCount("manualNodes", 1)
	expectCount("routingRules", 1)
	expectCount("ruleSets", 1)
	expectCount("nodeGroups", 1)
	redactedSettings := summary["settings"].(map[string]any)
	if redactedSettings["webdavPasswordSet"] != true || redactedSettings["inboundPasswordSet"] != true {
		t.Fatalf("expected presence flags for the sensitive settings, got %+v", redactedSettings)
	}
	if redactedSettings["configOverrideSet"] != true || redactedSettings["webdavConfigured"] != true {
		t.Fatalf("expected presence flags for config override and webdav, got %+v", redactedSettings)
	}
	proxyState := summary["proxy"].(map[string]any)
	if proxyState["status"] != "running" || proxyState["localAddr"] != "127.0.0.1:10808" {
		t.Fatalf("unexpected proxy state in summary: %+v", proxyState)
	}
	selfCheckSummary := summary["selfCheck"].(map[string]any)
	if selfCheckSummary["available"] != true || selfCheckSummary["total"] != float64(2) || selfCheckSummary["warn"] != float64(1) {
		t.Fatalf("unexpected self-check summary: %+v", selfCheckSummary)
	}

	var selfCheckResults []map[string]any
	if err := json.Unmarshal([]byte(selfCheckJSON), &selfCheckResults); err != nil {
		t.Fatalf("selfcheck.json is not valid JSON: %v", err)
	}
	if len(selfCheckResults) != 2 {
		t.Fatalf("expected 2 self-check results, got %+v", selfCheckResults)
	}
	if detail := selfCheckResults[0]["detail"].(string); !strings.Contains(detail, "[redacted-url]") {
		t.Fatalf("expected the self-check detail URL to be redacted, got %q", detail)
	}
}

func TestSelfCheckFuncAdapter(t *testing.T) {
	adapter := SelfCheckFunc(func() []model.CheckResult {
		return []model.CheckResult{{ID: "local-port", Name: "本地端口", Status: "ok"}}
	})
	if adapter == nil {
		t.Fatal("expected an adapter for a non-nil self-check runner")
	}
	if results := adapter(context.Background()); len(results) != 1 || results[0].ID != "local-port" {
		t.Fatalf("unexpected adapted results: %+v", results)
	}
	if SelfCheckFunc(nil) != nil {
		t.Fatal("expected a nil adapter for a nil self-check runner")
	}
}

func TestExportReportFallsBackToProxyLogs(t *testing.T) {
	st := newReportTestStore(t)
	provider := fakeProxyWithLogs{
		fakeProxyState: fakeProxyState{state: model.ProxyState{Status: "stopped"}},
		logs: []model.LogEntry{
			{Timestamp: "t1", Level: "info", Source: "core", Message: "普通日志行"},
		},
	}
	service := New(provider, ReportSources{Store: st})

	path, err := service.ExportReport()
	if err != nil {
		t.Fatalf("export report: %v", err)
	}
	files := readReportArchive(t, path)
	if !strings.Contains(files["logs.txt"], "普通日志行") {
		t.Fatalf("expected the proxy Logs() fallback to feed logs.txt, got %q", files["logs.txt"])
	}
	if _, ok := files["selfcheck.json"]; ok {
		t.Fatal("selfcheck.json must be omitted when no self-check provider is wired in")
	}
}

func TestExportReportWithoutStoreStillWritesArchive(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("FASTV_DATA_DIR", dataDir)
	service := New(fakeProxyState{state: model.ProxyState{Status: "stopped"}})

	path, err := service.ExportReport()
	if err != nil {
		t.Fatalf("export report: %v", err)
	}
	if filepath.Dir(path) != filepath.Clean(dataDir) {
		t.Fatalf("expected the fallback data dir %q, got %q", dataDir, filepath.Dir(path))
	}
	files := readReportArchive(t, path)
	summaryJSON, ok := files["summary.json"]
	if !ok {
		t.Fatal("summary.json is missing from the report archive")
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(summaryJSON), &summary); err != nil {
		t.Fatalf("summary.json is not valid JSON: %v", err)
	}
	if summary["os"] != runtime.GOOS {
		t.Fatalf("unexpected os in summary: %v", summary["os"])
	}
	if _, ok := files["logs.txt"]; !ok {
		t.Fatal("logs.txt is missing from the report archive")
	}
}
