package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
	"github.com/MardukPrime/Fast-V/internal/routing"
)

const (
	tunInterfaceName = "FastV-TUN"

	// proxyOutboundTag is the tag of the single-node proxy outbound. In load
	// balancing mode the same tag names the balancer, so routing rules and the
	// rest of the generated configuration keep a single "proxy" target.
	proxyOutboundTag = "proxy"
	// loadBalanceOutboundPrefix tags every per-node outbound created for the
	// balancer. The balancer selector and the observatory subject selector use
	// the prefix, so the tag of each node is loadBalanceOutboundPrefix + index.
	loadBalanceOutboundPrefix = "balancer-node-"
	// loadBalanceProbeInterval is the observatory probe period. Xray expects a
	// duration string here (for example "300s"), not a number of seconds.
	loadBalanceProbeInterval = "300s"
	// loadBalanceDefaultProbeURL is used when settings.TestURL is empty.
	loadBalanceDefaultProbeURL = "https://www.gstatic.com/generate_204"
	// loadBalanceDefaultStrategy is used when settings.LoadBalanceMode is empty
	// or not a supported strategy.
	loadBalanceDefaultStrategy = "leastPing"
)

// LoadBalanceStrategy canonicalizes the configured balancer strategy. Empty or
// unknown values fall back to leastPing so a hand-edited data file can never
// produce an invalid core configuration.
func LoadBalanceStrategy(mode string) string {
	trimmed := strings.TrimSpace(mode)
	switch trimmed {
	case "leastPing", "leastLoad", "roundRobin":
		return trimmed
	default:
		return loadBalanceDefaultStrategy
	}
}

// BuildConfig creates a complete Xray JSON configuration for the selected node and settings.
func BuildConfig(node model.Node, settings model.Settings, customRules ...model.RoutingRule) ([]byte, error) {
	if settings.CoreType != "xray" {
		return nil, errors.New("only Xray-core is currently supported")
	}

	outbound, err := buildOutbound(node, settings)
	if err != nil {
		return nil, err
	}

	apiEnabled := statsAPIEnabled(settings)
	outbounds := []any{outbound, buildDirectOutbound(), buildBlockOutbound()}
	routingConfig := buildRouting(settings.ProxyMode, customRules, apiEnabled, settings.XrayDomainStrategy)
	document := buildConfigDocument(settings, outbounds, routingConfig)
	if err := mergeConfigOverride(document, settings.ConfigOverride); err != nil {
		return nil, err
	}
	return json.MarshalIndent(document, "", "  ")
}

// BuildLoadBalanceConfig creates a complete Xray JSON configuration that routes
// every proxied connection through a balancer instead of a single node.
//
// One outbound is generated per node (tag loadBalanceOutboundPrefix + index,
// keeping the mux/sockopt/OutboundJSON overrides of the normal outbound path),
// the balancer itself is tagged "proxy" so the stored routing rules keep their
// proxy target, and an observatory probes the node outbounds for the leastPing
// and leastLoad strategies. Xray only resolves rule outboundTag values against
// real outbounds, therefore every routing rule that targets "proxy" is rewritten
// to the matching balancerTag.
func BuildLoadBalanceConfig(nodes []model.Node, settings model.Settings, customRules ...model.RoutingRule) ([]byte, error) {
	if settings.CoreType != "xray" {
		return nil, errors.New("only Xray-core is currently supported")
	}
	if len(nodes) < 2 {
		return nil, fmt.Errorf("负载均衡需要至少 2 个节点（当前 %d 个）", len(nodes))
	}

	outbounds := make([]any, 0, len(nodes)+2)
	for index, node := range nodes {
		outbound, err := buildOutbound(node, settings)
		if err != nil {
			return nil, fmt.Errorf("节点 %d 的 outbound 无效: %w", index+1, err)
		}
		outbound["tag"] = loadBalanceOutboundPrefix + strconv.Itoa(index)
		outbounds = append(outbounds, outbound)
	}
	outbounds = append(outbounds, buildDirectOutbound(), buildBlockOutbound())

	apiEnabled := statsAPIEnabled(settings)
	strategy := LoadBalanceStrategy(settings.LoadBalanceMode)
	routingConfig := buildLoadBalanceRouting(settings.ProxyMode, customRules, apiEnabled, settings.XrayDomainStrategy, strategy)
	document := buildConfigDocument(settings, outbounds, routingConfig)
	document["observatory"] = buildObservatory(settings)
	if err := mergeConfigOverride(document, settings.ConfigOverride); err != nil {
		return nil, err
	}
	return json.MarshalIndent(document, "", "  ")
}

