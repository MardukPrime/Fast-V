package ruledebug

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newTestService(t *testing.T, extra func() []model.RoutingRule) (*Service, *store.Store) {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return New(st, extra), st
}

func setUserRules(t *testing.T, st *store.Store, rules ...model.RoutingRule) {
	t.Helper()
	if err := st.ReplaceRoutingRules(rules); err != nil {
		t.Fatalf("replace routing rules: %v", err)
	}
}

func setProxyMode(t *testing.T, st *store.Store, mode string) {
	t.Helper()
	settings := st.Settings()
	settings.ProxyMode = mode
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
}

func newRule(id, name, ruleType, value, action string, enabled bool) model.RoutingRule {
	return model.RoutingRule{
		ID:      id,
		Name:    name,
		Type:    ruleType,
		Value:   value,
		Action:  action,
		Enabled: enabled,
	}
}

func requireMatchedRule(t *testing.T, result Result, want model.RoutingRule, wantSource string) {
	t.Helper()
	if !result.Matched {
		t.Fatalf("expected rule %q to match, got %+v", want.ID, result)
	}
	if result.RuleID != want.ID {
		t.Errorf("RuleID = %q, want %q", result.RuleID, want.ID)
	}
	if result.RuleName != want.Name {
		t.Errorf("RuleName = %q, want %q", result.RuleName, want.Name)
	}
	if result.RuleType != want.Type {
		t.Errorf("RuleType = %q, want %q", result.RuleType, want.Type)
	}
	if result.RuleValue != strings.TrimSpace(want.Value) {
		t.Errorf("RuleValue = %q, want %q", result.RuleValue, strings.TrimSpace(want.Value))
	}
	if result.Action != want.Action {
		t.Errorf("Action = %q, want %q", result.Action, want.Action)
	}
	if result.OutboundTag != want.Action {
		t.Errorf("OutboundTag = %q, want %q", result.OutboundTag, want.Action)
	}
	if result.Source != wantSource {
		t.Errorf("Source = %q, want %q", result.Source, wantSource)
	}
	if result.Reason == "" {
		t.Error("Reason is empty")
	}
	if result.Notes == nil {
		t.Error("Notes is nil, want an empty slice")
	}
	if result.Candidates == nil {
		t.Error("Candidates is nil, want an empty slice")
	}
}

func requireMatch(t *testing.T, result Result, want model.RoutingRule) {
	t.Helper()
	requireMatchedRule(t, result, want, sourceUser)
}

func requireNoMatch(t *testing.T, result Result, wantOutbound string) {
	t.Helper()
	if result.Matched {
		t.Fatalf("expected no match, got rule %q", result.RuleID)
	}
	if result.RuleID != "" || result.RuleType != "" || result.Action != "" {
		t.Errorf("default result carries rule fields: %+v", result)
	}
	if result.Source != sourceDefault {
		t.Errorf("Source = %q, want %q", result.Source, sourceDefault)
	}
	if result.OutboundTag != wantOutbound {
		t.Errorf("OutboundTag = %q, want %q", result.OutboundTag, wantOutbound)
	}
	if result.Reason == "" {
		t.Error("Reason is empty")
	}
	if result.Notes == nil {
		t.Error("Notes is nil, want an empty slice")
	}
	if result.Candidates == nil {
		t.Error("Candidates is nil, want an empty slice")
	}
}

func TestServiceName(t *testing.T) {
	service := New(nil, nil)
	if got, want := service.ServiceName(), "RuleDebugService"; got != want {
		t.Fatalf("ServiceName() = %q, want %q", got, want)
	}
}

