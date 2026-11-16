package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
)

// supportedClashTypes lists the Clash proxy types that can be converted into an
// Xray share link. Other entries of a Clash subscription are skipped with a
// per-entry reason instead of failing the whole import.
var supportedClashTypes = map[string]string{
	"vmess":       "vmess",
	"vless":       "vless",
	"trojan":      "trojan",
	"ss":          "shadowsocks",
	"shadowsocks": "shadowsocks",
}

// ParseClash converts a Clash / Clash.Meta YAML subscription into nodes that the
// rest of FastV understands. Each imported node keeps a generated share link in
// ParsedNode.Raw so it can be edited, re-exported and used by the node editor
// exactly like a link-based subscription entry.
//
// The second return value contains one human-readable reason per skipped proxy
// (unsupported type, missing credentials, Shadowsocks plugin, ...). When every
// proxy is skipped an error is returned instead of an empty node list.
func ParseClash(content string) ([]ParsedNode, []string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil, errors.New("Clash 订阅内容为空")
	}

	var document map[string]any
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		return nil, nil, fmt.Errorf("Clash YAML 解析失败: %w", err)
	}
	if document == nil {
		return nil, nil, errors.New("Clash 订阅不是有效的 YAML 映射")
	}

	rawProxies, ok := document["proxies"]
	if !ok || rawProxies == nil {
		return nil, nil, errors.New("Clash 订阅缺少 proxies 列表")
	}
	proxies, ok := rawProxies.([]any)
	if !ok {
		return nil, nil, errors.New("Clash 订阅的 proxies 字段不是列表")
	}
	if len(proxies) == 0 {
		return nil, nil, errors.New("Clash 订阅的 proxies 列表为空")
	}

	nodes := make([]ParsedNode, 0, len(proxies))
	issues := make([]string, 0, len(proxies))
	seen := make(map[string]struct{}, len(proxies))
	for index, rawEntry := range proxies {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("第 %d 个代理不是 YAML 映射，已跳过", index+1))
			continue
		}
		node, err := parseClashProxy(entry)
		if err != nil {
			issues = append(issues, fmt.Sprintf("第 %d 个代理 %q: %v", index+1, clashString(entry, "name"), err))
			continue
		}
		key := fmt.Sprintf("%s|%s|%d|%s", node.Protocol, node.Address, node.Port, node.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		nodes = append(nodes, node)
	}

	if len(nodes) == 0 {
		return nil, issues, fmt.Errorf("Clash 订阅中没有可导入的节点: %s", summarizeParseIssues(issues, 5))
	}
	return nodes, issues, nil
}

// looksLikeClashYAML reports whether content should be handled by ParseClash.
// It accepts both a valid YAML mapping (proxies/proxy-groups/proxy-providers or
// any other mapping) and a document whose first meaningful line already is a
// Clash key, so a malformed Clash document is reported by ParseClash with a
// proper YAML error instead of the generic "no nodes" message.
func looksLikeClashYAML(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}

	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}
		if hasClashKeyPrefix(line) {
			return true
		}
		break
	}

	var document map[string]any
	if err := yaml.Unmarshal([]byte(trimmed), &document); err != nil || len(document) == 0 {
		return false
	}
	for _, key := range []string{"proxies", "proxy-groups", "proxy-providers"} {
		if _, ok := document[key]; ok {
			return true
		}
	}
	// Any other YAML mapping is still worth a Clash parse attempt so the caller
	// gets a precise "missing proxies" error.
	return true
}

func hasClashKeyPrefix(line string) bool {
	for _, key := range []string{"proxies:", "proxy-groups:", "proxy-providers:"} {
		if strings.HasPrefix(line, key) {
			return true
		}
	}
	return false
}

func summarizeParseIssues(issues []string, limit int) string {
	if len(issues) == 0 {
		return "没有可识别的代理条目"
	}
	if len(issues) <= limit {
		return strings.Join(issues, "; ")
	}
	return strings.Join(issues[:limit], "; ") + fmt.Sprintf(" 等共 %d 条原因", len(issues))
}

