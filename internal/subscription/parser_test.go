package subscription

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseContent(t *testing.T) {
	vmessData := map[string]any{
		"ps":   "VMess Node",
		"add":  "vmess.example.com",
		"port": 443,
		"id":   "00000000-0000-0000-0000-000000000001",
		"net":  "ws",
	}
	vmessJSON, err := json.Marshal(vmessData)
	if err != nil {
		t.Fatal(err)
	}
	vmessLink := "vmess://" + base64.StdEncoding.EncodeToString(vmessJSON)

	links := []string{
		vmessLink,
		"vless://00000000-0000-0000-0000-000000000002@vless.example.com:443?security=reality&type=raw#VLESS%20Node",
		"trojan://password@trojan.example.com:443#Trojan%20Node",
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#SS%20Node",
	}
	content := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))

	nodes, err := ParseContent(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(nodes))
	}
	if nodes[0].Protocol != "vmess" || nodes[0].Address != "vmess.example.com" {
		t.Fatalf("unexpected vmess node: %+v", nodes[0])
	}
	if nodes[1].Protocol != "vless" || nodes[1].Name != "VLESS Node" {
		t.Fatalf("unexpected vless node: %+v", nodes[1])
	}
	if nodes[2].Protocol != "trojan" || nodes[2].Port != 443 {
		t.Fatalf("unexpected trojan node: %+v", nodes[2])
	}
	if nodes[3].Protocol != "shadowsocks" || nodes[3].Address != "ss.example.com" {
		t.Fatalf("unexpected shadowsocks node: %+v", nodes[3])
	}
}

func TestParseContentRejectsUnsupportedInput(t *testing.T) {
	if _, err := ParseContent("not a subscription"); err == nil {
		t.Fatal("expected error for unsupported input")
	}
}

func TestParseLinkSupportsAllProtocols(t *testing.T) {
	vmessLink := testVMessLink(t, "VMess Node", "vmess.example.com", 443)
	links := []struct {
		raw      string
		protocol string
		address  string
		port     int
		name     string
	}{
		{vmessLink, "vmess", "vmess.example.com", 443, "VMess Node"},
		{"vless://00000000-0000-0000-0000-000000000002@vless.example.com:8443?security=reality&type=tcp#VLESS%20Node", "vless", "vless.example.com", 8443, "VLESS Node"},
		{"trojan://password@trojan.example.com:443#Trojan%20Node", "trojan", "trojan.example.com", 443, "Trojan Node"},
		{"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#SS%20Node", "shadowsocks", "ss.example.com", 8388, "SS Node"},
	}
	for _, item := range links {
		node, err := ParseLink(item.raw)
		if err != nil {
			t.Fatalf("%s: parse link failed: %v", item.protocol, err)
		}
		if node.Protocol != item.protocol || node.Address != item.address || node.Port != item.port || node.Name != item.name {
			t.Fatalf("unexpected %s node: %+v", item.protocol, node)
		}
		if node.Raw != item.raw {
			t.Fatalf("raw link was not preserved: %q", node.Raw)
		}
	}
}

func TestParseLinkRejectsInvalidLinks(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"http://example.com",
		"vmess://not-base64",
		"vless://user@example.com",
		"not a share link",
	}
	for _, raw := range cases {
		if _, err := ParseLink(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestParseLinksHandlesClipboardTextAndDedupes(t *testing.T) {
	vmessLink := testVMessLink(t, "VMess Node", "vmess.example.com", 443)
	vlessLink := "vless://00000000-0000-0000-0000-000000000002@vless.example.com:443?security=tls&type=ws#VLESS%20Node"
	text := strings.Join([]string{
		"# 我的节点列表",
		"",
		"  " + vmessLink + "  ",
		vlessLink,
		vlessLink, // duplicate paste
		"trojan://password@trojan.example.com:443#Trojan%20Node",
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#SS%20Node",
		"this line is not a share link",
	}, "\r\n")

	nodes, issues := ParseLinksWithIssues(text)
	if len(nodes) != 4 {
		t.Fatalf("expected 4 unique nodes, got %d: %+v", len(nodes), nodes)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 rejected line, got %d: %+v", len(issues), issues)
	}
	for _, node := range nodes {
		if _, err := ParseLink(node.Raw); err != nil {
			t.Fatalf("parsed node raw link is not reusable: %v (%q)", err, node.Raw)
		}
	}
}

func TestParseLinksAcceptsBase64SubscriptionBody(t *testing.T) {
	links := []string{
		testVMessLink(t, "VMess Node", "vmess.example.com", 443),
		"vless://00000000-0000-0000-0000-000000000002@vless.example.com:443#VLESS%20Node",
		"trojan://password@trojan.example.com:443#Trojan%20Node",
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#SS%20Node",
	}
	content := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))

	nodes, err := ParseLinks(content)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(nodes))
	}
	if nodes[3].Protocol != "shadowsocks" {
		t.Fatalf("unexpected node order: %+v", nodes[3])
	}
}

