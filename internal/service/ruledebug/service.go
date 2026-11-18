// Package ruledebug implements an offline routing-rule debugger: given a
// destination (domain, IP, port, process or network) it replays the routing
// rules in the same order Xray evaluates them and reports which rule (if any)
// decides the outbound.
//
// Rules that depend on the GeoIP/GeoSite databases cannot be evaluated
// offline; the debugger reports them as unresolved candidates and keeps
// matching the rules that can be decided.
package ruledebug

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// serviceName is the Wails service name exposed to the frontend.
	serviceName = "RuleDebugService"

	// Rule sources reported in Result.Source.
	sourceUser    = "user"
	sourceRuleSet = "ruleset"
	sourceDefault = "default"

	// Actions and outbound tags used by the generated routing configuration.
	outboundProxy  = "proxy"
	outboundDirect = "direct"
	outboundBlock  = "block"

	// candidateStatusUnresolved marks a rule that needs the Geo databases at
	// runtime and therefore cannot be evaluated by the offline debugger.
	candidateStatusUnresolved = "unresolved"

	// Input kinds accepted by Debug.
	kindAuto    = "auto"
	kindDomain  = "domain"
	kindIP      = "ip"
	kindProcess = "process"
	kindPort    = "port"
	kindNetwork = "network"
)

// exeTokenPattern reports whether a value contains an executable suffix such
// as `C:\Tools\curl.exe` or `wechat.EXE --flag`. The suffix must not be
// followed by another name character so that host names like
// "example.exe.com" are not mistaken for process paths.
var exeTokenPattern = regexp.MustCompile(`(?i)\.exe($|[^a-z0-9._-])`)

// Service is the Wails service that answers "which rule decides this
// destination?" without starting Xray-core.
type Service struct {
	store      *store.Store
	extraRules func() []model.RoutingRule
}

// New creates the rule debugger. extraRules expands remote rule sets into
// routing rules and is evaluated on every Debug call; its rules are ordered
// after the user rules, mirroring the generated Xray configuration.
// extraRules may be nil. store may be nil too: the debugger then uses no user
// rules and the default "rule" proxy mode.
func New(st *store.Store, extraRules func() []model.RoutingRule) *Service {
	return &Service{store: st, extraRules: extraRules}
}

// ServiceName implements the Wails service naming convention.
func (s *Service) ServiceName() string { return serviceName }

// Result is the outcome of one debugging request.
type Result struct {
	// Matched reports whether an offline-decidable rule matched.
	Matched bool `json:"matched"`
	// RuleID, RuleName, RuleType and RuleValue identify the matched rule.
	// They stay empty when no rule matched.
	RuleID    string `json:"ruleId"`
	RuleName  string `json:"ruleName"`
	RuleType  string `json:"ruleType"`
	RuleValue string `json:"ruleValue"`
	// Action is the matched rule action (proxy, direct or block). It is empty
	// for the default outbound because no rule decided the traffic.
	Action string `json:"action"`
	// OutboundTag is the outbound the traffic would use: proxy, direct or
	// block.
	OutboundTag string `json:"outboundTag"`
	// Source is "user", "ruleset" or "default".
	Source string `json:"source"`
	// Reason is a human readable Chinese explanation of the decision.
	Reason string `json:"reason"`
	// Notes lists caveats such as GeoIP/GeoSite rules that cannot be decided
	// offline or rules with an invalid regex.
	Notes []string `json:"notes"`
	// Candidates lists rules that were skipped because they cannot be
	// evaluated offline (currently GeoIP/GeoSite rules). It is never nil.
	Candidates []Candidate `json:"candidates"`
}

// Candidate describes a rule that the offline debugger could not evaluate and
// therefore skipped while matching.
type Candidate struct {
	RuleID      string `json:"ruleId"`
	RuleName    string `json:"ruleName"`
	RuleType    string `json:"ruleType"`
	RuleValue   string `json:"ruleValue"`
	Action      string `json:"action"`
	OutboundTag string `json:"outboundTag"`
	// Source is "user" or "ruleset".
	Source string `json:"source"`
	// Status is "unresolved" for rules that need the Geo databases.
	Status string `json:"status"`
	// Reason explains why the rule was skipped.
	Reason string `json:"reason"`
}

