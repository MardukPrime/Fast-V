package routing

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
)

var validTypes = map[string]struct{}{
	"domain_suffix":  {},
	"domain_keyword": {},
	"domain_regex":   {},
	"ip":             {},
	"geosite":        {},
	"geoip":          {},
	"port":           {},
	"network":        {},
}

var validActions = map[string]struct{}{
	"proxy":  {},
	"direct": {},
	"block":  {},
}

// Normalize validates and canonicalizes a user routing rule.
func Normalize(rule model.RoutingRule) (model.RoutingRule, error) {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Type = strings.ToLower(strings.TrimSpace(rule.Type))
	rule.Value = strings.TrimSpace(rule.Value)
	rule.Action = strings.ToLower(strings.TrimSpace(rule.Action))
	if rule.Action == "" {
		rule.Action = "proxy"
	}
	if _, ok := validTypes[rule.Type]; !ok {
		return model.RoutingRule{}, errors.New("不支持的规则类型")
	}
	if _, ok := validActions[rule.Action]; !ok {
		return model.RoutingRule{}, errors.New("规则动作必须是 proxy、direct 或 block")
	}
	if rule.Value == "" {
		return model.RoutingRule{}, errors.New("规则内容不能为空")
	}
	if rule.Name == "" {
		rule.Name = rule.Type + ": " + truncate(rule.Value, 28)
	}
	switch rule.Type {
	case "domain_regex":
		if _, err := regexp.Compile(rule.Value); err != nil {
			return model.RoutingRule{}, fmt.Errorf("正则表达式无效: %w", err)
		}
	case "ip":
		for _, value := range splitValues(rule.Value) {
			if net.ParseIP(value) == nil {
				if _, _, err := net.ParseCIDR(value); err != nil {
					return model.RoutingRule{}, fmt.Errorf("IP/CIDR 无效: %s", value)
				}
			}
		}
	case "port":
		for _, value := range splitValues(rule.Value) {
			parts := strings.Split(value, "-")
			for _, part := range parts {
				port, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil || port < 1 || port > 65535 {
					return model.RoutingRule{}, fmt.Errorf("端口范围无效: %s", value)
				}
			}
			if len(parts) > 2 {
				return model.RoutingRule{}, fmt.Errorf("端口范围无效: %s", value)
			}
		}
	case "network":
		for _, value := range splitValues(rule.Value) {
			if value != "tcp" && value != "udp" {
				return model.RoutingRule{}, errors.New("网络类型只能是 tcp、udp 或 tcp,udp")
			}
		}
	}
	return rule, nil
}

// DefaultRules returns the starter rules shown in the routing editor.
func DefaultRules() []model.RoutingRule {
	return []model.RoutingRule{
		{ID: model.NewID(), Name: "私有网络直连", Type: "geoip", Value: "private", Action: "direct", Enabled: true},
		{ID: model.NewID(), Name: "广告域名拦截", Type: "geosite", Value: "category-ads-all", Action: "block", Enabled: true},
		{ID: model.NewID(), Name: "中国大陆 IP 直连", Type: "geoip", Value: "cn", Action: "direct", Enabled: true},
		{ID: model.NewID(), Name: "中国大陆域名直连", Type: "geosite", Value: "cn", Action: "direct", Enabled: true},
	}
}

// BuildXrayRules converts ordered user rules into Xray routing rules.
func BuildXrayRules(rules []model.RoutingRule, proxyMode string) []any {
	out := make([]any, 0, len(rules)+3)
	if len(rules) == 0 && proxyMode == "rule" {
		out = append(out,
			map[string]any{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": "direct"},
			map[string]any{"type": "field", "domain": []string{"geosite:category-ads-all"}, "outboundTag": "block"},
		)
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		xrayRule := map[string]any{"type": "field", "outboundTag": rule.Action}
		values := splitValues(rule.Value)
		switch rule.Type {
		case "domain_suffix":
			xrayRule["domain"] = prefixedValues("suffix:", values)
		case "domain_keyword":
			xrayRule["domain"] = prefixedValues("keyword:", values)
		case "domain_regex":
			xrayRule["domain"] = prefixedValues("regexp:", values)
		case "geosite":
			xrayRule["domain"] = prefixedValues("geosite:", values)
		case "ip":
			xrayRule["ip"] = values
		case "geoip":
			xrayRule["ip"] = prefixedValues("geoip:", values)
		case "port":
			xrayRule["port"] = strings.Join(values, ",")
		case "network":
			xrayRule["network"] = strings.Join(values, ",")
		default:
			continue
		}
		out = append(out, xrayRule)
	}
	switch proxyMode {
	case "direct":
		out = append(out, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "direct"})
	case "global":
		out = append(out, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "proxy"})
	default:
		out = append(out, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "proxy"})
	}
	return out
}

func splitValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func prefixedValues(prefix string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, prefix+value)
	}
	return out
}

func truncate(value string, size int) string {
	if len(value) <= size {
		return value
	}
	return value[:size] + "..."
}
