package ruleset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("NewStoreAt() error = %v", err)
	}
	return st
}

func newRuleServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)
	return server
}

func newFailingServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func saveSet(t *testing.T, service *Service, set model.RuleSet) model.RuleSet {
	t.Helper()
	saved, err := service.Save(set)
	if err != nil {
		t.Fatalf("Save(%+v) error = %v", set, err)
	}
	return saved
}

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

func TestSaveValidation(t *testing.T) {
	base := model.RuleSet{
		Name:   "测试规则集",
		URL:    "https://example.com/list.txt",
		Kind:   "domain",
		Action: "direct",
	}
	tests := []struct {
		name    string
		mutate  func(*model.RuleSet)
		wantErr string
	}{
		{name: "missing name", mutate: func(set *model.RuleSet) { set.Name = "  " }, wantErr: "名称"},
		{name: "missing url", mutate: func(set *model.RuleSet) { set.URL = " " }, wantErr: "地址不能为空"},
		{name: "unsupported scheme", mutate: func(set *model.RuleSet) { set.URL = "ftp://example.com/list" }, wantErr: "http:// 或 https://"},
		{name: "missing host", mutate: func(set *model.RuleSet) { set.URL = "https:///list" }, wantErr: "格式无效"},
		{name: "bad kind", mutate: func(set *model.RuleSet) { set.Kind = "geoip" }, wantErr: "类型"},
		{name: "bad action", mutate: func(set *model.RuleSet) { set.Action = "reject" }, wantErr: "动作"},
		{name: "interval too small", mutate: func(set *model.RuleSet) { set.IntervalMinutes = 5 }, wantErr: "更新间隔"},
		{name: "interval too large", mutate: func(set *model.RuleSet) { set.IntervalMinutes = 1441 }, wantErr: "更新间隔"},
		{name: "negative interval", mutate: func(set *model.RuleSet) { set.IntervalMinutes = -15 }, wantErr: "更新间隔"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := New(newTestStore(t))
			input := base
			test.mutate(&input)
			if _, err := service.Save(input); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Save() error = %v, want it to contain %q", err, test.wantErr)
			}
			if got := service.List(); len(got) != 0 {
				t.Fatalf("List() = %+v after rejected save", got)
			}
		})
	}
}

func TestSaveNormalizesAndPersists(t *testing.T) {
	st := newTestStore(t)
	service := New(st)
	saved := saveSet(t, service, model.RuleSet{
		Name:            "  广告  ",
		URL:             " https://example.com/list.txt ",
		Kind:            "DOMAIN",
		Action:          "Block",
		Enabled:         true,
		AutoUpdate:      true,
		IntervalMinutes: 60,
	})
	if saved.ID == "" {
		t.Fatal("Save() returned an empty ID")
	}
	if saved.Name != "广告" {
		t.Fatalf("Name = %q, want trimmed %q", saved.Name, "广告")
	}
	if saved.Kind != "domain" || saved.Action != "block" {
		t.Fatalf("Kind/Action = %q/%q, want domain/block", saved.Kind, saved.Action)
	}
	set, err := st.FindRuleSet(saved.ID)
	if err != nil {
		t.Fatalf("FindRuleSet() error = %v", err)
	}
	if set.IntervalMinutes != 60 || !set.AutoUpdate {
		t.Fatalf("stored set = %+v", set)
	}
}

func TestUpdateSuccessExpandsRules(t *testing.T) {
	server := newRuleServer(t, "example.com\nads.example.com\n||tracker.example^\n1.2.3.4\n10.0.0.0/8\n")
	service := New(newTestStore(t))
	saved := saveSet(t, service, model.RuleSet{
		Name:    "广告",
		URL:     server.URL,
		Kind:    "combined",
		Action:  "block",
		Enabled: true,
	})

	updated, err := service.Update(saved.ID)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.RuleCount != 5 {
		t.Fatalf("RuleCount = %d, want 5 entries", updated.RuleCount)
	}
	if updated.LastError != "" || updated.UpdatedAt == "" {
		t.Fatalf("Update() status = %+v, want a clean timestamped status", updated)
	}

	rules := service.ExpandedRules()
	if len(rules) != 2 {
		t.Fatalf("len(ExpandedRules()) = %d, want 2 merged rules: %+v", len(rules), rules)
	}
	if rules[0].Name != "广告 域名" || rules[0].Type != "domain_suffix" || rules[0].Action != "block" {
		t.Fatalf("domain rule = %+v", rules[0])
	}
	if len(collectValues(rules, "domain_suffix")) != 3 {
		t.Fatalf("domain values = %v", collectValues(rules, "domain_suffix"))
	}
	if rules[1].Name != "广告 IP" || rules[1].Type != "ip" || rules[1].Action != "block" {
		t.Fatalf("ip rule = %+v", rules[1])
	}
	// A later Save with another action applies to the already downloaded rules.
	saved.Action = "direct"
	if _, err := service.Save(saved); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	for _, rule := range service.ExpandedRules() {
		if rule.Action != "direct" {
			t.Fatalf("rule %q action = %q, want refreshed action direct", rule.Name, rule.Action)
		}
	}
}

