package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create test store: %v", err)
	}
	return st
}

// seedConfiguration fills a store with data covering every part of the backup
// format: settings, a subscription with nodes, a manual favorite node, routing
// rules and an explicit active node.
func seedConfiguration(t *testing.T, st *store.Store) {
	t.Helper()

	settings := store.DefaultSettings()
	settings.Theme = "dark"
	settings.Language = "en-US"
	settings.XrayLocalPort = 20808
	settings.StatsEnabled = false
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	raw1 := "vless://11111111-2222-3333-4444-555555555555@node1.example.com:443?security=tls#node-1"
	raw2 := "trojan://password@node2.example.com:443#node-2"
	sub := store.Subscription{
		ID:        "sub-1",
		Name:      "示例订阅",
		URL:       "https://example.com/sub?token=secret",
		Enabled:   true,
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-02T00:00:00Z",
	}
	nodes := []store.Node{
		{
			ID: "node-1", SubscriptionID: "sub-1", Name: "节点一", Protocol: "vless",
			Address: "node1.example.com", Port: 443, Raw: raw1, Enabled: true,
			Favorite: true, SourceKey: model.SourceKey(raw1),
		},
		{
			ID: "node-2", SubscriptionID: "sub-1", Name: "节点二", Protocol: "trojan",
			Address: "node2.example.com", Port: 443, Raw: raw2, Enabled: true,
			SourceKey: model.SourceKey(raw2),
		},
	}
	if err := st.AddSubscription(sub, nodes); err != nil {
		t.Fatalf("add subscription: %v", err)
	}

	manualRaw := "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@manual.example.com:8388#manual"
	if err := st.AddManualNodes([]store.Node{{
		ID: "node-manual", Name: "手动节点", Protocol: "ss", Address: "manual.example.com",
		Port: 8388, Raw: manualRaw, Enabled: true, Favorite: true,
	}}); err != nil {
		t.Fatalf("add manual node: %v", err)
	}

	if _, err := st.SaveRoutingRule(model.RoutingRule{
		ID: "rule-1", Name: "直连示例", Type: "domain_suffix", Value: "example.com",
		Action: "direct", Enabled: true,
	}); err != nil {
		t.Fatalf("save routing rule: %v", err)
	}

	if _, err := st.SetActiveNode("node-2"); err != nil {
		t.Fatalf("set active node: %v", err)
	}
}

func exportVersion(t *testing.T, data []byte) int {
	t.Helper()

	var doc struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if doc.Version <= 0 {
		t.Fatalf("export has no version, got %d", doc.Version)
	}
	return doc.Version
}

func TestStoreExportRestoreRoundTrip(t *testing.T) {
	st := newTestStore(t)
	seedConfiguration(t, st)

	before, err := st.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(before, &payload); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	for _, key := range []string{"version", "settings", "subscriptions", "nodes", "activeNodeId", "routingRules"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("export is missing %q", key)
		}
	}

	// Replace the configuration with something different so the restore has
	// something real to roll back.
	if err := st.SaveSettings(store.DefaultSettings()); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if err := st.DeleteSubscription("sub-1"); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}
	if err := st.DeleteNode("node-manual"); err != nil {
		t.Fatalf("delete manual node: %v", err)
	}
	if _, err := st.SaveRoutingRule(model.RoutingRule{
		ID: "rule-2", Name: "额外规则", Type: "domain", Value: "extra.example.com",
		Action: "direct", Enabled: true,
	}); err != nil {
		t.Fatalf("save extra rule: %v", err)
	}

	result, err := st.Restore(before)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.RestoredSubscriptions != 1 || result.RestoredNodes != 3 || result.RestoredRules != 1 {
		t.Fatalf("unexpected restore result: %+v", result)
	}

	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after restore: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("restored configuration differs from the export\nbefore:\n%s\nafter:\n%s", before, after)
	}

	settings := st.Settings()
	if settings.Theme != "dark" || settings.Language != "en-US" || settings.XrayLocalPort != 20808 || settings.StatsEnabled {
		t.Fatalf("settings were not restored: %+v", settings)
	}

	subscriptions := st.ListSubscriptions()
	if len(subscriptions) != 1 || subscriptions[0].ID != "sub-1" || subscriptions[0].URL != "https://example.com/sub?token=secret" {
		t.Fatalf("subscriptions were not restored: %+v", subscriptions)
	}
	if subscriptions[0].NodeCount != 2 {
		t.Fatalf("subscription node count was not restored: %+v", subscriptions[0])
	}

	nodes := st.ListNodes()
	if len(nodes) != 3 {
		t.Fatalf("expected 3 restored nodes, got %d", len(nodes))
	}
	wantOrder := []string{"node-1", "node-2", "node-manual"}
	for i, want := range wantOrder {
		if nodes[i].ID != want {
			t.Fatalf("unexpected node order at %d: got %s, want %s", i, nodes[i].ID, want)
		}
	}
	if !nodes[0].Favorite || nodes[0].Raw != "vless://11111111-2222-3333-4444-555555555555@node1.example.com:443?security=tls#node-1" {
		t.Fatalf("node favorite/raw was not restored: %+v", nodes[0])
	}
	if !nodes[2].Favorite || nodes[2].SubscriptionID != "" {
		t.Fatalf("manual favorite node was not restored: %+v", nodes[2])
	}
	if !nodes[2].Enabled {
		t.Fatalf("manual node enabled flag was not restored: %+v", nodes[2])
	}

	rules := st.ListRoutingRules()
	if len(rules) != 1 || rules[0].ID != "rule-1" {
		t.Fatalf("routing rules were not restored: %+v", rules)
	}

	active, ok := st.ActiveNode()
	if !ok || active.ID != "node-2" {
		t.Fatalf("active node was not restored: %+v (ok=%v)", active, ok)
	}
}

