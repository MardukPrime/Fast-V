package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeEmbeddedConfigRemovesGeoRules(t *testing.T) {
	input := []byte(`{
		"inbounds": [{"tag": "local-in", "port": 10808, "protocol": "socks"}],
		"routing": {"rules": [
			{"inboundTag": ["api-in"], "outboundTag": "api", "type": "field"},
			{"ip": ["geoip:private"], "outboundTag": "direct", "type": "field"},
			{"domain": ["geosite:cn", "example.com"], "outboundTag": "direct", "type": "field"},
			{"network": "tcp,udp", "outboundTag": "direct", "type": "field"}
		]}
	}`)
	out, removed, err := sanitizeEmbeddedConfig(input)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	var doc struct {
		Routing struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(doc.Routing.Rules) != 3 {
		t.Fatalf("rules = %d, want 3", len(doc.Routing.Rules))
	}
	// The mixed rule keeps the plain domain but loses the geosite value.
	for _, rule := range doc.Routing.Rules {
		values, _ := rule["domain"].([]any)
		for _, value := range values {
			if text := value.(string); strings.HasPrefix(text, "geosite:") || strings.HasPrefix(text, "geoip:") {
				t.Fatalf("geo value leaked: %s", text)
			}
		}
	}
}
