package routing

import (
	"encoding/json"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func TestNormalizeValidatesRules(t *testing.T) {
	rule, err := Normalize(model.RoutingRule{Type: "domain_suffix", Value: "example.com", Action: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Name == "" || rule.Action != "direct" {
		t.Fatalf("unexpected normalized rule: %+v", rule)
	}
	if _, err := Normalize(model.RoutingRule{Type: "domain_regex", Value: "["}); err == nil {
		t.Fatal("expected regex validation error")
	}
	if _, err := Normalize(model.RoutingRule{Type: "ip", Value: "not-an-ip"}); err == nil {
		t.Fatal("expected IP validation error")
	}
	if _, err := Normalize(model.RoutingRule{Type: "port", Value: "70000"}); err == nil {
		t.Fatal("expected port validation error")
	}
	if _, err := Normalize(model.RoutingRule{Type: "network", Value: "icmp"}); err == nil {
		t.Fatal("expected network validation error")
	}
}

func TestBuildXrayRules(t *testing.T) {
	rules := []model.RoutingRule{
		{Type: "domain_suffix", Value: "example.com,example.net", Action: "direct", Enabled: true},
		{Type: "domain_regex", Value: `^ads\.`, Action: "block", Enabled: true},
		{Type: "ip", Value: "10.0.0.0/8", Action: "direct", Enabled: true},
		{Type: "port", Value: "80,443", Action: "proxy", Enabled: true},
		{Type: "network", Value: "udp", Action: "direct", Enabled: true},
		{Type: "geoip", Value: "cn", Action: "direct", Enabled: false},
	}
	built := BuildXrayRules(rules, "rule")
	if len(built) != len(rules) {
		t.Fatalf("expected %d rules including catch-all, got %d", len(rules), len(built))
	}
	first := built[0].(map[string]any)
	domains := first["domain"].([]string)
	if len(domains) != 2 || domains[0] != "suffix:example.com" || domains[1] != "suffix:example.net" {
		t.Fatalf("unexpected domain rule: %+v", first)
	}
	if built[len(built)-1].(map[string]any)["outboundTag"] != "proxy" {
		t.Fatalf("expected proxy catch-all: %+v", built[len(built)-1])
	}
}

func TestBuildXrayRulesUsesDefaultsWhenEmpty(t *testing.T) {
	built := BuildXrayRules(nil, "rule")
	if len(built) != 3 {
		t.Fatalf("expected default private/direct, ads/block and proxy catch-all, got %d", len(built))
	}
	raw, err := json.Marshal(built)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" {
		t.Fatal("rules should be JSON serializable")
	}
}
