package nodeconfig

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func TestBuildRawAndExtractRoundTrip(t *testing.T) {
	cases := []model.NodeConfig{
		{
			Name: "VMess", Protocol: "vmess", Address: "vmess.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000001", Security: "auto", Network: "websocket",
			TLS: "tls", ServerName: "vmess.example.com", Host: "vmess.example.com", Path: "/vm",
		},
		{
			Name: "VLESS", Protocol: "vless", Address: "vless.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000002", Encryption: "mlkem768x25519plus.native.0rtt.test", Mldsa65Verify: "test-pqv", Network: "websocket", TLS: "reality",
			ServerName: "vless.example.com", Host: "vless.example.com", Path: "/vless",
			PublicKey: "public-key", ShortID: "abcd", SpiderX: "/",
		},
		{
			Name: "Trojan", Protocol: "trojan", Address: "trojan.example.com", Port: 443,
			Password: "password", Network: "grpc", TLS: "tls", ServerName: "trojan.example.com",
			ServiceName: "grpc-service",
		},
		{
			Name: "SS", Protocol: "shadowsocks", Address: "ss.example.com", Port: 8388,
			Method: "aes-256-gcm", Password: "password",
		},
	}
	for _, original := range cases {
		raw, err := BuildRaw(original)
		if err != nil {
			t.Fatalf("%s build raw: %v", original.Protocol, err)
		}
		extracted, err := Extract(raw, original.Protocol)
		if err != nil {
			t.Fatalf("%s extract: %v\n%s", original.Protocol, err, raw)
		}
		if extracted.Address != original.Address || extracted.Port != original.Port || extracted.Protocol != original.Protocol {
			t.Fatalf("%s round trip mismatch: %+v vs %+v", original.Protocol, original, extracted)
		}
		if original.Protocol == "vmess" && extracted.UUID != original.UUID {
			t.Fatalf("vmess UUID mismatch: %+v", extracted)
		}
		if original.Protocol == "vless" && extracted.UUID != original.UUID {
			t.Fatalf("vless UUID mismatch: %+v", extracted)
		}
		if original.Protocol == "trojan" && extracted.Password != original.Password {
			t.Fatalf("trojan password mismatch: %+v", extracted)
		}
		if original.Protocol == "shadowsocks" && (extracted.Method != original.Method || extracted.Password != original.Password) {
			t.Fatalf("ss credentials mismatch: %+v", extracted)
		}
	}
}

