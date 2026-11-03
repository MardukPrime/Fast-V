package proxy

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
)

type streamOptions struct {
	network     string
	security    string
	host        string
	path        string
	serviceName string
	serverName  string
	fingerprint string
	publicKey   string
	shortID     string
	spiderX     string
	alpn        []string
}

// BuildConfig creates a complete Xray JSON configuration for the selected node and settings.
func BuildConfig(node model.Node, settings model.Settings) ([]byte, error) {
	if settings.CoreType != "xray" {
		return nil, errors.New("only Xray-core is currently supported")
	}

	outbound, err := buildOutbound(node, settings)
	if err != nil {
		return nil, err
	}

	inbound := map[string]any{
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
	}

	config := map[string]any{
		"log": map[string]any{
			"loglevel": settings.XrayLogLevel,
		},
		"inbounds": []any{inbound},
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

func buildOutbound(node model.Node, settings model.Settings) (map[string]any, error) {
	if strings.TrimSpace(node.Raw) == "" {
		return nil, errors.New("node has no raw share link")
	}

	var outbound map[string]any
	var err error
	switch strings.ToLower(node.Protocol) {
	case "vmess":
		outbound, err = buildVMess(node.Raw)
	case "vless":
		outbound, err = buildVLESS(node.Raw)
	case "trojan":
		outbound, err = buildTrojan(node.Raw)
	case "shadowsocks":
		outbound, err = buildShadowsocks(node.Raw)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", node.Protocol)
	}
	if err != nil {
		return nil, err
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

func buildVMess(raw string) (map[string]any, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	decoded, ok := decodeBase64(payload)
	if !ok {
		return nil, errors.New("invalid vmess link")
	}
	var data map[string]any
	if err := json.Unmarshal(decoded, &data); err != nil {
		return nil, err
	}
	address := valueString(data, "add")
	port := valueInt(data, "port")
	id := valueString(data, "id")
	if address == "" || port <= 0 || id == "" {
		return nil, errors.New("incomplete vmess link")
	}
	network := valueString(data, "net")
	if network == "" {
		network = "raw"
	}
	security := valueString(data, "tls")
	path := valueString(data, "path")
	host := valueString(data, "host")
	sni := valueString(data, "sni")
	if sni == "" {
		sni = host
	}
	stream := buildStreamSettings(streamOptions{
		network:     network,
		security:    security,
		host:        host,
		path:        path,
		serverName:  sni,
		fingerprint: valueString(data, "fp"),
		alpn:        valueALPN(data["alpn"]),
	})

	settings := map[string]any{
		"address":  address,
		"port":     port,
		"id":       id,
		"security": firstNonEmpty(valueString(data, "scy"), "auto"),
	}
	return map[string]any{
		"protocol":       "vmess",
		"settings":       settings,
		"streamSettings": stream,
	}, nil
}

func buildVLESS(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("invalid vless link")
	}
	port, err := portFromURL(u)
	if err != nil {
		return nil, err
	}
	id := ""
	if u.User != nil {
		id = u.User.Username()
	}
	if id == "" {
		return nil, errors.New("vless link has no id")
	}
	q := u.Query()
	stream := buildStreamSettings(streamOptions{
		network:     q.Get("type"),
		security:    q.Get("security"),
		host:        q.Get("host"),
		path:        q.Get("path"),
		serviceName: q.Get("serviceName"),
		serverName:  q.Get("sni"),
		fingerprint: q.Get("fp"),
		publicKey:   q.Get("pbk"),
		shortID:     q.Get("sid"),
		spiderX:     q.Get("spx"),
		alpn:        splitALPN(q.Get("alpn")),
	})

	settings := map[string]any{
		"address":    u.Hostname(),
		"port":       port,
		"id":         id,
		"encryption": "none",
	}
	if flow := q.Get("flow"); flow != "" {
		settings["flow"] = flow
	}
	return map[string]any{
		"protocol":       "vless",
		"settings":       settings,
		"streamSettings": stream,
	}, nil
}

func buildTrojan(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, errors.New("invalid trojan link")
	}
	port, err := portFromURL(u)
	if err != nil {
		return nil, err
	}
	password := u.User.Username()
	if password == "" {
		return nil, errors.New("trojan link has no password")
	}
	q := u.Query()
	stream := buildStreamSettings(streamOptions{
		network:     q.Get("type"),
		security:    firstNonEmpty(q.Get("security"), "tls"),
		host:        q.Get("host"),
		path:        q.Get("path"),
		serviceName: q.Get("serviceName"),
		serverName:  q.Get("sni"),
		fingerprint: q.Get("fp"),
		publicKey:   q.Get("pbk"),
		shortID:     q.Get("sid"),
		spiderX:     q.Get("spx"),
		alpn:        splitALPN(q.Get("alpn")),
	})
	return map[string]any{
		"protocol": "trojan",
		"settings": map[string]any{
			"address":  u.Hostname(),
			"port":     port,
			"password": password,
		},
		"streamSettings": stream,
	}, nil
}

func buildShadowsocks(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User == nil {
		return nil, errors.New("invalid shadowsocks link")
	}
	method, password := parseSSUserInfo(u.User)
	if method == "" || password == "" {
		return nil, errors.New("incomplete shadowsocks credentials")
	}
	port, err := portFromURL(u)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"protocol": "shadowsocks",
		"settings": map[string]any{
			"address":  u.Hostname(),
			"port":     port,
			"method":   method,
			"password": password,
		},
	}, nil
}

func buildStreamSettings(opts streamOptions) map[string]any {
	network := normalizeNetwork(opts.network)
	security := strings.ToLower(strings.TrimSpace(opts.security))
	if security == "" {
		security = "none"
	}

	stream := map[string]any{
		"method": network,
	}
	if security != "none" {
		stream["security"] = security
	}

	switch security {
	case "tls":
		tlsSettings := map[string]any{}
		if opts.serverName != "" {
			tlsSettings["serverName"] = opts.serverName
		}
		if opts.fingerprint != "" {
			tlsSettings["fingerprint"] = opts.fingerprint
		}
		if len(opts.alpn) > 0 {
			tlsSettings["alpn"] = opts.alpn
		}
		if len(tlsSettings) > 0 {
			stream["tlsSettings"] = tlsSettings
		}
	case "reality":
		realitySettings := map[string]any{}
		if opts.serverName != "" {
			realitySettings["serverName"] = opts.serverName
		}
		if opts.fingerprint != "" {
			realitySettings["fingerprint"] = opts.fingerprint
		}
		if opts.publicKey != "" {
			realitySettings["password"] = opts.publicKey
		}
		if opts.shortID != "" {
			realitySettings["shortId"] = opts.shortID
		}
		if opts.spiderX != "" {
			realitySettings["spiderX"] = opts.spiderX
		}
		if len(realitySettings) > 0 {
			stream["realitySettings"] = realitySettings
		}
	}

	switch network {
	case "websocket":
		wsSettings := map[string]any{}
		if opts.path != "" {
			wsSettings["path"] = opts.path
		}
		if opts.host != "" {
			wsSettings["host"] = opts.host
		}
		if len(wsSettings) > 0 {
			stream["wsSettings"] = wsSettings
		}
	case "grpc":
		grpcSettings := map[string]any{}
		if opts.serviceName != "" {
			grpcSettings["serviceName"] = opts.serviceName
		}
		if opts.host != "" {
			grpcSettings["authority"] = opts.host
		}
		if len(grpcSettings) > 0 {
			stream["grpcSettings"] = grpcSettings
		}
	case "httpupgrade":
		upgradeSettings := map[string]any{}
		if opts.path != "" {
			upgradeSettings["path"] = opts.path
		}
		if opts.host != "" {
			upgradeSettings["host"] = opts.host
		}
		if len(upgradeSettings) > 0 {
			stream["httpupgradeSettings"] = upgradeSettings
		}
	case "xhttp":
		xhttpSettings := map[string]any{}
		if opts.path != "" {
			xhttpSettings["path"] = opts.path
		}
		if opts.host != "" {
			xhttpSettings["host"] = opts.host
		}
		if len(xhttpSettings) > 0 {
			stream["xhttpSettings"] = xhttpSettings
		}
	}

	return stream
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
	if user == nil {
		return "", ""
	}
	raw := user.String()
	if decoded, ok := decodeBase64(raw); ok && strings.Contains(string(decoded), ":") {
		parts := strings.SplitN(string(decoded), ":", 2)
		return parts[0], parts[1]
	}
	method := user.Username()
	password, _ := user.Password()
	return method, password
}

func portFromURL(u *url.URL) (int, error) {
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 0, errors.New("invalid node port")
	}
	return port, nil
}

func decodeBase64(value string) ([]byte, bool) {
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
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
	value := valueString(data, key)
	number, err := strconv.Atoi(value)
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
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
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
