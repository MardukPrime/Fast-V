package routing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	routingdomain "github.com/MardukPrime/Fast-V/internal/routing"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Service manages user-defined routing rules.
type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) ServiceName() string {
	return "RoutingService"
}

func (s *Service) List() []model.RoutingRule {
	return s.store.ListRoutingRules()
}

func (s *Service) Save(input model.RoutingRule) (model.RoutingRule, error) {
	normalized, err := routingdomain.Normalize(input)
	if err != nil {
		return model.RoutingRule{}, err
	}
	return s.store.SaveRoutingRule(normalized)
}

func (s *Service) Delete(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("rule id is required")
	}
	return s.store.DeleteRoutingRule(id)
}

func (s *Service) Reorder(ids []string) error {
	return s.store.ReorderRoutingRules(ids)
}

func (s *Service) Reset() []model.RoutingRule {
	rules := routingdomain.DefaultRules()
	_ = s.store.ReplaceRoutingRules(rules)
	return rules
}

// Presets returns the ready-made rule sets offered by the routing editor.
func (s *Service) Presets() []model.RoutingPreset {
	return routingdomain.Presets()
}

// ApplyPreset replaces all current rules with the rules of the given preset.
// Every applied rule receives a fresh ID so the list stays editable as a normal
// user rule list.
func (s *Service) ApplyPreset(id string) ([]model.RoutingRule, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("预设 ID 不能为空")
	}
	var preset *model.RoutingPreset
	for _, candidate := range routingdomain.Presets() {
		if candidate.ID == id {
			found := candidate
			preset = &found
			break
		}
	}
	if preset == nil {
		return nil, fmt.Errorf("路由预设不存在: %s", id)
	}
	rules := make([]model.RoutingRule, 0, len(preset.Rules))
	for _, rule := range preset.Rules {
		normalized, err := routingdomain.Normalize(rule)
		if err != nil {
			return nil, fmt.Errorf("预设规则无效: %w", err)
		}
		normalized.ID = model.NewID()
		rules = append(rules, normalized)
	}
	if err := s.store.ReplaceRoutingRules(rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// Import validates an imported rule set and replaces all current rules with it.
// Validation is all-or-nothing: if any rule is invalid nothing is persisted.
func (s *Service) Import(rules []model.RoutingRule) ([]model.RoutingRule, error) {
	normalized := make([]model.RoutingRule, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for i, rule := range rules {
		item, err := routingdomain.Normalize(rule)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条规则无效: %w", i+1, err)
		}
		if item.ID == "" {
			item.ID = model.NewID()
		}
		if _, ok := seen[item.ID]; ok {
			item.ID = model.NewID()
		}
		seen[item.ID] = struct{}{}
		normalized = append(normalized, item)
	}
	if err := s.store.ReplaceRoutingRules(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}