// Debug evaluates one destination against the configured routing rules.
//
// kind is one of "auto", "domain", "ip", "process", "port" or "network"; an
// empty kind behaves like "auto". With "auto" the destination is classified by
// inferKind. The input is normalized before matching (URLs and ports are
// stripped from domains, CIDRs and IP:port pairs are accepted for IPs, only
// the basename is kept for processes) and invalid inputs return a Chinese
// validation error.
func (s *Service) Debug(input string, kind string) (Result, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = kindAuto
	}
	if kind == kindAuto {
		kind = inferKind(input)
	}
	switch kind {
	case kindDomain, kindIP, kindProcess, kindPort, kindNetwork:
	default:
		return Result{}, fmt.Errorf("不支持的匹配类型: %s（支持 auto、domain、ip、process、port、network）", kind)
	}
	target, err := normalizeTarget(input, kind)
	if err != nil {
		return Result{}, err
	}
	return s.evaluate(target), nil
}

// sourcedRule pairs a routing rule with the place it came from so matches can
// report Source "user" or "ruleset".
type sourcedRule struct {
	rule   model.RoutingRule
	source string
}

// sourcedRules returns the user rules in stored order followed by the rules
// expanded from rule sets, which is the order used when generating the Xray
// routing configuration.
func (s *Service) sourcedRules() []sourcedRule {
	rules := make([]sourcedRule, 0)
	if s.store != nil {
		for _, rule := range s.store.ListRoutingRules() {
			rules = append(rules, sourcedRule{rule: rule, source: sourceUser})
		}
	}
	if s.extraRules != nil {
		for _, rule := range s.extraRules() {
			rules = append(rules, sourcedRule{rule: rule, source: sourceRuleSet})
		}
	}
	return rules
}

// evaluate walks the rules in order. Disabled rules are skipped, GeoIP/GeoSite
// rules are recorded as unresolved candidates and do not count as a match, and
// the first decidable matching rule wins. Without a match the default outbound
// is derived from Settings.ProxyMode.
func (s *Service) evaluate(t target) Result {
	result := Result{
		Notes:      []string{},
		Candidates: []Candidate{},
	}

	geoRules := 0
	geoNoted := false
	for _, item := range s.sourcedRules() {
		rule := item.rule
		if !rule.Enabled {
			continue
		}
		ruleType := strings.ToLower(strings.TrimSpace(rule.Type))
		if ruleType == "geosite" || ruleType == "geoip" {
			geoRules++
			result.Candidates = append(result.Candidates, newGeoCandidate(rule, ruleType, item.source))
			if !geoNoted {
				geoNoted = true
				result.Notes = append(result.Notes, fmt.Sprintf(
					"路由顺序中存在 GeoIP/GeoSite 规则（如「%s」），离线调试无法判定是否命中；实际运行时由 Xray 结合 Geo 数据匹配，可能优先命中。",
					displayRuleName(rule, ruleType)))
			}
			continue
		}

		matched, warning := ruleMatches(t, rule, ruleType)
		if warning != "" {
			result.Notes = append(result.Notes, warning)
		}
		if !matched {
			continue
		}

		action := normalizeAction(rule.Action)
		result.Matched = true
		result.RuleID = rule.ID
		result.RuleName = displayRuleName(rule, ruleType)
		result.RuleType = ruleType
		result.RuleValue = strings.TrimSpace(rule.Value)
		result.Action = action
		result.OutboundTag = action
		result.Source = item.source
		result.Reason = fmt.Sprintf(
			"%s「%s」按 %s 规则（%s）匹配%s %s 成功，动作 %s，使用出站 %s。",
			sourceLabel(item.source), result.RuleName, ruleType, truncateRunes(rule.Value, 60),
			kindLabel(t.kind), t.display, action, action)
		if geoRules > 0 {
			result.Reason += fmt.Sprintf(
				" 注意：匹配顺序中更靠前的 %d 条 GeoIP/GeoSite 规则无法离线判定，实际运行时可能由其先行命中。", geoRules)
		}
		return result
	}

	mode, unrecognized := s.proxyMode()
	outbound := defaultOutbound(mode)
	result.OutboundTag = outbound
	result.Source = sourceDefault
	if geoRules > 0 {
		result.Reason = fmt.Sprintf(
			"未命中任何可离线判定的路由规则；匹配顺序中 %d 条 GeoIP/GeoSite 规则需要运行时由 Xray 判定，实际结果可能不同。当前代理模式为 %s，默认出站 %s。",
			geoRules, modeLabel(mode), outbound)
	} else {
		result.Reason = fmt.Sprintf(
			"未命中任何可离线判定的路由规则，按当前代理模式 %s 使用默认出站 %s。",
			modeLabel(mode), outbound)
	}
	if unrecognized != "" {
		result.Notes = append(result.Notes, fmt.Sprintf("无法识别的代理模式「%s」，已按 rule 模式处理。", unrecognized))
	}
	return result
}