func TestDebugDomainRuleTypes(t *testing.T) {
	cases := []struct {
		name      string
		ruleType  string
		ruleValue string
		input     string
		wantMatch bool
	}{
		{"suffix matches subdomain", "domain_suffix", "example.com", "www.example.com", true},
		{"suffix matches exact domain", "domain_suffix", "example.com", "example.com", true},
		{"suffix does not match sibling label", "domain_suffix", "example.com", "notexample.com", false},
		{"suffix does not match other domain", "domain_suffix", "example.com", "example.org", false},
		{"suffix matches any comma value", "domain_suffix", "google.com,youtube.com", "m.youtube.com", true},
		{"suffix matches uppercase input", "domain_suffix", "EXAMPLE.com", "WWW.Example.COM", true},
		{"keyword matches substring", "domain_keyword", "ads", "ads.example.com", true},
		{"keyword misses", "domain_keyword", "ads", "example.com", false},
		{"regex matches", "domain_regex", `^ads\d+\.`, "ads123.example.com", true},
		{"regex misses", "domain_regex", `^ads\d+\.`, "www.example.com", false},
		{"regex is unanchored", "domain_regex", `example\.com$`, "sub.example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, st := newTestService(t, nil)
			rule := newRule("rule-1", "测试规则", tc.ruleType, tc.ruleValue, "direct", true)
			setUserRules(t, st, rule)
			result, err := service.Debug(tc.input, "domain")
			if err != nil {
				t.Fatalf("Debug(%q, domain) error: %v", tc.input, err)
			}
			if !tc.wantMatch {
				requireNoMatch(t, result, outboundProxy)
				return
			}
			requireMatch(t, result, rule)
		})
	}
}

func TestDebugIPRules(t *testing.T) {
	cases := []struct {
		name      string
		ruleValue string
		input     string
		wantMatch bool
	}{
		{"exact ipv4", "1.1.1.1", "1.1.1.1", true},
		{"exact ipv4 misses", "1.1.1.1", "1.1.1.2", false},
		{"comma values", "8.8.8.8,1.1.1.1", "1.1.1.1", true},
		{"ipv4 cidr contains", "10.0.0.0/8", "10.200.3.4", true},
		{"ipv4 cidr misses", "10.0.0.0/8", "11.0.0.1", false},
		{"ipv6 exact", "2001:db8::1", "2001:db8::1", true},
		{"ipv6 exact misses", "2001:db8::1", "2001:db8::2", false},
		{"ipv6 cidr contains", "2001:db8::/32", "2001:db8:abcd::1", true},
		{"ipv6 cidr misses", "2001:db8::/32", "2001:db9::1", false},
		{"invalid rule value is skipped", "not-an-ip,10.0.0.1", "10.0.0.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, st := newTestService(t, nil)
			rule := newRule("rule-ip", "IP 规则", "ip", tc.ruleValue, "direct", true)
			setUserRules(t, st, rule)
			result, err := service.Debug(tc.input, "ip")
			if err != nil {
				t.Fatalf("Debug(%q, ip) error: %v", tc.input, err)
			}
			if !tc.wantMatch {
				requireNoMatch(t, result, outboundProxy)
				return
			}
			requireMatch(t, result, rule)
		})
	}
}

func TestDebugIPRuleWithCIDRInput(t *testing.T) {
	t.Run("overlapping cidr matches", func(t *testing.T) {
		service, st := newTestService(t, nil)
		rule := newRule("rule-cidr", "重叠网段", "ip", "10.0.0.0/8", "direct", true)
		setUserRules(t, st, rule)
		result, err := service.Debug("10.1.2.0/24", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, rule)
	})
	t.Run("single ip inside queried cidr matches", func(t *testing.T) {
		service, st := newTestService(t, nil)
		rule := newRule("rule-host", "单个地址", "ip", "10.1.2.3", "direct", true)
		setUserRules(t, st, rule)
		result, err := service.Debug("10.0.0.0/8", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, rule)
	})
	t.Run("disjoint cidr misses", func(t *testing.T) {
		service, st := newTestService(t, nil)
		rule := newRule("rule-foreign", "外部网段", "ip", "192.168.0.0/16", "direct", true)
		setUserRules(t, st, rule)
		result, err := service.Debug("10.0.0.0/8", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireNoMatch(t, result, outboundProxy)
	})
}

func TestDebugPortRules(t *testing.T) {
	cases := []struct {
		name      string
		ruleValue string
		input     string
		wantMatch bool
	}{
		{"single port", "443", "443", true},
		{"comma values", "80,443", "443", true},
		{"comma values miss", "80,443", "8443", false},
		{"range inside", "8000-9000", "8080", true},
		{"range lower bound", "8000-9000", "8000", true},
		{"range upper bound", "8000-9000", "9000", true},
		{"range miss", "8000-9000", "9001", false},
		{"combined values", "80,443,8000-9000", "8080", true},
		{"spaces around values", "80, 443", "443", true},
		{"reversed range never matches", "9000-8000", "8500", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, st := newTestService(t, nil)
			rule := newRule("rule-port", "端口规则", "port", tc.ruleValue, "direct", true)
			setUserRules(t, st, rule)
			result, err := service.Debug(tc.input, "port")
			if err != nil {
				t.Fatalf("Debug(%q, port) error: %v", tc.input, err)
			}
			if !tc.wantMatch {
				requireNoMatch(t, result, outboundProxy)
				return
			}
			requireMatch(t, result, rule)
		})
	}
}