func TestStoreRestoreRejectsInvalidBackupsWithoutChangingData(t *testing.T) {
	st := newTestStore(t)
	seedConfiguration(t, st)

	before, err := st.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	version := exportVersion(t, before)

	cases := []struct {
		name string
		data string
		want string
	}{
		{"空内容", "", "备份内容为空"},
		{"损坏的 JSON", `{"version":`, "JSON"},
		{"缺少版本信息", `{"settings":{},"subscriptions":[],"nodes":[],"routingRules":[]}`, "版本"},
		{
			"版本过新",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[],"routingRules":[]}`, version+1),
			"版本过新",
		},
		{
			"重复的订阅 ID",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[{"id":"a","name":"a"},{"id":"a","name":"b"}],"nodes":[],"routingRules":[]}`, version),
			"重复的订阅 ID",
		},
		{
			"重复的节点 ID",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[{"id":"n","name":"a"},{"id":"n","name":"b"}],"routingRules":[]}`, version),
			"重复的节点 ID",
		},
		{
			"节点缺少 ID",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[{"name":"没有 ID"}],"routingRules":[]}`, version),
			"节点缺少 ID",
		},
		{
			"节点引用了不存在的订阅",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[{"id":"n","subscriptionId":"missing"}],"routingRules":[]}`, version),
			"不存在的订阅",
		},
		{
			"路由规则缺少 ID",
			fmt.Sprintf(`{"version":%d,"settings":{},"subscriptions":[],"nodes":[],"routingRules":[{"name":"没有 ID"}]}`, version),
			"路由规则缺少 ID",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := st.Restore([]byte(tc.data)); err == nil {
				t.Fatal("expected restore to fail")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}

			after, err := st.Export()
			if err != nil {
				t.Fatalf("export after failed restore: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("failed restore modified the store\nbefore:\n%s\nafter:\n%s", before, after)
			}
		})
	}
}

func TestStoreRestoreValidatesActiveNode(t *testing.T) {
	st := newTestStore(t)
	seedConfiguration(t, st)

	exported, err := st.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	patch := func(t *testing.T, mutate func(document map[string]any)) []byte {
		t.Helper()
		var document map[string]any
		if err := json.Unmarshal(exported, &document); err != nil {
			t.Fatalf("unmarshal export: %v", err)
		}
		mutate(document)
		patched, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("marshal patched backup: %v", err)
		}
		return patched
	}

	t.Run("missing active node is cleared", func(t *testing.T) {
		data := patch(t, func(document map[string]any) {
			document["activeNodeId"] = "missing-node"
		})
		if _, err := st.Restore(data); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if active, ok := st.ActiveNode(); ok {
			t.Fatalf("expected no active node, got %+v", active)
		}
	})

	t.Run("disabled active node is cleared", func(t *testing.T) {
		data := patch(t, func(document map[string]any) {
			document["activeNodeId"] = "node-2"
			nodes, _ := document["nodes"].([]any)
			for _, raw := range nodes {
				node, _ := raw.(map[string]any)
				if node["id"] == "node-2" {
					node["enabled"] = false
				}
			}
		})
		if _, err := st.Restore(data); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if active, ok := st.ActiveNode(); ok {
			t.Fatalf("expected no active node, got %+v", active)
		}
	})

	t.Run("enabled active node is kept", func(t *testing.T) {
		data := patch(t, func(document map[string]any) {
			document["activeNodeId"] = "node-1"
		})
		if _, err := st.Restore(data); err != nil {
			t.Fatalf("restore: %v", err)
		}
		active, ok := st.ActiveNode()
		if !ok || active.ID != "node-1" {
			t.Fatalf("expected node-1 to be active, got %+v (ok=%v)", active, ok)
		}
	})
}

func TestBackupServiceRoundTrip(t *testing.T) {
	source := newTestStore(t)
	seedConfiguration(t, source)

	exported, err := New(source).Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(exported, `"version"`) {
		t.Fatalf("exported backup is not versioned: %s", exported)
	}

	target := newTestStore(t)
	result, err := New(target).Import(exported)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.RestoredSubscriptions != 1 || result.RestoredNodes != 3 || result.RestoredRules != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	reExported, err := New(target).Export()
	if err != nil {
		t.Fatalf("export after import: %v", err)
	}
	if reExported != exported {
		t.Fatalf("imported configuration differs from the backup\nwant:\n%s\ngot:\n%s", exported, reExported)
	}

	if active, ok := target.ActiveNode(); !ok || active.ID != "node-2" {
		t.Fatalf("active node was not imported: %+v (ok=%v)", active, ok)
	}
	if nodes := target.ListNodes(); len(nodes) != 3 || !nodes[0].Favorite {
		t.Fatalf("nodes were not imported: %+v", nodes)
	}
}

func TestBackupServiceImportRejectsInvalidData(t *testing.T) {
	st := newTestStore(t)
	seedConfiguration(t, st)

	before, err := st.Export()
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	cases := []struct {
		name string
		data string
		want string
	}{
		{"空字符串", "", "备份内容为空"},
		{"只有空白", "  \n\t ", "备份内容为空"},
		{"损坏的 JSON", `{"version":`, "JSON"},
		{"不是对象", `[]`, "JSON"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(st).Import(tc.data); err == nil {
				t.Fatal("expected import to fail")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}

			after, err := st.Export()
			if err != nil {
				t.Fatalf("export after failed import: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("failed import modified the configuration")
			}
		})
	}
}

func TestAutostartPlatformBehavior(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The Windows implementation writes to HKCU\...\Run; tests only probe the
		// read-only path so no registry state is modified.
		if _, err := platform.AutostartEnabled(); err != nil {
			t.Fatalf("query autostart state: %v", err)
		}
		if _, err := platform.IsEnabled(); err != nil {
			t.Fatalf("query autostart state via IsEnabled: %v", err)
		}
		return
	}

	if _, err := platform.AutostartEnabled(); err == nil || !strings.Contains(err.Error(), "仅支持 Windows") {
		t.Fatalf("expected unsupported autostart error, got %v", err)
	}
	if _, err := platform.IsEnabled(); err == nil || !strings.Contains(err.Error(), "仅支持 Windows") {
		t.Fatalf("expected unsupported autostart error from IsEnabled, got %v", err)
	}
	if err := platform.SetAutostart(true); err == nil || !strings.Contains(err.Error(), "仅支持 Windows") {
		t.Fatalf("expected unsupported SetAutostart error, got %v", err)
	}
}

// backupDAVServer is a minimal WebDAV server for the backup service tests. It
// implements the subset the service uses (MKCOL, PUT, GET, HEAD, DELETE,
// PROPFIND) and validates HTTP Basic authentication.
type backupDAVServer struct {
	mu       sync.Mutex
	username string
	password string
	basePath string

	files       map[string][]byte
	collections map[string]bool
	mkcolPaths  []string
	methods     []string
}

func newBackupDAVServer(t *testing.T, username, password, basePath string) (*backupDAVServer, *httptest.Server) {
	t.Helper()

	dav := &backupDAVServer{
		username:    username,
		password:    password,
		files:       map[string][]byte{},
		collections: map[string]bool{"/": true},
	}
	if basePath != "" {
		basePath = "/" + strings.Trim(basePath, "/")
		dav.basePath = basePath
		dav.collections[basePath+"/"] = true
	}
	server := httptest.NewServer(http.HandlerFunc(dav.handle))
	t.Cleanup(server.Close)
	return dav, server
}

func (d *backupDAVServer) handle(w http.ResponseWriter, r *http.Request) {
	user, pass, hasAuth := r.BasicAuth()

	d.mu.Lock()
	d.methods = append(d.methods, r.Method+" "+r.URL.Path)
	d.mu.Unlock()

	if !hasAuth || user != d.username || pass != d.password {
		w.Header().Set("WWW-Authenticate", `Basic realm="fastv-backup-test"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "MKCOL":
		d.mu.Lock()
		d.mkcolPaths = append(d.mkcolPaths, r.URL.Path)
		exists := d.collections[r.URL.Path]
		parentOK := d.parentExistsLocked(r.URL.Path)
		if !exists && parentOK && strings.HasSuffix(r.URL.Path, "/") {
			d.collections[r.URL.Path] = true
		}
		d.mu.Unlock()
		switch {
		case exists:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case !parentOK:
			w.WriteHeader(http.StatusConflict)
		default:
			w.WriteHeader(http.StatusCreated)
		}
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		d.mu.Lock()
		parentOK := d.parentExistsLocked(r.URL.Path)
		if parentOK {
			d.files[r.URL.Path] = body
		}
		d.mu.Unlock()
		if !parentOK {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case http.MethodGet:
		d.mu.Lock()
		data, exists := d.files[r.URL.Path]
		d.mu.Unlock()
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	case http.MethodHead:
		d.mu.Lock()
		_, exists := d.files[r.URL.Path]
		d.mu.Unlock()
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		d.mu.Lock()
		_, exists := d.files[r.URL.Path]
		if exists {
			delete(d.files, r.URL.Path)
		}
		d.mu.Unlock()
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "PROPFIND":
		d.mu.Lock()
		_, fileOK := d.files[r.URL.Path]
		_, collectionOK := d.collections[r.URL.Path]
		d.mu.Unlock()
		if !fileOK && !collectionOK {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (d *backupDAVServer) parentExistsLocked(p string) bool {
	parent := path.Dir(strings.TrimSuffix(p, "/"))
	if parent == "/" || parent == "." {
		return true
	}
	if d.basePath != "" && parent == d.basePath {
		return true
	}
	return d.collections[parent] || d.collections[parent+"/"]
}

func (d *backupDAVServer) seed(remotePath string, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files[remotePath] = append([]byte(nil), data...)
}

func (d *backupDAVServer) fileAt(remotePath string) ([]byte, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, ok := d.files[remotePath]
	return append([]byte(nil), data...), ok
}

func (d *backupDAVServer) mkcolLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.mkcolPaths...)
}

func (d *backupDAVServer) methodsLog() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.methods...)
}

