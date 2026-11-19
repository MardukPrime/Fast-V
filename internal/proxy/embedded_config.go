package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// sanitizeEmbeddedConfig removes routing conditions that need GeoIP/GeoSite
// data files. Android builds do not ship those datasets yet, and Xray refuses
// to start when a rule references a missing geosite/geoip asset. Rules that
// still carry other conditions are kept; rules that only referenced Geo data
// are dropped. The count is the number of removed domain/ip entries.
func sanitizeEmbeddedConfig(config []byte) ([]byte, int, error) {
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return nil, 0, fmt.Errorf("解析内嵌内核配置失败: %w", err)
	}
	routing, _ := doc["routing"].(map[string]any)
	if routing == nil {
		return config, 0, nil
	}
	rules, _ := routing["rules"].([]any)
	if len(rules) == 0 {
		return config, 0, nil
	}
	removed := 0
	kept := make([]any, 0, len(rules))
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			kept = append(kept, rawRule)
			continue
		}
		hadGeo := false
		for _, field := range []string{"domain", "ip"} {
			values, ok := rule[field].([]any)
			if !ok {
				continue
			}
			filtered := make([]any, 0, len(values))
			for _, raw := range values {
				value, _ := raw.(string)
				if strings.HasPrefix(value, "geosite:") || strings.HasPrefix(value, "geoip:") {
					hadGeo = true
					removed++
					continue
				}
				filtered = append(filtered, raw)
			}
			if len(filtered) == 0 {
				delete(rule, field)
			} else {
				rule[field] = filtered
			}
		}
		if hadGeo && len(rule) == 0 {
			continue
		}
		if hadGeo {
			// Strip rules that only matched on Geo data; other conditions stay.
			onlyInbound := true
			for key := range rule {
				if key != "inboundTag" && key != "outboundTag" && key != "type" {
					onlyInbound = false
					break
				}
			}
			if onlyInbound {
				continue
			}
		}
		kept = append(kept, rule)
	}
	routing["rules"] = kept
	doc["routing"] = routing
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, 0, fmt.Errorf("序列化内嵌内核配置失败: %w", err)
	}
	return encoded, removed, nil
}