func TestUpdateFailureRecordsStatus(t *testing.T) {
	server := newFailingServer(t, http.StatusInternalServerError)
	service := New(newTestStore(t))
	saved := saveSet(t, service, model.RuleSet{
		Name:    "失效规则集",
		URL:     server.URL,
		Kind:    "domain",
		Action:  "direct",
		Enabled: true,
	})

	updated, err := service.Update(saved.ID)
	if err == nil {
		t.Fatal("Update() error = nil, want download error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("Update() error = %q, want it to mention HTTP 500", err)
	}
	if updated.RuleCount != 0 || updated.LastError == "" || updated.UpdatedAt != "" {
		t.Fatalf("Update() status = %+v, want count 0, error text and no timestamp", updated)
	}
	if got := service.ExpandedRules(); len(got) != 0 {
		t.Fatalf("ExpandedRules() = %+v after failed first download", got)
	}
	stored, findErr := service.store.FindRuleSet(saved.ID)
	if findErr != nil {
		t.Fatalf("FindRuleSet() error = %v", findErr)
	}
	if stored.LastError == "" {
		t.Fatal("LastError was not persisted")
	}
}

func TestUpdateKeepsLastGoodRulesOnFailure(t *testing.T) {
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("keep.example\n"))
	}))
	t.Cleanup(server.Close)

	service := New(newTestStore(t))
	saved := saveSet(t, service, model.RuleSet{Name: "保底", URL: server.URL, Kind: "domain", Action: "direct", Enabled: true})
	if _, err := service.Update(saved.ID); err != nil {
		t.Fatalf("first Update() error = %v", err)
	}
	if got := len(service.ExpandedRules()); got != 1 {
		t.Fatalf("ExpandedRules() after success = %d rules, want 1", got)
	}

	failing.Store(true)
	if _, err := service.Update(saved.ID); err == nil {
		t.Fatal("second Update() error = nil, want 404 error")
	}
	if got := len(service.ExpandedRules()); got != 1 {
		t.Fatalf("ExpandedRules() after failure = %d rules, want the last good rules kept", got)
	}
}

func TestExpandedRulesOnlyEnabledAndDownloaded(t *testing.T) {
	firstServer := newRuleServer(t, "one.example\n")
	secondServer := newRuleServer(t, "two.example\n")
	service := New(newTestStore(t))

	first := saveSet(t, service, model.RuleSet{Name: "一", URL: firstServer.URL, Kind: "domain", Action: "direct", Enabled: true})
	second := saveSet(t, service, model.RuleSet{Name: "二", URL: secondServer.URL, Kind: "domain", Action: "block", Enabled: true})
	// A third enabled set that was never downloaded contributes no rules.
	saveSet(t, service, model.RuleSet{Name: "三", URL: "https://example.com/never.txt", Kind: "domain", Action: "direct", Enabled: true})
	if _, err := service.Update(first.ID); err != nil {
		t.Fatalf("Update(first) error = %v", err)
	}
	if _, err := service.Update(second.ID); err != nil {
		t.Fatalf("Update(second) error = %v", err)
	}

	rules := service.ExpandedRules()
	if len(rules) != 2 {
		t.Fatalf("len(ExpandedRules()) = %d, want 2: %+v", len(rules), rules)
	}
	if got := collectValues(rules, "domain_suffix"); !equalStrings(got, []string{"one.example", "two.example"}) {
		t.Fatalf("expanded values = %v, want store order", got)
	}
	if rules[0].Action != "direct" || rules[1].Action != "block" {
		t.Fatalf("expanded actions = %q/%q", rules[0].Action, rules[1].Action)
	}

	// Disabling a rule set removes its expanded rules.
	first.Enabled = false
	if _, err := service.Save(first); err != nil {
		t.Fatalf("Save(disabled) error = %v", err)
	}
	rules = service.ExpandedRules()
	if len(rules) != 1 || rules[0].Action != "block" {
		t.Fatalf("ExpandedRules() after disabling = %+v", rules)
	}
}