// resetLogs forgets recorded requests so a test can assert on one phase only.
func (d *backupDAVServer) resetLogs() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.mkcolPaths = nil
	d.methods = nil
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// configureWebDAV persists the WebDAV settings used by the service methods.
func configureWebDAV(t *testing.T, st *store.Store, baseURL, username, password, remotePath string) {
	t.Helper()

	settings := st.Settings()
	settings.WebDavURL = baseURL
	settings.WebDavUsername = username
	settings.WebDavPassword = password
	settings.WebDavPath = remotePath
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save WebDAV settings: %v", err)
	}
}

func TestBackupServiceWebDAVRequiresConfiguration(t *testing.T) {
	st := newTestStore(t)
	service := New(st)

	if err := service.WebDAVTest(); err == nil || !strings.Contains(err.Error(), "请先配置 WebDAV 地址") {
		t.Fatalf("WebDAVTest without URL = %v", err)
	}
	if _, err := service.WebDAVUpload(); err == nil || !strings.Contains(err.Error(), "请先配置 WebDAV 地址") {
		t.Fatalf("WebDAVUpload without URL = %v", err)
	}
	if _, err := service.WebDAVDownload(); err == nil || !strings.Contains(err.Error(), "请先配置 WebDAV 地址") {
		t.Fatalf("WebDAVDownload without URL = %v", err)
	}
}

