package proxy

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func testSettings() model.Settings {
	return model.Settings{
		CoreType:           "xray",
		XrayLocalPort:      10809,
		XrayLogLevel:       "debug",
		XrayMuxEnabled:     true,
		XrayMuxConcurrency: 16,
		XraySniffing:       true,
		ProxyMode:          "rule",
		TestURL:            "https://www.gstatic.com/generate_204",
	}
}

func TestBuildConfigUsesSettingsAndNode(t *testing.T) {
	node := model.Node{
		Protocol: "vless",
		Raw:      "vless://00000000-0000-0000-0000-000000000002@vless.example.com:443?security=tls&type=ws&path=%2Fws&host=vless.example.com&sni=vless.example.com#VLESS",
	}
	settings := testSettings()

	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}

	logConfig := config["log"].(map[string]any)
	if logConfig["loglevel"] != "debug" {
		t.Fatalf("unexpected log config: %+v", logConfig)
	}

	inbounds := config["inbounds"].([]any)
	inbound := inbounds[0].(map[string]any)
	if int(inbound["port"].(float64)) != 10809 {
		t.Fatalf("unexpected inbound port: %+v", inbound)
	}
	sniffing := inbound["sniffing"].(map[string]any)
	if sniffing["enabled"] != true {
		t.Fatalf("unexpected sniffing config: %+v", sniffing)
	}

	outbounds := config["outbounds"].([]any)
	outbound := outbounds[0].(map[string]any)
	if outbound["protocol"] != "vless" {
		t.Fatalf("unexpected outbound protocol: %+v", outbound)
	}
	mux := outbound["mux"].(map[string]any)
	if mux["enabled"] != true || int(mux["concurrency"].(float64)) != 16 {
		t.Fatalf("unexpected mux config: %+v", mux)
	}
	stream := outbound["streamSettings"].(map[string]any)
	if stream["network"] != "websocket" || stream["security"] != "tls" {
		t.Fatalf("unexpected stream settings: %+v", stream)
	}
	// Xray silently ignores unknown stream keys, so a typo here degrades the node
	// to raw TCP without any error. Keep the old "method" key out for good.
	if _, bad := stream["method"]; bad {
		t.Fatalf("stream settings must not use the ignored \"method\" key: %+v", stream)
	}

	routing := config["routing"].(map[string]any)
	if len(routing["rules"].([]any)) != 3 {
		t.Fatalf("unexpected routing rules: %+v", routing)
	}
}

func TestBuildConfigRoutingModes(t *testing.T) {
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Trojan"}
	for _, mode := range []string{"direct", "global", "rule"} {
		settings := testSettings()
		settings.ProxyMode = mode
		raw, err := BuildConfig(node, settings)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		var config map[string]any
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		rules := config["routing"].(map[string]any)["rules"].([]any)
		if len(rules) == 0 {
			t.Fatalf("%s: expected routing rules", mode)
		}
	}
}

func TestBuildConfigSupportsCommonProtocols(t *testing.T) {
	vmessData, _ := json.Marshal(map[string]any{
		"ps": "VMess", "add": "vmess.example.com", "port": 443,
		"id": "00000000-0000-0000-0000-000000000001", "net": "ws", "tls": "tls", "path": "/vm",
	})
	ssUser := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:password"))
	cases := []model.Node{
		{Protocol: "vmess", Raw: "vmess://" + base64.StdEncoding.EncodeToString(vmessData)},
		{Protocol: "vless", Raw: "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls#VLESS"},
		{Protocol: "trojan", Raw: "trojan://password@example.com:443#Trojan"},
		{Protocol: "shadowsocks", Raw: "ss://" + ssUser + "@example.com:8388#SS"},
	}
	for _, node := range cases {
		if _, err := BuildConfig(node, testSettings()); err != nil {
			t.Fatalf("%s config failed: %v", node.Protocol, err)
		}
	}
}

func TestConfigPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	node := model.Node{
		Protocol: "vless",
		Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls&type=ws&path=%2Fws&host=example.com&sni=example.com#Validation",
	}
	raw, err := BuildConfig(node, testSettings())
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray config validation failed: %v\n%s", err, output)
	}
}

func TestBuildConfigIncludesTunInbound(t *testing.T) {
	settings := testSettings()
	settings.TunMode = true
	node := model.Node{Protocol: "vless", Raw: "vless://00000000-0000-0000-0000-000000000002@example.com:443#TUN"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	inbounds := config["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("expected TUN and local inbound, got %+v", inbounds)
	}
	tunInbound := inbounds[0].(map[string]any)
	if tunInbound["protocol"] != "tun" {
		t.Fatalf("unexpected tun inbound: %+v", tunInbound)
	}
	tunSettings := tunInbound["settings"].(map[string]any)
	if tunSettings["name"] != tunInterfaceName {
		t.Fatalf("unexpected tun settings: %+v", tunSettings)
	}
	localInbound := inbounds[1].(map[string]any)
	if localInbound["protocol"] != "socks" {
		t.Fatalf("unexpected local inbound: %+v", localInbound)
	}
}

func TestTunConfigPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	settings := testSettings()
	settings.TunMode = true
	node := model.Node{
		Protocol: "vless",
		Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls#TUN",
	}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "tun-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray TUN config validation failed: %v\n%s", err, output)
	}
}

func TestBuildConfigUsesAdvancedOutboundJSON(t *testing.T) {
	node := model.Node{
		Protocol:     "trojan",
		Raw:          "trojan://password@example.com:443#Original",
		OutboundJSON: `{"protocol":"trojan","settings":{"address":"advanced.example.com","port":8443,"password":"advanced-password"}}`,
	}
	raw, err := BuildConfig(node, testSettings())
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	outbound := config["outbounds"].([]any)[0].(map[string]any)
	if outbound["protocol"] != "trojan" || outbound["tag"] != "proxy" {
		t.Fatalf("unexpected advanced outbound: %+v", outbound)
	}
	settings := outbound["settings"].(map[string]any)
	if settings["address"] != "advanced.example.com" || settings["port"].(float64) != 8443 {
		t.Fatalf("advanced outbound was not used: %+v", settings)
	}
}

