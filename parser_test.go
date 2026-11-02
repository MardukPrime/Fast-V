package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSubscriptionContent(t *testing.T) {
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

	nodes, err := parseSubscriptionContent(content)
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

func TestParseSubscriptionContentRejectsUnsupportedInput(t *testing.T) {
	if _, err := parseSubscriptionContent("not a subscription"); err == nil {
		t.Fatal("expected error for unsupported input")
	}
}
