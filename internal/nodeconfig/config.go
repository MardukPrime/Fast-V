package nodeconfig

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// Extract reads a supported share link into an editable node configuration.
func Extract(raw string, protocolHint string) (model.NodeConfig, error) {
	protocol := strings.ToLower(strings.TrimSpace(protocolHint))
	if raw != "" {
		if scheme, _, ok := strings.Cut(raw, "://"); ok {
			protocol = strings.ToLower(strings.TrimSpace(scheme))
		}
	}
	switch protocol {
	case "vmess":
		return extractVMess(raw)
	case "vless":
		return extractVLESS(raw)
	case "trojan":
		return extractTrojan(raw)
	case "shadowsocks", "ss":
		return extractShadowsocks(raw)
	default:
		return model.NodeConfig{}, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

func extractVMess(raw string) (model.NodeConfig, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	decoded, ok := decodeBase64(payload)
	if !ok {
		return model.NodeConfig{}, errors.New("invalid vmess link")
	}
	var data map[string]any
	if err := json.Unmarshal(decoded, &data); err != nil {
		return model.NodeConfig{}, err
	}
	address := valueString(data, "add")
	port := valueInt(data, "port")
	id := valueString(data, "id")
	if address == "" || port <= 0 || id == "" {
		return model.NodeConfig{}, errors.New("incomplete vmess link")
	}
	return model.NodeConfig{
		Name:        valueString(data, "ps"),
		Protocol:    "vmess",
		Address:     address,
		Port:        port,
		UUID:        id,
		Security:    firstNonEmpty(valueString(data, "scy"), "auto"),
		Network:     normalizeNetwork(valueString(data, "net")),
		TLS:         valueString(data, "tls"),
		ServerName:  valueString(data, "sni"),
		Fingerprint: valueString(data, "fp"),
		ALPN:        valueALPN(data["alpn"]),
		Host:        valueString(data, "host"),
		Path:        valueString(data, "path"),
	}, nil
}

func extractVLESS(raw string) (model.NodeConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return model.NodeConfig{}, errors.New("invalid vless link")
	}
	port, err := portFromURL(u)
	if err != nil {
		return model.NodeConfig{}, err
	}
	if u.User == nil || u.User.Username() == "" {
		return model.NodeConfig{}, errors.New("vless link has no id")
	}
	q := u.Query()
	return model.NodeConfig{
		Name:          u.Fragment,
		Protocol:      "vless",
		Address:       u.Hostname(),
		Port:          port,
		UUID:          u.User.Username(),
		Flow:          q.Get("flow"),
		Network:       normalizeNetwork(q.Get("type")),
		TLS:           firstNonEmpty(q.Get("security"), "none"),
		ServerName:    q.Get("sni"),
		Fingerprint:   q.Get("fp"),
		ALPN:          splitALPN(q.Get("alpn")),
		Host:          q.Get("host"),
		Path:          q.Get("path"),
		ServiceName:   q.Get("serviceName"),
		PublicKey:     q.Get("pbk"),
		ShortID:       q.Get("sid"),
		SpiderX:       q.Get("spx"),
		AllowInsecure: q.Get("allowInsecure") == "1" || q.Get("allowInsecure") == "true" || q.Get("insecure") == "1",
	}, nil
}

func extractTrojan(raw string) (model.NodeConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return model.NodeConfig{}, errors.New("invalid trojan link")
	}
	port, err := portFromURL(u)
	if err != nil {
		return model.NodeConfig{}, err
	}
	q := u.Query()
	return model.NodeConfig{
		Name:          u.Fragment,
		Protocol:      "trojan",
		Address:       u.Hostname(),
		Port:          port,
		Password:      u.User.Username(),
		Network:       normalizeNetwork(q.Get("type")),
		TLS:           firstNonEmpty(q.Get("security"), "tls"),
		ServerName:    q.Get("sni"),
		Fingerprint:   q.Get("fp"),
		ALPN:          splitALPN(q.Get("alpn")),
		Host:          q.Get("host"),
		Path:          q.Get("path"),
		ServiceName:   q.Get("serviceName"),
		PublicKey:     q.Get("pbk"),
		ShortID:       q.Get("sid"),
		SpiderX:       q.Get("spx"),
		AllowInsecure: q.Get("allowInsecure") == "1" || q.Get("allowInsecure") == "true" || q.Get("insecure") == "1",
	}, nil
}

func extractShadowsocks(raw string) (model.NodeConfig, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return model.NodeConfig{}, errors.New("invalid shadowsocks link")
	}
	port, err := portFromURL(u)
	if err != nil {
		return model.NodeConfig{}, err
	}
	method, password := parseSSUserInfo(u.User)
	return model.NodeConfig{
		Name:     u.Fragment,
		Protocol: "shadowsocks",
		Address:  u.Hostname(),
		Port:     port,
		Method:   method,
		Password: password,
		Network:  "raw",
		TLS:      "none",
	}, nil
}