// mergeConfigOverride deep-merges the user supplied JSON object override into a
// generated configuration document. Generated objects are merged recursively,
// arrays and scalars replace the generated value, and a null value removes the
// key so advanced users can drop generated fields. An empty override is a
// no-op. Only the top level of the override has to be a JSON object; the
// settings service validates the same shape before saving.
func mergeConfigOverride(document map[string]any, override string) error {
	trimmed := strings.TrimSpace(override)
	if trimmed == "" {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return fmt.Errorf("invalid config override JSON: %w", err)
	}
	if parsed == nil {
		return errors.New("config override must be a JSON object")
	}
	mergeConfigDocument(document, parsed)
	return nil
}

// mergeConfigDocument recursively applies an already parsed override object to
// a configuration document (or nested object) following the semantics of
// mergeConfigOverride: objects merge, arrays and scalars replace, null deletes.
func mergeConfigDocument(target map[string]any, override map[string]any) {
	for key, value := range override {
		if value == nil {
			delete(target, key)
			continue
		}
		overrideObject, isObject := value.(map[string]any)
		if !isObject {
			target[key] = value
			continue
		}
		targetObject, isObject := target[key].(map[string]any)
		if !isObject {
			target[key] = overrideObject
			continue
		}
		mergeConfigDocument(targetObject, overrideObject)
	}
}

// buildConfigDocument assembles the shared parts of the Xray configuration. The
// outbounds and the routing block differ between the single-node and the load
// balancing configuration and are passed in.
func buildConfigDocument(settings model.Settings, outbounds []any, routingConfig map[string]any) map[string]any {
	// The stats API is required both for the optional management API and for the
	// live traffic dashboard; either feature enables the same Xray API config.
	apiEnabled := statsAPIEnabled(settings)

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
		"listen":   localInboundListen(settings),
		"port":     settings.XrayLocalPort,
		"protocol": "socks",
		"settings": buildSocksInboundSettings(settings),
		"sniffing": map[string]any{
			"enabled":      settings.XraySniffing,
			"destOverride": []string{"http", "tls", "quic"},
		},
	})
	if apiEnabled {
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
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   routingConfig,
		"dns": map[string]any{
			"servers":       splitList(settings.XrayDNSServers),
			"queryStrategy": settings.XrayDNSStrategy,
		},
	}
	if apiEnabled {
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

	return config
}

// statsAPIEnabled reports whether the Xray API/stats configuration is needed.
func statsAPIEnabled(settings model.Settings) bool {
	return settings.XrayAPIEnabled || settings.StatsEnabled
}

// buildDirectOutbound and buildBlockOutbound return the fixed outbounds shared by
// both configuration shapes.
func buildDirectOutbound() map[string]any {
	return map[string]any{"tag": "direct", "protocol": "freedom"}
}

func buildBlockOutbound() map[string]any {
	return map[string]any{"tag": "block", "protocol": "blackhole"}
}

// buildObservatory builds the Xray observatory that feeds the leastPing and
// leastLoad balancer strategies. roundRobin does not need it but tolerates it,
// and the probe results keep the configuration ready for future strategies.
func buildObservatory(settings model.Settings) map[string]any {
	probeURL := strings.TrimSpace(settings.TestURL)
	if probeURL == "" {
		probeURL = loadBalanceDefaultProbeURL
	}
	return map[string]any{
		"subjectSelector":   []string{loadBalanceOutboundPrefix},
		"probeURL":          probeURL,
		"probeInterval":     loadBalanceProbeInterval,
		"enableConcurrency": true,
	}
}

