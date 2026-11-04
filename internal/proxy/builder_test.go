package proxy

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	if stream["method"] != "websocket" || stream["security"] != "tls" {
		t.Fatalf("unexpected stream settings: %+v", stream)
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
