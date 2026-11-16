package node

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

func TestManageNodes(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}

	parsed := []subdomain.ParsedNode{
		{Name: "Alpha", Protocol: "vless", Address: "example.com", Port: 443, Raw: "vless://alpha"},
		{Name: "Beta", Protocol: "trojan", Address: "example.org", Port: 443, Raw: "trojan://beta"},
	}
	nodes := subdomain.BuildNodes("sub-1", parsed)
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Test Sub", URL: "https://example.com/sub"}, nodes); err != nil {
		t.Fatal(err)
	}

	service := New(st, nil)
	list := service.List(model.NodeFilter{})
	if len(list) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(list))
	}
	if !list[0].Active {
		t.Fatal("first node should be active by default")
	}
	if list[0].SubscriptionName != "Test Sub" {
		t.Fatalf("expected subscription name, got %q", list[0].SubscriptionName)
	}

	second := list[1]
	updated, err := service.SetActive(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Active {
		t.Fatal("expected second node to be active")
	}

	disabled, err := service.SetEnabled(second.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatal("node should be disabled")
	}

	filtered := service.List(model.NodeFilter{Protocol: "vless"})
	if len(filtered) != 1 || filtered[0].Protocol != "vless" {
		t.Fatalf("unexpected filtered nodes: %+v", filtered)
	}
}

func TestLatency(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := subdomain.BuildNodes("sub-1", []subdomain.ParsedNode{{
		Name: "Local", Protocol: "socks", Address: "127.0.0.1", Port: port, Raw: "socks://local",
	}})
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Local"}, nodes); err != nil {
		t.Fatal(err)
	}

	service := New(st, nil)
	results := service.TestLatency([]string{nodes[0].ID})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "" || results[0].LatencyMS <= 0 {
		t.Fatalf("unexpected latency result: %+v", results[0])
	}
}

func TestReplaceNodesPreservesIdentityAndEnabledState(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := []subdomain.ParsedNode{{Name: "Stable", Protocol: "vless", Address: "example.com", Port: 443, Raw: "vless://stable"}}
	first := subdomain.BuildNodes("sub-1", parsed)
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Stable Sub"}, first); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetNodeEnabled(first[0].ID, false); err != nil {
		t.Fatal(err)
	}

	second := subdomain.BuildNodes("sub-1", parsed)
	if err := st.ReplaceSubscriptionNodes("sub-1", second); err != nil {
		t.Fatal(err)
	}
	nodes := st.NodesForSubscription("sub-1")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != first[0].ID || nodes[0].Enabled {
		t.Fatalf("node identity or enabled state was not preserved: %+v", nodes[0])
	}
}

func TestConfigExtractPreviewAndSave(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw := "vless://00000000-0000-0000-0000-000000000002@old.example.com:443?security=tls&type=ws&path=%2Fws&host=old.example.com&sni=old.example.com#Original"
	nodes := subdomain.BuildNodes("sub-1", []subdomain.ParsedNode{{
		Name: "Original", Protocol: "vless", Address: "old.example.com", Port: 443, Raw: raw,
	}})
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Config Test"}, nodes); err != nil {
		t.Fatal(err)
	}
	service := New(st, nil)

	cfg, err := service.Config(nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "old.example.com" || cfg.UUID == "" || cfg.Network != "websocket" || cfg.TLS != "tls" {
		t.Fatalf("unexpected extracted config: %+v", cfg)
	}

	cfg.Name = "Edited"
	cfg.Address = "new.example.com"
	cfg.Port = 8443
	cfg.Path = "/new"
	cfg.ServerName = "new.example.com"
	preview, err := service.Preview(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.RawLink, "new.example.com:8443") || !strings.Contains(preview.OutboundJSON, `"websocket"`) {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	saved, err := service.SaveConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Edited" || saved.Address != "new.example.com" || saved.Port != 8443 {
		t.Fatalf("unexpected saved node: %+v", saved)
	}
	updated, err := st.FindNode(nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Raw, "new.example.com") {
		t.Fatalf("raw link was not regenerated: %s", updated.Raw)
	}
}

