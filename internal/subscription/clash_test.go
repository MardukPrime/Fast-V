package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
)

// testClashSubscriptionYAML is a representative Clash.Meta subscription body:
// four supported proxy types with nested ws/reality options plus one
// hysteria2 proxy that has to be skipped.
const testClashSubscriptionYAML = `port: 7890
socks-port: 7891
allow-lan: true
mode: rule
log-level: info
proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - VMess WS
      - VLESS Reality
      - Trojan WS
      - SS Node
proxies:
  - name: VMess WS
    type: vmess
    server: vmess.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000001
    alterId: 64
    cipher: auto
    tls: true
    servername: vmess.example.com
    network: ws
    ws-opts:
      path: /vm
      headers:
        Host: vmess.example.com
  - name: VLESS Reality
    type: vless
    server: vless.example.com
    port: 8443
    uuid: 00000000-0000-0000-0000-000000000002
    flow: xtls-rprx-vision
    tls: true
    servername: vless.example.com
    skip-cert-verify: true
    alpn:
      - h2
      - http/1.1
    network: tcp
    reality-opts:
      public-key: test-public-key
      short-id: abcd1234
  - name: Trojan WS
    type: trojan
    server: trojan.example.com
    port: 443
    password: trojan-password
    sni: trojan.example.com
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: trojan.example.com
  - name: SS Node
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-password
  - name: HY2 Node
    type: hysteria2
    server: hysteria.example.com
    port: 443
    password: hy2-password
`

func TestParseClashSupportedProxies(t *testing.T) {
	nodes, issues, err := ParseClash(testClashSubscriptionYAML)
	if err != nil {
		t.Fatalf("parse clash failed: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d: %+v", len(nodes), nodes)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 skipped proxy, got %d: %+v", len(issues), issues)
	}
	if !strings.Contains(issues[0], "hysteria2") || !strings.Contains(issues[0], "HY2 Node") {
		t.Fatalf("skip reason should name the unsupported type and node: %q", issues[0])
	}

	want := []struct {
		name     string
		protocol string
		address  string
		port     int
	}{
		{"VMess WS", "vmess", "vmess.example.com", 443},
		{"VLESS Reality", "vless", "vless.example.com", 8443},
		{"Trojan WS", "trojan", "trojan.example.com", 443},
		{"SS Node", "shadowsocks", "ss.example.com", 8388},
	}
	for i, item := range want {
		if nodes[i].Name != item.name || nodes[i].Protocol != item.protocol ||
			nodes[i].Address != item.address || nodes[i].Port != item.port {
			t.Fatalf("unexpected node %d: %+v", i, nodes[i])
		}
		if _, err := ParseLink(nodes[i].Raw); err != nil {
			t.Fatalf("generated raw link for %q does not parse: %v (%q)", nodes[i].Name, err, nodes[i].Raw)
		}
		parsed, err := ParseLink(nodes[i].Raw)
		if err != nil {
			t.Fatalf("generated raw link for %q does not parse: %v", nodes[i].Name, err)
		}
		if parsed.Protocol != item.protocol || parsed.Name != item.name {
			t.Fatalf("raw link round trip mismatch for %q: %+v", nodes[i].Name, parsed)
		}
	}

	// Field-level mapping is verified through the same extraction path the node
	// editor uses, so the generated links stay editable.
	vmessCfg, err := nodeconfig.Extract(nodes[0].Raw, nodes[0].Protocol)
	if err != nil {
		t.Fatalf("extract vmess failed: %v", err)
	}
	if vmessCfg.UUID != "00000000-0000-0000-0000-000000000001" || vmessCfg.Security != "auto" ||
		vmessCfg.Network != "websocket" || vmessCfg.TLS != "tls" ||
		vmessCfg.ServerName != "vmess.example.com" || vmessCfg.Host != "vmess.example.com" ||
		vmessCfg.Path != "/vm" {
		t.Fatalf("unexpected vmess mapping: %+v", vmessCfg)
	}

	vlessCfg, err := nodeconfig.Extract(nodes[1].Raw, nodes[1].Protocol)
	if err != nil {
		t.Fatalf("extract vless failed: %v", err)
	}
	if vlessCfg.UUID != "00000000-0000-0000-0000-000000000002" || vlessCfg.Flow != "xtls-rprx-vision" ||
		vlessCfg.TLS != "reality" || vlessCfg.ServerName != "vless.example.com" ||
		vlessCfg.PublicKey != "test-public-key" || vlessCfg.ShortID != "abcd1234" ||
		!vlessCfg.AllowInsecure || len(vlessCfg.ALPN) != 2 {
		t.Fatalf("unexpected vless mapping: %+v", vlessCfg)
	}

	trojanCfg, err := nodeconfig.Extract(nodes[2].Raw, nodes[2].Protocol)
	if err != nil {
		t.Fatalf("extract trojan failed: %v", err)
	}
	if trojanCfg.Password != "trojan-password" || trojanCfg.Network != "websocket" ||
		trojanCfg.TLS != "tls" || trojanCfg.ServerName != "trojan.example.com" ||
		trojanCfg.Host != "trojan.example.com" || trojanCfg.Path != "/ws" {
		t.Fatalf("unexpected trojan mapping: %+v", trojanCfg)
	}

	ssCfg, err := nodeconfig.Extract(nodes[3].Raw, nodes[3].Protocol)
	if err != nil {
		t.Fatalf("extract shadowsocks failed: %v", err)
	}
	if ssCfg.Method != "aes-256-gcm" || ssCfg.Password != "ss-password" {
		t.Fatalf("unexpected shadowsocks mapping: %+v", ssCfg)
	}
}

