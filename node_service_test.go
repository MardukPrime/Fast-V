package main

import (
	"net"
	"path/filepath"
	"testing"
)

func TestNodeServiceManageNodes(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}

	parsed := []parsedNode{
		{Name: "Alpha", Protocol: "vless", Address: "example.com", Port: 443, Raw: "vless://alpha"},
		{Name: "Beta", Protocol: "trojan", Address: "example.org", Port: 443, Raw: "trojan://beta"},
	}
	nodes := buildNodes("sub-1", parsed)
	if err := store.AddSubscription(Subscription{ID: "sub-1", Name: "Test Sub", URL: "https://example.com/sub"}, nodes); err != nil {
		t.Fatal(err)
	}

	service := NewNodeService(store)
	list := service.List(NodeFilter{})
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

	filtered := service.List(NodeFilter{Protocol: "vless"})
	if len(filtered) != 1 || filtered[0].Protocol != "vless" {
		t.Fatalf("unexpected filtered nodes: %+v", filtered)
	}
}

func TestNodeServiceLatency(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := buildNodes("sub-1", []parsedNode{{
		Name: "Local", Protocol: "socks", Address: "127.0.0.1", Port: port, Raw: "socks://local",
	}})
	if err := store.AddSubscription(Subscription{ID: "sub-1", Name: "Local"}, nodes); err != nil {
		t.Fatal(err)
	}

	service := NewNodeService(store)
	results := service.TestLatency([]string{nodes[0].ID})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "" || results[0].LatencyMS <= 0 {
		t.Fatalf("unexpected latency result: %+v", results[0])
	}
}

func TestReplaceSubscriptionNodesPreservesIdentityAndEnabledState(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := []parsedNode{{Name: "Stable", Protocol: "vless", Address: "example.com", Port: 443, Raw: "vless://stable"}}
	first := buildNodes("sub-1", parsed)
	if err := store.AddSubscription(Subscription{ID: "sub-1", Name: "Stable Sub"}, first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetNodeEnabled(first[0].ID, false); err != nil {
		t.Fatal(err)
	}

	second := buildNodes("sub-1", parsed)
	if err := store.ReplaceSubscriptionNodes("sub-1", second); err != nil {
		t.Fatal(err)
	}
	nodes := store.NodesForSubscription("sub-1")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != first[0].ID || nodes[0].Enabled {
		t.Fatalf("node identity or enabled state was not preserved: %+v", nodes[0])
	}
}