func TestSaveConfigAcceptsAdvancedOutboundJSON(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := subdomain.BuildNodes("sub-1", []subdomain.ParsedNode{{
		Name: "Advanced", Protocol: "vless", Address: "example.com", Port: 443,
		Raw: "vless://00000000-0000-0000-0000-000000000003@example.com:443#Advanced",
	}})
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Advanced"}, nodes); err != nil {
		t.Fatal(err)
	}
	service := New(st, nil)
	cfg, err := service.Config(nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OutboundJSON = `{"protocol":"vless","settings":{"address":"example.com","port":443,"id":"00000000-0000-0000-0000-000000000003","encryption":"none"}}`
	saved, err := service.SaveConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != nodes[0].ID {
		t.Fatalf("unexpected node: %+v", saved)
	}
	extracted, err := service.Config(nodes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if extracted.OutboundJSON == "" {
		t.Fatal("advanced outbound JSON was not preserved")
	}
}

func TestImportLinksDedupesAndReportsErrors(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	links := testShareLinks(t)

	text := strings.Join([]string{
		"# 粘贴导入的节点",
		"",
		strings.Join(links, "\n"),
		links[1], // duplicate inside the same paste
		"https://example.com/not-a-share-link",
	}, "\n")

	result, err := service.ImportLinks(text)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Imported != 4 {
		t.Fatalf("expected 4 imported nodes, got %d", result.Imported)
	}
	if result.Skipped != 0 {
		t.Fatalf("duplicates inside one paste should be removed while parsing, skipped=%d", result.Skipped)
	}
	if len(result.Nodes) != 4 {
		t.Fatalf("expected 4 node views, got %d", len(result.Nodes))
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 rejected line, got %d: %+v", len(result.Errors), result.Errors)
	}
	for _, view := range result.Nodes {
		if !view.Manual {
			t.Fatalf("imported node should be manual: %+v", view)
		}
		if view.Favorite || !view.Enabled {
			t.Fatalf("imported node should be enabled and not favorite: %+v", view)
		}
	}
	if len(st.ListNodes()) != 4 {
		t.Fatalf("expected 4 stored nodes, got %d", len(st.ListNodes()))
	}
	// Ordering follows the paste order for a fresh store.
	stored := st.ListNodes()
	if stored[0].Address != "vmess.example.com" || stored[3].Address != "ss.example.com" {
		t.Fatalf("unexpected import order: %+v", stored)
	}

	// Pasting the same text again must not create duplicates.
	again, err := service.ImportLinks(text)
	if err != nil {
		t.Fatalf("second import failed: %v", err)
	}
	if again.Imported != 0 || again.Skipped != 4 || len(again.Nodes) != 0 {
		t.Fatalf("expected everything to be skipped on re-paste: %+v", again)
	}
	if len(st.ListNodes()) != 4 {
		t.Fatalf("re-paste duplicated nodes: %d", len(st.ListNodes()))
	}
}

func TestImportLinksRejectsEmptyTextAndKeepsErrorDetails(t *testing.T) {
	service := New(newTestStore(t), nil)

	if _, err := service.ImportLinks("   \r\n\t"); err == nil {
		t.Fatal("expected error for empty import text")
	}
	result, err := service.ImportLinks("not a link\nhttp://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Imported != 0 || len(result.Nodes) != 0 {
		t.Fatalf("expected nothing imported: %+v", result)
	}
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 error entries, got %d: %+v", len(result.Errors), result.Errors)
	}
}

func TestImportLinksDedupesAgainstSubscriptionNodes(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	links := testShareLinks(t)

	parsed := []subdomain.ParsedNode{{
		Name: "VMess 节点", Protocol: "vmess", Address: "vmess.example.com", Port: 443, Raw: links[0],
	}}
	nodes := subdomain.BuildNodes("sub-1", parsed)
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Test Sub"}, nodes); err != nil {
		t.Fatal(err)
	}

	result, err := service.ImportLinks(links[0])
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 1 || len(result.Nodes) != 0 {
		t.Fatalf("link already provided by a subscription should be skipped: %+v", result)
	}
	if got := len(st.NodesForSubscription("sub-1")); got != 1 {
		t.Fatalf("subscription nodes were changed: %d", got)
	}
}