// BuildRaw converts an editable configuration back into a share link.
func BuildRaw(cfg model.NodeConfig) (string, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return "", errors.New("节点地址不能为空")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return "", errors.New("节点端口必须在 1-65535 之间")
	}
	switch strings.ToLower(cfg.Protocol) {
	case "vmess":
		return buildVMessRaw(cfg)
	case "vless":
		return buildURLRaw(cfg, "vless")
	case "trojan":
		return buildURLRaw(cfg, "trojan")
	case "shadowsocks", "ss":
		return buildShadowsocksRaw(cfg)
	default:
		return "", fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
	}
}

func buildVMessRaw(cfg model.NodeConfig) (string, error) {
	if strings.TrimSpace(cfg.UUID) == "" {
		return "", errors.New("VMess UUID 不能为空")
	}
	payload := map[string]any{
		"v":    "2",
		"ps":   cfg.Name,
		"add":  cfg.Address,
		"port": cfg.Port,
		"id":   cfg.UUID,
		"aid":  0,
		"scy":  firstNonEmpty(cfg.Security, "auto"),
		"net":  networkLinkValue(cfg.Network),
		"type": "none",
		"host": cfg.Host,
		"path": cfg.Path,
		"tls":  cfg.TLS,
		"sni":  cfg.ServerName,
		"fp":   cfg.Fingerprint,
	}
	if len(cfg.ALPN) > 0 {
		payload["alpn"] = strings.Join(cfg.ALPN, ",")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(raw), nil
}

func buildURLRaw(cfg model.NodeConfig, scheme string) (string, error) {
	credentials := cfg.UUID
	if scheme == "trojan" {
		credentials = cfg.Password
	}
	if strings.TrimSpace(credentials) == "" {
		return "", fmt.Errorf("%s 凭证不能为空", scheme)
	}
	q := url.Values{}
	q.Set("type", networkLinkValue(cfg.Network))
	if cfg.TLS != "" && cfg.TLS != "none" {
		q.Set("security", cfg.TLS)
	}
	if scheme == "vless" {
		q.Set("encryption", "none")
	}
	if cfg.ServerName != "" {
		q.Set("sni", cfg.ServerName)
	}
	if cfg.Fingerprint != "" {
		q.Set("fp", cfg.Fingerprint)
	}
	if len(cfg.ALPN) > 0 {
		q.Set("alpn", strings.Join(cfg.ALPN, ","))
	}
	if cfg.Host != "" {
		q.Set("host", cfg.Host)
	}
	if cfg.Path != "" {
		q.Set("path", cfg.Path)
	}
	if cfg.ServiceName != "" {
		q.Set("serviceName", cfg.ServiceName)
	}
	if cfg.Flow != "" {
		q.Set("flow", cfg.Flow)
	}
	if cfg.PublicKey != "" {
		q.Set("pbk", cfg.PublicKey)
	}
	if cfg.ShortID != "" {
		q.Set("sid", cfg.ShortID)
	}
	if cfg.SpiderX != "" {
		q.Set("spx", cfg.SpiderX)
	}
	if cfg.AllowInsecure {
		q.Set("allowInsecure", "1")
	}
	u := &url.URL{
		Scheme:   scheme,
		User:     url.User(credentials),
		Host:     net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port)),
		RawQuery: q.Encode(),
		Fragment: cfg.Name,
	}
	return u.String(), nil
}

func buildShadowsocksRaw(cfg model.NodeConfig) (string, error) {
	if strings.TrimSpace(cfg.Method) == "" || strings.TrimSpace(cfg.Password) == "" {
		return "", errors.New("Shadowsocks 加密方式和密码不能为空")
	}
	userinfo := base64.RawURLEncoding.EncodeToString([]byte(cfg.Method + ":" + cfg.Password))
	u := &url.URL{
		Scheme:   "ss",
		User:     url.User(userinfo),
		Host:     net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port)),
		Fragment: cfg.Name,
	}
	return u.String(), nil
}

// BuildOutbound converts an editable configuration to an Xray outbound object.
func BuildOutbound(cfg model.NodeConfig) (map[string]any, error) {
	protocol := strings.ToLower(cfg.Protocol)
	var settings map[string]any
	var stream map[string]any
	switch protocol {
	case "vmess":
		if cfg.UUID == "" {
			return nil, errors.New("VMess UUID 不能为空")
		}
		settings = map[string]any{"address": cfg.Address, "port": cfg.Port, "id": cfg.UUID, "security": firstNonEmpty(cfg.Security, "auto")}
		stream = buildStreamSettings(cfg)
	case "vless":
		if cfg.UUID == "" {
			return nil, errors.New("VLESS UUID 不能为空")
		}
		settings = map[string]any{"address": cfg.Address, "port": cfg.Port, "id": cfg.UUID, "encryption": "none"}
		if cfg.Flow != "" {
			settings["flow"] = cfg.Flow
		}
		stream = buildStreamSettings(cfg)
	case "trojan":
		if cfg.Password == "" {
			return nil, errors.New("Trojan 密码不能为空")
		}
		settings = map[string]any{"address": cfg.Address, "port": cfg.Port, "password": cfg.Password}
		stream = buildStreamSettings(cfg)
	case "shadowsocks", "ss":
		if cfg.Method == "" || cfg.Password == "" {
			return nil, errors.New("Shadowsocks 加密方式和密码不能为空")
		}
		settings = map[string]any{"address": cfg.Address, "port": cfg.Port, "method": cfg.Method, "password": cfg.Password}
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
	}
	outbound := map[string]any{"protocol": protocol, "settings": settings}
	if protocol == "shadowsocks" || protocol == "ss" {
		outbound["protocol"] = "shadowsocks"
	} else if stream != nil {
		outbound["streamSettings"] = stream
	}
	return outbound, nil
}

