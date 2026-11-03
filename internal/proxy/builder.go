package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
)

const tunInterfaceName = "FastV-TUN"

// BuildConfig creates a complete Xray JSON configuration for the selected node and settings.
func BuildConfig(node model.Node, settings model.Settings) ([]byte, error) {
	if settings.CoreType != "xray" {
		return nil, errors.New("only Xray-core is currently supported")
	}

	outbound, err := buildOutbound(node, settings)
	if err != nil {
		return nil, err
	}

	inbounds := make([]any, 0, 2)
	if settings.TunMode {
		inbounds = append(inbounds, map[string]any{
			"tag":      "tun-in",
			"protocol": "tun",
			"settings": map[string]any{
				"name": tunInterfaceName,
				"MTU":  1500,
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
		"routing": buildRouting(settings.ProxyMode),
		"dns": map[string]any{
			"servers":       []string{"1.1.1.1", "8.8.8.8"},
			"queryStrategy": "UseIP",
		},
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

func buildRouting(proxyMode string) map[string]any {
	rules := make([]any, 0)
	switch proxyMode {
	case "direct":
		rules = append(rules, map[string]any{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "direct",
		})
	case "global":
		rules = append(rules, map[string]any{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "proxy",
		})
	default:
		rules = append(rules,
			map[string]any{
				"type":        "field",
				"ip":          []string{"geoip:private"},
				"outboundTag": "direct",
			},
			map[string]any{
				"type":        "field",
				"domain":      []string{"geosite:category-ads-all"},
				"outboundTag": "block",
			},
			map[string]any{
				"type":        "field",
				"network":     "tcp,udp",
				"outboundTag": "proxy",
			},
		)
	}

	return map[string]any{
		"domainStrategy": "IPIfNonMatch",
		"rules":          rules,
	}
}