func TestExportLinksRoundTripsAllProtocols(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	links := testShareLinks(t)

	result, err := service.ImportLinks(strings.Join(links, "\n"))
	if err != nil || result.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, result)
	}
	ids := make([]string, 0, len(result.Nodes))
	for _, view := range result.Nodes {
		ids = append(ids, view.ID)
	}

	requestOrder := []string{ids[1], ids[0], ids[3], ids[2]}
	exported, err := service.ExportLinks(requestOrder)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if len(exported) != 4 {
		t.Fatalf("expected 4 exported links, got %d", len(exported))
	}
	wantProtocols := []string{"vless", "vmess", "shadowsocks", "trojan"}
	for i, export := range exported {
		if export.ID != requestOrder[i] {
			t.Fatalf("export order mismatch at %d: %s", i, export.ID)
		}
		node, err := subdomain.ParseLink(export.Link)
		if err != nil {
			t.Fatalf("exported %s link does not parse: %v (%q)", export.Name, err, export.Link)
		}
		if node.Protocol != wantProtocols[i] {
			t.Fatalf("unexpected protocol at %d: %s (%q)", i, node.Protocol, export.Link)
		}
		if node.Name != export.Name {
			t.Fatalf("exported name mismatch: %q vs %q", node.Name, export.Name)
		}
	}

	all, err := service.ExportLinks(nil)
	if err != nil {
		t.Fatalf("export all failed: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected all 4 nodes to be exported, got %d", len(all))
	}
}

func TestExportLinkRebuildsWhenRawProtocolDiffers(t *testing.T) {
	// A legacy node whose stored raw link does not match its protocol is rebuilt
	// from the extracted configuration instead of returning the wrong link.
	node := model.Node{
		ID: "legacy-1", Name: "Rebuilt", Protocol: "ss",
		Address: "legacy.example.com", Port: 8388,
		Raw: "trojan://password@legacy.example.com:8443#Legacy", Enabled: true,
	}
	link, err := exportNodeLink(node)
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	parsed, err := subdomain.ParseLink(link)
	if err != nil {
		t.Fatalf("rebuilt link does not parse: %v (%q)", err, link)
	}
	if parsed.Protocol != "trojan" || parsed.Address != "legacy.example.com" || parsed.Name != "Rebuilt" {
		t.Fatalf("unexpected rebuilt link: %+v", parsed)
	}
}

func TestFavoriteAndReorderPersistAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := store.NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	service := New(st, nil)
	links := testShareLinks(t)
	result, err := service.ImportLinks(strings.Join(links[:3], "\n"))
	if err != nil || result.Imported != 3 {
		t.Fatalf("import failed: %v (%+v)", err, result)
	}
	ids := make([]string, 0, 3)
	for _, view := range result.Nodes {
		ids = append(ids, view.ID)
	}

	favorite, err := service.SetFavorite(ids[0], true)
	if err != nil {
		t.Fatal(err)
	}
	if !favorite.Favorite {
		t.Fatalf("favorite flag was not set: %+v", favorite)
	}
	if _, err := service.SetFavorite("missing-id", true); err == nil {
		t.Fatal("expected error for unknown node")
	}

	if err := service.Reorder([]string{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatalf("reorder failed: %v", err)
	}
	wantOrder := []string{"Trojan 节点", "VMess 节点", "VLESS 节点"}
	assertNodeOrder(t, service.List(model.NodeFilter{}), wantOrder)

	// Reopen the store file: favorites and order must survive a restart.
	reloaded, err := store.NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	reloadedService := New(reloaded, nil)
	list := reloadedService.List(model.NodeFilter{})
	assertNodeOrder(t, list, wantOrder)
	if !list[1].Favorite {
		t.Fatalf("favorite flag was lost after reload: %+v", list[1])
	}

	// Deleting a manual node must work even though it has no subscription.
	if err := reloadedService.Delete(ids[1]); err != nil {
		t.Fatalf("delete manual node failed: %v", err)
	}
	if got := len(reloadedService.List(model.NodeFilter{})); got != 2 {
		t.Fatalf("expected 2 nodes after delete, got %d", got)
	}
}

