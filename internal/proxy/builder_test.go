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
