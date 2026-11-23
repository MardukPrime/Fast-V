package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
)

// BuildSingBoxConfig creates a complete sing-box configuration for the active
// node and settings. Xray-specific options (TUN, load balancing, realtime stats)
// are handled or rejected by the manager before this builder runs.
// singBoxRuleSetURL returns the official SagerNet binary rule-set URL for one
// geosite/geoip code. Legacy geoip.db/geosite.db routing was removed in
// sing-box 1.12, so the generator uses remote rule sets instead.
func singBoxRuleSetURL(kind string, code string) string {
	return fmt.Sprintf("https://raw.githubusercontent.com/SagerNet/sing-%s/rule-set/%s-%s.srs", kind, kind, code)
}

// singBoxRuleSetTag builds a stable tag for a rule-set code (lowercase letters,
// digits and dashes). The same code always maps to the same tag so multiple
// rules referencing it share one download.
func singBoxRuleSetTag(kind string, code string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, strings.TrimSpace(code))
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		cleaned = "default"
	}
	return kind + "-" + cleaned
}

// BuildSingBoxConfig creates a complete sing-box configuration for the active
// node and settings. Xray-specific options (TUN, load balancing, realtime stats)
// are handled or rejected by the manager before this builder runs.
func BuildSingBoxConfig(node model.Node, settings model.Settings, customRules ...model.RoutingRule) ([]byte, error) {
	return buildSingBoxConfig(node, settings, "", customRules...)
}

// BuildSingBoxConfigWithGeoAssets is BuildSingBoxConfig with an explicit
// directory for the sing-box geoip.db/geosite.db databases, so the core
// downloads the data next to its binary instead of an arbitrary working dir.
func BuildSingBoxConfigWithGeoAssets(node model.Node, settings model.Settings, geoAssetDir string, customRules ...model.RoutingRule) ([]byte, error) {
	return buildSingBoxConfig(node, settings, geoAssetDir, customRules...)
}

func buildSingBoxConfig(node model.Node, settings model.Settings, geoAssetDir string, customRules ...model.RoutingRule) ([]byte, error) {
	outbound, err := buildSingBoxOutbound(node, settings)
	if err != nil {
		return nil, err
	}
	inbounds, err := buildSingBoxInbounds(settings)
	if err != nil {
		return nil, err
	}
	route, skipped, ruleSets := buildSingBoxRoute(settings, customRules)
	document := map[string]any{
		"log":      map[string]any{"level": "warn"},
		"inbounds": inbounds,
		"outbounds": []any{
			outbound,
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "block", "tag": "block"},
		},
		"route": route,
	}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
		experimental, _ := document["experimental"].(map[string]any)
		if experimental == nil {
			experimental = map[string]any{}
		}
		cacheFile := map[string]any{"enabled": true}
		if geoAssetDir != "" {
			cacheFile["path"] = filepath.Join(geoAssetDir, "cache.db")
		}
		experimental["cache_file"] = cacheFile
		document["experimental"] = experimental
	}
	if settings.TunMode {
		// TUN receives DNS queries from applications; hijack them so sing-box
		// answers locally instead of leaking plaintext DNS through the tunnel.
		document["dns"] = map[string]any{
			"servers": []any{
				map[string]any{"tag": "local", "type": "local"},
			},
			"final": "local",
		}
	}
	// The Clash API is only useful for traffic statistics; the manager enables
	// the sampler separately, so expose it whenever a valid API port is set.
	if settings.StatsEnabled || settings.XrayAPIEnabled {
		if port := settings.XrayAPIPort; port > 0 && port != settings.XrayLocalPort {
			document["experimental"] = map[string]any{
				"clash_api": map[string]any{
					"external_controller": fmt.Sprintf("127.0.0.1:%d", port),
					"default_mode":        "rule",
				},
			}
		}
	}
	if skipped > 0 {
		// The warning is emitted by the manager through config warnings; embed it
		// in the config comment is impossible in JSON, so leave the count in the
		// return value through an unexported side channel is overkill. The manager
		// logs the generic limitation instead.
		_ = skipped
	}
	return json.MarshalIndent(document, "", "  ")
}