func TestBackupServiceWebDAVRoundTrip(t *testing.T) {
	dav, server := newBackupDAVServer(t, "alice", "secret", "dav")
	source := newTestStore(t)
	seedConfiguration(t, source)
	configureWebDAV(t, source, server.URL+"/dav", "alice", "secret", "/FastV/fastv-backup.json")

	service := New(source)
	if err := service.WebDAVTest(); err != nil {
		t.Fatalf("WebDAVTest: %v", err)
	}
	dav.resetLogs()

	remoteURL, err := service.WebDAVUpload()
	if err != nil {
		t.Fatalf("WebDAVUpload: %v", err)
	}
	wantURL := server.URL + "/dav/FastV/fastv-backup.json"
	if remoteURL != wantURL {
		t.Fatalf("WebDAVUpload returned %q, want %q", remoteURL, wantURL)
	}
	if wants := []string{"/dav/FastV/"}; !equalStringSlices(dav.mkcolLog(), wants) {
		t.Fatalf("MKCOL log = %v, want %v", dav.mkcolLog(), wants)
	}
	if _, ok := dav.fileAt("/dav/FastV/fastv-backup.json"); !ok {
		t.Fatal("remote server does not hold the uploaded backup")
	}

	// Restore into a separate store to prove the full cloud round trip. The
	// target must know where the server is, exactly like a fresh install would.
	target := newTestStore(t)
	configureWebDAV(t, target, server.URL+"/dav", "alice", "secret", "/FastV/fastv-backup.json")
	result, err := New(target).WebDAVDownload()
	if err != nil {
		t.Fatalf("WebDAVDownload: %v", err)
	}
	if result.RestoredSubscriptions != 1 || result.RestoredNodes != 3 || result.RestoredRules != 1 {
		t.Fatalf("unexpected restore result: %+v", result)
	}

	sourceExport, err := source.Export()
	if err != nil {
		t.Fatalf("export source: %v", err)
	}
	targetExport, err := target.Export()
	if err != nil {
		t.Fatalf("export target: %v", err)
	}
	if !bytes.Equal(sourceExport, targetExport) {
		t.Fatalf("restored configuration differs from the uploaded backup\nwant:\n%s\ngot:\n%s", sourceExport, targetExport)
	}
	if active, ok := target.ActiveNode(); !ok || active.ID != "node-2" {
		t.Fatalf("active node was not restored: %+v (ok=%v)", active, ok)
	}
	if nodes := target.ListNodes(); len(nodes) != 3 || !nodes[0].Favorite {
		t.Fatalf("nodes were not restored: %+v", nodes)
	}
	if settings := target.Settings(); settings.WebDavURL != server.URL+"/dav" {
		t.Fatalf("WebDAV settings were not restored: %+v", settings)
	}
}