func TestDebugProcessRules(t *testing.T) {
	cases := []struct {
		name      string
		ruleValue string
		input     string
		wantMatch bool
	}{
		{"windows path basename", "wechat.exe", `C:\Program Files\WeChat\WeChat.exe`, true},
		{"plain process name", "wechat.exe", "WeChat.exe", true},
		{"mixed case rule", "WeChat.EXE", "wechat.exe", true},
		{"comma values", "chrome.exe,wechat.exe", `D:\Tools\WeChat.exe`, true},
		{"different process", "chrome.exe", `C:\Program Files\WeChat\WeChat.exe`, false},
		{"forward slash path", "curl.exe", "/usr/local/bin/curl.exe", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, st := newTestService(t, nil)
			rule := newRule("rule-process", "进程规则", "process", tc.ruleValue, "proxy", true)
			setUserRules(t, st, rule)
			result, err := service.Debug(tc.input, "process")
			if err != nil {
				t.Fatalf("Debug(%q, process) error: %v", tc.input, err)
			}
			if !tc.wantMatch {
				requireNoMatch(t, result, outboundProxy)
				return
			}
			requireMatch(t, result, rule)
		})
	}
}

func TestDebugNetworkRules(t *testing.T) {
	cases := []struct {
		name      string
		ruleValue string
		input     string
		wantMatch bool
	}{
		{"tcp", "tcp", "tcp", true},
		{"udp in comma values", "tcp,udp", "udp", true},
		{"udp not in tcp", "tcp", "udp", false},
		{"case insensitive", "TCP", "Tcp", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, st := newTestService(t, nil)
			rule := newRule("rule-network", "网络规则", "network", tc.ruleValue, "direct", true)
			setUserRules(t, st, rule)
			result, err := service.Debug(tc.input, "network")
			if err != nil {
				t.Fatalf("Debug(%q, network) error: %v", tc.input, err)
			}
			if !tc.wantMatch {
				requireNoMatch(t, result, outboundProxy)
				return
			}
			requireMatch(t, result, rule)
		})
	}
}

func TestDebugActionBecomesOutboundTag(t *testing.T) {
	for _, action := range []string{"proxy", "direct", "block"} {
		t.Run(action, func(t *testing.T) {
			service, st := newTestService(t, nil)
			rule := newRule("rule-action", "动作测试", "domain_suffix", "example.com", action, true)
			setUserRules(t, st, rule)
			result, err := service.Debug("example.com", "domain")
			if err != nil {
				t.Fatalf("Debug error: %v", err)
			}
			requireMatch(t, result, rule)
		})
	}
}