func TestBuildConfigIncludesCustomRoutingRules(t *testing.T) {
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Trojan"}
	rule := model.RoutingRule{ID: "rule-1", Name: "Direct Example", Type: "domain_suffix", Value: "example.com", Action: "direct", Enabled: true}
	raw, err := BuildConfig(node, testSettings(), rule)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	rules := config["routing"].(map[string]any)["rules"].([]any)
	if len(rules) < 2 {
		t.Fatalf("expected custom rule and catch-all: %+v", rules)
	}
	first := rules[0].(map[string]any)
	if first["outboundTag"] != "direct" {
		t.Fatalf("unexpected custom rule action: %+v", first)
	}
	domains := first["domain"].([]any)
	if len(domains) != 1 || domains[0] != "suffix:example.com" {
		t.Fatalf("unexpected custom rule domain: %+v", first)
	}
}

func TestCustomRoutingPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Routing"}
	rules := []model.RoutingRule{
		{ID: "r1", Name: "Direct", Type: "domain_suffix", Value: "example.com", Action: "direct", Enabled: true},
		{ID: "r2", Name: "Block Ads", Type: "geosite", Value: "category-ads-all", Action: "block", Enabled: true},
	}
	raw, err := BuildConfig(node, testSettings(), rules...)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "routing-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray routing config validation failed: %v\n%s", err, output)
	}
}

func TestBuildConfigIncludesXrayAPI(t *testing.T) {
	settings := testSettings()
	settings.XrayAPIEnabled = true
	settings.XrayAPIPort = 10085
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#API"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	api, ok := config["api"].(map[string]any)
	if !ok || api["tag"] != "api" {
		t.Fatalf("unexpected api config: %+v", config["api"])
	}
	if _, ok := config["stats"]; !ok {
		t.Fatal("stats config is missing")
	}
	if _, ok := config["policy"]; !ok {
		t.Fatal("policy config is missing")
	}
	inbounds := config["inbounds"].([]any)
	if len(inbounds) != 2 || inbounds[1].(map[string]any)["tag"] != "api-in" {
		t.Fatalf("unexpected inbounds: %+v", inbounds)
	}
	rules := config["routing"].(map[string]any)["rules"].([]any)
	if rules[0].(map[string]any)["outboundTag"] != "api" {
		t.Fatalf("api routing rule is missing: %+v", rules)
	}
}

func TestAPIConfigPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	settings := testSettings()
	settings.XrayAPIEnabled = true
	settings.XrayAPIPort = 10085
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#API"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "api-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray API config validation failed: %v\n%s", err, output)
	}
}

func TestBuildConfigIncludesStatsAPIWhenStatsEnabled(t *testing.T) {
	settings := testSettings()
	settings.StatsEnabled = true
	settings.XrayAPIEnabled = false
	settings.XrayAPIPort = 10086
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Stats"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}

	api, ok := config["api"].(map[string]any)
	if !ok || api["tag"] != "api" {
		t.Fatalf("stats mode must generate api config: %+v", config["api"])
	}
	services, _ := api["services"].([]any)
	mustContainValue(t, services, "StatsService")
	if _, ok := config["stats"]; !ok {
		t.Fatal("stats mode must generate stats config")
	}
	policy, ok := config["policy"].(map[string]any)
	if !ok {
		t.Fatalf("stats mode must generate policy config: %+v", config["policy"])
	}
	system, _ := policy["system"].(map[string]any)
	if system["statsOutboundUplink"] != true || system["statsOutboundDownlink"] != true {
		t.Fatalf("outbound traffic counters are not enabled: %+v", system)
	}

	inbounds := config["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("expected local and api inbounds, got %+v", inbounds)
	}
	apiInbound, _ := inbounds[1].(map[string]any)
	if apiInbound["tag"] != "api-in" || apiInbound["protocol"] != "dokodemo-door" {
		t.Fatalf("unexpected api inbound: %+v", apiInbound)
	}
	if int(apiInbound["port"].(float64)) != 10086 {
		t.Fatalf("unexpected api inbound port: %+v", apiInbound)
	}

	rules, _ := config["routing"].(map[string]any)["rules"].([]any)
	if len(rules) == 0 {
		t.Fatal("routing rules are missing")
	}
	first, _ := rules[0].(map[string]any)
	if first["outboundTag"] != "api" {
		t.Fatalf("api routing rule is missing: %+v", first)
	}
	mustContainValue(t, first["inboundTag"], "api-in")
}

func TestBuildConfigOmitsStatsAPIWhenDisabled(t *testing.T) {
	settings := testSettings()
	settings.StatsEnabled = false
	settings.XrayAPIEnabled = false
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#NoStats"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"api", "stats", "policy"} {
		if value, ok := config[key]; ok {
			t.Fatalf("expected no %q config when stats/API are disabled, got %+v", key, value)
		}
	}
	if inbounds := config["inbounds"].([]any); len(inbounds) != 1 {
		t.Fatalf("expected a single inbound, got %+v", inbounds)
	}
	rules := config["routing"].(map[string]any)["rules"].([]any)
	for _, rule := range rules {
		if entry, ok := rule.(map[string]any); ok && entry["outboundTag"] == "api" {
			t.Fatalf("unexpected api routing rule: %+v", entry)
		}
	}
}

func TestStatsConfigPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	cases := []struct {
		name   string
		modify func(*model.Settings)
	}{
		{"stats only", func(settings *model.Settings) {
			settings.StatsEnabled = true
			settings.XrayAPIEnabled = false
		}},
		{"stats with tun", func(settings *model.Settings) {
			settings.StatsEnabled = true
			settings.XrayAPIEnabled = false
			settings.TunMode = true
		}},
		{"api and stats", func(settings *model.Settings) {
			settings.StatsEnabled = true
			settings.XrayAPIEnabled = true
		}},
	}
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Stats"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := testSettings()
			settings.XrayAPIPort = 10086
			tc.modify(&settings)
			raw, err := BuildConfig(node, settings)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "stats-config.json")
			if err := os.WriteFile(configPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "run", "-test", "-config", configPath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("xray stats config validation failed: %v\n%s", err, output)
			}
		})
	}
}