func TestBuildOutboundUsesTransportAndSecurity(t *testing.T) {
	cfg := model.NodeConfig{
		Protocol: "vless", Address: "example.com", Port: 443,
		UUID:    "00000000-0000-0000-0000-000000000002",
		Network: "websocket", TLS: "tls", ServerName: "example.com",
		Host: "example.com", Path: "/ws", ALPN: []string{"h2", "http/1.1"},
	}
	outbound, err := BuildOutbound(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if outbound["protocol"] != "vless" {
		t.Fatalf("unexpected protocol: %+v", outbound)
	}
	stream := outbound["streamSettings"].(map[string]any)
	if stream["network"] != "websocket" || stream["security"] != "tls" {
		t.Fatalf("unexpected stream: %+v", stream)
	}
	ws := stream["wsSettings"].(map[string]any)
	if ws["path"] != "/ws" || ws["host"] != "example.com" {
		t.Fatalf("unexpected ws settings: %+v", ws)
	}
	tlsSettings := stream["tlsSettings"].(map[string]any)
	if tlsSettings["serverName"] != "example.com" {
		t.Fatalf("unexpected tls settings: %+v", tlsSettings)
	}
}

func TestExtractRejectsUnsupportedInput(t *testing.T) {
	if _, err := Extract("unknown://value", "unknown"); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRawRoundTripKeepsProtocolFields(t *testing.T) {
	vmessPayload, err := json.Marshal(map[string]any{
		"v": "2", "ps": "VMess Node", "add": "vmess.example.com", "port": 443,
		"id": "00000000-0000-0000-0000-000000000001", "aid": 0, "scy": "aes-128-gcm",
		"net": "ws", "type": "none", "host": "vmess.example.com", "path": "/vm",
		"tls": "tls", "sni": "vmess.example.com", "fp": "chrome", "alpn": "h2,http/1.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ssUserInfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:p@ss:word"))
	cases := []string{
		"vmess://" + base64.StdEncoding.EncodeToString(vmessPayload),
		"vless://00000000-0000-0000-0000-000000000002@vless.example.com:443" +
			"?encryption=mlkem768x25519plus.native.0rtt.test&flow=xtls-rprx-vision&security=reality" +
			"&sni=www.microsoft.com&fp=chrome&pbk=public-key&sid=6ba85179e30d4fc2&spx=%2F" +
			"&pqv=mldsa65-verify&type=tcp#VLESS%20Reality",
		"trojan://pass%40word@trojan.example.com:8443" +
			"?security=tls&type=ws&host=trojan.example.com&path=%2Fws" +
			"&sni=trojan.example.com&fp=firefox&alpn=h2%2Chttp%2F1.1&allowInsecure=1#Trojan%20WS",
		// Trojan without a security parameter means TLS; with security=none that
		// explicit choice must not be turned back into TLS by BuildRaw.
		"trojan://password@trojan.example.com:443#Trojan%20Default",
		"trojan://password@trojan.example.com:443?security=none&type=tcp#Trojan%20Plain",
		"ss://" + ssUserInfo + "@ss.example.com:8388#SS%20Node%20%E4%B8%AD%E6%96%87",
	}

	for _, raw := range cases {
		original, err := Extract(raw, "")
		if err != nil {
			t.Fatalf("extract %q: %v", raw, err)
		}
		rebuilt, err := BuildRaw(original)
		if err != nil {
			t.Fatalf("build raw %s: %v", original.Protocol, err)
		}
		roundTripped, err := Extract(rebuilt, "")
		if err != nil {
			t.Fatalf("re-extract %s: %v\n%s", original.Protocol, err, rebuilt)
		}
		if diff := compareNodeConfigs(original, roundTripped); diff != "" {
			t.Fatalf("%s round trip lost fields: %s\noriginal: %+v\nrebuilt: %s\nextracted: %+v",
				original.Protocol, diff, original, rebuilt, roundTripped)
		}
	}
}

func compareNodeConfigs(left, right model.NodeConfig) string {
	if left.Name != right.Name {
		return "name"
	}
	if left.Protocol != right.Protocol {
		return "protocol"
	}
	if left.Address != right.Address || left.Port != right.Port {
		return "endpoint"
	}
	if left.UUID != right.UUID {
		return "uuid"
	}
	if left.Password != right.Password {
		return "password"
	}
	if left.Method != right.Method {
		return "method"
	}
	if left.Security != right.Security {
		return "security"
	}
	if left.Flow != right.Flow {
		return "flow"
	}
	if left.Encryption != right.Encryption {
		return "encryption"
	}
	if left.Mldsa65Verify != right.Mldsa65Verify {
		return "mldsa65Verify"
	}
	if left.Network != right.Network {
		return "network"
	}
	if left.TLS != right.TLS {
		return "tls"
	}
	if left.ServerName != right.ServerName {
		return "serverName"
	}
	if left.Fingerprint != right.Fingerprint {
		return "fingerprint"
	}
	if strings.Join(left.ALPN, ",") != strings.Join(right.ALPN, ",") {
		return "alpn"
	}
	if left.Host != right.Host {
		return "host"
	}
	if left.Path != right.Path {
		return "path"
	}
	if left.ServiceName != right.ServiceName {
		return "serviceName"
	}
	if left.PublicKey != right.PublicKey {
		return "publicKey"
	}
	if left.ShortID != right.ShortID {
		return "shortId"
	}
	if left.SpiderX != right.SpiderX {
		return "spiderX"
	}
	if left.AllowInsecure != right.AllowInsecure {
		return "allowInsecure"
	}
	return ""
}

func TestBuildRawRequiresCredentials(t *testing.T) {
	cases := []model.NodeConfig{
		{Protocol: "vmess", Address: "example.com", Port: 443},
		{Protocol: "vless", Address: "example.com", Port: 443},
		{Protocol: "trojan", Address: "example.com", Port: 443},
		{Protocol: "shadowsocks", Address: "example.com", Port: 8388},
		{Protocol: "unknown", Address: "example.com", Port: 443},
	}
	for _, cfg := range cases {
		if _, err := BuildRaw(cfg); err == nil {
			t.Fatalf("expected error for %+v", cfg)
		}
	}
}
