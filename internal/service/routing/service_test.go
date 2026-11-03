package routing

import (
	"path/filepath"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func TestRoutingServiceLifecycle(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)

	defaults := service.Reset()
	if len(defaults) == 0 {
		t.Fatal("expected default rules")
	}
	if len(service.List()) != len(defaults) {
		t.Fatal("default rules were not persisted")
	}

	saved, err := service.Save(model.RoutingRule{
		Type: "domain_suffix", Value: "example.com", Action: "direct", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.Name == "" {
		t.Fatalf("rule was not normalized: %+v", saved)
	}
	if len(service.List()) != len(defaults)+1 {
		t.Fatal("saved rule is missing")
	}

	ids := make([]string, 0)
	for _, rule := range service.List() {
		ids = append([]string{rule.ID}, ids...)
	}
	if err := service.Reorder(ids); err != nil {
		t.Fatal(err)
	}
	if service.List()[0].ID != saved.ID {
		t.Fatal("reorder did not move the saved rule to the front")
	}

	if err := service.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.List()) != len(defaults) {
		t.Fatal("rule was not deleted")
	}
}

func TestRoutingServiceRejectsInvalidRule(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)
	if _, err := service.Save(model.RoutingRule{Type: "unknown", Value: "x", Action: "proxy"}); err == nil {
		t.Fatal("expected invalid rule type error")
	}
}