func TestBuildConfigLocalInboundListen(t *testing.T) {
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#LAN"}
	cases := []struct {
		name     string
		allowLan bool
		tunMode  bool
	}{
		{"loopback only", false, false},
		{"allow lan", true, false},
		{"tun mode keeps local inbound", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := testSettings()
			settings.AllowLan = tc.allowLan
			settings.TunMode = tc.tunMode
			raw, err := BuildConfig(node, settings)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			if tc.tunMode {
				tunInbound := config["inbounds"].([]any)[0].(map[string]any)
				if tunInbound["protocol"] != "tun" {
					t.Fatalf("unexpected TUN inbound: %+v", tunInbound)
				}
				if _, ok := tunInbound["listen"]; ok {
					t.Fatalf("TUN inbound must not gain a listen address: %+v", tunInbound)
				}
			}
			localInbound := findSocksInbound(t, config)
			want := "127.0.0.1"
			if tc.allowLan {
				want = "0.0.0.0"
			}
			if localInbound["listen"] != want {
				t.Fatalf("expected listen %q, got %+v", want, localInbound)
			}
		})
	}
}

func TestBuildConfigLocalInboundAuth(t *testing.T) {
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Auth"}

	disabled := testSettings()
	raw, err := BuildConfig(node, disabled)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	inboundSettings := findSocksInbound(t, config)["settings"].(map[string]any)
	if inboundSettings["auth"] != "noauth" {
		t.Fatalf("expected noauth when inbound auth is disabled: %+v", inboundSettings)
	}
	if _, ok := inboundSettings["accounts"]; ok {
		t.Fatalf("noauth inbound must not contain accounts: %+v", inboundSettings)
	}

	enabled := testSettings()
	enabled.InboundAuthEnabled = true
	enabled.InboundUsername = "fastv"
	enabled.InboundPassword = "fastv-pass"
	raw, err = BuildConfig(node, enabled)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	inboundSettings = findSocksInbound(t, config)["settings"].(map[string]any)
	if inboundSettings["auth"] != "password" {
		t.Fatalf("expected password auth when inbound auth is enabled: %+v", inboundSettings)
	}
	accounts, ok := inboundSettings["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("expected a single inbound account: %+v", inboundSettings)
	}
	account := accounts[0].(map[string]any)
	if account["user"] != "fastv" || account["pass"] != "fastv-pass" {
		t.Fatalf("unexpected inbound account: %+v", account)
	}
}

func TestInboundAuthConfigPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	settings := testSettings()
	settings.AllowLan = true
	settings.InboundAuthEnabled = true
	settings.InboundUsername = "fastv"
	settings.InboundPassword = "fastv-pass"
	node := model.Node{Protocol: "vless", Raw: "vless://00000000-0000-0000-0000-000000000005@example.com:443?security=tls#InboundAuth"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "inbound-auth-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray inbound auth config validation failed: %v\n%s", err, output)
	}
}

func findSocksInbound(t *testing.T, config map[string]any) map[string]any {
	t.Helper()
	inbounds, ok := config["inbounds"].([]any)
	if !ok {
		t.Fatalf("inbounds are missing: %+v", config)
	}
	for _, entry := range inbounds {
		inbound, ok := entry.(map[string]any)
		if ok && inbound["protocol"] == "socks" {
			return inbound
		}
	}
	t.Fatalf("local SOCKS inbound is missing: %+v", inbounds)
	return nil
}

func mustContainValue(t *testing.T, values any, want string) {
	t.Helper()
	list, ok := values.([]any)
	if !ok {
		t.Fatalf("expected a list, got %+v", values)
	}
	for _, value := range list {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %+v", want, list)
}

func TestBuildConfigUsesAdvancedNetworkSettings(t *testing.T) {
	settings := testSettings()
	settings.TunMode = true
	settings.XrayTunMTU = 1400
	settings.XrayDNSStrategy = "UseIPv4"
	settings.XrayDNSServers = "1.1.1.1, 9.9.9.9"
	settings.XrayDomainStrategy = "IPOnDemand"
	node := model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Advanced"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	dns := config["dns"].(map[string]any)
	servers := dns["servers"].([]any)
	if len(servers) != 2 || dns["queryStrategy"] != "UseIPv4" {
		t.Fatalf("unexpected dns config: %+v", dns)
	}
	routing := config["routing"].(map[string]any)
	if routing["domainStrategy"] != "IPOnDemand" {
		t.Fatalf("unexpected routing strategy: %+v", routing)
	}
	tun := config["inbounds"].([]any)[0].(map[string]any)
	if tun["settings"].(map[string]any)["MTU"].(float64) != 1400 {
		t.Fatalf("unexpected TUN settings: %+v", tun)
	}
}

func TestBuildConfigIncludesSockoptAndFinalMask(t *testing.T) {
	settings := testSettings()
	settings.XraySockoptTFO = true
	settings.XraySockoptMPTCP = true
	settings.XraySockoptKeepIdle = 30
	settings.XraySockoptKeepIntvl = 10
	settings.XraySockoptUserTimeout = 60000
	settings.XraySockoptCongestion = "bbr"
	settings.XraySockoptDomainStrategy = "UseIPv4"
	settings.XrayFinalMaskJSON = `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}}]}`
	node := model.Node{Protocol: "vless", Raw: "vless://00000000-0000-0000-0000-000000000004@example.com:443?security=tls&type=tcp#FinalMask"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	outbound := config["outbounds"].([]any)[0].(map[string]any)
	stream := outbound["streamSettings"].(map[string]any)
	sockopt := stream["sockopt"].(map[string]any)
	if sockopt["tcpFastOpen"] != true || sockopt["tcpMptcp"] != true || sockopt["tcpKeepAliveIdle"].(float64) != 30 {
		t.Fatalf("unexpected sockopt: %+v", sockopt)
	}
	finalmask := stream["finalmask"].(map[string]any)
	if len(finalmask["tcp"].([]any)) != 1 {
		t.Fatalf("unexpected finalmask: %+v", finalmask)
	}
}

func TestSockoptFinalMaskPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	settings := testSettings()
	settings.XraySockoptTFO = true
	settings.XraySockoptKeepIdle = 30
	settings.XraySockoptKeepIntvl = 10
	settings.XraySockoptUserTimeout = 60000
	settings.XrayFinalMaskJSON = `{"tcp":[{"type":"fragment","settings":{"packets":"tlshello","length":"100-200","delay":"10-20"}}]}`
	node := model.Node{Protocol: "vless", Raw: "vless://00000000-0000-0000-0000-000000000004@example.com:443?security=tls&type=tcp#FinalMask"}
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "finalmask-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-test", "-config", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("xray finalmask/sockopt config validation failed: %v\n%s", err, output)
	}
}