func TestDebugFirstMatchWins(t *testing.T) {
	t.Run("same type", func(t *testing.T) {
		service, st := newTestService(t, nil)
		first := newRule("first", "第一条", "domain_suffix", "example.com", "direct", true)
		second := newRule("second", "第二条", "domain_suffix", "example.com", "block", true)
		setUserRules(t, st, first, second)
		result, err := service.Debug("www.example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, first)
	})

	t.Run("different types", func(t *testing.T) {
		service, st := newTestService(t, nil)
		first := newRule("first-keyword", "关键词规则", "domain_keyword", "example", "direct", true)
		second := newRule("second-suffix", "后缀规则", "domain_suffix", "example.com", "block", true)
		setUserRules(t, st, first, second)
		result, err := service.Debug("www.example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, first)
	})
}

func TestDebugDisabledRulesAreSkipped(t *testing.T) {
	service, st := newTestService(t, nil)
	disabled := newRule("disabled", "禁用规则", "domain_suffix", "example.com", "block", false)
	enabled := newRule("enabled", "启用规则", "domain_suffix", "example.com", "direct", true)
	setUserRules(t, st, disabled, enabled)

	result, err := service.Debug("www.example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatch(t, result, enabled)

	// A disabled Geo rule is skipped before it can add unresolved candidates.
	disabledGeo := newRule("disabled-geo", "禁用 Geo", "geosite", "cn", "block", false)
	setUserRules(t, st, disabledGeo, enabled)
	result, err = service.Debug("www.example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatch(t, result, enabled)
	if len(result.Notes) != 0 {
		t.Errorf("Notes = %v, want none for a disabled Geo rule", result.Notes)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("Candidates = %v, want none for a disabled Geo rule", result.Candidates)
	}
}

func TestDebugProviderRulesComeAfterUserRules(t *testing.T) {
	providerRules := []model.RoutingRule{
		newRule("provider-1", "规则集示例", "domain_suffix", "example.com", "block", true),
	}
	service, st := newTestService(t, func() []model.RoutingRule { return providerRules })
	setUserRules(t, st, newRule("user-1", "用户规则", "domain_suffix", "user.com", "direct", true))

	result, err := service.Debug("www.example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatchedRule(t, result, providerRules[0], sourceRuleSet)

	result, err = service.Debug("www.user.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatch(t, result, newRule("user-1", "用户规则", "domain_suffix", "user.com", "direct", true))

	// A matching user rule always wins over a matching provider rule.
	userMatch := newRule("user-2", "用户示例域名", "domain_suffix", "example.com", "direct", true)
	setUserRules(t, st, userMatch)
	result, err = service.Debug("www.example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatch(t, result, userMatch)

	// The provider function is evaluated on every call, so a refresh that
	// changes the expanded rules is immediately visible.
	providerRules = []model.RoutingRule{
		newRule("provider-2", "规则集关键词", "domain_keyword", "example", "proxy", true),
	}
	setUserRules(t, st)
	result, err = service.Debug("www.example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatchedRule(t, result, providerRules[0], sourceRuleSet)
}

func TestDebugGeoRulesAreReportedAsCandidates(t *testing.T) {
	t.Run("geosite before matched user rule", func(t *testing.T) {
		service, st := newTestService(t, nil)
		geo := newRule("geo-site", "广告域名拦截", "geosite", "category-ads-all", "block", true)
		suffix := newRule("suffix", "示例直连", "domain_suffix", "example.com", "direct", true)
		setUserRules(t, st, geo, suffix)

		result, err := service.Debug("www.example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, suffix)
		if len(result.Notes) == 0 || !strings.Contains(strings.Join(result.Notes, "\n"), "Geo") {
			t.Fatalf("Notes = %v, want a GeoIP/GeoSite hint", result.Notes)
		}
		if !strings.Contains(result.Reason, "Geo") {
			t.Errorf("Reason = %q, want it to mention the unresolved Geo rules", result.Reason)
		}
		if len(result.Candidates) != 1 {
			t.Fatalf("Candidates = %v, want exactly one unresolved Geo rule", result.Candidates)
		}
		candidate := result.Candidates[0]
		if candidate.RuleID != "geo-site" || candidate.RuleType != "geosite" {
			t.Errorf("candidate = %+v, want the geosite rule", candidate)
		}
		if candidate.Source != sourceUser {
			t.Errorf("candidate.Source = %q, want %q", candidate.Source, sourceUser)
		}
		if candidate.Status != candidateStatusUnresolved {
			t.Errorf("candidate.Status = %q, want %q", candidate.Status, candidateStatusUnresolved)
		}
		if candidate.Action != outboundBlock || candidate.OutboundTag != outboundBlock {
			t.Errorf("candidate action = %q/%q, want block", candidate.Action, candidate.OutboundTag)
		}
		if candidate.Reason == "" {
			t.Error("candidate.Reason is empty")
		}
	})

	t.Run("geoip before matched IP rule", func(t *testing.T) {
		service, st := newTestService(t, nil)
		geo := newRule("geo-ip", "大陆 IP 直连", "geoip", "cn", "direct", true)
		ipRule := newRule("ip-rule", "示例 IP", "ip", "1.1.1.1", "proxy", true)
		setUserRules(t, st, geo, ipRule)

		result, err := service.Debug("1.1.1.1", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, ipRule)
		if len(result.Candidates) != 1 || result.Candidates[0].RuleType != "geoip" {
			t.Fatalf("Candidates = %v, want the geoip rule", result.Candidates)
		}
	})

	t.Run("geo only without match", func(t *testing.T) {
		service, st := newTestService(t, nil)
		geo := newRule("geo-site", "广告域名拦截", "geosite", "category-ads-all", "block", true)
		setUserRules(t, st, geo)

		result, err := service.Debug("www.example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireNoMatch(t, result, outboundProxy)
		if len(result.Notes) == 0 || !strings.Contains(strings.Join(result.Notes, "\n"), "Geo") {
			t.Fatalf("Notes = %v, want a GeoIP/GeoSite hint", result.Notes)
		}
		if len(result.Candidates) != 1 {
			t.Fatalf("Candidates = %v, want one unresolved Geo rule", result.Candidates)
		}
		if !strings.Contains(result.Reason, "Geo") {
			t.Errorf("Reason = %q, want it to mention the unresolved Geo rules", result.Reason)
		}
	})

	t.Run("geo rule after the match is not reported", func(t *testing.T) {
		service, st := newTestService(t, nil)
		suffix := newRule("suffix", "示例直连", "domain_suffix", "example.com", "direct", true)
		geo := newRule("geo-site", "广告域名拦截", "geosite", "category-ads-all", "block", true)
		setUserRules(t, st, suffix, geo)

		result, err := service.Debug("www.example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, suffix)
		if len(result.Notes) != 0 {
			t.Errorf("Notes = %v, want none: the match happens before the Geo rule", result.Notes)
		}
		if len(result.Candidates) != 0 {
			t.Errorf("Candidates = %v, want none: the match happens before the Geo rule", result.Candidates)
		}
	})
}

func TestDebugAutoInference(t *testing.T) {
	service, st := newTestService(t, nil)
	domainRule := newRule("auto-domain", "域名示例", "domain_suffix", "example.com", "direct", true)
	ipRule := newRule("auto-ip", "网段示例", "ip", "10.0.0.0/8", "proxy", true)
	ipv6Rule := newRule("auto-ipv6", "IPv6 示例", "ip", "2001:db8::/32", "block", true)
	portRule := newRule("auto-port", "端口示例", "port", "8080", "direct", true)
	processRule := newRule("auto-process", "进程示例", "process", "curl.exe", "proxy", true)
	setUserRules(t, st, domainRule, ipRule, ipv6Rule, portRule, processRule)

	cases := []struct {
		name       string
		input      string
		wantRuleID string
	}{
		{"url becomes domain", "https://WWW.Example.COM:8443/login?next=1", "auto-domain"},
		{"host with path becomes domain", "www.example.com/login", "auto-domain"},
		{"plain domain", "Example.COM", "auto-domain"},
		{"ipv4 becomes ip", "10.1.2.3", "auto-ip"},
		{"ipv4 cidr becomes ip", "10.9.0.0/16", "auto-ip"},
		{"ipv6 becomes ip", "2001:db8::1", "auto-ipv6"},
		{"digits become port", "8080", "auto-port"},
		{"windows process path becomes process", `C:\Tools\curl.exe`, "auto-process"},
		{"executable name becomes process", "CURL.EXE", "auto-process"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.Debug(tc.input, "auto")
			if err != nil {
				t.Fatalf("Debug(%q, auto) error: %v", tc.input, err)
			}
			if !result.Matched {
				t.Fatalf("expected a match for %q, got %+v", tc.input, result)
			}
			if result.RuleID != tc.wantRuleID {
				t.Errorf("RuleID = %q, want %q (type %q)", result.RuleID, tc.wantRuleID, result.RuleType)
			}
		})
	}

	t.Run("empty kind falls back to auto", func(t *testing.T) {
		result, err := service.Debug("10.1.2.3", "")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		if result.RuleID != "auto-ip" {
			t.Fatalf("RuleID = %q, want auto-ip", result.RuleID)
		}
	})
}

func TestDebugDefaultOutboundPerProxyMode(t *testing.T) {
	cases := []struct {
		mode         string
		wantOutbound string
	}{
		{"rule", outboundProxy},
		{"global", outboundProxy},
		{"direct", outboundDirect},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			service, st := newTestService(t, nil)
			setProxyMode(t, st, tc.mode)
			setUserRules(t, st, newRule("other", "其他域名", "domain_suffix", "other.com", "block", true))

			result, err := service.Debug("example.com", "domain")
			if err != nil {
				t.Fatalf("Debug error: %v", err)
			}
			requireNoMatch(t, result, tc.wantOutbound)
			if !strings.Contains(result.Reason, "默认出站") {
				t.Errorf("Reason = %q, want it to mention the default outbound", result.Reason)
			}
		})
	}

	t.Run("unknown mode falls back to rule", func(t *testing.T) {
		service, st := newTestService(t, nil)
		setProxyMode(t, st, "weird")
		result, err := service.Debug("example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireNoMatch(t, result, outboundProxy)
		if !strings.Contains(strings.Join(result.Notes, "\n"), "无法识别") {
			t.Errorf("Notes = %v, want an unknown proxy mode hint", result.Notes)
		}
	})

	t.Run("no store uses rule mode", func(t *testing.T) {
		service := New(nil, nil)
		result, err := service.Debug("example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireNoMatch(t, result, outboundProxy)
	})
}

func TestDebugValidationErrors(t *testing.T) {
	service := New(nil, nil)
	longDomain := strings.Repeat("a", 254)
	cases := []struct {
		name    string
		input   string
		kind    string
		wantErr string
	}{
		{"empty domain", "", "domain", "域名不能为空"},
		{"blank domain", "   ", "domain", "域名不能为空"},
		{"domain with whitespace", "bad domain", "domain", "空白"},
		{"domain with user info", "user@@host", "domain", "域名无效"},
		{"domain with extra colon", "example.com:bad:port", "domain", "域名无效"},
		{"domain too long", longDomain, "domain", "域名过长"},
		{"empty ip", "", "ip", "IP 不能为空"},
		{"malformed ip", "not-an-ip", "ip", "IP 地址无效"},
		{"malformed ipv4", "999.1.1.1", "ip", "IP 地址无效"},
		{"malformed cidr", "10.0.0.0/99", "ip", "IP/CIDR 无效"},
		{"empty port", "", "port", "端口不能为空"},
		{"port zero", "0", "port", "1-65535"},
		{"port too large", "65536", "port", "1-65535"},
		{"port not numeric", "abc", "port", "1-65535"},
		{"empty process", "", "process", "进程名不能为空"},
		{"root path process", "/", "process", "进程名无效"},
		{"empty network", "", "network", "网络类型不能为空"},
		{"unsupported network", "http", "network", "tcp 或 udp"},
		{"unknown kind", "example.com", "hostname", "不支持的匹配类型"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.Debug(tc.input, tc.kind)
			if err == nil {
				t.Fatalf("Debug(%q, %q) = %+v, want an error", tc.input, tc.kind, result)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if result.Matched {
				t.Errorf("result is a match despite the validation error: %+v", result)
			}
		})
	}
}

func TestDebugInputNormalization(t *testing.T) {
	t.Run("domain strips scheme userinfo port and path", func(t *testing.T) {
		service, st := newTestService(t, nil)
		rule := newRule("suffix", "示例直连", "domain_suffix", "example.com", "direct", true)
		setUserRules(t, st, rule)
		for _, input := range []string{
			"HTTPS://User:Pass@WWW.Example.COM:8443/path?next=1#frag",
			"WWW.Example.COM:8443/path?next=1",
			"www.example.com.",
		} {
			result, err := service.Debug(input, "domain")
			if err != nil {
				t.Fatalf("Debug(%q, domain) error: %v", input, err)
			}
			requireMatch(t, result, rule)
		}
	})

	t.Run("ip accepts host port and cidr", func(t *testing.T) {
		service, st := newTestService(t, nil)
		rule := newRule("ip", "示例 IP", "ip", "1.1.1.1", "direct", true)
		setUserRules(t, st, rule)
		result, err := service.Debug("1.1.1.1:443", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, rule)

		ipv6Rule := newRule("ipv6", "IPv6 网段", "ip", "2001:db8::/32", "block", true)
		setUserRules(t, st, ipv6Rule)
		result, err = service.Debug("2001:DB8:1::/48", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, ipv6Rule)
	})

	t.Run("process strips quotes and directory", func(t *testing.T) {
		service, st := newTestService(t, nil)
		rule := newRule("process", "微信", "process", "wechat.exe", "direct", true)
		setUserRules(t, st, rule)
		result, err := service.Debug(`"C:\Program Files\WeChat\WeChat.exe"`, "process")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireMatch(t, result, rule)
	})
}

func TestDebugInvalidRegexRuleAddsNote(t *testing.T) {
	service, st := newTestService(t, nil)
	bad := newRule("bad-regex", "坏正则", "domain_regex", "[", "proxy", true)
	good := newRule("good", "示例直连", "domain_suffix", "example.com", "direct", true)
	setUserRules(t, st, bad, good)

	result, err := service.Debug("example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatch(t, result, good)
	if len(result.Notes) == 0 || !strings.Contains(strings.Join(result.Notes, "\n"), "正则表达式无效") {
		t.Fatalf("Notes = %v, want an invalid regex warning", result.Notes)
	}
}

func TestDebugRuleKindsAreIsolated(t *testing.T) {
	t.Run("domain rule does not match an IP", func(t *testing.T) {
		service, st := newTestService(t, nil)
		setUserRules(t, st, newRule("domain-only", "域名规则", "domain_suffix", "example.com", "block", true))
		result, err := service.Debug("1.1.1.1", "ip")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireNoMatch(t, result, outboundProxy)
	})

	t.Run("IP rule does not match a domain", func(t *testing.T) {
		service, st := newTestService(t, nil)
		setUserRules(t, st, newRule("ip-only", "IP 规则", "ip", "1.1.1.1", "block", true))
		result, err := service.Debug("example.com", "domain")
		if err != nil {
			t.Fatalf("Debug error: %v", err)
		}
		requireNoMatch(t, result, outboundProxy)
	})
}

func TestDebugNilStoreAndProvider(t *testing.T) {
	providerRules := []model.RoutingRule{
		newRule("provider", "规则集示例", "domain_suffix", "example.com", "direct", true),
	}
	service := New(nil, func() []model.RoutingRule { return providerRules })
	result, err := service.Debug("www.example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatchedRule(t, result, providerRules[0], sourceRuleSet)

	result, err = service.Debug("other.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireNoMatch(t, result, outboundProxy)

	// A completely empty service must not panic.
	empty := New(nil, nil)
	result, err = empty.Debug("example.com", "auto")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireNoMatch(t, result, outboundProxy)
}

func TestDebugUnknownRuleTypeIsIgnored(t *testing.T) {
	service, st := newTestService(t, nil)
	unknown := newRule("unknown", "未知规则", "domain_blackhole", "example.com", "block", true)
	good := newRule("good", "示例直连", "domain_suffix", "example.com", "direct", true)
	setUserRules(t, st, unknown, good)

	result, err := service.Debug("example.com", "domain")
	if err != nil {
		t.Fatalf("Debug error: %v", err)
	}
	requireMatch(t, result, good)
}