func TestBackupServiceWebDAVUsesDefaultRemotePath(t *testing.T) {
	_, server := newBackupDAVServer(t, "alice", "secret", "")
	st := newTestStore(t)
	seedConfiguration(t, st)
	configureWebDAV(t, st, server.URL, "alice", "secret", "")

	remoteURL, err := New(st).WebDAVUpload()
	if err != nil {
		t.Fatalf("WebDAVUpload: %v", err)
	}
	want := server.URL + "/FastV/fastv-backup.json"
	if remoteURL != want {
		t.Fatalf("WebDAVUpload returned %q, want default path %q", remoteURL, want)
	}
}

func TestBackupServiceWebDAVTestRejectsBadCredentials(t *testing.T) {
	_, server := newBackupDAVServer(t, "alice", "s3cret-server-password", "")
	st := newTestStore(t)
	configureWebDAV(t, st, server.URL, "alice", "wrong-client-password", "/FastV/fastv-backup.json")

	err := New(st).WebDAVTest()
	if err == nil {
		t.Fatal("expected bad credentials to fail the connection test")
	}
	message := err.Error()
	for _, want := range []string{"认证", "401"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q does not contain %q", message, want)
		}
	}
	for _, secret := range []string{"s3cret-server-password", "wrong-client-password"} {
		if strings.Contains(message, secret) {
			t.Fatalf("error leaks a password: %q", message)
		}
	}
}

func TestBackupServiceWebDAVDownloadMissingBackup(t *testing.T) {
	_, server := newBackupDAVServer(t, "alice", "secret", "")
	st := newTestStore(t)
	seedConfiguration(t, st)
	configureWebDAV(t, st, server.URL, "alice", "secret", "/FastV/fastv-backup.json")

	before, err := st.Export()
	if err != nil {
		t.Fatalf("export before download: %v", err)
	}
	if _, err := New(st).WebDAVDownload(); err == nil || !strings.Contains(err.Error(), "远端没有备份文件") {
		t.Fatalf("WebDAVDownload with no remote file = %v", err)
	}
	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after failed download: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a failed download modified the configuration")
	}
}