// proxyMode returns the effective proxy mode and the raw unrecognized value (if
// any). An empty or missing mode falls back to "rule".
func (s *Service) proxyMode() (string, string) {
	raw := "rule"
	if s.store != nil {
		raw = strings.ToLower(strings.TrimSpace(s.store.Settings().ProxyMode))
	}
	if raw == "" {
		raw = "rule"
	}
	switch raw {
	case "rule", "global", "direct":
		return raw, ""
	default:
		return "rule", raw
	}
}

// defaultOutbound maps the proxy mode to the outbound used by unmatched
// traffic: rule/global send it to the proxy, direct connects directly.
func defaultOutbound(mode string) string {
	if mode == "direct" {
		return outboundDirect
	}
	return outboundProxy
}

func modeLabel(mode string) string {
	if mode == "" {
		return "rule"
	}
	return mode
}

func sourceLabel(source string) string {
	switch source {
	case sourceUser:
		return "用户规则"
	case sourceRuleSet:
		return "规则集规则"
	default:
		return "默认出站"
	}
}

func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case outboundProxy:
		return outboundProxy
	case outboundDirect:
		return outboundDirect
	case outboundBlock:
		return outboundBlock
	default:
		return outboundProxy
	}
}

// displayRuleName returns the stored name or a readable fallback derived from
// the type and value.
func displayRuleName(rule model.RoutingRule, ruleType string) string {
	if name := strings.TrimSpace(rule.Name); name != "" {
		return name
	}
	value := truncateRunes(rule.Value, 28)
	if value == "" {
		return ruleType
	}
	return ruleType + ": " + value
}

func newGeoCandidate(rule model.RoutingRule, ruleType string, source string) Candidate {
	action := normalizeAction(rule.Action)
	return Candidate{
		RuleID:      rule.ID,
		RuleName:    displayRuleName(rule, ruleType),
		RuleType:    ruleType,
		RuleValue:   strings.TrimSpace(rule.Value),
		Action:      action,
		OutboundTag: action,
		Source:      source,
		Status:      candidateStatusUnresolved,
		Reason:      "该规则依赖 GeoIP/GeoSite 数据，离线调试无法判定是否命中，需要由 Xray 在运行时匹配。",
	}
}

// target is the normalized destination used for matching.
type target struct {
	kind    string
	display string
	domain  string
	ip      ipQuery
	port    int
	process string
	network string
}

// ipQuery is either a single IP (network == nil) or a CIDR range.
type ipQuery struct {
	ip      net.IP
	network *net.IPNet
}

// normalizeTarget validates and canonicalizes the user input for one kind.
func normalizeTarget(input string, kind string) (target, error) {
	switch kind {
	case kindDomain:
		domain, err := normalizeDomain(input)
		if err != nil {
			return target{}, err
		}
		return target{kind: kindDomain, display: domain, domain: domain}, nil
	case kindIP:
		query, display, err := normalizeIP(input)
		if err != nil {
			return target{}, err
		}
		return target{kind: kindIP, display: display, ip: query}, nil
	case kindPort:
		port, err := normalizePort(input)
		if err != nil {
			return target{}, err
		}
		return target{kind: kindPort, display: strconv.Itoa(port), port: port}, nil
	case kindProcess:
		process, err := normalizeProcess(input)
		if err != nil {
			return target{}, err
		}
		return target{kind: kindProcess, display: process, process: process}, nil
	case kindNetwork:
		network, err := normalizeNetwork(input)
		if err != nil {
			return target{}, err
		}
		return target{kind: kindNetwork, display: network, network: network}, nil
	default:
		return target{}, fmt.Errorf("不支持的匹配类型: %s", kind)
	}
}