func TestParseClashPreservesAlterID(t *testing.T) {
	nodes, _, err := ParseClash(testClashSubscriptionYAML)
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := decodeBase64(strings.TrimPrefix(nodes[0].Raw, "vmess://"))
	if !ok {
		t.Fatalf("vmess raw link is not base64: %q", nodes[0].Raw)
	}
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatalf("vmess payload is not JSON: %v", err)
	}
	if aid, ok := data["aid"].(float64); !ok || int(aid) != 64 {
		t.Fatalf("alterId was not preserved: %+v", data)
	}
}

// TestParseClashNestedTransportOptions focuses on the nested ws-opts/grpc-opts
// structures, which are easy to mis-map because Clash nests them one level
// deeper than the share links.
func TestParseClashNestedTransportOptions(t *testing.T) {
	content := `proxies:
  - name: WS Transport
    type: vless
    server: ws.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000010
    tls: true
    servername: sni.example.com
    network: ws
    ws-opts:
      path: /nested/path
      headers:
        Host: cdn.example.com
  - name: GRPC Transport
    type: vless
    server: grpc.example.com
    port: 8443
    uuid: 00000000-0000-0000-0000-000000000011
    tls: true
    network: grpc
    grpc-opts:
      grpc-service-name: my-grpc-service
  - name: Reality GRPC
    type: vless
    server: reality.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000012
    network: grpc
    grpc-opts:
      grpc-service-name: reality-service
    reality-opts:
      public-key: reality-public-key
      short-id: "0011"
`
	nodes, issues, err := ParseClash(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 3 || len(issues) != 0 {
		t.Fatalf("unexpected result: %d nodes, issues=%v", len(nodes), issues)
	}

	wsCfg, err := nodeconfig.Extract(nodes[0].Raw, nodes[0].Protocol)
	if err != nil {
		t.Fatalf("extract ws failed: %v", err)
	}
	if wsCfg.Network != "websocket" || wsCfg.Path != "/nested/path" ||
		wsCfg.Host != "cdn.example.com" || wsCfg.ServerName != "sni.example.com" || wsCfg.TLS != "tls" {
		t.Fatalf("unexpected ws mapping: %+v", wsCfg)
	}

	grpcCfg, err := nodeconfig.Extract(nodes[1].Raw, nodes[1].Protocol)
	if err != nil {
		t.Fatalf("extract grpc failed: %v", err)
	}
	if grpcCfg.Network != "grpc" || grpcCfg.ServiceName != "my-grpc-service" {
		t.Fatalf("unexpected grpc mapping: %+v", grpcCfg)
	}

	realityCfg, err := nodeconfig.Extract(nodes[2].Raw, nodes[2].Protocol)
	if err != nil {
		t.Fatalf("extract reality grpc failed: %v", err)
	}
	if realityCfg.Network != "grpc" || realityCfg.ServiceName != "reality-service" ||
		realityCfg.TLS != "reality" || realityCfg.PublicKey != "reality-public-key" || realityCfg.ShortID != "0011" {
		t.Fatalf("unexpected reality mapping: %+v", realityCfg)
	}
}

func TestParseClashSkipsUnusableEntries(t *testing.T) {
	content := `proxies:
  - name: Valid
    type: vmess
    server: valid.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000020
  - name: Missing UUID
    type: vless
    server: vless.example.com
    port: 443
  - name: Missing Password
    type: trojan
    server: trojan.example.com
    port: 443
  - name: SS With Plugin
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-password
    plugin: obfs
  - name: Missing Server
    type: vmess
    port: 443
    uuid: 00000000-0000-0000-0000-000000000021
`
	nodes, issues, err := ParseClash(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Name != "Valid" {
		t.Fatalf("expected only the valid node, got %+v", nodes)
	}
	if len(issues) != 4 {
		t.Fatalf("expected 4 skip reasons, got %d: %+v", len(issues), issues)
	}
	joined := strings.Join(issues, "\n")
	for _, want := range []string{"Missing UUID", "Missing Password", "SS With Plugin", "Missing Server"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("skip reasons do not mention %q: %s", want, joined)
		}
	}
}

func TestParseClashAllUnsupportedReturnsError(t *testing.T) {
	content := `proxies:
  - name: HY2
    type: hysteria2
    server: hy2.example.com
    port: 443
    password: pass
  - name: TUIC
    type: tuic
    server: tuic.example.com
    port: 443
    password: pass
  - name: SOCKS
    type: socks5
    server: socks.example.com
    port: 1080
`
	nodes, issues, err := ParseClash(content)
	if err == nil {
		t.Fatal("expected an error when no proxy can be imported")
	}
	if len(nodes) != 0 {
		t.Fatalf("expected no nodes, got %+v", nodes)
	}
	if len(issues) != 3 {
		t.Fatalf("expected 3 skip reasons, got %d: %+v", len(issues), issues)
	}
	if !strings.Contains(err.Error(), "没有可导入的节点") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestParseClashRejectsBrokenDocuments(t *testing.T) {
	cases := map[string]string{
		"empty":            "   ",
		"invalid yaml":     "proxies:\n  - name: [broken",
		"not a mapping":    "- just\n- a\n- list\n",
		"missing proxies":  "proxy-groups:\n  - name: Proxy\n    type: select\n",
		"empty proxies":    "proxies: []\n",
		"proxies not list": "proxies:\n  name: not-a-list\n",
	}
	for name, content := range cases {
		if _, _, err := ParseClash(content); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestParseClashDeduplicatesProxies(t *testing.T) {
	content := `proxies:
  - name: Duplicate
    type: vmess
    server: dup.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000030
  - name: Duplicate
    type: vmess
    server: dup.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000031
`
	nodes, _, err := ParseClash(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected duplicates to be removed, got %d nodes", len(nodes))
	}
}

// Ensure decoding a generated vmess link keeps working after the alterId patch.
func TestParseClashGeneratedLinkRoundTrip(t *testing.T) {
	nodes, _, err := ParseClash(testClashSubscriptionYAML)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(nodes[0].Raw, "vmess://")); err != nil {
		t.Fatalf("generated vmess link is not standard base64: %v", err)
	}
}
