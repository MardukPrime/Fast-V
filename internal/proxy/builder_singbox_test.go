package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func singBoxTestSettings() model.Settings {
	return model.Settings{
		CoreType:      "singbox",
		ProxyMode:     "rule",
		XrayLocalPort: 18080,
		TestURL:       "http://127.0.0.1:9004/data.bin",
	}
}

func singBoxTestNode() model.Node {
	return model.Node{
		ID:           "node-ss-local",
		Name:         "本地 SS 测试",
		Protocol:     "shadowsocks",
		Address:      "127.0.0.1",
		Port:         18388,
		Enabled:      true,
		OutboundJSON: `{"protocol":"shadowsocks","settings":{"address":"127.0.0.1","port":18388,"method":"aes-128-gcm","password":"testpass"}}`,
	}
}

func TestBuildSingBoxConfigShadowsocks(t *testing.T) {
	raw, err := BuildSingBoxConfig(singBoxTestNode(), singBoxTestSettings())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	inbounds, _ := doc["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("inbounds = %d", len(inbounds))
	}
	inbound := inbounds[0].(map[string]any)
	if inbound["type"] != "mixed" || inbound["listen_port"].(float64) != 18080 {
		t.Fatalf("unexpected inbound: %#v", inbound)
	}
	outbounds, _ := doc["outbounds"].([]any)
	if len(outbounds) != 3 {
		t.Fatalf("outbounds = %d", len(outbounds))
	}
	proxy := outbounds[0].(map[string]any)
	if proxy["type"] != "shadowsocks" || proxy["tag"] != "proxy" {
		t.Fatalf("unexpected outbound: %#v", proxy)
	}
	route := doc["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("final = %v", route["final"])
	}
	if dump := os.Getenv("FASTV_DUMP_SINGBOX_CONFIG"); dump != "" {
		if err := os.WriteFile(dump, raw, 0o600); err != nil {
			t.Fatalf("dump: %v", err)
		}
	}
}