func TestBackupServiceWebDAVDownloadRejectsInvalidBackup(t *testing.T) {
	dav, server := newBackupDAVServer(t, "alice", "secret", "")
	dav.seed("/FastV/fastv-backup.json", []byte(`{"version":`))

	st := newTestStore(t)
	seedConfiguration(t, st)
	configureWebDAV(t, st, server.URL, "alice", "secret", "/FastV/fastv-backup.json")

	before, err := st.Export()
	if err != nil {
		t.Fatalf("export before download: %v", err)
	}
	if _, err := New(st).WebDAVDownload(); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("WebDAVDownload with a broken remote backup = %v", err)
	}
	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after failed download: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("restoring an invalid backup modified the configuration")
	}
}

func TestBackupServiceWebDAVDownloadRejectsOversizedBackup(t *testing.T) {
	dav, server := newBackupDAVServer(t, "alice", "secret", "")
	dav.seed("/FastV/fastv-backup.json", bytes.Repeat([]byte("a"), 16<<20+1))

	st := newTestStore(t)
	configureWebDAV(t, st, server.URL, "alice", "secret", "/FastV/fastv-backup.json")

	if _, err := New(st).WebDAVDownload(); err == nil || !strings.Contains(err.Error(), "大小限制") {
		t.Fatalf("WebDAVDownload with an oversized backup = %v", err)
	}
}

func TestBackupServiceWebDAVRejectsUnsafeRemotePath(t *testing.T) {
	dav, server := newBackupDAVServer(t, "alice", "secret", "")
	st := newTestStore(t)
	seedConfiguration(t, st)
	configureWebDAV(t, st, server.URL, "alice", "secret", "../../outside.json")

	if _, err := New(st).WebDAVUpload(); err == nil || !strings.Contains(err.Error(), "上级目录") {
		t.Fatalf("WebDAVUpload with a traversal path = %v", err)
	}
	if methods := dav.methodsLog(); len(methods) != 0 {
		t.Fatalf("unsafe remote path reached the server: %v", methods)
	}
}

func TestBackupServiceSnapshotLifecycle(t *testing.T) {
	st := newTestStore(t)
	seedConfiguration(t, st)
	service := New(st)

	info, err := service.CreateSnapshot("  发布前  ")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if info.ID == "" || info.Label != "发布前" || info.Automatic || info.Size <= 0 {
		t.Fatalf("unexpected snapshot info: %+v", info)
	}

	snapshots, err := service.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0] != info {
		t.Fatalf("ListSnapshots = %+v, want [%+v]", snapshots, info)
	}

	// Replace the configuration, then roll it back through the service. The app
	// instance is nil in tests, so the settings:changed broadcast must be skipped
	// without panicking or failing the rollback.
	if err := st.SaveSettings(store.DefaultSettings()); err != nil {
		t.Fatalf("save default settings: %v", err)
	}
	if err := st.DeleteSubscription("sub-1"); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}
	if nodes := st.ListNodes(); len(nodes) != 1 {
		t.Fatalf("expected only the manual node before the rollback, got %+v", nodes)
	}

	result, err := service.RestoreSnapshot(info.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	if result.RestoredSubscriptions != 1 || result.RestoredNodes != 3 || result.RestoredRules != 1 {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	if settings := st.Settings(); settings.Theme != "dark" || settings.Language != "en-US" || settings.XrayLocalPort != 20808 {
		t.Fatalf("settings were not rolled back through the service: %+v", settings)
	}
	if active, ok := st.ActiveNode(); !ok || active.ID != "node-2" {
		t.Fatalf("active node was not rolled back: %+v (ok=%v)", active, ok)
	}

	// The automatic pre-restore safety snapshot is exposed like any other.
	snapshots, err = service.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots after rollback: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected the manual and the safety snapshot, got %+v", snapshots)
	}
	if !snapshots[0].Automatic || snapshots[0].Label != "回滚前快照" {
		t.Fatalf("newest snapshot is not the safety snapshot: %+v", snapshots[0])
	}

	if err := service.DeleteSnapshot(info.ID); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if snapshots, err = service.ListSnapshots(); err != nil {
		t.Fatalf("ListSnapshots after delete: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ID == info.ID {
		t.Fatalf("snapshot was not deleted: %+v", snapshots)
	}
}

func TestBackupServiceSnapshotErrors(t *testing.T) {
	st := newTestStore(t)
	service := New(st)

	// A fresh store has no snapshots and an empty (non-nil) list.
	snapshots, err := service.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if snapshots == nil || len(snapshots) != 0 {
		t.Fatalf("fresh store snapshots = %#v, want an empty slice", snapshots)
	}

	// An empty label uses the manual default.
	info, err := service.CreateSnapshot("")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if info.Label != "手动快照" || info.Automatic {
		t.Fatalf("unexpected default snapshot: %+v", info)
	}

	// Invalid and unknown ids are forwarded with the store's Chinese errors.
	if _, err := service.RestoreSnapshot("../data"); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("RestoreSnapshot with a traversal id = %v", err)
	}
	if err := service.DeleteSnapshot(""); err == nil || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("DeleteSnapshot with an empty id = %v", err)
	}
	if _, err := service.RestoreSnapshot("snap-20260101-000000-9999"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("RestoreSnapshot with a missing id = %v", err)
	}
	if err := service.DeleteSnapshot("snap-20260101-000000-9999"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("DeleteSnapshot with a missing id = %v", err)
	}
}