func TestSubscriptionRefreshKeepsFavoriteOrderAndManualNodes(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)

	firstLinks := []string{
		"vless://00000000-0000-0000-0000-0000000000a1@a.example.com:443#Sub%20A",
		"trojan://pass-b@b.example.com:443#Sub%20B",
	}
	firstParsed := []subdomain.ParsedNode{
		{Name: "Sub A", Protocol: "vless", Address: "a.example.com", Port: 443, Raw: firstLinks[0]},
		{Name: "Sub B", Protocol: "trojan", Address: "b.example.com", Port: 443, Raw: firstLinks[1]},
	}
	firstNodes := subdomain.BuildNodes("sub-1", firstParsed)
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Test Sub"}, firstNodes); err != nil {
		t.Fatal(err)
	}

	imported, err := service.ImportLinks(testShareLinks(t)[0])
	if err != nil || imported.Imported != 1 {
		t.Fatalf("manual import failed: %v (%+v)", err, imported)
	}
	manualID := imported.Nodes[0].ID

	if _, err := service.SetActive(manualID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetFavorite(firstNodes[1].ID, true); err != nil {
		t.Fatal(err)
	}
	// User order: favorite subscription node, then the other subscription node,
	// then the manual node.
	if err := service.Reorder([]string{firstNodes[1].ID, firstNodes[0].ID, manualID}); err != nil {
		t.Fatal(err)
	}

	nextParsed := append([]subdomain.ParsedNode{}, firstParsed...)
	nextParsed = append(nextParsed, subdomain.ParsedNode{
		Name: "Sub C", Protocol: "shadowsocks", Address: "c.example.com", Port: 8388,
		Raw: "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@c.example.com:8388#Sub%20C",
	})
	nextNodes := subdomain.BuildNodes("sub-1", nextParsed)
	if err := st.ReplaceSubscriptionNodes("sub-1", nextNodes); err != nil {
		t.Fatal(err)
	}

	// The manual node survives the subscription refresh.
	if _, err := st.FindNode(manualID); err != nil {
		t.Fatalf("manual node was removed by subscription refresh: %v", err)
	}
	refreshed, err := st.FindNode(firstNodes[1].ID)
	if err != nil {
		t.Fatalf("subscription node identity was not preserved: %v", err)
	}
	if !refreshed.Favorite {
		t.Fatalf("favorite flag was lost after refresh: %+v", refreshed)
	}
	if refreshed.Order != 0 {
		t.Fatalf("user order was lost after refresh: %+v", refreshed)
	}
	// The active node may be a manual node; a subscription refresh must not
	// silently switch the user to another node.
	active, ok := st.ActiveNode()
	if !ok || active.ID != manualID {
		t.Fatalf("active manual node was lost after refresh: %+v (ok=%v)", active, ok)
	}

	list := service.List(model.NodeFilter{})
	wantOrder := []string{"Sub B", "Sub A", "VMess 节点", "Sub C"}
	assertNodeOrder(t, list, wantOrder)
	if !list[0].Favorite || list[0].Manual {
		t.Fatalf("unexpected first node view: %+v", list[0])
	}
	if list[1].Favorite || list[1].Manual {
		t.Fatalf("unexpected second node view: %+v", list[1])
	}
	if !list[2].Manual {
		t.Fatalf("imported node should be marked manual: %+v", list[2])
	}
	if list[3].Manual {
		t.Fatalf("refreshed subscription node should not be manual: %+v", list[3])
	}
}

// testCoreProvider reports a fixed core path used by URL latency tests.
type testCoreProvider struct {
	path string
}

func (p testCoreProvider) Info() model.CoreInfo {
	return model.CoreInfo{Installed: p.path != "", Path: p.path, Status: "ready"}
}

