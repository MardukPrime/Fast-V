// Package ruleset parses remote domain/IP rule set lists into routing rules and
// expands them when the Xray configuration is generated.
package ruleset

import (
	"errors"
	"net"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
)

const (
	// KindDomain, KindIP and KindCombined are the supported rule set kinds.
	KindDomain   = "domain"
	KindIP       = "ip"
	KindCombined = "combined"

	// MaxParseLines caps how many unique entries a single rule set may
	// contribute. Extra lines are ignored so an unreasonably large endpoint
	// cannot exhaust memory or produce an unusable configuration.
	MaxParseLines = 100_000

	// maxRuleValueBytes is the largest comma-joined value a single generated
	// routing rule may carry. Longer rule sets are split across several rules.
	maxRuleValueBytes = 150 * 1024

	// defaultRuleAction is a placeholder assigned by the parser. The rule set
	// service replaces it with the action configured for the rule set.
	defaultRuleAction = "proxy"

	// domainRuleBaseName and ipRuleBaseName are the rule names used before the
	// caller supplies the rule set name (see NameRules).
	domainRuleBaseName = "域名"
	ipRuleBaseName     = "IP"
)

// ruleCandidate is one parsed entry of a list line.
type ruleCandidate struct {
	value string
	isIP  bool
}

// valueSet keeps deduplicated values in first-seen order.
type valueSet struct {
	seen map[string]struct{}
	list []string
}

func newValueSet() *valueSet {
	return &valueSet{seen: make(map[string]struct{})}
}

func (s *valueSet) add(value string) {
	if _, ok := s.seen[value]; ok {
		return
	}
	s.seen[value] = struct{}{}
	s.list = append(s.list, value)
}

func (s *valueSet) len() int { return len(s.list) }

// Parse converts raw rule set content into merged routing rules.
//
// Supported line formats:
//   - plain domains ("example.com"), wildcards (".example.com", "*.example.com");
//   - hosts blocklist entries ("0.0.0.0 example.com", ":: example.com");
//   - Adblock domain rules ("||example.com^");
//   - Clash/Mihomo prefixes ("DOMAIN-SUFFIX,example.com", "DOMAIN,example.com",
//     "IP-CIDR,1.2.3.0/24[,no-resolve]");
//   - plain IP addresses and CIDR ranges.
//
// Lines that are not a supported domain or IP are skipped silently. Domains are
// merged into domain_suffix rules and IPs into ip rules; when the joined value
// would exceed maxRuleValueBytes the type is split across several rules.
//
// The kind filters the accepted entries: "domain" drops IP entries, "ip" drops
// domain entries, anything else keeps both. The returned rules carry the
// placeholder action "proxy" and Enabled=true; the rule set service applies the
// configured action and name. A list whose entries are all filtered out
// returns the error "规则集内容为空".
func Parse(content string, kind string) ([]model.RoutingRule, error) {
	kind = normalizeKind(kind)
	domains := newValueSet()
	ips := newValueSet()

	for _, rawLine := range strings.Split(content, "\n") {
		line := cleanLine(rawLine)
		if line == "" {
			continue
		}
		for _, candidate := range parseLine(line) {
			if candidate.isIP {
				if kind != KindDomain {
					ips.add(candidate.value)
				}
				continue
			}
			if kind != KindIP {
				domains.add(candidate.value)
			}
		}
		if domains.len()+ips.len() >= MaxParseLines {
			break
		}
	}
	if domains.len() == 0 && ips.len() == 0 {
		return nil, errors.New("规则集内容为空")
	}

	rules := make([]model.RoutingRule, 0, 2)
	rules = append(rules, mergeRules("domain_suffix", domainRuleBaseName, domains.list)...)
	rules = append(rules, mergeRules("ip", ipRuleBaseName, ips.list)...)
	return rules, nil
}

// NameRules assigns display names such as "广告 域名" / "广告 IP" to parsed
// rules. The parser only knows the rule types; the rule set service supplies
// the configured name of the rule set.
func NameRules(rules []model.RoutingRule, setName string) {
	name := strings.TrimSpace(setName)
	for i := range rules {
		base := domainRuleBaseName
		if rules[i].Type == "ip" {
			base = ipRuleBaseName
		}
		if name == "" {
			rules[i].Name = base
			continue
		}
		rules[i].Name = name + " " + base
	}
}

// CountValues returns how many individual entries the parsed rules carry.
// Merging keeps one routing rule per type even for huge lists, so the rule set
// status shown to the user counts entries rather than rules.
func CountValues(rules []model.RoutingRule) int {
	count := 0
	for _, rule := range rules {
		for _, value := range strings.Split(rule.Value, ",") {
			if strings.TrimSpace(value) != "" {
				count++
			}
		}
	}
	return count
}

func normalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindDomain:
		return KindDomain
	case KindIP:
		return KindIP
	default:
		return KindCombined
	}
}

// cleanLine trims a raw line and strips comments. Full-line comments are
// dropped and a trailing " #" or "\t#" inline comment is removed.
func cleanLine(rawLine string) string {
	line := strings.TrimSpace(strings.TrimRight(rawLine, "\r"))
	if line == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(line, "#"),
		strings.HasPrefix(line, "//"),
		strings.HasPrefix(line, ";"),
		strings.HasPrefix(line, "!"):
		return ""
	}
	if index := inlineCommentIndex(line); index >= 0 {
		line = strings.TrimSpace(line[:index])
	}
	return line
}