func buildSingBoxInbounds(settings model.Settings) ([]any, error) {
	inbounds := make([]any, 0, 2)
	if settings.TunMode {
		inbounds = append(inbounds, buildSingBoxTunInbound(settings))
	}
	port := settings.XrayLocalPort
	if port <= 0 || port > 65535 {
		return nil, errors.New("本地代理端口无效")
	}
	inbound := map[string]any{
		"type":        "mixed",
		"tag":         "mixed-in",
		"listen":      localInboundListen(settings),
		"listen_port": port,
	}
	if settings.InboundAuthEnabled {
		if settings.InboundUsername == "" || settings.InboundPassword == "" {
			return nil, errors.New("启用入站认证时用户名和密码不能为空")
		}
		inbound["users"] = []any{
			map[string]any{
				"username": settings.InboundUsername,
				"password": settings.InboundPassword,
			},
		}
	}
	inbounds = append(inbounds, inbound)
	return inbounds, nil
}

// buildSingBoxTunInbound creates a Windows TUN inbound. auto_route configures
// the interface as the default route; route.auto_detect_interface (set by
// buildSingBoxRoute) keeps the core's own outbound traffic out of the TUN to
// avoid a traffic loop. sing-box manages adapter/routes/DNS cleanup itself.
func buildSingBoxTunInbound(settings model.Settings) map[string]any {
	mtu := settings.XrayTunMTU
	if mtu <= 0 {
		mtu = 1500
	}
	return map[string]any{
		"type":                     "tun",
		"tag":                      "tun-in",
		"interface_name":           tunInterfaceName,
		"address":                  []string{"198.18.0.1/15"},
		"mtu":                      mtu,
		"stack":                    "mixed",
		"auto_route":               true,
		"strict_route":             false,
		"endpoint_independent_nat": true,
	}
}

func buildSingBoxOutbound(node model.Node, settings model.Settings) (map[string]any, error) {
	xray, err := xrayOutboundForNode(node)
	if err != nil {
		return nil, err
	}
	// A pinned CDN edge applies to sing-box exactly like it does to Xray.
	applyCdnEndpoint(xray, node)
	protocol := strings.ToLower(stringOf(xray["protocol"]))
	settingsMap, _ := xray["settings"].(map[string]any)
	if settingsMap == nil {
		return nil, errors.New("节点出站配置缺少 settings")
	}
	address := stringOf(settingsMap["address"])
	port := intOf(settingsMap["port"])
	if address == "" || port <= 0 {
		return nil, errors.New("节点地址或端口无效")
	}
	outbound := map[string]any{
		"type":        protocol,
		"tag":         proxyOutboundTag,
		"server":      address,
		"server_port": port,
	}
	switch protocol {
	case "vmess":
		outbound["uuid"] = stringOf(settingsMap["id"])
		if security := stringOf(settingsMap["security"]); security != "" && !strings.EqualFold(security, "auto") {
			outbound["security"] = strings.ToLower(security)
		}
		outbound["alter_id"] = 0
	case "vless":
		outbound["uuid"] = stringOf(settingsMap["id"])
		if flow := stringOf(settingsMap["flow"]); flow != "" {
			outbound["flow"] = flow
		}
	case "trojan":
		outbound["password"] = stringOf(settingsMap["password"])
	case "shadowsocks":
		outbound["method"] = stringOf(settingsMap["method"])
		outbound["password"] = stringOf(settingsMap["password"])
		if outbound["method"] == "" || outbound["password"] == "" {
			return nil, errors.New("Shadowsocks 加密方式或密码为空")
		}
	default:
		return nil, fmt.Errorf("sing-box 暂不支持协议 %s", protocol)
	}
	stream, _ := xray["streamSettings"].(map[string]any)
	if stream == nil {
		return outbound, nil
	}
	network := strings.ToLower(stringOf(stream["method"]))
	transport, err := buildSingBoxTransport(network, stream)
	if err != nil {
		return nil, err
	}
	if transport != nil {
		outbound["transport"] = transport
	}
	security := strings.ToLower(stringOf(stream["security"]))
	switch security {
	case "tls":
		tls, err := buildSingBoxTLS(stream, false)
		if err != nil {
			return nil, err
		}
		outbound["tls"] = tls
	case "reality":
		tls, err := buildSingBoxTLS(stream, true)
		if err != nil {
			return nil, err
		}
		outbound["tls"] = tls
	}
	return outbound, nil
}