func TestTestURLWithoutCoreReportsPerNodeErrors(t *testing.T) {
	st := newTestStore(t)
	nodes := []model.Node{
		{ID: "enabled-1", Name: "Enabled", Protocol: "freedom", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
		{ID: "disabled-1", Name: "Disabled", Protocol: "freedom", Enabled: false, OutboundJSON: `{"protocol":"freedom"}`},
	}
	if err := st.AddManualNodes(nodes); err != nil {
		t.Fatal(err)
	}

	service := New(st, nil)
	results := service.TestURL(nil)
	if len(results) != 1 {
		t.Fatalf("expected only the enabled node, got %+v", results)
	}
	if results[0].NodeID != "enabled-1" {
		t.Fatalf("unexpected node: %+v", results[0])
	}
	if !strings.Contains(results[0].Error, "Xray-core") {
		t.Fatalf("expected a missing-core error: %+v", results[0])
	}

	// Failed measurements are recorded with their node name and error.
	history := st.LatencyHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 recorded latency sample, got %+v", history)
	}
	if history[0].NodeID != "enabled-1" || history[0].NodeName != "Enabled" {
		t.Fatalf("unexpected recorded sample: %+v", history[0])
	}
	if !strings.Contains(history[0].Error, "Xray-core") || history[0].CheckedAt == "" {
		t.Fatalf("recorded failure sample must keep its error and timestamp: %+v", history[0])
	}

	explicit := service.TestURL([]string{"disabled-1", "missing"})
	if len(explicit) != 2 {
		t.Fatalf("expected 2 results, got %+v", explicit)
	}
	if explicit[0].NodeID != "disabled-1" || explicit[0].Error != "节点未启用" {
		t.Fatalf("unexpected disabled node result: %+v", explicit[0])
	}
	if explicit[1].NodeID != "missing" || explicit[1].Error != "节点不存在" {
		t.Fatalf("unexpected unknown node result: %+v", explicit[1])
	}

	history = st.LatencyHistory(10)
	if len(history) != 3 {
		t.Fatalf("expected 3 recorded latency samples, got %+v", history)
	}
	if history[1].NodeID != "disabled-1" || history[1].NodeName != "Disabled" || history[1].Error != "节点未启用" {
		t.Fatalf("unexpected disabled node sample: %+v", history[1])
	}
	// Unknown node ids have no store record, so only the id is kept.
	if history[2].NodeID != "missing" || history[2].NodeName != "" || history[2].Error != "节点不存在" {
		t.Fatalf("unexpected unknown node sample: %+v", history[2])
	}
}

