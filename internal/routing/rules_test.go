package routing

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestNormalizeProcessRules(t *testing.T) {
	rule, err := Normalize(model.RoutingRule{Type: "process", Value: " chrome.exe, Telegram.exe\nCode.exe "})
	if err != nil {
		t.Fatalf("process rule rejected: %v", err)
	}
	if rule.Type != "process" || rule.Action != "proxy" {
		t.Fatalf("unexpected normalized process rule: %+v", rule)
	}
	if rule.Name == "" {
		t.Fatal("process rule name should be generated automatically")
	}

	for _, value := range []string{"", " ", " , \n "} {
		if _, err := Normalize(model.RoutingRule{Type: "process", Value: value}); err == nil {
			t.Fatalf("expected empty process value %q to be rejected", value)
		}
	}
}

func TestBuildXrayRulesProcessField(t *testing.T) {
	built := BuildXrayRules([]model.RoutingRule{
		{Type: "process", Value: "chrome.exe,Telegram.exe", Action: "direct", Enabled: true},
		{Type: "process", Value: "disabled.exe", Action: "proxy", Enabled: false},
	}, "rule")
	if len(built) != 2 {
		t.Fatalf("expected one process rule and a catch-all, got %d: %+v", len(built), built)
	}
	first, ok := built[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected rule type: %T", built[0])
	}
	processes, ok := first["process"].([]string)
	if !ok || len(processes) != 2 || processes[0] != "chrome.exe" || processes[1] != "Telegram.exe" {
		t.Fatalf("unexpected process mapping: %+v", first)
	}
	if first["outboundTag"] != "direct" {
		t.Fatalf("unexpected process rule action: %+v", first)
	}
}

func TestPresetsAreValidAndComplete(t *testing.T) {
	presets := Presets()
	if len(presets) < 4 {
		t.Fatalf("expected at least 4 routing presets, got %d", len(presets))
	}
	seen := make(map[string]struct{}, len(presets))
	for _, preset := range presets {
		if preset.ID == "" || preset.Name == "" || len(preset.Rules) == 0 {
			t.Fatalf("incomplete preset: %+v", preset)
		}
		if _, ok := seen[preset.ID]; ok {
			t.Fatalf("duplicate preset ID: %s", preset.ID)
		}
		seen[preset.ID] = struct{}{}
		for _, rule := range preset.Rules {
			normalized, err := Normalize(rule)
			if err != nil {
				t.Fatalf("preset %s contains an invalid rule: %v", preset.ID, err)
			}
			if !normalized.Enabled {
				t.Fatalf("preset %s rule should be enabled: %+v", preset.ID, rule)
			}
		}
	}
}

func TestProcessRulesPassXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	rules := []model.RoutingRule{
		{ID: "r1", Name: "Chrome Direct", Type: "process", Value: "chrome.exe,Telegram.exe", Action: "direct", Enabled: true},
		{ID: "r2", Name: "Editor Proxy", Type: "process", Value: "Code.exe", Action: "proxy", Enabled: true},
	}
	raw, err := buildValidationConfig(rules)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "process-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray process routing config validation failed: %v\n%s", err, output)
	}
}

// buildValidationConfig builds the smallest complete Xray config that exercises
// routing rule generation against the real core binary.
func buildValidationConfig(rules []model.RoutingRule) ([]byte, error) {
	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"tag":      "local-in",
			"listen":   "127.0.0.1",
			"port":     19281,
			"protocol": "socks",
			"settings": map[string]any{"auth": "noauth", "udp": true},
			"sniffing": map[string]any{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
			},
		}},
		"outbounds": []any{
			map[string]any{"tag": "proxy", "protocol": "freedom"},
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          BuildXrayRules(rules, "rule"),
		},
	}
	return json.MarshalIndent(config, "", "  ")
}