func TestBuildSingBoxRouteRules(t *testing.T) {
	settings := singBoxTestSettings()
	rules := []model.RoutingRule{
		{Type: "domain_suffix", Value: "example.com, example.org", Action: "proxy", Enabled: true},
		{Type: "domain_keyword", Value: "ads", Action: "block", Enabled: true},
		{Type: "ip", Value: "10.0.0.0/8", Action: "direct", Enabled: true},
		{Type: "port", Value: "443, 8000-9000", Action: "proxy", Enabled: true},
		{Type: "network", Value: "udp", Action: "block", Enabled: true},
		{Type: "geosite", Value: "cn", Action: "direct", Enabled: true},
		{Type: "geoip", Value: "cn", Action: "direct", Enabled: true},
		{Type: "domain_suffix", Value: "disabled.example", Action: "proxy", Enabled: false},
	}
	geoDir := t.TempDir()
	raw, err := BuildSingBoxConfigWithGeoAssets(singBoxTestNode(), settings, geoDir, rules...)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var doc struct {
		Route struct {
			Rules   []map[string]any `json:"rules"`
			Final   string           `json:"final"`
			RuleSet []map[string]any `json:"rule_set"`
		} `json:"route"`
		Experimental struct {
			CacheFile map[string]any `json:"cache_file"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dump := os.Getenv("FASTV_DUMP_SINGBOX_GEO_CONFIG"); dump != "" {
		if err := os.WriteFile(dump, raw, 0o600); err != nil {
			t.Fatalf("dump: %v", err)
		}
	}
	if doc.Route.Final != "proxy" {
		t.Fatalf("final = %s", doc.Route.Final)
	}
	if len(doc.Route.Rules) != 7 {
		t.Fatalf("rules = %d, want 7", len(doc.Route.Rules))
	}
	if values, ok := doc.Route.Rules[0]["domain_suffix"].([]any); !ok || len(values) != 2 {
		t.Fatalf("domain_suffix = %#v", doc.Route.Rules[0]["domain_suffix"])
	}
	if doc.Route.Rules[1]["action"] != "reject" {
		t.Fatalf("block action = %#v", doc.Route.Rules[1])
	}
	if doc.Route.Rules[2]["outbound"] != "direct" {
		t.Fatalf("direct rule = %#v", doc.Route.Rules[2])
	}
	if values, ok := doc.Route.Rules[3]["port"].([]any); !ok || len(values) != 1 {
		t.Fatalf("port = %#v", doc.Route.Rules[3]["port"])
	}
	if _, ok := doc.Route.Rules[3]["port_range"]; !ok {
		t.Fatalf("port_range missing: %#v", doc.Route.Rules[3])
	}
	if tags, ok := doc.Route.Rules[5]["rule_set"].([]any); !ok || len(tags) != 1 || tags[0] != "geosite-cn" {
		t.Fatalf("geosite rule = %#v", doc.Route.Rules[5])
	}
	if tags, ok := doc.Route.Rules[6]["rule_set"].([]any); !ok || len(tags) != 1 || tags[0] != "geoip-cn" {
		t.Fatalf("geoip rule = %#v", doc.Route.Rules[6])
	}
	if len(doc.Route.RuleSet) != 2 {
		t.Fatalf("rule_set definitions = %#v", doc.Route.RuleSet)
	}
	foundGeosite := false
	for _, definition := range doc.Route.RuleSet {
		url, _ := definition["url"].(string)
		if definition["download_detour"] != proxyOutboundTag || definition["format"] != "binary" {
			t.Fatalf("rule_set definition = %#v", definition)
		}
		if strings.Contains(url, "sing-geosite") && strings.Contains(url, "geosite-cn.srs") {
			foundGeosite = true
		}
	}
	if !foundGeosite {
		t.Fatalf("geosite rule_set url missing: %#v", doc.Route.RuleSet)
	}
	if doc.Experimental.CacheFile["path"] != filepath.Join(geoDir, "cache.db") {
		t.Fatalf("cache_file = %#v", doc.Experimental.CacheFile)
	}
	if dump := os.Getenv("FASTV_DUMP_SINGBOX_GEO_CONFIG"); dump != "" {
		if err := os.WriteFile(dump, raw, 0o600); err != nil {
			t.Fatalf("dump: %v", err)
		}
	}
}

func TestBuildSingBoxDirectModeFinal(t *testing.T) {
	settings := singBoxTestSettings()
	settings.ProxyMode = "direct"
	raw, err := BuildSingBoxConfig(singBoxTestNode(), settings)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	if doc["route"].(map[string]any)["final"] != "direct" {
		t.Fatalf("final = %v", doc["route"])
	}
}

func TestBuildSingBoxTunConfig(t *testing.T) {
	settings := singBoxTestSettings()
	settings.TunMode = true
	settings.XrayTunMTU = 1400
	raw, err := BuildSingBoxConfig(singBoxTestNode(), settings)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var doc struct {
		Inbounds []map[string]any `json:"inbounds"`
		DNS      map[string]any   `json:"dns"`
		Route    struct {
			Rules []map[string]any `json:"rules"`
			Final string           `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(doc.Inbounds) != 2 {
		t.Fatalf("inbounds = %d, want TUN + mixed", len(doc.Inbounds))
	}
	tun := doc.Inbounds[0]
	if tun["type"] != "tun" || tun["interface_name"] != tunInterfaceName {
		t.Fatalf("unexpected TUN inbound: %#v", tun)
	}
	if tun["auto_route"] != true {
		t.Fatalf("auto_route missing: %#v", tun)
	}
	if mtu, ok := tun["mtu"].(float64); !ok || int(mtu) != 1400 {
		t.Fatalf("mtu = %#v", tun["mtu"])
	}
	if doc.Inbounds[1]["type"] != "mixed" {
		t.Fatalf("mixed inbound missing: %#v", doc.Inbounds[1])
	}
	if doc.DNS == nil {
		t.Fatal("TUN config is missing dns section")
	}
	if len(doc.Route.Rules) < 2 || doc.Route.Rules[0]["action"] != "hijack-dns" {
		t.Fatalf("DNS hijack rule missing: %#v", doc.Route.Rules)
	}
	if doc.Route.Rules[1]["ip_is_private"] != true || doc.Route.Rules[1]["outbound"] != "direct" {
		t.Fatalf("private IP direct rule missing: %#v", doc.Route.Rules[1])
	}
	if doc.Route.Final != "proxy" {
		t.Fatalf("final = %s", doc.Route.Final)
	}
	if dump := os.Getenv("FASTV_DUMP_SINGBOX_TUN_CONFIG"); dump != "" {
		if err := os.WriteFile(dump, raw, 0o600); err != nil {
			t.Fatalf("dump: %v", err)
		}
	}
}