func TestTestURLThroughRealCore(t *testing.T) {
	binary := filepath.Join("..", "..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	st := newTestStore(t)
	if err := st.AddManualNodes([]model.Node{
		{ID: "freedom-1", Name: "Freedom", Protocol: "freedom", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
	}); err != nil {
		t.Fatal(err)
	}
	settings := st.Settings()
	settings.TestURL = server.URL
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	service := New(st, testCoreProvider{path: binary})
	results := service.TestURL([]string{"freedom-1"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %+v", results)
	}
	if results[0].Error != "" || results[0].LatencyMS <= 0 {
		t.Fatalf("real URL latency test failed: %+v", results[0])
	}

	// Successful measurements are recorded as well, with the measured latency.
	history := st.LatencyHistory(10)
	if len(history) != 1 {
		t.Fatalf("expected 1 recorded latency sample, got %+v", history)
	}
	sample := history[0]
	if sample.NodeID != "freedom-1" || sample.NodeName != "Freedom" || sample.Error != "" {
		t.Fatalf("unexpected recorded sample: %+v", sample)
	}
	if sample.LatencyMS != results[0].LatencyMS || sample.CheckedAt != results[0].CheckedAt {
		t.Fatalf("recorded sample does not match the measurement result: sample=%+v result=%+v", sample, results[0])
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// testShareLinks returns one valid share link per supported protocol.
func testShareLinks(t *testing.T) []string {
	t.Helper()
	vmessPayload, err := json.Marshal(map[string]any{
		"v": "2", "ps": "VMess 节点", "add": "vmess.example.com", "port": 443,
		"id": "00000000-0000-0000-0000-000000000001", "net": "ws", "tls": "tls",
		"host": "vmess.example.com", "path": "/vm",
	})
	if err != nil {
		t.Fatal(err)
	}
	return []string{
		"vmess://" + base64.StdEncoding.EncodeToString(vmessPayload),
		"vless://00000000-0000-0000-0000-000000000002@vless.example.com:443" +
			"?security=reality&type=tcp&pbk=public-key&sid=abcd#VLESS%20%E8%8A%82%E7%82%B9",
		"trojan://password@trojan.example.com:443?security=tls&type=ws&path=%2Fws#Trojan%20%E8%8A%82%E7%82%B9",
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#SS%20%E8%8A%82%E7%82%B9",
	}
}

func assertNodeOrder(t *testing.T, list []model.NodeView, wantNames []string) {
	t.Helper()
	if len(list) != len(wantNames) {
		t.Fatalf("expected %d nodes, got %d: %+v", len(wantNames), len(list), list)
	}
	for i, want := range wantNames {
		if list[i].Name != want {
			got := make([]string, 0, len(list))
			for _, view := range list {
				got = append(got, view.Name)
			}
			t.Fatalf("unexpected node order: got %v, want %v", got, wantNames)
		}
	}
}

func TestQRCodeForNode(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	imported, err := service.ImportLinks(strings.Join(testShareLinks(t), "\n"))
	if err != nil || imported.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, imported)
	}

	dataURL, err := service.QRCode(imported.Nodes[1].ID)
	if err != nil {
		t.Fatalf("qr code failed: %v", err)
	}
	assertPNGDataURL(t, dataURL, 320)

	if _, err := service.QRCode("missing-node"); err == nil || !strings.Contains(err.Error(), "节点") {
		t.Fatalf("expected a Chinese not-found error, got %v", err)
	}

	// A node whose link does not fit into a QR code must fail with a clear
	// Chinese error instead of returning an empty image.
	longNode := model.Node{
		ID: "long-link-1", Name: "Long Link", Protocol: "vless",
		Address: "long.example.com", Port: 443, Enabled: true,
		Raw: "vless://" + strings.Repeat("A", 4096),
	}
	if err := st.AddManualNodes([]model.Node{longNode}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.QRCode(longNode.ID); err == nil || !strings.Contains(err.Error(), "二维码") {
		t.Fatalf("expected a too-long QR error, got %v", err)
	}
}

func assertPNGDataURL(t *testing.T, dataURL string, wantSize int) {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("unexpected data URL prefix: %q", shortenForTest(dataURL))
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("data URL is not valid base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("data URL is not a PNG image: %v", err)
	}
	if img.Bounds().Dx() != wantSize || img.Bounds().Dy() != wantSize {
		t.Fatalf("expected a %dx%d PNG, got %v", wantSize, wantSize, img.Bounds())
	}
}

func shortenForTest(value string) string {
	if len(value) > 32 {
		return value[:32] + "..."
	}
	return value
}

func TestSetEnabledBatch(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	imported, err := service.ImportLinks(strings.Join(testShareLinks(t), "\n"))
	if err != nil || imported.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, imported)
	}
	ids := make([]string, 0, len(imported.Nodes))
	for _, view := range imported.Nodes {
		ids = append(ids, view.ID)
	}
	// The first imported node is active by default.
	if _, ok := st.ActiveNode(); !ok {
		t.Fatal("expected an active node before disabling")
	}

	result, err := service.SetEnabledBatch(ids, false)
	if err != nil {
		t.Fatalf("batch disable failed: %v", err)
	}
	if result.Affected != 4 || len(result.Errors) != 0 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	for _, node := range st.ListNodes() {
		if node.Enabled {
			t.Fatalf("node %q was not disabled: %+v", node.Name, node)
		}
	}
	// Disabling the active node clears the active selection.
	if _, ok := st.ActiveNode(); ok {
		t.Fatal("active node was not cleared after disabling it")
	}

	// Partial failures are collected and do not abort the batch.
	result, err = service.SetEnabledBatch([]string{ids[0], "missing-node", ids[2]}, true)
	if err != nil {
		t.Fatalf("batch enable failed: %v", err)
	}
	if result.Affected != 2 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "missing-node") {
		t.Fatalf("unexpected partial batch result: %+v", result)
	}
	if node, err := st.FindNode(ids[0]); err != nil || !node.Enabled {
		t.Fatalf("first node state is wrong: %+v (err=%v)", node, err)
	}

	// Duplicated ids are counted only once.
	result, err = service.SetEnabledBatch([]string{ids[1], ids[1]}, false)
	if err != nil || result.Affected != 1 {
		t.Fatalf("duplicate ids were not deduplicated: %+v (err=%v)", result, err)
	}

	if _, err := service.SetEnabledBatch(nil, true); err == nil {
		t.Fatal("expected an error for an empty batch")
	}
	if _, err := service.SetEnabledBatch([]string{"  "}, true); err == nil {
		t.Fatal("expected an error when every id is empty")
	}
}

func TestDeleteBatch(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	imported, err := service.ImportLinks(strings.Join(testShareLinks(t), "\n"))
	if err != nil || imported.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, imported)
	}
	ids := make([]string, 0, len(imported.Nodes))
	for _, view := range imported.Nodes {
		ids = append(ids, view.ID)
	}
	if _, err := service.SetActive(ids[1]); err != nil {
		t.Fatal(err)
	}

	result, err := service.DeleteBatch([]string{ids[1], "missing-node", ids[3]})
	if err != nil {
		t.Fatalf("batch delete failed: %v", err)
	}
	if result.Affected != 2 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "missing-node") {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	// Deleting the active node clears the active selection.
	if _, ok := st.ActiveNode(); ok {
		t.Fatal("active node was not cleared after deleting it")
	}
	if _, err := st.FindNode(ids[1]); err == nil {
		t.Fatal("deleted node still exists")
	}
	if _, err := st.FindNode(ids[3]); err == nil {
		t.Fatal("deleted node still exists")
	}
	if got := len(service.List(model.NodeFilter{})); got != 2 {
		t.Fatalf("expected 2 remaining nodes, got %d", got)
	}

	if _, err := service.DeleteBatch(nil); err == nil {
		t.Fatal("expected an error for an empty batch")
	}
}