// inlineCommentIndex returns the position of a '#' that starts an inline
// comment (preceded by whitespace), or -1 when the line has none.
func inlineCommentIndex(line string) int {
	for i := 1; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		if line[i-1] == ' ' || line[i-1] == '\t' {
			return i
		}
	}
	return -1
}

// parseLine extracts every supported entry of one cleaned line.
func parseLine(line string) []ruleCandidate {
	// Clash / Mihomo style "TYPE,value[,options]" rule. Only the value field is
	// used; trailing options such as "no-resolve" are ignored.
	if parts := strings.SplitN(line, ",", 3); len(parts) >= 2 {
		switch strings.ToUpper(strings.TrimSpace(parts[0])) {
		case "DOMAIN", "DOMAIN-SUFFIX":
			if domain, ok := normalizeDomain(parts[1]); ok {
				return []ruleCandidate{{value: domain}}
			}
			return nil
		case "IP-CIDR", "IP-CIDR6", "IP":
			if ip, ok := normalizeIP(parts[1]); ok {
				return []ruleCandidate{{value: ip, isIP: true}}
			}
			return nil
		}
		// An unrecognised "X,y" line is not one of the supported formats; fall
		// through so a plain domain list containing a comma is still rejected
		// by its invalid characters.
	}

	fields := strings.Fields(line)
	// hosts blocklist entries: "0.0.0.0 example.com www.example.com".
	if len(fields) > 1 && net.ParseIP(fields[0]) != nil {
		out := make([]ruleCandidate, 0, len(fields)-1)
		for _, host := range fields[1:] {
			if domain, ok := normalizeDomain(host); ok {
				out = append(out, ruleCandidate{value: domain})
			}
		}
		return out
	}
	// Every other supported format is one entry per line.
	if len(fields) != 1 {
		return nil
	}

	token := fields[0]
	if strings.HasPrefix(token, "@@") {
		// Adblock exception rules allow traffic; rule sets only collect
		// blocked/redirected entries, so they are ignored.
		return nil
	}
	if strings.HasPrefix(token, "||") {
		token = strings.TrimPrefix(token, "||")
	} else if strings.HasPrefix(token, "|") {
		// Anchored URL filters ("|https://...") are not domain entries.
		return nil
	}
	// Adblock separates the domain from modifiers with '^'.
	if cut := strings.IndexByte(token, '^'); cut >= 0 {
		token = token[:cut]
	}

	if ip, ok := normalizeIP(token); ok {
		return []ruleCandidate{{value: ip, isIP: true}}
	}
	if domain, ok := normalizeDomain(token); ok {
		return []ruleCandidate{{value: domain}}
	}
	return nil
}

// normalizeDomain canonicalizes a domain token to a lowercase suffix and
// reports whether it looks like a usable domain.
func normalizeDomain(token string) (string, bool) {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'")
	token = strings.ToLower(token)
	token = strings.TrimLeft(token, ".*")
	token = strings.TrimRight(token, ".")
	if token == "" || !strings.Contains(token, ".") {
		return "", false
	}
	if strings.ContainsAny(token, " \t/\\:*|^$#?&=;@!+%()[]{}<>`,") {
		return "", false
	}
	// A real domain carries at least one letter, which rejects malformed
	// numeric tokens such as an invalid IP address ("300.1.1.1").
	hasLetter := false
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || r >= 0x80 {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return "", false
	}
	for _, label := range strings.Split(token, ".") {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", false
		}
		if !validDomainLabel(label) {
			return "", false
		}
	}
	return token, true
}

func validDomainLabel(label string) bool {
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r >= 0x80:
		default:
			return false
		}
	}
	return true
}

// normalizeIP canonicalizes an IP address or CIDR range and reports whether the
// token is a valid one.
func normalizeIP(token string) (string, bool) {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'")
	if ip := net.ParseIP(token); ip != nil {
		return ip.String(), true
	}
	if _, network, err := net.ParseCIDR(token); err == nil {
		return network.String(), true
	}
	return "", false
}

// mergeRules turns the collected values of one rule type into the minimum
// number of routing rules, splitting the joined value when it grows past
// maxRuleValueBytes.
func mergeRules(ruleType, baseName string, values []string) []model.RoutingRule {
	if len(values) == 0 {
		return nil
	}
	chunks := chunkValues(values, maxRuleValueBytes)
	rules := make([]model.RoutingRule, 0, len(chunks))
	for _, chunk := range chunks {
		rules = append(rules, model.RoutingRule{
			ID:      model.NewID(),
			Name:    baseName,
			Type:    ruleType,
			Value:   chunk,
			Action:  defaultRuleAction,
			Enabled: true,
		})
	}
	return rules
}

// chunkValues joins values with commas into chunks of at most maxBytes. A
// single oversized value still produces its own chunk; individual list entries
// are far below the cap in practice.
func chunkValues(values []string, maxBytes int) []string {
	chunks := make([]string, 0, len(values)/64+1)
	var current strings.Builder
	currentLen := 0
	for _, value := range values {
		extra := len(value)
		if currentLen > 0 {
			extra++
		}
		if currentLen > 0 && currentLen+extra > maxBytes {
			chunks = append(chunks, current.String())
			current.Reset()
			currentLen = 0
			extra = len(value)
		}
		if currentLen > 0 {
			current.WriteByte(',')
		}
		current.WriteString(value)
		currentLen += extra
	}
	if currentLen > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
