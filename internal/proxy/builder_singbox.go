package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
)

// BuildSingBoxConfig creates a complete sing-box configuration for the active
// node and settings. Xray-specific options (TUN, load balancing, realtime stats)
// are handled or rejected by the manager before this builder runs.
func BuildSingBoxConfig(node model.Node, settings model.Settings, customRules ...model.RoutingRule) ([]byte, error) {
	outbound, err := buildSingBoxOutbound(node, settings)
	if err != nil {
		return nil, err
	}
	inbound, err := buildSingBoxInbound(settings)
	if err != nil {
		return nil, err
	}
	route, skipped := buildSingBoxRoute(settings, customRules)
	document := map[string]any{
		"log": map[string]any{"level": "warn"},
		"inbounds": []any{
			inbound,
		},
		"outbounds": []any{
			outbound,
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "block", "tag": "block"},
		},
		"route": route,
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

func buildSingBoxInbound(settings model.Settings) (map[string]any, error) {
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
	return inbound, nil
}

func buildSingBoxOutbound(node model.Node, settings model.Settings) (map[string]any, error) {
	xray, err := xrayOutboundForNode(node)
	if err != nil {
		return nil, err
	}
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

func buildSingBoxRoute(settings model.Settings, customRules []model.RoutingRule) (map[string]any, int) {
	rules := make([]any, 0, len(customRules))
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
					ranges = append(ranges, value)
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
		case "geosite", "geoip":
			// No geo datasets are bundled with FastV; skipping is safer than
			// producing a config that fails to start on missing .dat files.
			skipped++
			continue
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
	}, skipped
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