func TestDeleteBatchUpdatesSubscriptionNodeCount(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	nodes := subdomain.BuildNodes("sub-batch", []subdomain.ParsedNode{
		{Name: "One", Protocol: "vless", Address: "one.example.com", Port: 443, Raw: "vless://one"},
		{Name: "Two", Protocol: "vless", Address: "two.example.com", Port: 443, Raw: "vless://two"},
	})
	if err := st.AddSubscription(model.Subscription{ID: "sub-batch", Name: "Batch Sub"}, nodes); err != nil {
		t.Fatal(err)
	}
	if _, err := service.DeleteBatch([]string{nodes[0].ID}); err != nil {
		t.Fatal(err)
	}
	sub, err := st.FindSubscription("sub-batch")
	if err != nil {
		t.Fatal(err)
	}
	if sub.NodeCount != 1 {
		t.Fatalf("subscription node count was not updated: %+v", sub)
	}
}

func TestSetTag(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	imported, err := service.ImportLinks(strings.Join(testShareLinks(t), "\n"))
	if err != nil || imported.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, imported)
	}
	id := imported.Nodes[0].ID

	// Imported nodes start without a tag and NodeView carries it.
	for _, view := range imported.Nodes {
		if view.Tag != "" {
			t.Fatalf("expected imported nodes to have no tag, got %+v", view)
		}
	}

	view, err := service.SetTag(id, "  美国节点  ")
	if err != nil {
		t.Fatalf("SetTag failed: %v", err)
	}
	if view.Tag != "美国节点" || view.ID != id {
		t.Fatalf("unexpected NodeView after SetTag: %+v", view)
	}

	// The list view exposes the stored tag too.
	found := false
	for _, item := range service.List(model.NodeFilter{}) {
		if item.ID != id {
			continue
		}
		found = true
		if item.Tag != "美国节点" {
			t.Fatalf("List did not carry the node tag: %+v", item)
		}
	}
	if !found {
		t.Fatal("tagged node missing from List")
	}

	// Clearing a tag with whitespace works like an empty string.
	view, err = service.SetTag(id, "   ")
	if err != nil {
		t.Fatalf("clearing the tag failed: %v", err)
	}
	if view.Tag != "" {
		t.Fatalf("expected an empty tag after clearing, got %q", view.Tag)
	}

	// Invalid tags are rejected with Chinese errors and change nothing.
	if _, err := service.SetTag(id, "香港\n节点"); err == nil || !strings.Contains(err.Error(), "换行或控制字符") {
		t.Fatalf("expected a Chinese newline error, got %v", err)
	}
	if _, err := service.SetTag(id, strings.Repeat("标", store.MaxTagLength+1)); err == nil || !strings.Contains(err.Error(), "标签最多 24 个字符") {
		t.Fatalf("expected a Chinese length error, got %v", err)
	}
	if node, err := st.FindNode(id); err != nil || node.Tag != "" {
		t.Fatalf("a rejected tag changed the stored value: %+v (err=%v)", node, err)
	}

	if _, err := service.SetTag("missing-node", "香港"); err == nil {
		t.Fatal("expected an error for an unknown node id")
	}
}