func buildStreamSettings(cfg model.NodeConfig) map[string]any {
	network := normalizeNetwork(cfg.Network)
	security := strings.ToLower(strings.TrimSpace(cfg.TLS))
	if security == "" {
		security = "none"
	}
	stream := map[string]any{"method": network}
	if security != "none" {
		stream["security"] = security
	}
	switch security {
	case "tls":
		tlsSettings := map[string]any{}
		if cfg.ServerName != "" {
			tlsSettings["serverName"] = cfg.ServerName
		}
		if cfg.Fingerprint != "" {
			tlsSettings["fingerprint"] = cfg.Fingerprint
		}
		if len(cfg.ALPN) > 0 {
			tlsSettings["alpn"] = cfg.ALPN
		}
		if cfg.AllowInsecure {
			tlsSettings["allowInsecure"] = true
		}
		if len(tlsSettings) > 0 {
			stream["tlsSettings"] = tlsSettings
		}
	case "reality":
		realitySettings := map[string]any{}
		if cfg.ServerName != "" {
			realitySettings["serverName"] = cfg.ServerName
		}
		if cfg.Fingerprint != "" {
			realitySettings["fingerprint"] = cfg.Fingerprint
		}
		if cfg.PublicKey != "" {
			realitySettings["password"] = cfg.PublicKey
		}
		if cfg.ShortID != "" {
			realitySettings["shortId"] = cfg.ShortID
		}
		if cfg.SpiderX != "" {
			realitySettings["spiderX"] = cfg.SpiderX
		}
		if len(realitySettings) > 0 {
			stream["realitySettings"] = realitySettings
		}
	}
	switch network {
	case "websocket":
		ws := map[string]any{}
		if cfg.Path != "" {
			ws["path"] = cfg.Path
		}
		if cfg.Host != "" {
			ws["host"] = cfg.Host
		}
		if len(ws) > 0 {
			stream["wsSettings"] = ws
		}
	case "grpc":
		grpc := map[string]any{}
		if cfg.ServiceName != "" {
			grpc["serviceName"] = cfg.ServiceName
		}
		if cfg.Host != "" {
			grpc["authority"] = cfg.Host
		}
		if len(grpc) > 0 {
			stream["grpcSettings"] = grpc
		}
	case "httpupgrade":
		upgrade := map[string]any{}
		if cfg.Path != "" {
			upgrade["path"] = cfg.Path
		}
		if cfg.Host != "" {
			upgrade["host"] = cfg.Host
		}
		if len(upgrade) > 0 {
			stream["httpupgradeSettings"] = upgrade
		}
	case "xhttp":
		xhttp := map[string]any{}
		if cfg.Path != "" {
			xhttp["path"] = cfg.Path
		}
		if cfg.Host != "" {
			xhttp["host"] = cfg.Host
		}
		if len(xhttp) > 0 {
			stream["xhttpSettings"] = xhttp
		}
	}
	return stream
}

func networkLinkValue(network string) string {
	switch normalizeNetwork(network) {
	case "websocket":
		return "ws"
	case "raw":
		return "tcp"
	default:
		return normalizeNetwork(network)
	}
}

func normalizeNetwork(network string) string {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "", "tcp", "raw":
		return "raw"
	case "ws", "websocket":
		return "websocket"
	case "httpupgrade":
		return "httpupgrade"
	case "xhttp":
		return "xhttp"
	case "grpc":
		return "grpc"
	case "mkcp", "kcp":
		return "mkcp"
	default:
		return strings.ToLower(strings.TrimSpace(network))
	}
}

func parseSSUserInfo(user *url.Userinfo) (string, string) {
	raw := user.String()
	if decoded, ok := decodeBase64(raw); ok && strings.Contains(string(decoded), ":") {
		parts := strings.SplitN(string(decoded), ":", 2)
		return parts[0], parts[1]
	}
	password, _ := user.Password()
	return user.Username(), password
}

func portFromURL(u *url.URL) (int, error) {
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 0, errors.New("节点端口无效")
	}
	return port, nil
}

func decodeBase64(value string) ([]byte, bool) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

func valueString(data map[string]any, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func valueInt(data map[string]any, key string) int {
	number, err := strconv.Atoi(valueString(data, key))
	if err != nil {
		return 0
	}
	return number
}

func valueALPN(value any) []string {
	switch typed := value.(type) {
	case string:
		return splitALPN(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func splitALPN(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		if text := strings.TrimSpace(part); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