// localInboundListen returns the listen address of the local SOCKS inbound.
// AllowLan exposes the inbound to the LAN; the TUN inbound is not affected.
func localInboundListen(settings model.Settings) string {
	if settings.AllowLan {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// buildSocksInboundSettings configures the local SOCKS inbound. When inbound
// authentication is enabled the authentication method switches to password and
// the configured account is added; otherwise the inbound stays on noauth.
func buildSocksInboundSettings(settings model.Settings) map[string]any {
	inbound := map[string]any{
		"auth": "noauth",
		"udp":  true,
	}
	if settings.InboundAuthEnabled {
		inbound["auth"] = "password"
		inbound["accounts"] = []any{map[string]any{
			"user": settings.InboundUsername,
			"pass": settings.InboundPassword,
		}}
	}
	return inbound
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
	outbound["tag"] = proxyOutboundTag
	applyCdnEndpoint(outbound, node)
	if settings.XrayMuxEnabled {
		outbound["mux"] = map[string]any{
			"enabled":     true,
			"concurrency": settings.XrayMuxConcurrency,
		}
	}
	if err := applyStreamOverrides(outbound, settings); err != nil {
		return nil, err
	}
	return outbound, nil
}

// applyCdnEndpoint points the outbound at the CDN edge the user picked. The
// node's own address stays in the record because the Host/SNI still has to be
// the domain the edge routes on — only the destination IP changes.
func applyCdnEndpoint(outbound map[string]any, node model.Node) {
	ip := strings.TrimSpace(node.CdnIP)
	if ip == "" {
		return
	}
	settings, _ := outbound["settings"].(map[string]any)
	if settings == nil {
		return
	}
	settings["address"] = ip
}

// buildRouting builds the routing block for the single-node configuration.
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

// buildLoadBalanceRouting builds the routing block for load balancing. Every rule
// that targets the single-node proxy tag is redirected to the balancer tag of the
// same name: Xray resolves rule outboundTag values only against real outbounds,
// so leaving the rules untouched would produce "non existing outTag: proxy".
func buildLoadBalanceRouting(proxyMode string, customRules []model.RoutingRule, apiEnabled bool, domainStrategy string, strategy string) map[string]any {
	routingConfig := buildRouting(proxyMode, customRules, apiEnabled, domainStrategy)
	rules, _ := routingConfig["rules"].([]any)
	for _, entry := range rules {
		rule, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if rule["outboundTag"] != proxyOutboundTag {
			continue
		}
		delete(rule, "outboundTag")
		rule["balancerTag"] = proxyOutboundTag
	}
	routingConfig["balancers"] = []any{
		map[string]any{
			"tag":      proxyOutboundTag,
			"selector": []string{loadBalanceOutboundPrefix},
			"strategy": map[string]any{"type": strategy},
		},
	}
	return routingConfig
}

// Fragment masking defaults. The mask splits the opening TCP segments so that
// SNI-based blocking and simple DPI cannot match a stable byte pattern; the
// values mirror the ranges Xray itself documents.
const (
	defaultFragmentPackets = "tlshello"
	defaultFragmentLength  = "100-200"
	defaultFragmentDelay   = "10-20"
)

// fragmentMask builds the streamSettings.finalmask block for the TLS splitter.
// Invalid or empty ranges fall back to the defaults instead of handing Xray a
// value it cannot parse, which would keep the core from starting at all.
func fragmentMask(settings model.Settings) map[string]any {
	if !settings.XrayFragmentEnabled {
		return nil
	}
	packets := strings.TrimSpace(settings.XrayFragmentPackets)
	if packets == "" {
		packets = defaultFragmentPackets
	}
	fragment := map[string]any{
		"packets": packets,
		"length":  normalizedRange(settings.XrayFragmentLength, 1, 65535, defaultFragmentLength),
	}
	if delay := normalizedRange(settings.XrayFragmentDelay, 0, 1000, ""); delay != "" {
		fragment["delay"] = delay
	}
	return map[string]any{
		"tcp": []any{map[string]any{"type": "fragment", "settings": fragment}},
	}
}

// normalizedRange returns value when it is a range Xray can parse, and fallback
// otherwise.
func normalizedRange(value string, min, max int, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if _, _, ok := model.ParseIntRange(value, min, max); !ok {
		return fallback
	}
	return value
}

// applyTLSFingerprintDefault fills in the uTLS fingerprint for TLS/REALITY nodes
// that do not carry one of their own. A node-level fingerprint always wins: the
// setting is a default for links that do not specify one, not an override.
func applyTLSFingerprintDefault(stream map[string]any, settings model.Settings) {
	fingerprint := strings.TrimSpace(settings.XrayTLSFingerprint)
	if fingerprint == "" {
		return
	}
	var key string
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(stream["security"]))) {
	case "tls":
		key = "tlsSettings"
	case "reality":
		key = "realitySettings"
	default:
		return
	}
	section, _ := stream[key].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	if existing, ok := section["fingerprint"].(string); ok && strings.TrimSpace(existing) != "" {
		return
	}
	section["fingerprint"] = fingerprint
	stream[key] = section
}