func TestParseLinksRejectsEmptyOrUnusableText(t *testing.T) {
	if _, err := ParseLinks(""); err == nil {
		t.Fatal("expected error for empty text")
	}
	if _, err := ParseLinks("# only a comment\n// another comment"); err == nil {
		t.Fatal("expected error when nothing can be parsed")
	}
}

func testVMessLink(t *testing.T, name, address string, port int) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"ps":   name,
		"add":  address,
		"port": port,
		"id":   "00000000-0000-0000-0000-000000000001",
		"net":  "ws",
	})
	if err != nil {
		t.Fatal(err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(payload)
}

func TestParseContentSupportsClashYAML(t *testing.T) {
	nodes, err := ParseContent(testClashSubscriptionYAML)
	if err != nil {
		t.Fatalf("parse clash subscription failed: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d: %+v", len(nodes), nodes)
	}
	if nodes[0].Protocol != "vmess" || nodes[0].Address != "vmess.example.com" {
		t.Fatalf("unexpected vmess node: %+v", nodes[0])
	}
	if nodes[1].Protocol != "vless" || nodes[1].Port != 8443 {
		t.Fatalf("unexpected vless node: %+v", nodes[1])
	}
	if nodes[2].Protocol != "trojan" || nodes[2].Name != "Trojan WS" {
		t.Fatalf("unexpected trojan node: %+v", nodes[2])
	}
	if nodes[3].Protocol != "shadowsocks" || nodes[3].Address != "ss.example.com" {
		t.Fatalf("unexpected shadowsocks node: %+v", nodes[3])
	}
}

func TestParseContentWithIssuesReportsClashSkips(t *testing.T) {
	nodes, issues, err := ParseContentWithIssues(testClashSubscriptionYAML)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(nodes))
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "hysteria2") {
		t.Fatalf("expected the hysteria2 proxy to be reported, got %+v", issues)
	}
}

func TestParseContentReportsClashErrors(t *testing.T) {
	cases := []string{
		"proxies:\n  - name: [broken",    // invalid YAML
		"proxies: []",                    // empty proxy list
		"proxy-groups:\n  - name: Proxy", // no proxies section
	}
	for _, content := range cases {
		if _, err := ParseContent(content); err == nil {
			t.Fatalf("expected an error for Clash-like content %q", content)
		}
	}
}

func TestParseContentWithIssuesKeepsLinkBehavior(t *testing.T) {
	nodes, issues, err := ParseContentWithIssues(testVMessLink(t, "VMess Node", "vmess.example.com", 443))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Protocol != "vmess" || len(issues) != 0 {
		t.Fatalf("unexpected link parse result: nodes=%+v issues=%+v", nodes, issues)
	}
}

func TestParseContentSupportsBase64WrappedClashYAML(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte(testClashSubscriptionYAML))
	nodes, err := ParseContent(content)
	if err != nil {
		t.Fatalf("parse base64 clash subscription failed: %v", err)
	}
	if len(nodes) != 4 || nodes[0].Protocol != "vmess" || nodes[3].Protocol != "shadowsocks" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}
}