// xrayOutboundForNode resolves the normalized Xray outbound object stored on
// the node, falling back to parsing its share link.
func xrayOutboundForNode(node model.Node) (map[string]any, error) {
	if strings.TrimSpace(node.OutboundJSON) != "" {
		var outbound map[string]any
		if err := json.Unmarshal([]byte(node.OutboundJSON), &outbound); err != nil {
			return nil, fmt.Errorf("invalid outbound JSON: %w", err)
		}
		if outbound == nil {
			return nil, errors.New("outbound JSON must be an object")
		}
		return outbound, nil
	}
	cfg, err := nodeconfig.Extract(node.Raw, node.Protocol)
	if err != nil {
		return nil, err
	}
	return nodeconfig.BuildOutbound(cfg)
}

func buildSingBoxTransport(network string, stream map[string]any) (map[string]any, error) {
	switch network {
	case "", "raw", "tcp":
		return nil, nil
	case "websocket", "ws":
		ws, _ := stream["wsSettings"].(map[string]any)
		transport := map[string]any{"type": "ws"}
		if path := stringOf(ws["path"]); path != "" {
			transport["path"] = path
		}
		if host := stringOf(ws["host"]); host != "" {
			transport["headers"] = map[string]any{"Host": host}
		}
		return transport, nil
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]any)
		transport := map[string]any{"type": "grpc"}
		if service := stringOf(grpc["serviceName"]); service != "" {
			transport["service_name"] = service
		}
		return transport, nil
	case "httpupgrade":
		settings, _ := stream["httpupgradeSettings"].(map[string]any)
		transport := map[string]any{"type": "httpupgrade"}
		if path := stringOf(settings["path"]); path != "" {
			transport["path"] = path
		}
		if host := stringOf(settings["host"]); host != "" {
			transport["host"] = host
		}
		return transport, nil
	case "xhttp", "splithttp":
		return nil, errors.New("sing-box 暂不支持 XHTTP 传输")
	default:
		return nil, fmt.Errorf("sing-box 暂不支持传输方式 %s", network)
	}
}

func buildSingBoxTLS(stream map[string]any, reality bool) (map[string]any, error) {
	settingsKey := "tlsSettings"
	if reality {
		settingsKey = "realitySettings"
	}
	settings, _ := stream[settingsKey].(map[string]any)
	tls := map[string]any{"enabled": true}
	if serverName := stringOf(settings["serverName"]); serverName != "" {
		tls["server_name"] = serverName
	}
	if booleanOf(settings["allowInsecure"]) {
		tls["insecure"] = true
	}
	if alpn := stringSliceOf(settings["alpn"]); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	fingerprint := stringOf(settings["fingerprint"])
	if fingerprint != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fingerprint}
	}
	if reality {
		publicKey := stringOf(settings["password"])
		if publicKey == "" {
			return nil, errors.New("REALITY 节点缺少公钥")
		}
		tls["reality"] = map[string]any{
			"enabled":    true,
			"public_key": publicKey,
			"short_id":   stringOf(settings["shortId"]),
		}
	}
	return tls, nil
}

