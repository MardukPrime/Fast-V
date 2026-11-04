package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
	"github.com/MardukPrime/Fast-V/internal/routing"
)

const tunInterfaceName = "FastV-TUN"

// BuildConfig creates a complete Xray JSON configuration for the selected node and settings.
func BuildConfig(node model.Node, settings model.Settings, customRules ...model.RoutingRule) ([]byte, error) {
	if settings.CoreType != "xray" {
		return nil, errors.New("only Xray-core is currently supported")
	}

	outbound, err := buildOutbound(node, settings)
	if err != nil {
		return nil, err
	}

	inbounds := make([]any, 0, 3)
	if settings.TunMode {
		inbounds = append(inbounds, map[string]any{
			"tag":      "tun-in",
			"protocol": "tun",
			"settings": map[string]any{
				"name": tunInterfaceName,
				"MTU":  settings.XrayTunMTU,
			},
			"sniffing": map[string]any{
				"enabled":      settings.XraySniffing,
				"destOverride": []string{"http", "tls", "quic"},
			},
		})
	}
	inbounds = append(inbounds, map[string]any{
		"tag":      "local-in",
		"listen":   "127.0.0.1",
		"port":     settings.XrayLocalPort,
		"protocol": "socks",
		"settings": map[string]any{
			"auth": "noauth",
			"udp":  true,
		},
		"sniffing": map[string]any{
			"enabled":      settings.XraySniffing,
			"destOverride": []string{"http", "tls", "quic"},
		},
	})
	if settings.XrayAPIEnabled {
		inbounds = append(inbounds, map[string]any{
			"tag":      "api-in",
			"listen":   "127.0.0.1",
			"port":     settings.XrayAPIPort,
			"protocol": "dokodemo-door",
			"settings": map[string]any{
				"address": "127.0.0.1",
			},
		})
	}

	config := map[string]any{
		"log": map[string]any{
			"loglevel": settings.XrayLogLevel,
		},
		"inbounds": inbounds,
		"outbounds": []any{
			outbound,
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		"routing": buildRouting(settings.ProxyMode, customRules, settings.XrayAPIEnabled, settings.XrayDomainStrategy),
		"dns": map[string]any{
			"servers":       splitList(settings.XrayDNSServers),
			"queryStrategy": settings.XrayDNSStrategy,
		},
	}
	if settings.XrayAPIEnabled {
		config["api"] = map[string]any{
			"tag": "api",
			"services": []string{
				"HandlerService",
				"LoggerService",
				"StatsService",
				"RoutingService",
			},
		}
		config["stats"] = map[string]any{}
		config["policy"] = map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"handshake":         4,
					"connIdle":          300,
					"uplinkOnly":        2,
					"downlinkOnly":      5,
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		}
	}

	return json.MarshalIndent(config, "", "  ")
}

func buildOutbound(node model.Node, settings model.Settings) (map[string]any, error) {
	var outbound map[string]any
	if strings.TrimSpace(node.OutboundJSON) != "" {
		if err := json.Unmarshal([]byte(node.OutboundJSON), &outbound); err != nil {
			return nil, fmt.Errorf("invalid outbound JSON: %w", err)
		}
		if outbound == nil {
			return nil, errors.New("outbound JSON must be an object")
		}
	} else {
		cfg, err := nodeconfig.Extract(node.Raw, node.Protocol)
		if err != nil {
			return nil, err
		}
		outbound, err = nodeconfig.BuildOutbound(cfg)
		if err != nil {
			return nil, err
		}
	}
	outbound["tag"] = "proxy"
	if settings.XrayMuxEnabled {
		outbound["mux"] = map[string]any{
			"enabled":     true,
			"concurrency": settings.XrayMuxConcurrency,
		}
	}
	return outbound, nil
}

func buildRouting(proxyMode string, customRules []model.RoutingRule, apiEnabled bool, domainStrategy string) map[string]any {
	rules := routing.BuildXrayRules(customRules, proxyMode)
	if apiEnabled {
		rules = append([]any{map[string]any{
			"type":        "field",
			"inboundTag":  []string{"api-in"},
			"outboundTag": "api",
		}}, rules...)
	}
	return map[string]any{
		"domainStrategy": domainStrategy,
		"rules":          rules,
	}
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			out = append(out, item)
		}
	}
	return out
}
