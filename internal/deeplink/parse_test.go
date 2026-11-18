package deeplink

import (
	"strings"
	"testing"
)

func TestParseValidLinks(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		kind string
		url  string
	}{
		{
			name: "encoded subscription URL",
			raw:  "fastv://import?url=https%3A%2F%2Fexample.com%2Fapi%2Fv1%2Fclient%2Fsubscribe%3Ftoken%3Dabc%26flag%3Dclash",
			kind: KindSubscription,
			url:  "https://example.com/api/v1/client/subscribe?token=abc&flag=clash",
		},
		{
			name: "unencoded subscription URL",
			raw:  "fastv://import?url=https://example.com/sub",
			kind: KindSubscription,
			url:  "https://example.com/sub",
		},
		{
			name: "quoted and padded argument",
			raw:  "  \"fastv://import?url=https%3A%2F%2Fexample.com%2Fsub\"  ",
			kind: KindSubscription,
			url:  "https://example.com/sub",
		},
		{
			name: "import with encoded share link is a node",
			raw:  "fastv://import?url=vless%3A%2F%2Fuuid%40example.com%3A443%3Fencryption%3Dnone%23node-one",
			kind: KindNode,
			url:  "vless://uuid@example.com:443?encryption=none#node-one",
		},
		{
			name: "node link parameter",
			raw:  "fastv://node?link=trojan%3A%2F%2Fsecret%40example.com%3A443%23trojan-node",
			kind: KindNode,
			url:  "trojan://secret@example.com:443#trojan-node",
		},
		{
			name: "node url parameter alias",
			raw:  "fastv://node?url=hysteria2%3A%2F%2Fsecret%40example.com%3A443",
			kind: KindNode,
			url:  "hysteria2://secret@example.com:443",
		},
		{
			name: "opaque fastv form",
			raw:  "fastv:import?url=https%3A%2F%2Fexample.com%2Fsub",
			kind: KindSubscription,
			url:  "https://example.com/sub",
		},
		{
			name: "path fastv form",
			raw:  "fastv:///import?url=https%3A%2F%2Fexample.com%2Fsub",
			kind: KindSubscription,
			url:  "https://example.com/sub",
		},
		{
			name: "raw vless link",
			raw:  "vless://uuid@example.com:443?encryption=none#raw",
			kind: KindNode,
			url:  "vless://uuid@example.com:443?encryption=none#raw",
		},
		{
			name: "raw uppercase scheme is normalized",
			raw:  "VLESS://uuid@example.com:443",
			kind: KindNode,
			url:  "vless://uuid@example.com:443",
		},
		{
			name: "raw vmess link",
			raw:  "vmess://eyJ2IjoiMiIsInBzIjoibm9kZSJ9",
			kind: KindNode,
			url:  "vmess://eyJ2IjoiMiIsInBzIjoibm9kZSJ9",
		},
		{
			name: "raw ss link",
			raw:  "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388#ss-node",
			kind: KindNode,
			url:  "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388#ss-node",
		},
		{
			name: "raw hysteria2 link",
			raw:  "hysteria2://secret@example.com:443?sni=example.com#hy2",
			kind: KindNode,
			url:  "hysteria2://secret@example.com:443?sni=example.com#hy2",
		},
		{
			name: "raw tuic link",
			raw:  "tuic://uuid:password@example.com:443?congestion_control=bbr#tuic",
			kind: KindNode,
			url:  "tuic://uuid:password@example.com:443?congestion_control=bbr#tuic",
		},
		{
			name: "raw link surrounded by quotes",
			raw:  `"vless://uuid@example.com:443#quoted"`,
			kind: KindNode,
			url:  "vless://uuid@example.com:443#quoted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request, err := Parse(tc.raw)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tc.raw, err)
			}
			if request.Kind != tc.kind {
				t.Fatalf("Parse(%q).Kind = %q, want %q", tc.raw, request.Kind, tc.kind)
			}
			if request.URL != tc.url {
				t.Fatalf("Parse(%q).URL = %q, want %q", tc.raw, request.URL, tc.url)
			}
		})
	}
}

func TestParseInvalidLinks(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "whitespace only", raw: "   "},
		{name: "plain http url", raw: "https://example.com/sub"},
		{name: "mailto link", raw: "mailto:someone@example.com"},
		{name: "fastv without action", raw: "fastv://"},
		{name: "fastv missing url parameter", raw: "fastv://import"},
		{name: "fastv empty url parameter", raw: "fastv://import?url="},
		{name: "fastv blank url parameter", raw: "fastv://import?url=%20"},
		{name: "subscription ftp scheme", raw: "fastv://import?url=ftp%3A%2F%2Fexample.com%2Fsub"},
		{name: "subscription without host", raw: "fastv://import?url=https%3A%2F%2F"},
		{name: "subscription not a url", raw: "fastv://import?url=not%20a%20url"},
		{name: "node missing link", raw: "fastv://node"},
		{name: "node http link", raw: "fastv://node?link=https%3A%2F%2Fexample.com"},
		{name: "node empty share link", raw: "fastv://node?link=vless%3A%2F%2F"},
		{name: "unsupported action", raw: "fastv://unknown?url=https%3A%2F%2Fexample.com"},
		{name: "unsupported raw scheme", raw: "ftp://example.com/file"},
		{name: "raw share link without payload", raw: "vless://"},
		{name: "command line flag", raw: "--debug"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request, err := Parse(tc.raw)
			if err == nil {
				t.Fatalf("Parse(%q) = %+v, want an error", tc.raw, request)
			}
			if !strings.ContainsAny(err.Error(), "深链接") &&
				!strings.Contains(err.Error(), "订阅链接") &&
				!strings.Contains(err.Error(), "分享链接") {
				t.Fatalf("Parse(%q) error is not descriptive: %v", tc.raw, err)
			}
		})
	}
}

func TestParseErrorMessages(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "fastv://import", want: "缺少 url"},
		{raw: "fastv://node", want: "缺少 link"},
		{raw: "fastv://import?url=ftp%3A%2F%2Fexample.com", want: "http 或 https"},
		{raw: "fastv://unknown", want: "不支持的深链接类型"},
		{raw: "fastv://node?link=vless%3A%2F%2F", want: "分享链接"},
	}
	for _, tc := range tests {
		_, err := Parse(tc.raw)
		if err == nil {
			t.Fatalf("Parse(%q) returned no error", tc.raw)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("Parse(%q) error = %q, want it to contain %q", tc.raw, err.Error(), tc.want)
		}
	}
}

// TestParseDoesNotPanicOnOddInputs makes sure hostile or truncated input never
// crashes the command line handling.
func TestParseDoesNotPanicOnOddInputs(t *testing.T) {
	inputs := []string{
		`"`,
		`""`,
		`'`,
		`fastv://`,
		`fastv://?url=`,
		`fastv:`,
		`fastv://import?url=%zz`,
		`fastv://import?url=%%%`,
		`fastv://node?link=`,
		`fastv://import?url=fastv%3A%2F%2Fimport%3Furl%3Dx`,
		`///`,
		strings.Repeat("v", 10000),
		strings.Repeat("%", 1000),
	}
	for _, input := range inputs {
		_, _ = Parse(input)
	}
}