func applyStreamOverrides(outbound map[string]any, settings model.Settings) error {
	stream, _ := outbound["streamSettings"].(map[string]any)
	if stream == nil {
		stream = map[string]any{}
	}
	sockopt := buildSockopt(settings)
	if len(sockopt) > 0 {
		stream["sockopt"] = sockopt
	}
	finalmask := fragmentMask(settings)
	if settings.XrayFinalMaskJSON != "" {
		var advanced map[string]any
		if err := json.Unmarshal([]byte(settings.XrayFinalMaskJSON), &advanced); err != nil {
			return fmt.Errorf("invalid finalmask JSON: %w", err)
		}
		if advanced == nil {
			return errors.New("finalmask JSON must be an object")
		}
		if finalmask == nil {
			finalmask = advanced
		} else {
			// The hand-written mask is the escape hatch, so it wins over the
			// generated one field by field.
			mergeConfigDocument(finalmask, advanced)
		}
	}
	if len(finalmask) > 0 {
		stream["finalmask"] = finalmask
	}
	applyTLSFingerprintDefault(stream, settings)
	if len(stream) > 0 {
		outbound["streamSettings"] = stream
	}
	return nil
}

func buildSockopt(settings model.Settings) map[string]any {
	sockopt := map[string]any{}
	if settings.XraySockoptTFO {
		sockopt["tcpFastOpen"] = true
	}
	if settings.XraySockoptMPTCP {
		sockopt["tcpMptcp"] = true
	}
	if settings.XraySockoptKeepIdle > 0 {
		sockopt["tcpKeepAliveIdle"] = settings.XraySockoptKeepIdle
	}
	if settings.XraySockoptKeepIntvl > 0 {
		sockopt["tcpKeepAliveInterval"] = settings.XraySockoptKeepIntvl
	}
	if settings.XraySockoptUserTimeout > 0 {
		sockopt["tcpUserTimeout"] = settings.XraySockoptUserTimeout
	}
	if settings.XraySockoptCongestion != "" {
		sockopt["tcpCongestion"] = settings.XraySockoptCongestion
	}
	if settings.XraySockoptDomainStrategy != "" {
		sockopt["domainStrategy"] = settings.XraySockoptDomainStrategy
	}
	if settings.XraySockoptJSON != "" {
		var advanced map[string]any
		if json.Unmarshal([]byte(settings.XraySockoptJSON), &advanced) == nil {
			for key, value := range advanced {
				sockopt[key] = value
			}
		}
	}
	return sockopt
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