func TestBackupServiceSnapshotExportImportRequiresDialog(t *testing.T) {
	st := newTestStore(t)
	seedConfiguration(t, st)
	service := New(st)

	info, err := service.CreateSnapshot("发布前")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// The Wails application instance is nil in tests, so the native file dialogs
	// cannot be shown. The service must report that instead of touching files.
	if _, err := service.ExportSnapshot(info.ID); err == nil || !strings.Contains(err.Error(), "文件对话框不可用") {
		t.Fatalf("ExportSnapshot without a running app = %v, want a dialog error", err)
	}
	if _, err := service.ImportSnapshot(""); err == nil || !strings.Contains(err.Error(), "文件对话框不可用") {
		t.Fatalf("ImportSnapshot without a running app = %v, want a dialog error", err)
	}

	// Unknown snapshots are rejected before the save dialog would open.
	if _, err := service.ExportSnapshot("snap-20260101-000000-9999"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("ExportSnapshot with a missing id = %v, want a not-found error", err)
	}
	if _, err := service.ExportSnapshot(""); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("ExportSnapshot with an empty id = %v, want a not-found error", err)
	}

	// None of the rejected calls may change the stored snapshots.
	snapshots, err := service.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0] != info {
		t.Fatalf("snapshots changed by rejected export/import calls: %+v", snapshots)
	}
}

func TestSnapshotExportFilename(t *testing.T) {
	cases := []struct {
		name string
		info model.SnapshotInfo
		want string
	}{
		{
			name: "label",
			info: model.SnapshotInfo{ID: "snap-20260101-000000-aaaa", Label: "发布前"},
			want: "FastV-snapshot-发布前.json",
		},
		{
			name: "empty label falls back to id",
			info: model.SnapshotInfo{ID: "snap-20260101-000000-aaaa", Label: "   "},
			want: "FastV-snapshot-snap-20260101-000000-aaaa.json",
		},
		{
			name: "illegal characters are removed",
			info: model.SnapshotInfo{ID: "id", Label: `a/b\c:d*e?f"g<h>i|j`},
			want: "FastV-snapshot-abcdefghij.json",
		},
		{
			name: "surrounding whitespace and dots are trimmed",
			info: model.SnapshotInfo{ID: "id", Label: "\t 配置 . . "},
			want: "FastV-snapshot-配置.json",
		},
		{
			name: "label of only dots falls back to id",
			info: model.SnapshotInfo{ID: "snap-20260101-000000-bbbb", Label: "..."},
			want: "FastV-snapshot-snap-20260101-000000-bbbb.json",
		},
		{
			name: "long label is truncated",
			info: model.SnapshotInfo{ID: "id", Label: strings.Repeat("长", snapshotExportMaxLabelRunes+10)},
			want: "FastV-snapshot-" + strings.Repeat("长", snapshotExportMaxLabelRunes) + ".json",
		},
	}
	for _, tt := range cases {
		if got := snapshotExportFilename(tt.info); got != tt.want {
			t.Errorf("%s: snapshotExportFilename() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