// inferKind classifies an "auto" input:
//   - URLs (scheme://...) are domain rules, like values with a path but no
//     path prefix (`www.example.com/login`);
//   - IP addresses, CIDR ranges and IP:port pairs are IP rules;
//   - executable names, filesystem paths and Windows paths are process rules;
//   - all-digit values are ports;
//   - everything else is treated as a domain.
func inferKind(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return kindDomain
	}
	if strings.Contains(value, "://") {
		return kindDomain
	}
	if _, _, err := net.ParseCIDR(value); err == nil {
		return kindIP
	}
	if net.ParseIP(value) != nil {
		return kindIP
	}
	if host, _, err := net.SplitHostPort(value); err == nil && net.ParseIP(host) != nil {
		return kindIP
	}
	if looksLikeProcess(value) {
		return kindProcess
	}
	if strings.Contains(value, "/") {
		return kindDomain
	}
	if isAllDigits(value) {
		return kindPort
	}
	return kindDomain
}

// looksLikeProcess reports whether a value is an executable name or a
// filesystem path (Windows backslash paths, Unix absolute/relative paths or
// anything carrying an .exe suffix).
func looksLikeProcess(value string) bool {
	if strings.Contains(value, `\`) {
		return true
	}
	if exeTokenPattern.MatchString(value) {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return true
	}
	return false
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeDomain strips the scheme, user info, path, query, fragment, port and
// trailing dot, then lowercases the remaining host name.
func normalizeDomain(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("域名不能为空")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || strings.TrimSpace(parsed.Host) == "" {
			return "", fmt.Errorf("域名无效: %s", value)
		}
		value = parsed.Host
	}
	if at := strings.IndexByte(value, '@'); at >= 0 {
		value = value[at+1:]
	}
	if separator := strings.IndexAny(value, "/?#"); separator >= 0 {
		value = value[:separator]
	}
	value = strings.TrimSpace(value)
	if strings.Contains(value, ":") {
		if host, _, err := net.SplitHostPort(value); err == nil {
			value = host
		}
	}
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ".")
	value = strings.ToLower(value)
	if value == "" {
		return "", errors.New("域名不能为空")
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", fmt.Errorf("域名不能包含空白字符: %s", strings.TrimSpace(input))
		}
	}
	if strings.ContainsAny(value, "/?#@:") {
		return "", fmt.Errorf("域名无效: %s", strings.TrimSpace(input))
	}
	if len(value) > 253 {
		return "", fmt.Errorf("域名过长: %s", strings.TrimSpace(input))
	}
	return value, nil
}

// normalizeIP accepts an IP address, a CIDR range or an IP:port pair and
// returns the parsed query plus its canonical display form.
func normalizeIP(input string) (ipQuery, string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return ipQuery{}, "", errors.New("IP 不能为空")
	}
	if host, _, err := net.SplitHostPort(value); err == nil && net.ParseIP(host) != nil {
		value = host
	}
	if strings.Contains(value, "/") {
		if _, network, err := net.ParseCIDR(value); err == nil {
			return ipQuery{ip: network.IP, network: network}, network.String(), nil
		}
		return ipQuery{}, "", fmt.Errorf("IP/CIDR 无效: %s", strings.TrimSpace(input))
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return ipQuery{}, "", fmt.Errorf("IP 地址无效: %s", strings.TrimSpace(input))
	}
	return ipQuery{ip: ip}, ip.String(), nil
}

func normalizePort(input string) (int, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return 0, errors.New("端口不能为空")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("端口无效，必须是 1-65535 的数字: %s", value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("端口超出范围，必须是 1-65535: %s", value)
	}
	return port, nil
}

// normalizeProcess strips surrounding quotes, keeps only the basename and
// lowercases it. Matching is case-insensitive because process names are.
func normalizeProcess(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("进程名不能为空")
	}
	value = strings.Trim(value, `"'`)
	value = strings.ReplaceAll(value, `\`, "/")
	base := strings.TrimSpace(path.Base(value))
	if base == "" || base == "." || base == ".." || base == "/" {
		return "", fmt.Errorf("进程名无效: %s", strings.TrimSpace(input))
	}
	for _, r := range base {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("进程名不能包含控制字符: %s", strings.TrimSpace(input))
		}
	}
	return strings.ToLower(base), nil
}

func normalizeNetwork(input string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return "", errors.New("网络类型不能为空")
	}
	if value != "tcp" && value != "udp" {
		return "", fmt.Errorf("网络类型只能是 tcp 或 udp: %s", strings.TrimSpace(input))
	}
	return value, nil
}

// ruleMatches reports whether one rule matches the target. The second return
// value carries a Chinese warning when the rule itself is malformed (for
// example an invalid regular expression); the rule is then treated as a
// non-match so later rules still get a chance.
func ruleMatches(t target, rule model.RoutingRule, ruleType string) (bool, string) {
	values := splitRuleValues(rule.Value)
	switch ruleType {
	case "domain_suffix":
		if t.kind != kindDomain {
			return false, ""
		}
		for _, raw := range values {
			suffix := strings.ToLower(strings.TrimSpace(raw))
			if suffix == "" {
				continue
			}
			if t.domain == suffix || strings.HasSuffix(t.domain, "."+suffix) {
				return true, ""
			}
		}
	case "domain_keyword":
		if t.kind != kindDomain {
			return false, ""
		}
		for _, raw := range values {
			keyword := strings.ToLower(strings.TrimSpace(raw))
			if keyword != "" && strings.Contains(t.domain, keyword) {
				return true, ""
			}
		}
	case "domain_regex":
		if t.kind != kindDomain {
			return false, ""
		}
		warning := ""
		for _, raw := range values {
			pattern := strings.TrimSpace(raw)
			if pattern == "" {
				continue
			}
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				warning = fmt.Sprintf("规则「%s」的正则表达式无效（%s），已跳过。", displayRuleName(rule, ruleType), pattern)
				continue
			}
			if compiled.MatchString(t.domain) {
				return true, ""
			}
		}
		return false, warning
	case "ip":
		if t.kind != kindIP {
			return false, ""
		}
		for _, raw := range values {
			if ipValueMatches(t.ip, raw) {
				return true, ""
			}
		}
	case "port":
		if t.kind != kindPort {
			return false, ""
		}
		for _, raw := range values {
			if portValueMatches(t.port, raw) {
				return true, ""
			}
		}
	case "process":
		if t.kind != kindProcess {
			return false, ""
		}
		for _, raw := range values {
			if name := strings.TrimSpace(raw); name != "" && strings.EqualFold(name, t.process) {
				return true, ""
			}
		}
	case "network":
		if t.kind != kindNetwork {
			return false, ""
		}
		for _, raw := range values {
			if protocol := strings.TrimSpace(raw); protocol != "" && strings.EqualFold(protocol, t.network) {
				return true, ""
			}
		}
	}
	return false, ""
}

// ipValueMatches compares one comma separated rule value against the queried
// IP or CIDR. Both directions are supported so debugging a CIDR range reports
// rules that overlap it.
func ipValueMatches(query ipQuery, raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return false
		}
		if query.network == nil {
			return network.Contains(query.ip)
		}
		return network.Contains(query.network.IP) || query.network.Contains(network.IP)
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	if query.network == nil {
		return query.ip.Equal(ip)
	}
	return query.network.Contains(ip)
}

// portValueMatches compares one comma separated rule value (a port or an
// inclusive "start-end" range) against the queried port.
func portValueMatches(port int, raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	if separator := strings.IndexByte(value, '-'); separator >= 0 {
		start, errStart := strconv.Atoi(strings.TrimSpace(value[:separator]))
		end, errEnd := strconv.Atoi(strings.TrimSpace(value[separator+1:]))
		if errStart != nil || errEnd != nil {
			return false
		}
		return start <= port && port <= end
	}
	expected, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	return expected == port
}

// splitRuleValues mirrors how the Xray configuration builder splits rule
// values: commas, newlines and carriage returns separate the entries.
func splitRuleValues(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
}

func kindLabel(kind string) string {
	switch kind {
	case kindDomain:
		return "域名"
	case kindIP:
		return "IP"
	case kindPort:
		return "端口"
	case kindProcess:
		return "进程"
	case kindNetwork:
		return "网络"
	default:
		return kind
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}