// parseClashProxy maps one entry of the Clash proxies list to a ParsedNode.
func parseClashProxy(entry map[string]any) (ParsedNode, error) {
	rawType := strings.ToLower(clashString(entry, "type"))
	if rawType == "" {
		return ParsedNode{}, errors.New("缺少 type 字段")
	}
	protocol, ok := supportedClashTypes[rawType]
	if !ok {
		return ParsedNode{}, fmt.Errorf("不支持的协议类型 %q", rawType)
	}

	server := clashString(entry, "server")
	port := clashInt(entry, "port")
	if server == "" || port <= 0 || port > 65535 {
		return ParsedNode{}, errors.New("缺少有效的 server/port")
	}

	name := clashString(entry, "name")
	if name == "" {
		name = fallbackNodeName(protocol, server, port)
	}

	cfg := model.NodeConfig{
		Name:          name,
		Protocol:      protocol,
		Address:       server,
		Port:          port,
		Network:       clashString(entry, "network"),
		ServerName:    clashFirstNonEmpty(clashString(entry, "sni"), clashString(entry, "servername")),
		Fingerprint:   clashString(entry, "client-fingerprint"),
		ALPN:          clashStringSlice(entry, "alpn"),
		AllowInsecure: clashBool(entry, "skip-cert-verify"),
		Flow:          clashString(entry, "flow"),
	}

	// Transport options are nested one level deeper in the Clash format.
	if wsOptions := clashMap(entry, "ws-opts"); wsOptions != nil {
		if cfg.Path == "" {
			cfg.Path = clashString(wsOptions, "path")
		}
		if headers := clashMap(wsOptions, "headers"); headers != nil {
			cfg.Host = clashHeader(headers, "Host")
		}
	}
	if grpcOptions := clashMap(entry, "grpc-opts"); grpcOptions != nil {
		cfg.ServiceName = clashString(grpcOptions, "grpc-service-name")
	}
	if realityOptions := clashMap(entry, "reality-opts"); realityOptions != nil {
		cfg.PublicKey = clashString(realityOptions, "public-key")
		cfg.ShortID = clashString(realityOptions, "short-id")
	}

	alterID := 0
	switch protocol {
	case "vmess":
		cfg.UUID = clashString(entry, "uuid")
		cfg.Security = clashFirstNonEmpty(clashString(entry, "cipher"), "auto")
		if clashBool(entry, "tls") {
			cfg.TLS = "tls"
		}
		alterID = clashInt(entry, "alterId")
	case "vless":
		cfg.UUID = clashString(entry, "uuid")
		cfg.TLS = clashTLSMode(entry)
	case "trojan":
		cfg.Password = clashString(entry, "password")
		// Trojan is TLS-only by convention.
		cfg.TLS = "tls"
	case "shadowsocks":
		if plugin := clashString(entry, "plugin"); plugin != "" {
			return ParsedNode{}, fmt.Errorf("暂不支持 Shadowsocks 插件 %q", plugin)
		}
		cfg.Method = clashString(entry, "cipher")
		cfg.Password = clashString(entry, "password")
		cfg.Network = "raw"
		cfg.TLS = "none"
	}

	raw, err := buildClashShareLink(cfg, alterID)
	if err != nil {
		return ParsedNode{}, fmt.Errorf("无法生成分享链接: %w", err)
	}
	// Validate the generated link with the normal parser so a malformed upstream
	// entry can never poison the node list with an unusable raw link.
	if _, err := ParseLink(raw); err != nil {
		return ParsedNode{}, fmt.Errorf("生成的分享链接无效: %w", err)
	}

	return ParsedNode{
		Name:     name,
		Protocol: protocol,
		Address:  server,
		Port:     port,
		Raw:      raw,
	}, nil
}

// clashTLSMode maps the different ways Clash expresses transport security.
func clashTLSMode(entry map[string]any) string {
	if reality := clashMap(entry, "reality-opts"); reality != nil && clashString(reality, "public-key") != "" {
		return "reality"
	}
	switch security := strings.ToLower(clashString(entry, "security")); security {
	case "reality":
		return "reality"
	case "tls", "xtls":
		// XTLS is deprecated in Xray; a plain TLS link is the closest usable form.
		return "tls"
	case "none":
		return "none"
	}
	if clashBool(entry, "tls") {
		return "tls"
	}
	return "none"
}

// buildClashShareLink builds the share link with nodeconfig.BuildRaw. The shared
// NodeConfig has no alterId field, so a non-zero Clash alterId is patched back
// into the generated VMess JSON payload to keep the legacy value intact.
func buildClashShareLink(cfg model.NodeConfig, alterID int) (string, error) {
	raw, err := nodeconfig.BuildRaw(cfg)
	if err != nil {
		return "", err
	}
	if cfg.Protocol != "vmess" || alterID <= 0 {
		return raw, nil
	}

	decoded, ok := decodeBase64(strings.TrimPrefix(raw, "vmess://"))
	if !ok {
		return raw, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return raw, nil
	}
	payload["aid"] = alterID
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, nil
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(encoded), nil
}

func clashFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clashString(entry map[string]any, key string) string {
	value, ok := entry[key]
	if !ok || value == nil {
		return ""
	}
	return clashValueString(value)
}

func clashValueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func clashInt(entry map[string]any, key string) int {
	switch typed := entry[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0
		}
		return number
	default:
		return 0
	}
}

func clashBool(entry map[string]any, key string) bool {
	switch typed := entry[key].(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "on":
			return true
		}
		return false
	case int:
		return typed != 0
	default:
		return false
	}
}

func clashMap(entry map[string]any, key string) map[string]any {
	value, ok := entry[key]
	if !ok || value == nil {
		return nil
	}
	nested, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return nested
}

func clashStringSlice(entry map[string]any, key string) []string {
	value, ok := entry[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := clashValueString(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case string:
		out := make([]string, 0)
		for _, part := range strings.Split(typed, ",") {
			if text := strings.TrimSpace(part); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		if text := clashValueString(value); text != "" {
			return []string{text}
		}
		return nil
	}
}

func clashHeader(headers map[string]any, key string) string {
	for headerName, value := range headers {
		if strings.EqualFold(headerName, key) {
			return clashValueString(value)
		}
	}
	return ""
}