func buildSingBoxRoute(settings model.Settings, customRules []model.RoutingRule) (map[string]any, int, []any) {
	rules := make([]any, 0, len(customRules)+2)
	ruleSets := make([]any, 0, 2)
	seenRuleSets := map[string]bool{}
	addRuleSet := func(kind string, values []string) []string {
		tags := make([]string, 0, len(values))
		for _, value := range values {
			tag := singBoxRuleSetTag(kind, value)
			if !seenRuleSets[tag] {
				seenRuleSets[tag] = true
				ruleSets = append(ruleSets, map[string]any{
					"tag":             tag,
					"type":            "remote",
					"format":          "binary",
					"url":             singBoxRuleSetURL(kind, value),
					"download_detour": proxyOutboundTag,
				})
			}
			tags = append(tags, tag)
		}
		return tags
	}
	if settings.TunMode {
		// DNS queries captured by the TUN are answered by sing-box, and local
		// network traffic avoids the proxy.
		rules = append(rules,
			map[string]any{"protocol": "dns", "action": "hijack-dns"},
			map[string]any{"ip_is_private": true, "action": "route", "outbound": "direct"},
		)
	}
	skipped := 0
	for _, rule := range customRules {
		if !rule.Enabled {
			continue
		}
		singboxRule := map[string]any{}
		values := routingSplitValues(rule.Value)
		if len(values) == 0 {
			continue
		}
		switch rule.Type {
		case "domain_suffix":
			singboxRule["domain_suffix"] = values
		case "domain_keyword":
			singboxRule["domain_keyword"] = values
		case "domain_regex":
			singboxRule["domain_regex"] = values
		case "ip":
			singboxRule["ip_cidr"] = values
		case "port":
			ranges := make([]string, 0, len(values))
			ports := make([]int, 0, len(values))
			for _, value := range values {
				if strings.Contains(value, "-") {
					// Xray uses "start-end" while sing-box expects "start:end".
					ranges = append(ranges, strings.Replace(value, "-", ":", 1))
					continue
				}
				if port, err := strconv.Atoi(value); err == nil {
					ports = append(ports, port)
				}
			}
			if len(ports) > 0 {
				singboxRule["port"] = ports
			}
			if len(ranges) > 0 {
				singboxRule["port_range"] = ranges
			}
		case "network":
			networks := make([]string, 0, len(values))
			for _, value := range values {
				network := strings.ToLower(value)
				if network == "tcp" || network == "udp" {
					networks = append(networks, network)
				}
			}
			if len(networks) == 0 {
				skipped++
				continue
			}
			singboxRule["network"] = networks
		case "process":
			singboxRule["process_name"] = values
		case "geosite":
			singboxRule["rule_set"] = addRuleSet("geosite", values)
		case "geoip":
			singboxRule["rule_set"] = addRuleSet("geoip", values)
		default:
			skipped++
			continue
		}
		switch rule.Action {
		case "direct":
			singboxRule["action"] = "route"
			singboxRule["outbound"] = "direct"
		case "block":
			singboxRule["action"] = "reject"
		default:
			singboxRule["action"] = "route"
			singboxRule["outbound"] = proxyOutboundTag
		}
		rules = append(rules, singboxRule)
	}
	final := proxyOutboundTag
	if strings.EqualFold(settings.ProxyMode, "direct") {
		final = "direct"
	}
	return map[string]any{
		"rules":                 rules,
		"final":                 final,
		"auto_detect_interface": true,
	}, skipped, ruleSets
}

func routingSplitValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stringOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
	}
	return ""
}

func intOf(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		number, _ := typed.Int64()
		return int(number)
	case string:
		number, _ := strconv.Atoi(typed)
		return number
	}
	return 0
}

func booleanOf(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func stringSliceOf(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := stringOf(item); text != "" {
			out = append(out, text)
		}
	}
	if values, ok := value.([]string); ok {
		return values
	}
	return out
}
