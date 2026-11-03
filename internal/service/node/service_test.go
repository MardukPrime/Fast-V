package node

import (
	"net"
	"path/filepath"
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

	service := New(st)
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

	service := New(st)
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
