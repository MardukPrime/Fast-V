package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type ParsedNode struct {
	Name     string
	Protocol string
	Address  string
	Port     int
	Raw      string
}

func ParseContent(content string) ([]ParsedNode, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("subscription content is empty")
	}

	normalized := decodeSubscriptionBase64(content)
	lines := strings.Split(strings.ReplaceAll(normalized, "\r\n", "\n"), "\n")
	seen := make(map[string]struct{})
	nodes := make([]ParsedNode, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		var node ParsedNode
		var err error
		switch {
		case strings.HasPrefix(line, "vmess://"):
			node, err = parseVMessLink(line)
		case strings.HasPrefix(line, "vless://"), strings.HasPrefix(line, "trojan://"):
			node, err = parseURILink(line)
		case strings.HasPrefix(line, "ss://"):
			node, err = parseShadowsocksLink(line)
		default:
			continue
		}
		if err != nil || node.Address == "" || node.Port <= 0 {
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
		return nil, errors.New("no supported nodes found in subscription")
	}
	return nodes, nil
}

func decodeSubscriptionBase64(content string) string {
	if strings.Contains(content, "://") {
		return content
	}

	compact := strings.Join(strings.Fields(content), "")
	if decoded, ok := decodeBase64(compact); ok {
		text := string(decoded)
		if strings.Contains(text, "://") {
			return text
		}
	}
	return content
}

func decodeBase64(value string) ([]byte, bool) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

func parseVMessLink(raw string) (ParsedNode, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	decoded, ok := decodeBase64(payload)
	if !ok {
		return ParsedNode{}, errors.New("invalid vmess payload")
	}

	var data map[string]any
	if err := json.Unmarshal(decoded, &data); err != nil {
		return ParsedNode{}, err
	}

	address := getString(data, "add")
	port := getInt(data, "port")
	name := getString(data, "ps")
	if name == "" {
		name = fallbackNodeName("vmess", address, port)
	}
	if address == "" || port <= 0 {
		return ParsedNode{}, errors.New("invalid vmess endpoint")
	}

	return ParsedNode{
		Name:     name,
		Protocol: "vmess",
		Address:  address,
		Port:     port,
		Raw:      raw,
	}, nil
}

func parseURILink(raw string) (ParsedNode, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ParsedNode{}, err
	}
	protocol := strings.ToLower(u.Scheme)
	if protocol != "vless" && protocol != "trojan" {
		return ParsedNode{}, errors.New("unsupported protocol")
	}
	if u.Hostname() == "" {
		return ParsedNode{}, errors.New("missing host")
	}

	port, err := parsePort(u.Port())
	if err != nil {
		return ParsedNode{}, err
	}

	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = fallbackNodeName(protocol, u.Hostname(), port)
	}

	return ParsedNode{
		Name:     name,
		Protocol: protocol,
		Address:  u.Hostname(),
		Port:     port,
		Raw:      raw,
	}, nil
}

func parseShadowsocksLink(raw string) (ParsedNode, error) {
	payload := strings.TrimPrefix(raw, "ss://")
	name := ""
	if idx := strings.Index(payload, "#"); idx >= 0 {
		name = payload[idx+1:]
		payload = payload[:idx]
	}
	if decodedName, err := url.QueryUnescape(name); err == nil {
		name = decodedName
	}

	if u, err := url.Parse("ss://" + payload); err == nil && u.Hostname() != "" {
		port, portErr := parsePort(u.Port())
		if portErr == nil {
			if name == "" {
				name = fallbackNodeName("shadowsocks", u.Hostname(), port)
			}
			return ParsedNode{
				Name:     name,
				Protocol: "shadowsocks",
				Address:  u.Hostname(),
				Port:     port,
				Raw:      raw,
			}, nil
		}
	}

	decoded, ok := decodeBase64(payload)
	if !ok {
		return ParsedNode{}, errors.New("invalid shadowsocks payload")
	}
	u, err := url.Parse("ss://" + string(decoded))
	if err != nil || u.Hostname() == "" {
		return ParsedNode{}, errors.New("invalid shadowsocks endpoint")
	}
	port, err := parsePort(u.Port())
	if err != nil {
		return ParsedNode{}, err
	}
	if name == "" {
		name = fallbackNodeName("shadowsocks", u.Hostname(), port)
	}
	return ParsedNode{
		Name:     name,
		Protocol: "shadowsocks",
		Address:  u.Hostname(),
		Port:     port,
		Raw:      raw,
	}, nil
}

func parsePort(value string) (int, error) {
	if value == "" {
		return 0, errors.New("missing port")
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, errors.New("invalid port")
	}
	return port, nil
}

func getString(data map[string]any, key string) string {
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

func getInt(data map[string]any, key string) int {
	value := getString(data, key)
	if value == "" {
		return 0
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func fallbackNodeName(protocol, address string, port int) string {
	if address == "" {
		return protocol
	}
	return fmt.Sprintf("%s %s:%d", protocol, address, port)
}
