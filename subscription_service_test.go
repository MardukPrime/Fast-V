package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSubscriptionServiceAddUpdateDelete(t *testing.T) {
	content := "vless://00000000-0000-0000-0000-000000000003@example.com:443?security=tls#Node"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, encoded)
	}))
	defer server.Close()

	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewSubscriptionService(store)

	sub, err := service.Add(SubscriptionInput{Name: "Test", URL: server.URL})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if sub.NodeCount != 1 {
		t.Fatalf("expected 1 node, got %d", sub.NodeCount)
	}

	list := service.List()
	if len(list) != 1 || list[0].Name != "Test" {
		t.Fatalf("unexpected list: %+v", list)
	}

	nodes, err := service.Nodes(sub.ID)
	if err != nil {
		t.Fatalf("nodes failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Protocol != "vless" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}

	updated, err := service.Update(sub.ID)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.NodeCount != 1 || updated.LastError != "" {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	if err := service.Delete(sub.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(service.List()) != 0 {
		t.Fatal("subscription was not deleted")
	}
}

func TestSubscriptionServiceAddRejectsInvalidURL(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewSubscriptionService(store)
	if _, err := service.Add(SubscriptionInput{URL: "not-a-url"}); err == nil {
		t.Fatal("expected invalid URL error")
	}
}
