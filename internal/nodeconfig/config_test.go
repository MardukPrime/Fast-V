package nodeconfig

import (
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
			UUID: "00000000-0000-0000-0000-000000000002", Network: "websocket", TLS: "reality",
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
	if stream["method"] != "websocket" || stream["security"] != "tls" {
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