// loadBalanceNodes returns two enabled VLESS nodes for load balancing tests.
func loadBalanceNodes() []model.Node {
	return []model.Node{
		{
			ID: "node-0", Name: "Node 0", Protocol: "vless", Address: "node0.example.com", Port: 443, Enabled: true,
			Raw: "vless://00000000-0000-0000-0000-000000000011@node0.example.com:443?security=tls#Node0",
		},
		{
			ID: "node-1", Name: "Node 1", Protocol: "vless", Address: "node1.example.com", Port: 443, Enabled: true,
			Raw: "vless://00000000-0000-0000-0000-000000000012@node1.example.com:443?security=tls#Node1",
		},
	}
}

func TestBuildLoadBalanceConfigOutboundsAndBalancer(t *testing.T) {
	settings := testSettings()
	settings.LoadBalanceEnabled = true
	settings.LoadBalanceMode = "leastPing"
	settings.XrayAPIEnabled = true
	settings.XrayAPIPort = 10085
	raw, err := BuildLoadBalanceConfig(loadBalanceNodes(), settings)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}

	outbounds, _ := config["outbounds"].([]any)
	if len(outbounds) != 4 {
		t.Fatalf("expected two node outbounds and direct/block, got %+v", outbounds)
	}
	wantTags := []string{"balancer-node-0", "balancer-node-1", "direct", "block"}
	for i, want := range wantTags {
		outbound, _ := outbounds[i].(map[string]any)
		if outbound["tag"] != want {
			t.Fatalf("outbound %d: expected tag %q, got %+v", i, want, outbound)
		}
	}
	first, _ := outbounds[0].(map[string]any)
	if first["protocol"] != "vless" {
		t.Fatalf("unexpected node outbound: %+v", first)
	}
	// The normal outbound path (mux here) must keep working for node outbounds.
	mux, _ := first["mux"].(map[string]any)
	if mux["enabled"] != true || int(mux["concurrency"].(float64)) != 16 {
		t.Fatalf("node outbound lost the mux settings: %+v", first)
	}

	routing := config["routing"].(map[string]any)
	balancers, _ := routing["balancers"].([]any)
	if len(balancers) != 1 {
		t.Fatalf("expected one balancer, got %+v", routing["balancers"])
	}
	balancer, _ := balancers[0].(map[string]any)
	if balancer["tag"] != "proxy" {
		t.Fatalf("balancer tag must stay \"proxy\" for routing compatibility: %+v", balancer)
	}
	mustContainValue(t, balancer["selector"], "balancer-node-")
	strategy, _ := balancer["strategy"].(map[string]any)
	if strategy["type"] != "leastPing" {
		t.Fatalf("unexpected balancer strategy: %+v", strategy)
	}

	observatory, ok := config["observatory"].(map[string]any)
	if !ok {
		t.Fatalf("observatory is required for leastPing: %+v", config["observatory"])
	}
	mustContainValue(t, observatory["subjectSelector"], "balancer-node-")
	if observatory["probeURL"] != settings.TestURL {
		t.Fatalf("observatory probe URL must follow the test URL: %+v", observatory)
	}
	if observatory["probeInterval"] != "300s" {
		t.Fatalf("observatory probe interval must be the duration string 300s: %+v", observatory)
	}
	if observatory["enableConcurrency"] != true {
		t.Fatalf("observatory concurrent probing is expected: %+v", observatory)
	}

	rules, _ := routing["rules"].([]any)
	if len(rules) != 4 {
		t.Fatalf("expected api rule plus the default rules, got %+v", rules)
	}
	if apiRule, _ := rules[0].(map[string]any); apiRule["outboundTag"] != "api" {
		t.Fatalf("api routing rule must stay untouched: %+v", apiRule)
	}
	catchAll, _ := rules[len(rules)-1].(map[string]any)
	if catchAll["balancerTag"] != "proxy" {
		t.Fatalf("proxy rules must target the balancer tag: %+v", catchAll)
	}
	if _, ok := catchAll["outboundTag"]; ok {
		t.Fatalf("proxy rules must not keep an outboundTag: %+v", catchAll)
	}
}

