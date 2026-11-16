package routing

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func TestRoutingServiceLifecycle(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)

	defaults := service.Reset()
	if len(defaults) == 0 {
		t.Fatal("expected default rules")
	}
	if len(service.List()) != len(defaults) {
		t.Fatal("default rules were not persisted")
	}

	saved, err := service.Save(model.RoutingRule{
		Type: "domain_suffix", Value: "example.com", Action: "direct", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || saved.Name == "" {
		t.Fatalf("rule was not normalized: %+v", saved)
	}
	if len(service.List()) != len(defaults)+1 {
		t.Fatal("saved rule is missing")
	}

	ids := make([]string, 0)
	for _, rule := range service.List() {
		ids = append([]string{rule.ID}, ids...)
	}
	if err := service.Reorder(ids); err != nil {
		t.Fatal(err)
	}
	if service.List()[0].ID != saved.ID {
		t.Fatal("reorder did not move the saved rule to the front")
	}

	if err := service.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	if len(service.List()) != len(defaults) {
		t.Fatal("rule was not deleted")
	}
}

func TestRoutingServiceRejectsInvalidRule(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)
	if _, err := service.Save(model.RoutingRule{Type: "unknown", Value: "x", Action: "proxy"}); err == nil {
		t.Fatal("expected invalid rule type error")
	}
}

func newRoutingService(t *testing.T) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := store.NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	return New(st), path
}

func TestRoutingServicePresetsAndApply(t *testing.T) {
	service, path := newRoutingService(t)
	presets := service.Presets()
	if len(presets) < 4 {
		t.Fatalf("expected at least 4 presets, got %d", len(presets))
	}
	requiredNames := map[string]bool{
		"私有网络直连":     false,
		"广告拦截":       false,
		"中国大陆域名直连":   false,
		"中国大陆 IP 直连": false,
	}
	seenPresetIDs := make(map[string]struct{}, len(presets))
	for _, preset := range presets {
		if preset.ID == "" || preset.Name == "" || len(preset.Rules) == 0 {
			t.Fatalf("incomplete preset: %+v", preset)
		}
		if _, ok := seenPresetIDs[preset.ID]; ok {
			t.Fatalf("duplicate preset ID: %s", preset.ID)
		}
		seenPresetIDs[preset.ID] = struct{}{}
		if _, ok := requiredNames[preset.Name]; ok {
			requiredNames[preset.Name] = true
		}

		applied, err := service.ApplyPreset(preset.ID)
		if err != nil {
			t.Fatalf("apply preset %s failed: %v", preset.ID, err)
		}
		if len(applied) != len(preset.Rules) {
			t.Fatalf("preset %s applied %d rules, want %d", preset.ID, len(applied), len(preset.Rules))
		}
		ids := make(map[string]struct{}, len(applied))
		for i, rule := range applied {
			if rule.ID == "" {
				t.Fatalf("preset %s rule was stored without an ID: %+v", preset.ID, rule)
			}
			if _, ok := ids[rule.ID]; ok {
				t.Fatalf("preset %s rule IDs are not unique", preset.ID)
			}
			ids[rule.ID] = struct{}{}
			want := preset.Rules[i]
			if rule.Type != want.Type || rule.Value != want.Value || rule.Action != want.Action || !rule.Enabled {
				t.Fatalf("preset %s rule %d was changed: got %+v want %+v", preset.ID, i, rule, want)
			}
			if rule.Name == "" {
				t.Fatalf("preset %s rule name is empty: %+v", preset.ID, rule)
			}
		}
		if list := service.List(); !reflect.DeepEqual(list, applied) {
			t.Fatalf("preset %s was not persisted: %+v vs %+v", preset.ID, list, applied)
		}

		reloaded, err := store.NewStoreAt(path)
		if err != nil {
			t.Fatal(err)
		}
		if list := New(reloaded).List(); !reflect.DeepEqual(list, applied) {
			t.Fatalf("preset %s was not persisted to disk: %+v vs %+v", preset.ID, list, applied)
		}
	}
	for name, found := range requiredNames {
		if !found {
			t.Fatalf("required preset %q is missing", name)
		}
	}
}

