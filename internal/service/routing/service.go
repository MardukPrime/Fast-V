package routing

import (
	"errors"
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