func TestDeleteRuleSet(t *testing.T) {
	server := newRuleServer(t, "gone.example\n")
	service := New(newTestStore(t))
	saved := saveSet(t, service, model.RuleSet{Name: "删除", URL: server.URL, Kind: "domain", Action: "direct", Enabled: true})
	if _, err := service.Update(saved.ID); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := service.Delete(saved.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := service.List(); len(got) != 0 {
		t.Fatalf("List() = %+v after delete", got)
	}
	if got := service.ExpandedRules(); len(got) != 0 {
		t.Fatalf("ExpandedRules() = %+v after delete", got)
	}
	if _, err := service.store.FindRuleSet(saved.ID); err == nil {
		t.Fatal("FindRuleSet() error = nil after delete")
	}
	if err := service.Delete("  "); err == nil {
		t.Fatal("Delete(\"\") error = nil")
	}
	if err := service.Delete("missing"); err == nil {
		t.Fatal("Delete(missing) error = nil")
	}
	if _, err := service.Update("missing"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("Update(missing) error = %v, want a not-found error", err)
	}
}

func TestUpdateAllCollectsFailures(t *testing.T) {
	goodServer := newRuleServer(t, "good.example\n")
	badServer := newFailingServer(t, http.StatusBadGateway)
	var disabledHits atomic.Int32
	disabledServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disabledHits.Add(1)
		_, _ = w.Write([]byte("unused.example\n"))
	}))
	t.Cleanup(disabledServer.Close)

	service := New(newTestStore(t))
	saveSet(t, service, model.RuleSet{Name: "好的", URL: goodServer.URL, Kind: "domain", Action: "direct", Enabled: true})
	bad := saveSet(t, service, model.RuleSet{Name: "坏的", URL: badServer.URL, Kind: "domain", Action: "direct", Enabled: true})
	saveSet(t, service, model.RuleSet{Name: "停用", URL: disabledServer.URL, Kind: "domain", Action: "direct", Enabled: false})

	updated, err := service.UpdateAll()
	if updated != 1 {
		t.Fatalf("UpdateAll() updated = %d, want 1", updated)
	}
	if err == nil || !strings.Contains(err.Error(), "更新失败") {
		t.Fatalf("UpdateAll() error = %v, want aggregated failure", err)
	}
	if hit := disabledHits.Load(); hit != 0 {
		t.Fatalf("disabled rule set was downloaded %d times", hit)
	}
	stored, findErr := service.store.FindRuleSet(bad.ID)
	if findErr != nil {
		t.Fatalf("FindRuleSet() error = %v", findErr)
	}
	if stored.LastError == "" {
		t.Fatal("failed rule set LastError was not persisted")
	}

	emptyService := New(newTestStore(t))
	if updated, err := emptyService.UpdateAll(); updated != 0 || err != nil {
		t.Fatalf("UpdateAll() on empty store = (%d, %v), want (0, nil)", updated, err)
	}
}

func TestDueRuleSets(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	stamp := func(offset time.Duration) string { return now.Add(offset).Format(time.RFC3339) }
	sets := []model.RuleSet{
		{ID: "never", Enabled: true, AutoUpdate: true},
		{ID: "due", Enabled: true, AutoUpdate: true, UpdatedAt: stamp(-2 * time.Hour), IntervalMinutes: 60},
		{ID: "fresh", Enabled: true, AutoUpdate: true, UpdatedAt: stamp(-30 * time.Minute), IntervalMinutes: 60},
		{ID: "auto-off", Enabled: true, AutoUpdate: false, UpdatedAt: stamp(-48 * time.Hour)},
		{ID: "disabled", Enabled: false, AutoUpdate: true, UpdatedAt: stamp(-48 * time.Hour)},
		{ID: "default-due", Enabled: true, AutoUpdate: true, UpdatedAt: stamp(-(defaultIntervalMinutes + 1) * time.Minute)},
		{ID: "default-fresh", Enabled: true, AutoUpdate: true, UpdatedAt: stamp(-(defaultIntervalMinutes - 1) * time.Minute)},
		{ID: "bad-time", Enabled: true, AutoUpdate: true, UpdatedAt: "not-a-time"},
	}
	want := []string{"never", "due", "default-due", "bad-time"}
	if got := dueRuleSets(sets, now); !equalStrings(got, want) {
		t.Fatalf("dueRuleSets() = %v, want %v", got, want)
	}
}

func TestServiceStartupRefreshesNeverDownloaded(t *testing.T) {
	server := newRuleServer(t, "startup.example\n")
	var disabledHits atomic.Int32
	disabledServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disabledHits.Add(1)
		_, _ = w.Write([]byte("disabled.example\n"))
	}))
	t.Cleanup(disabledServer.Close)

	st := newTestStore(t)
	service := New(st)
	saved := saveSet(t, service, model.RuleSet{Name: "启动更新", URL: server.URL, Kind: "domain", Action: "direct", Enabled: true})
	saveSet(t, service, model.RuleSet{Name: "停用", URL: disabledServer.URL, Kind: "domain", Action: "direct", Enabled: false})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(service.ExpandedRules()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("startup did not download the never-downloaded enabled rule set")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hit := disabledHits.Load(); hit != 0 {
		t.Fatalf("disabled rule set was downloaded %d times during startup", hit)
	}
	stored, err := st.FindRuleSet(saved.ID)
	if err != nil {
		t.Fatalf("FindRuleSet() error = %v", err)
	}
	if stored.RuleCount != 1 || stored.UpdatedAt == "" {
		t.Fatalf("startup status = %+v, want one entry and a timestamp", stored)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown() error = %v", err)
	}
	if err := service.ServiceShutdown(); err != nil {
		t.Fatalf("repeated ServiceShutdown() error = %v", err)
	}
}
