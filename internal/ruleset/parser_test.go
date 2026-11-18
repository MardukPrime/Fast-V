package ruleset

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// collectValues flattens the comma-separated values of all rules of one type.
func collectValues(rules []model.RoutingRule, ruleType string) []string {
	values := make([]string, 0)
	for _, rule := range rules {
		if rule.Type != ruleType {
			continue
		}
		for _, value := range strings.Split(rule.Value, ",") {
			if value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestParseSupportedFormats(t *testing.T) {
	content := strings.Join([]string{
		"# full line comment",
		"// slash comment",
		"; semicolon comment",
		"",
		"plain.example",
		".wild.example",
		"*.star.example",
		"0.0.0.0 hosts.example blocked.hosts.example",
		":: ipv6-host.example",
		"||adblock.example^",
		"DOMAIN-SUFFIX,clash-suffix.example",
		"DOMAIN,clash-domain.example",
		"1.2.3.4",
		"10.0.0.0/8",
		"IP-CIDR,192.168.0.0/16,no-resolve",
		"IP-CIDR6,2001:db8::/32",
		"not a valid rule line",
		"another.example # inline comment",
	}, "\n")

	rules, err := Parse(content, KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	wantDomains := []string{
		"plain.example",
		"wild.example",
		"star.example",
		"hosts.example",
		"blocked.hosts.example",
		"ipv6-host.example",
		"adblock.example",
		"clash-suffix.example",
		"clash-domain.example",
		"another.example",
	}
	if got := collectValues(rules, "domain_suffix"); !equalStrings(got, wantDomains) {
		t.Fatalf("domain values = %v, want %v", got, wantDomains)
	}
	wantIPs := []string{"1.2.3.4", "10.0.0.0/8", "192.168.0.0/16", "2001:db8::/32"}
	if got := collectValues(rules, "ip"); !equalStrings(got, wantIPs) {
		t.Fatalf("ip values = %v, want %v", got, wantIPs)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2 merged rules, got %+v", len(rules), rules)
	}
	for _, rule := range rules {
		if rule.Action != defaultRuleAction {
			t.Fatalf("rule action = %q, want %q", rule.Action, defaultRuleAction)
		}
		if !rule.Enabled {
			t.Fatalf("rule %q must be enabled", rule.Name)
		}
		if rule.ID == "" {
			t.Fatalf("rule %q must have an ID", rule.Name)
		}
	}
	if rules[0].Name != domainRuleBaseName || rules[1].Name != ipRuleBaseName {
		t.Fatalf("unexpected default names: %q, %q", rules[0].Name, rules[1].Name)
	}
}

func TestParseAdblockVariants(t *testing.T) {
	content := "||blocked.example^$third-party\n@@||allowed.example^\nexample.org^\n"
	rules, err := Parse(content, KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := collectValues(rules, "domain_suffix")
	want := []string{"blocked.example", "example.org"}
	if !equalStrings(got, want) {
		t.Fatalf("domain values = %v, want %v", got, want)
	}
}

func TestParseKindFilter(t *testing.T) {
	content := "example.com\n1.2.3.4\n0.0.0.0 hosts.example\n"

	for _, test := range []struct {
		kind        string
		wantDomains []string
		wantIPs     []string
	}{
		{kind: KindDomain, wantDomains: []string{"example.com", "hosts.example"}},
		{kind: KindIP, wantIPs: []string{"1.2.3.4"}},
		{kind: KindCombined, wantDomains: []string{"example.com", "hosts.example"}, wantIPs: []string{"1.2.3.4"}},
		{kind: "", wantDomains: []string{"example.com", "hosts.example"}, wantIPs: []string{"1.2.3.4"}},
		{kind: "garbage", wantDomains: []string{"example.com", "hosts.example"}, wantIPs: []string{"1.2.3.4"}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			rules, err := Parse(content, test.kind)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got := collectValues(rules, "domain_suffix"); !equalStrings(got, test.wantDomains) {
				t.Fatalf("domain values = %v, want %v", got, test.wantDomains)
			}
			if got := collectValues(rules, "ip"); !equalStrings(got, test.wantIPs) {
				t.Fatalf("ip values = %v, want %v", got, test.wantIPs)
			}
		})
	}
}

func TestParseDeduplicates(t *testing.T) {
	content := "example.com\nexample.com\n*.example.com\nEXAMPLE.COM\n1.2.3.4\n1.2.3.4\n"
	rules, err := Parse(content, KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := collectValues(rules, "domain_suffix"); !equalStrings(got, []string{"example.com"}) {
		t.Fatalf("domain values = %v, want one deduplicated entry", got)
	}
	if got := collectValues(rules, "ip"); !equalStrings(got, []string{"1.2.3.4"}) {
		t.Fatalf("ip values = %v, want one deduplicated entry", got)
	}
}

func TestParseEmptyContentErrors(t *testing.T) {
	for name, content := range map[string]string{
		"empty":     "",
		"comments":  "# a\n// b\n; c\n\n",
		"invalid":   "not a domain\nhttp://bad.example/path\n",
		"filtered":  "1.2.3.4\n",
		"filtered2": "example.com\n",
	} {
		t.Run(name, func(t *testing.T) {
			kind := KindCombined
			switch name {
			case "filtered":
				kind = KindDomain
			case "filtered2":
				kind = KindIP
			}
			rules, err := Parse(content, kind)
			if err == nil || err.Error() != "规则集内容为空" {
				t.Fatalf("Parse() = (%v, %v), want nil rules and %q", rules, err, "规则集内容为空")
			}
		})
	}
}

func TestParseSkipsInvalidIPLines(t *testing.T) {
	content := "300.1.1.1\n10.0.0.0/33\n1.2.3.4-1.2.3.5\n0.0.0.0 999.1.1.1\n1.2.3.4\n"
	rules, err := Parse(content, KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := collectValues(rules, "ip"); !equalStrings(got, []string{"1.2.3.4"}) {
		t.Fatalf("ip values = %v, want only the valid address", got)
	}
	if got := collectValues(rules, "domain_suffix"); len(got) != 0 {
		t.Fatalf("invalid IP tokens leaked into domain rules: %v", got)
	}
}

func TestParseMergesValuesIntoFewRules(t *testing.T) {
	content := "a.example\nb.example\nc.example\n1.1.1.1\n2.2.2.2\n"
	rules, err := Parse(content, KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2 merged rules: %+v", len(rules), rules)
	}
	if rules[0].Type != "domain_suffix" || rules[0].Value != "a.example,b.example,c.example" {
		t.Fatalf("domain rule = %+v", rules[0])
	}
	if rules[1].Type != "ip" || rules[1].Value != "1.1.1.1,2.2.2.2" {
		t.Fatalf("ip rule = %+v", rules[1])
	}
}

func TestParseSplitsOversizeValues(t *testing.T) {
	const count = 20000
	var builder strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&builder, "very-long-subdomain-%05d.example.com\n", i)
	}
	rules, err := Parse(builder.String(), KindDomain)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(rules) < 2 {
		t.Fatalf("expected the oversized list to be split into several rules, got %d", len(rules))
	}
	values := collectValues(rules, "domain_suffix")
	if len(values) != count {
		t.Fatalf("value count = %d, want %d", len(values), count)
	}
	for i, rule := range rules {
		if len(rule.Value) > maxRuleValueBytes {
			t.Fatalf("rule %d value is %d bytes, cap is %d", i, len(rule.Value), maxRuleValueBytes)
		}
	}
	if values[0] != "very-long-subdomain-00000.example.com" || values[count-1] != "very-long-subdomain-19999.example.com" {
		t.Fatalf("split rules lost or reordered values: first=%q last=%q", values[0], values[count-1])
	}
}

func TestParseCapsEntryCount(t *testing.T) {
	var builder strings.Builder
	for i := 0; i < MaxParseLines+5; i++ {
		fmt.Fprintf(&builder, "entry-%06d.example.com\n", i)
	}
	rules, err := Parse(builder.String(), KindDomain)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := len(collectValues(rules, "domain_suffix")); got != MaxParseLines {
		t.Fatalf("value count = %d, want cap %d", got, MaxParseLines)
	}
}

func TestNameRules(t *testing.T) {
	rules, err := Parse("example.com\n1.2.3.4\n", KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	NameRules(rules, "广告")
	if rules[0].Name != "广告 域名" || rules[1].Name != "广告 IP" {
		t.Fatalf("unexpected names: %q, %q", rules[0].Name, rules[1].Name)
	}
	NameRules(rules, "  ")
	if rules[0].Name != "域名" || rules[1].Name != "IP" {
		t.Fatalf("unexpected base names: %q, %q", rules[0].Name, rules[1].Name)
	}
}

func TestCountValues(t *testing.T) {
	rules, err := Parse("a.example\nb.example\n1.2.3.4\n", KindCombined)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := CountValues(rules); got != 3 {
		t.Fatalf("CountValues() = %d, want 3", got)
	}
	if got := CountValues(nil); got != 0 {
		t.Fatalf("CountValues(nil) = %d, want 0", got)
	}
}