func TestSetTagBatch(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	imported, err := service.ImportLinks(strings.Join(testShareLinks(t), "\n"))
	if err != nil || imported.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, imported)
	}
	ids := make([]string, 0, len(imported.Nodes))
	for _, view := range imported.Nodes {
		ids = append(ids, view.ID)
	}

	if _, err := service.SetTagBatch(nil, "香港"); err == nil || !strings.Contains(err.Error(), "请先选择要设置标签的节点") {
		t.Fatalf("expected a Chinese empty selection error, got %v", err)
	}

	// Partial failures are collected and do not abort the batch.
	result, err := service.SetTagBatch([]string{ids[0], "missing-node", ids[2], ids[0]}, "香港")
	if err != nil {
		t.Fatalf("batch tagging failed: %v", err)
	}
	if result.Affected != 2 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "missing-node") {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	for _, id := range []string{ids[0], ids[2]} {
		node, err := st.FindNode(id)
		if err != nil || node.Tag != "香港" {
			t.Fatalf("node %s tag was not applied: %+v (err=%v)", id, node, err)
		}
	}
	if node, _ := st.FindNode(ids[1]); node.Tag != "" {
		t.Fatalf("node outside the batch was tagged: %+v", node)
	}

	// An invalid tag is rejected before anything is written.
	if _, err := service.SetTagBatch(ids, strings.Repeat("标", store.MaxTagLength+1)); err == nil || !strings.Contains(err.Error(), "标签最多 24 个字符") {
		t.Fatalf("expected a Chinese length error, got %v", err)
	}
	for _, id := range ids {
		if node, err := st.FindNode(id); err != nil || node.Tag == strings.Repeat("标", store.MaxTagLength+1) {
			t.Fatalf("invalid batch tag was applied to %s: %+v (err=%v)", id, node, err)
		}
	}

	// An empty tag clears the tags of the selected nodes.
	result, err = service.SetTagBatch(ids, "")
	if err != nil || result.Affected != 4 || len(result.Errors) != 0 {
		t.Fatalf("batch clear failed: %+v (err=%v)", result, err)
	}
	if tags := service.Tags(); len(tags) != 0 {
		t.Fatalf("expected no remaining tags, got %v", tags)
	}
}

func TestTags(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	imported, err := service.ImportLinks(strings.Join(testShareLinks(t), "\n"))
	if err != nil || imported.Imported != 4 {
		t.Fatalf("import failed: %v (%+v)", err, imported)
	}
	ids := make([]string, 0, len(imported.Nodes))
	for _, view := range imported.Nodes {
		ids = append(ids, view.ID)
	}

	if tags := service.Tags(); len(tags) != 0 {
		t.Fatalf("expected an empty tag dictionary, got %v", tags)
	}
	setTags := map[string]string{ids[0]: "香港", ids[1]: "us", ids[2]: "香港"}
	for id, tag := range setTags {
		if _, err := service.SetTag(id, tag); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"us", "香港"}
	got := service.Tags()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected deduplicated sorted tags %v, got %v", want, got)
	}

	// A new tag is inserted at its sorted position.
	if _, err := service.SetTag(ids[3], "jp"); err != nil {
		t.Fatal(err)
	}
	want = []string{"jp", "us", "香港"}
	got = service.Tags()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestSubscriptionRefreshKeepsNodeTags(t *testing.T) {
	st := newTestStore(t)
	service := New(st, nil)
	parsed := []subdomain.ParsedNode{
		{Name: "Alpha", Protocol: "vless", Address: "alpha.example.com", Port: 443, Raw: "vless://alpha"},
		{Name: "Beta", Protocol: "trojan", Address: "beta.example.com", Port: 443, Raw: "trojan://beta"},
	}
	nodes := subdomain.BuildNodes("sub-tag", parsed)
	if err := st.AddSubscription(model.Subscription{ID: "sub-tag", Name: "Tag Sub"}, nodes); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetTag(nodes[0].ID, "香港"); err != nil {
		t.Fatal(err)
	}

	// A refresh rebuilds every node id; the tag must follow the SourceKey.
	if err := st.ReplaceSubscriptionNodes("sub-tag", subdomain.BuildNodes("sub-tag", parsed)); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	for _, item := range service.List(model.NodeFilter{}) {
		seen[item.Name] = item.Tag
	}
	if got := seen["Alpha"]; got != "香港" {
		t.Fatalf("expected Alpha to keep its tag after a refresh, got %q (all: %+v)", got, seen)
	}
	if got := seen["Beta"]; got != "" {
		t.Fatalf("expected Beta to stay untagged, got %q", got)
	}
	if tags := service.Tags(); len(tags) != 1 || tags[0] != "香港" {
		t.Fatalf("expected the tag dictionary to keep the surviving tag, got %v", tags)
	}
}