func TestBuildLoadBalanceConfigRewritesProxyRules(t *testing.T) {
	settings := testSettings()
	settings.LoadBalanceEnabled = true
	proxyRule := model.RoutingRule{ID: "r1", Name: "Proxy Example", Type: "domain_suffix", Value: "example.com", Action: "proxy", Enabled: true}
	directRule := model.RoutingRule{ID: "r2", Name: "Direct Private", Type: "geoip", Value: "private", Action: "direct", Enabled: true}
	blockRule := model.RoutingRule{ID: "r3", Name: "Block Ads", Type: "geosite", Value: "category-ads-all", Action: "block", Enabled: true}
	raw, err := BuildLoadBalanceConfig(loadBalanceNodes(), settings, proxyRule, directRule, blockRule)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	rules, _ := config["routing"].(map[string]any)["rules"].([]any)
	if len(rules) != 4 {
		t.Fatalf("expected three custom rules and the catch-all, got %+v", rules)
	}
	proxy, _ := rules[0].(map[string]any)
	if proxy["balancerTag"] != "proxy" {
		t.Fatalf("custom proxy rule must target the balancer: %+v", proxy)
	}
	if _, ok := proxy["outboundTag"]; ok {
		t.Fatalf("custom proxy rule must not keep an outboundTag: %+v", proxy)
	}
	direct, _ := rules[1].(map[string]any)
	if direct["outboundTag"] != "direct" {
		t.Fatalf("direct rule must stay an outbound rule: %+v", direct)
	}
	block, _ := rules[2].(map[string]any)
	if block["outboundTag"] != "block" {
		t.Fatalf("block rule must stay an outbound rule: %+v", block)
	}

	// Direct proxy mode keeps its direct catch-all while the balancer stays in the
	// configuration (it is simply unused by the rules).
	directMode := settings
	directMode.ProxyMode = "direct"
	raw, err = BuildLoadBalanceConfig(loadBalanceNodes(), directMode, proxyRule)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	rules, _ = config["routing"].(map[string]any)["rules"].([]any)
	catchAll, _ := rules[len(rules)-1].(map[string]any)
	if catchAll["outboundTag"] != "direct" {
		t.Fatalf("direct mode catch-all must stay a direct rule: %+v", catchAll)
	}
	if _, ok := config["routing"].(map[string]any)["balancers"]; !ok {
		t.Fatal("the balancer must still be generated in direct mode")
	}
}

func TestBuildLoadBalanceConfigStrategies(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"leastPing", "leastPing"},
		{"leastLoad", "leastLoad"},
		{"roundRobin", "roundRobin"},
		{"", "leastPing"},
		{"unknown", "leastPing"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			settings := testSettings()
			settings.LoadBalanceEnabled = true
			settings.LoadBalanceMode = tc.mode
			raw, err := BuildLoadBalanceConfig(loadBalanceNodes(), settings)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			balancers, _ := config["routing"].(map[string]any)["balancers"].([]any)
			strategy, _ := balancers[0].(map[string]any)["strategy"].(map[string]any)
			if strategy["type"] != tc.want {
				t.Fatalf("mode %q: expected strategy %q, got %+v", tc.mode, tc.want, strategy)
			}
			if _, ok := config["observatory"].(map[string]any); !ok {
				t.Fatalf("mode %q: observatory is missing", tc.mode)
			}
		})
	}
}

func TestBuildLoadBalanceConfigRequiresTwoNodes(t *testing.T) {
	settings := testSettings()
	settings.LoadBalanceEnabled = true
	if _, err := BuildLoadBalanceConfig(nil, settings); err == nil {
		t.Fatal("expected an error without nodes")
	}
	nodes := loadBalanceNodes()
	if _, err := BuildLoadBalanceConfig(nodes[:1], settings); err == nil {
		t.Fatal("expected an error with a single node")
	}
	singbox := settings
	singbox.CoreType = "singbox"
	if _, err := BuildLoadBalanceConfig(nodes, singbox); err == nil {
		t.Fatal("expected an error for a non-Xray core")
	}
}

func TestLoadBalanceConfigPassesXrayValidation(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	customRules := []model.RoutingRule{
		{ID: "r1", Name: "Direct", Type: "domain_suffix", Value: "example.com", Action: "direct", Enabled: true},
		{ID: "r2", Name: "Block Ads", Type: "geosite", Value: "category-ads-all", Action: "block", Enabled: true},
		{ID: "r3", Name: "Proxy Force", Type: "domain_keyword", Value: "example.org", Action: "proxy", Enabled: true},
	}
	cases := []struct {
		name   string
		modify func(*model.Settings)
		rules  []model.RoutingRule
	}{
		{"leastPing default", func(settings *model.Settings) {}, nil},
		{"leastLoad", func(settings *model.Settings) { settings.LoadBalanceMode = "leastLoad" }, nil},
		{"roundRobin", func(settings *model.Settings) { settings.LoadBalanceMode = "roundRobin" }, nil},
		{"stats api and custom rules", func(settings *model.Settings) {
			settings.StatsEnabled = true
			settings.XrayAPIEnabled = true
			settings.XrayAPIPort = 10085
		}, customRules},
		{"global mode", func(settings *model.Settings) {
			settings.ProxyMode = "global"
		}, customRules},
		{"direct mode", func(settings *model.Settings) {
			settings.ProxyMode = "direct"
		}, customRules},
		{"tun inbound", func(settings *model.Settings) { settings.TunMode = true }, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := testSettings()
			settings.LoadBalanceEnabled = true
			tc.modify(&settings)
			raw, err := BuildLoadBalanceConfig(loadBalanceNodes(), settings, tc.rules...)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "loadbalance-config.json")
			if err := os.WriteFile(configPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "run", "-test", "-config", configPath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("xray load balancing config validation failed: %v\n%s", err, output)
			}
		})
	}
}

func overrideTestNode() model.Node {
	return model.Node{Protocol: "trojan", Raw: "trojan://password@example.com:443#Override"}
}

