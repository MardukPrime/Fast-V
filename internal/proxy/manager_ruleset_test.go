package proxy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func TestMergeRoutingRules(t *testing.T) {
	user := []model.RoutingRule{
		{Name: "用户规则一", Type: "geosite", Value: "cn", Action: "direct", Enabled: true},
		{Name: "用户规则二", Type: "geoip", Value: "private", Action: "direct", Enabled: true},
	}
	extra := []model.RoutingRule{
		{Name: "规则集 域名", Type: "domain_suffix", Value: "example.com", Action: "block", Enabled: true},
	}
	merged := mergeRoutingRules(user, extra)
	if len(merged) != 3 {
		t.Fatalf("len(merged) = %d, want 3", len(merged))
	}
	if merged[0].Name != "用户规则一" || merged[1].Name != "用户规则二" {
		t.Fatalf("user rules were reordered: %+v", merged[:2])
	}
	if merged[2].Name != "规则集 域名" {
		t.Fatalf("extra rule missing from the end: %+v", merged)
	}

	if got := mergeRoutingRules(user, nil); len(got) != 2 {
		t.Fatalf("mergeRoutingRules(user, nil) = %+v, want the user rules unchanged", got)
	}
	if got := mergeRoutingRules(nil, extra); len(got) != 1 || got[0].Name != "规则集 域名" {
		t.Fatalf("mergeRoutingRules(nil, extra) = %+v", got)
	}
}

func TestManagerExtraRulesProvider(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("NewStoreAt() error = %v", err)
	}
	manager := NewManager(st, fakeCoreProvider{})
	if rules := manager.extraRules(); rules != nil {
		t.Fatalf("extraRules() without provider = %+v, want nil", rules)
	}

	calls := 0
	manager.SetExtraRulesProvider(func() []model.RoutingRule {
		calls++
		return []model.RoutingRule{{Name: "规则集 域名", Type: "domain_suffix", Value: "example.com", Action: "block", Enabled: true}}
	})
	rules := manager.extraRules()
	if len(rules) != 1 || rules[0].Name != "规则集 域名" {
		t.Fatalf("extraRules() = %+v", rules)
	}
	if calls != 1 {
		t.Fatalf("provider called %d times, want 1", calls)
	}

	// The provider must be invoked without the manager lock: a provider that
	// reads manager state would deadlock if the lock were still held.
	manager.SetExtraRulesProvider(func() []model.RoutingRule {
		state := manager.State()
		if state.Status != "stopped" {
			t.Errorf("manager state inside provider = %+v", state)
		}
		return nil
	})
	done := make(chan struct{})
	go func() {
		manager.extraRules()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("extra rules provider was invoked while holding the manager lock")
	}

	// Start consults the provider before building a configuration; the fake
	// core is not installed, which is enough to prove the lookup happened.
	manager.SetExtraRulesProvider(func() []model.RoutingRule {
		calls++
		return nil
	})
	if _, err := manager.Start(context.Background()); err == nil {
		t.Fatal("Start() error = nil without an installed core")
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2 after Start", calls)
	}

	manager.SetExtraRulesProvider(nil)
	if rules := manager.extraRules(); rules != nil {
		t.Fatalf("extraRules() after clearing = %+v, want nil", rules)
	}
}