func TestRoutingServiceApplyPresetRegeneratesIDs(t *testing.T) {
	service, _ := newRoutingService(t)
	service.Reset()

	first, err := service.ApplyPreset("private-network")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("expected rules for private network preset")
	}
	firstIDs := make(map[string]struct{}, len(first))
	for _, rule := range first {
		firstIDs[rule.ID] = struct{}{}
	}
	second, err := service.ApplyPreset("private-network")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range second {
		if _, ok := firstIDs[rule.ID]; ok {
			t.Fatalf("applying a preset must regenerate rule IDs, got reused ID %s", rule.ID)
		}
	}

	if _, err := service.ApplyPreset("does-not-exist"); err == nil {
		t.Fatal("expected unknown preset error")
	}
	if _, err := service.ApplyPreset("  "); err == nil {
		t.Fatal("expected empty preset ID error")
	}
	if list := service.List(); !reflect.DeepEqual(list, second) {
		t.Fatal("failed preset application must not touch the stored rules")
	}

	combined, err := service.ApplyPreset("bypass-cn")
	if err != nil {
		t.Fatal(err)
	}
	if len(combined) < 4 {
		t.Fatalf("expected the combined preset to contain at least 4 rules, got %+v", combined)
	}
}

func TestRoutingServiceImportReplacesAndPersists(t *testing.T) {
	service, path := newRoutingService(t)
	defaults := service.Reset()

	imported, err := service.Import([]model.RoutingRule{
		{Name: "示例域名直连", Type: "domain_suffix", Value: "example.com,example.net", Action: "direct", Enabled: true},
		{Type: "process", Value: "chrome.exe,Telegram.exe", Action: "proxy", Enabled: true},
		{Type: "ip", Value: "10.0.0.0,192.168.0.0/16", Enabled: true},
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if len(imported) != 3 {
		t.Fatalf("expected 3 imported rules, got %d", len(imported))
	}
	if imported[0].Name != "示例域名直连" {
		t.Fatalf("explicit rule name was not preserved: %+v", imported[0])
	}
	for i, rule := range imported {
		if rule.ID == "" {
			t.Fatalf("imported rule %d has no ID", i)
		}
		if rule.Name == "" {
			t.Fatalf("imported rule %d has no name", i)
		}
	}
	if imported[1].Type != "process" || imported[1].Action != "proxy" {
		t.Fatalf("process rule was not normalized: %+v", imported[1])
	}
	if imported[2].Action != "proxy" || imported[2].Name == "" {
		t.Fatalf("default action/name was not applied: %+v", imported[2])
	}
	defaultIDs := make(map[string]struct{}, len(defaults))
	for _, rule := range defaults {
		defaultIDs[rule.ID] = struct{}{}
	}
	for _, rule := range imported {
		if _, ok := defaultIDs[rule.ID]; ok {
			t.Fatalf("import must replace the current rules, found recycled ID %s", rule.ID)
		}
	}

	list := service.List()
	if !reflect.DeepEqual(list, imported) {
		t.Fatalf("imported rules were not stored in order: %+v vs %+v", list, imported)
	}
	reloaded, err := store.NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedList := New(reloaded).List(); !reflect.DeepEqual(reloadedList, imported) {
		t.Fatalf("imported rules were not persisted: %+v vs %+v", reloadedList, imported)
	}
}

func TestRoutingServiceImportIsAtomic(t *testing.T) {
	service, _ := newRoutingService(t)
	before := service.Reset()
	cookie := before[0].ID

	cases := []struct {
		name  string
		rules []model.RoutingRule
	}{
		{
			name: "invalid type",
			rules: []model.RoutingRule{
				{Type: "domain_suffix", Value: "valid.example.com", Action: "direct", Enabled: true},
				{Type: "unknown", Value: "x", Action: "proxy", Enabled: true},
			},
		},
		{
			name: "invalid action",
			rules: []model.RoutingRule{
				{Type: "domain_suffix", Value: "valid.example.com", Action: "direct", Enabled: true},
				{Type: "domain_suffix", Value: "example.com", Action: "forward", Enabled: true},
			},
		},
		{
			name: "empty value",
			rules: []model.RoutingRule{
				{Type: "domain_suffix", Value: "valid.example.com", Action: "direct", Enabled: true},
				{Type: "ip", Value: " ", Action: "direct", Enabled: true},
			},
		},
		{
			name: "invalid process value",
			rules: []model.RoutingRule{
				{Type: "domain_suffix", Value: "valid.example.com", Action: "direct", Enabled: true},
				{Type: "process", Value: " , \n ", Action: "direct", Enabled: true},
			},
		},
		{
			name: "invalid ip",
			rules: []model.RoutingRule{
				{Type: "domain_suffix", Value: "valid.example.com", Action: "direct", Enabled: true},
				{Type: "ip", Value: "not-an-ip", Action: "direct", Enabled: true},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.Import(tc.rules)
			if err == nil {
				t.Fatal("expected import to fail")
			}
			if !strings.Contains(err.Error(), "第 2 条规则无效") {
				t.Fatalf("unexpected error message: %v", err)
			}
			after := service.List()
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("failed import must not change stored rules: %+v vs %+v", after, before)
			}
			if len(after) == 0 || after[0].ID != cookie {
				t.Fatal("failed import must keep the existing rules untouched")
			}
		})
	}
}