func mustBuildOverrideConfig(t *testing.T, settings model.Settings) map[string]any {
	t.Helper()
	raw, err := BuildConfig(overrideTestNode(), settings)
	if err != nil {
		t.Fatalf("BuildConfig failed: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	return config
}

func mustBuildLoadBalanceOverrideConfig(t *testing.T, nodes []model.Node, settings model.Settings) map[string]any {
	t.Helper()
	raw, err := BuildLoadBalanceConfig(nodes, settings)
	if err != nil {
		t.Fatalf("BuildLoadBalanceConfig failed: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("generated load balance config is not valid JSON: %v", err)
	}
	return config
}

func TestBuildConfigEmptyOverrideIsNoOp(t *testing.T) {
	settings := testSettings()
	settings.XrayDNSServers = "1.1.1.1, 9.9.9.9"
	baseline, err := BuildConfig(overrideTestNode(), settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{"", "   \n\t ", "{}"} {
		withOverride := settings
		withOverride.ConfigOverride = override
		raw, err := BuildConfig(overrideTestNode(), withOverride)
		if err != nil {
			t.Fatalf("override %q: BuildConfig failed: %v", override, err)
		}
		if string(raw) != string(baseline) {
			t.Fatalf("override %q changed the generated config:\nbaseline: %s\nwith override: %s", override, baseline, raw)
		}
	}
}

func TestBuildConfigOverrideDeepMergesObjects(t *testing.T) {
	settings := testSettings()
	settings.XrayDNSServers = "1.1.1.1, 9.9.9.9"
	settings.XrayDomainStrategy = "IPIfNonMatch"
	settings.ConfigOverride = `{
		"log": {"loglevel": "warning", "error": "fastv-error.log"},
		"dns": {"queryStrategy": "UseIPv6"}
	}`
	config := mustBuildOverrideConfig(t, settings)

	logConfig, ok := config["log"].(map[string]any)
	if !ok || logConfig["loglevel"] != "warning" || logConfig["error"] != "fastv-error.log" {
		t.Fatalf("override did not deep merge log: %+v", config["log"])
	}
	dnsConfig, ok := config["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns config is missing: %+v", config)
	}
	if dnsConfig["queryStrategy"] != "UseIPv6" {
		t.Fatalf("override did not replace dns query strategy: %+v", dnsConfig)
	}
	servers, _ := dnsConfig["servers"].([]any)
	if len(servers) != 2 || servers[0] != "1.1.1.1" || servers[1] != "9.9.9.9" {
		t.Fatalf("override removed the generated dns servers: %+v", dnsConfig)
	}
	routing, _ := config["routing"].(map[string]any)
	if routing["domainStrategy"] != "IPIfNonMatch" {
		t.Fatalf("unrelated generated routing fields must survive: %+v", routing)
	}
	if rules, _ := routing["rules"].([]any); len(rules) == 0 {
		t.Fatalf("generated routing rules must survive: %+v", routing)
	}
	if inbounds, _ := config["inbounds"].([]any); len(inbounds) != 1 {
		t.Fatalf("generated inbounds must survive: %+v", config["inbounds"])
	}
	if outbounds, _ := config["outbounds"].([]any); len(outbounds) != 3 {
		t.Fatalf("generated outbounds must survive: %+v", config["outbounds"])
	}
}

func TestBuildConfigOverrideReplacesArrays(t *testing.T) {
	settings := testSettings()
	settings.ConfigOverride = `{"routing": {"rules": [{"type": "field", "domain": ["full:example.org"], "outboundTag": "direct"}]}}`
	config := mustBuildOverrideConfig(t, settings)

	routing, _ := config["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("override must replace the routing rules array, got %+v", rules)
	}
	rule, _ := rules[0].(map[string]any)
	if rule["type"] != "field" || rule["outboundTag"] != "direct" {
		t.Fatalf("unexpected overridden rule: %+v", rule)
	}
	domains, _ := rule["domain"].([]any)
	if len(domains) != 1 || domains[0] != "full:example.org" {
		t.Fatalf("unexpected overridden domains: %+v", rule)
	}
	if routing["domainStrategy"] == nil {
		t.Fatalf("deep merge must keep sibling routing fields: %+v", routing)
	}
}

func TestBuildConfigOverrideDeletesNullFields(t *testing.T) {
	settings := testSettings()
	settings.XrayAPIEnabled = true
	settings.XrayAPIPort = 10085
	settings.ConfigOverride = `{
		"log": null,
		"stats": null,
		"observatory": null,
		"routing": {"domainStrategy": null},
		"policy": {"system": {"statsInboundUplink": null}}
	}`
	config := mustBuildOverrideConfig(t, settings)

	if _, ok := config["log"]; ok {
		t.Fatalf("null override must delete the log section: %+v", config["log"])
	}
	if _, ok := config["stats"]; ok {
		t.Fatalf("null override must delete the stats section: %+v", config["stats"])
	}
	if _, ok := config["observatory"]; ok {
		t.Fatalf("null override must not create the observatory section: %+v", config["observatory"])
	}
	routing, _ := config["routing"].(map[string]any)
	if _, ok := routing["domainStrategy"]; ok {
		t.Fatalf("nested null must delete routing.domainStrategy: %+v", routing)
	}
	if rules, _ := routing["rules"].([]any); len(rules) == 0 {
		t.Fatalf("nested null must not delete sibling routing rules: %+v", routing)
	}
	policy, _ := config["policy"].(map[string]any)
	system, _ := policy["system"].(map[string]any)
	if _, ok := system["statsInboundUplink"]; ok {
		t.Fatalf("nested null must delete policy.system.statsInboundUplink: %+v", system)
	}
	if system["statsOutboundUplink"] != true {
		t.Fatalf("nested null must not delete sibling policy fields: %+v", system)
	}
	if _, ok := config["api"]; !ok {
		t.Fatalf("unrelated api section must survive the override: %+v", config)
	}
}

func TestBuildConfigRejectsInvalidOverride(t *testing.T) {
	cases := []struct {
		name     string
		override string
	}{
		{"truncated json", `{"log":`},
		{"top-level array", `[]`},
		{"top-level number", `123`},
		{"top-level string", `"text"`},
		{"top-level null", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := testSettings()
			settings.ConfigOverride = tc.override
			_, err := BuildConfig(overrideTestNode(), settings)
			if err == nil {
				t.Fatalf("override %q: expected an error", tc.override)
			}
			if !strings.Contains(err.Error(), "config override") {
				t.Fatalf("override %q: error must mention config override: %v", tc.override, err)
			}
		})
	}
}

func TestBuildConfigOverrideAddsTopLevelKeys(t *testing.T) {
	settings := testSettings()
	settings.ConfigOverride = `{"stats": {}, "customFeature": {"enabled": true, "level": 3}}`
	config := mustBuildOverrideConfig(t, settings)

	stats, ok := config["stats"].(map[string]any)
	if !ok {
		t.Fatalf("override must add the stats key: %+v", config["stats"])
	}
	if len(stats) != 0 {
		t.Fatalf("the new stats object must stay empty: %+v", stats)
	}
	custom, ok := config["customFeature"].(map[string]any)
	if !ok {
		t.Fatalf("override must add the customFeature key: %+v", config["customFeature"])
	}
	if custom["enabled"] != true || custom["level"].(float64) != 3 {
		t.Fatalf("unexpected new top-level object: %+v", custom)
	}
	if _, ok := config["api"]; ok {
		t.Fatalf("override must not fabricate unrelated sections: %+v", config["api"])
	}
	if outbounds, _ := config["outbounds"].([]any); len(outbounds) != 3 {
		t.Fatalf("generated outbounds must survive: %+v", config["outbounds"])
	}
}

func TestBuildLoadBalanceConfigOverrideApplies(t *testing.T) {
	settings := testSettings()
	settings.LoadBalanceEnabled = true
	settings.LoadBalanceMode = "leastPing"
	settings.ConfigOverride = `{
		"log": {"loglevel": "warning"},
		"observatory": {"probeInterval": "120s"},
		"routing": {"balancers": [{"tag": "proxy", "selector": ["balancer-node-"], "strategy": {"type": "roundRobin"}}]}
	}`
	config := mustBuildLoadBalanceOverrideConfig(t, loadBalanceNodes(), settings)

	logConfig, _ := config["log"].(map[string]any)
	if logConfig["loglevel"] != "warning" {
		t.Fatalf("override did not deep merge the load balance log: %+v", logConfig)
	}
	observatory, ok := config["observatory"].(map[string]any)
	if !ok {
		t.Fatalf("observatory is missing: %+v", config["observatory"])
	}
	if observatory["probeInterval"] != "120s" {
		t.Fatalf("override did not replace the observatory probe interval: %+v", observatory)
	}
	if observatory["probeURL"] != settings.TestURL {
		t.Fatalf("override removed the generated probe URL: %+v", observatory)
	}
	routing, _ := config["routing"].(map[string]any)
	balancers, _ := routing["balancers"].([]any)
	if len(balancers) != 1 {
		t.Fatalf("override must replace the balancers array, got %+v", balancers)
	}
	balancer, _ := balancers[0].(map[string]any)
	strategy, _ := balancer["strategy"].(map[string]any)
	if strategy["type"] != "roundRobin" {
		t.Fatalf("unexpected overridden balancer: %+v", balancer)
	}
	if outbounds, _ := config["outbounds"].([]any); len(outbounds) != 4 {
		t.Fatalf("generated node outbounds must survive: %+v", config["outbounds"])
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) == 0 {
		t.Fatalf("generated routing rules must survive: %+v", routing)
	}
	last, _ := rules[len(rules)-1].(map[string]any)
	if last["balancerTag"] != "proxy" {
		t.Fatalf("generated rules must keep targeting the balancer: %+v", last)
	}
}

// firstStreamSettings returns the streamSettings block of the proxy outbound for
// tests that only care about the transport surface.
func firstStreamSettings(t *testing.T, settings model.Settings, raw string) map[string]any {
	t.Helper()
	document, err := BuildConfig(model.Node{Protocol: "vless", Raw: raw}, settings)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(document, &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	outbounds, _ := config["outbounds"].([]any)
	if len(outbounds) == 0 {
		t.Fatal("config has no outbounds")
	}
	outbound, _ := outbounds[0].(map[string]any)
	stream, _ := outbound["streamSettings"].(map[string]any)
	if stream == nil {
		t.Fatalf("outbound has no streamSettings: %+v", outbound)
	}
	return stream
}

const tlsWSLink = "vless://00000000-0000-0000-0000-000000000002@vless.example.com:443?security=tls&type=ws&path=%2Fws&host=vless.example.com&sni=vless.example.com#VLESS"

// TestFragmentMaskIsGenerated covers the first-class TLS splitter: enabling it
// must produce the finalmask block Xray expects, with the configured ranges.
func TestFragmentMaskIsGenerated(t *testing.T) {
	settings := testSettings()
	settings.XrayFragmentEnabled = true
	settings.XrayFragmentPackets = ""
	settings.XrayFragmentLength = "100-200"
	settings.XrayFragmentDelay = "10-20"
	settings.XrayTLSFingerprint = "chrome"

	stream := firstStreamSettings(t, settings, tlsWSLink)

	finalmask, _ := stream["finalmask"].(map[string]any)
	if finalmask == nil {
		t.Fatalf("finalmask missing from stream settings: %+v", stream)
	}
	tcp, _ := finalmask["tcp"].([]any)
	if len(tcp) != 1 {
		t.Fatalf("expected one tcp mask, got %+v", finalmask)
	}
	mask, _ := tcp[0].(map[string]any)
	if mask["type"] != "fragment" {
		t.Fatalf("unexpected mask type: %+v", mask)
	}
	fragment, _ := mask["settings"].(map[string]any)
	if fragment["packets"] != "tlshello" || fragment["length"] != "100-200" || fragment["delay"] != "10-20" {
		t.Fatalf("unexpected fragment settings: %+v", fragment)
	}

	tls, _ := stream["tlsSettings"].(map[string]any)
	if tls["fingerprint"] != "chrome" {
		t.Fatalf("expected the default fingerprint to be applied: %+v", tls)
	}
}

// TestFragmentMaskPrecedence pins the rules around the generated mask: the node's
// own fingerprint wins, the hand-written finalmask wins field by field, and a
// disabled switch emits nothing.
func TestFragmentMaskPrecedence(t *testing.T) {
	withNodeFingerprint := strings.Replace(tlsWSLink, "#VLESS", "&fp=firefox#VLESS", 1)
	settings := testSettings()
	settings.XrayTLSFingerprint = "chrome"
	stream := firstStreamSettings(t, settings, withNodeFingerprint)
	if tls, _ := stream["tlsSettings"].(map[string]any); tls["fingerprint"] != "firefox" {
		t.Fatalf("node fingerprint must win over the default: %+v", tls)
	}
	if _, ok := stream["finalmask"]; ok {
		t.Fatalf("disabled fragment must not emit a mask: %+v", stream)
	}

	settings.XrayFragmentEnabled = true
	settings.XrayFinalMaskJSON = `{"udp":[{"type":"noise","settings":{"noise":"rand:8"}}]}`
	stream = firstStreamSettings(t, settings, tlsWSLink)
	finalmask, _ := stream["finalmask"].(map[string]any)
	if _, ok := finalmask["tcp"]; !ok {
		t.Fatalf("generated tcp mask lost when merging advanced JSON: %+v", finalmask)
	}
	if _, ok := finalmask["udp"]; !ok {
		t.Fatalf("advanced udp mask not merged: %+v", finalmask)
	}

	settings.XrayFinalMaskJSON = `{"tcp":[{"type":"fragment","settings":{"packets":"1-3","length":"1-10"}}]}`
	stream = firstStreamSettings(t, settings, tlsWSLink)
	finalmask, _ = stream["finalmask"].(map[string]any)
	tcp, _ := finalmask["tcp"].([]any)
	mask, _ := tcp[0].(map[string]any)
	fragment, _ := mask["settings"].(map[string]any)
	if fragment["packets"] != "1-3" || fragment["length"] != "1-10" {
		t.Fatalf("hand-written mask must replace the generated one: %+v", fragment)
	}
}

// TestFragmentMaskFallsBackOnInvalidRange keeps an unparseable range from
// reaching Xray, which would refuse to start.
func TestFragmentMaskFallsBackOnInvalidRange(t *testing.T) {
	settings := testSettings()
	settings.XrayFragmentEnabled = true
	settings.XrayFragmentLength = "not-a-range"
	settings.XrayFragmentDelay = ""

	stream := firstStreamSettings(t, settings, tlsWSLink)
	finalmask, _ := stream["finalmask"].(map[string]any)
	tcp, _ := finalmask["tcp"].([]any)
	mask, _ := tcp[0].(map[string]any)
	fragment, _ := mask["settings"].(map[string]any)
	if fragment["length"] != defaultFragmentLength {
		t.Fatalf("expected the default length, got %+v", fragment)
	}
	if _, ok := fragment["delay"]; ok {
		t.Fatalf("empty delay must be omitted: %+v", fragment)
	}
}

// TestAntiDPIConfigPassesXrayValidation feeds the generated anti-DPI config to a
// real Xray binary: finalmask/fragment is only useful if the core actually loads
// the block, and a typo there fails at startup rather than at connect time.
// Set FASTV_XRAY_BINARY to validate against a core outside the repo data dir.
func TestAntiDPIConfigPassesXrayValidation(t *testing.T) {
	binary := os.Getenv("FASTV_XRAY_BINARY")
	if binary == "" {
		binary = filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}

	nodes := []model.Node{
		{
			Protocol: "vless",
			Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls&type=ws&path=%2Fws&host=example.com&sni=example.com#Fragment",
		},
		{
			Protocol: "vless",
			Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=reality&type=tcp&sni=www.microsoft.com&pbk=jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0&sid=0123&flow=xtls-rprx-vision#Reality",
		},
		{
			Protocol: "vless",
			Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls&type=grpc&serviceName=gun&sni=example.com#GRPC",
		},
		{
			Protocol: "vless",
			Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls&type=xhttp&path=%2Fx&host=example.com&sni=example.com#XHTTP",
		},
		{
			Protocol: "vless",
			Raw:      "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls&type=kcp&headerType=none&seed=fastv&sni=example.com#KCP",
		},
	}
	for _, node := range nodes {
		settings := testSettings()
		settings.XrayFragmentEnabled = true
		settings.XrayFragmentPackets = "tlshello"
		settings.XrayFragmentLength = "100-200"
		settings.XrayFragmentDelay = "10-20"
		settings.XrayTLSFingerprint = "chrome"

		raw, err := BuildConfig(node, settings)
		if err != nil {
			t.Fatalf("%s: build config: %v", node.Protocol, err)
		}
		configPath := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(configPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(binary, "run", "-test", "-config", configPath).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: xray rejected the anti-DPI config: %v\n%s\n%s", node.Protocol, err, output, raw)
		}
	}
}

// TestCdnEndpointOverridesAddress covers the Cloudflare ä¼˜é€‰ path: the pinned edge
// replaces the destination IP while the node keeps its domain for Host/SNI.
func TestCdnEndpointOverridesAddress(t *testing.T) {
	node := model.Node{
		Protocol: "vless",
		Raw:      tlsWSLink,
		CdnIP:    "104.22.0.7",
	}
	settings := testSettings()
	raw, err := BuildConfig(node, settings)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	outbounds, _ := config["outbounds"].([]any)
	outbound, _ := outbounds[0].(map[string]any)
	settingsMap, _ := outbound["settings"].(map[string]any)
	if settingsMap["address"] != "104.22.0.7" {
		t.Fatalf("edge not applied: %+v", settingsMap)
	}
	stream, _ := outbound["streamSettings"].(map[string]any)
	if stream["network"] != "websocket" {
		t.Fatalf("transport changed unexpectedly: %+v", stream)
	}
	if wsSettings, _ := stream["wsSettings"].(map[string]any); wsSettings["host"] != "vless.example.com" {
		t.Fatalf("Host header must stay on the node domain: %+v", wsSettings)
	}

	// Without a pinned edge the node keeps its own address.
	node.CdnIP = ""
	raw, err = BuildConfig(node, settings)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	config = nil
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	outbounds, _ = config["outbounds"].([]any)
	outbound, _ = outbounds[0].(map[string]any)
	settingsMap, _ = outbound["settings"].(map[string]any)
	if settingsMap["address"] != "vless.example.com" {
		t.Fatalf("node address not restored: %+v", settingsMap)
	}
}
