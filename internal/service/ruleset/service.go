// Package ruleset implements the Wails service that manages remote rule set
// subscriptions: validation, download, in-memory expansion and automatic
// updates.
package ruleset

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	rulesetdomain "github.com/MardukPrime/Fast-V/internal/ruleset"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// minIntervalMinutes and maxIntervalMinutes bound the per-rule-set refresh
	// interval. Zero means the default interval.
	minIntervalMinutes = 15
	maxIntervalMinutes = 1440
	// defaultIntervalMinutes is used when a rule set enables automatic updates
	// without an interval of its own (360 minutes, matching subscriptions).
	defaultIntervalMinutes = 360

	// autoUpdatePollInterval is how often due rule sets are evaluated.
	autoUpdatePollInterval = time.Minute

	// ruleSetChangedEvent is emitted after automatic downloads changed the
	// expanded rule list; the frontend refreshes counts and the routing view.
	ruleSetChangedEvent = "rulesets:changed"
)

// Service manages remote rule sets and the routing rules they expand into.
type Service struct {
	store *store.Store

	mu     sync.RWMutex
	cache  map[string][]model.RoutingRule
	cancel context.CancelFunc
}

func New(st *store.Store) *Service {
	return &Service{
		store: st,
		cache: make(map[string][]model.RoutingRule),
	}
}

func (s *Service) ServiceName() string {
	return "RuleSetService"
}

// ServiceStartup starts the automatic refresh loop and repopulates the
// in-memory rule cache in the background. Never-downloaded enabled rule sets are
// fetched immediately; already-downloaded sets are refreshed as soon as their
// interval elapsed while the app was closed.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if s.store == nil {
		return nil
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil
	}
	serviceCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	go s.autoUpdateLoop(serviceCtx)
	go s.startupRefresh(serviceCtx)
	return nil
}

func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// List returns the stored rule set definitions in display order.
func (s *Service) List() []model.RuleSet {
	if s.store == nil {
		return []model.RuleSet{}
	}
	return s.store.ListRuleSets()
}

// Save validates and persists a rule set definition. The downloaded rules are
// not stored; only the source URL, kind, action and schedule are persisted.
func (s *Service) Save(input model.RuleSet) (model.RuleSet, error) {
	if s.store == nil {
		return model.RuleSet{}, errors.New("规则集服务未初始化")
	}

	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return model.RuleSet{}, errors.New("规则集名称不能为空")
	}
	input.URL = strings.TrimSpace(input.URL)
	if input.URL == "" {
		return model.RuleSet{}, errors.New("规则集地址不能为空")
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Host == "" {
		return model.RuleSet{}, errors.New("规则集地址格式无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return model.RuleSet{}, errors.New("规则集地址必须以 http:// 或 https:// 开头")
	}

	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	switch kind {
	case "", rulesetdomain.KindDomain, rulesetdomain.KindIP, rulesetdomain.KindCombined:
	default:
		return model.RuleSet{}, errors.New("规则集类型必须是 domain、ip 或 combined")
	}
	input.Kind = kind

	action := strings.ToLower(strings.TrimSpace(input.Action))
	switch action {
	case "", "proxy", "direct", "block":
	default:
		return model.RuleSet{}, errors.New("规则动作必须是 proxy、direct 或 block")
	}
	input.Action = action

	if input.IntervalMinutes != 0 && (input.IntervalMinutes < minIntervalMinutes || input.IntervalMinutes > maxIntervalMinutes) {
		return model.RuleSet{}, fmt.Errorf("更新间隔必须为 0 或 %d 到 %d 分钟", minIntervalMinutes, maxIntervalMinutes)
	}
	return s.store.SaveRuleSet(input)
}

// Delete removes a rule set and its in-memory expanded rules.
func (s *Service) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("规则集 ID 不能为空")
	}
	if s.store == nil {
		return errors.New("规则集服务未初始化")
	}
	err := s.store.DeleteRuleSet(id)
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
	return err
}

// Update downloads and parses one rule set. On success the parsed rules are
// cached for configuration generation and the status reports the entry count
// and refresh time; on failure the error is recorded on the rule set and the
// previously downloaded rules stay active.
func (s *Service) Update(id string) (model.RuleSet, error) {
	return s.update(context.Background(), id)
}

func (s *Service) update(ctx context.Context, id string) (model.RuleSet, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.RuleSet{}, errors.New("规则集 ID 不能为空")
	}
	if s.store == nil {
		return model.RuleSet{}, errors.New("规则集服务未初始化")
	}
	set, err := s.store.FindRuleSet(id)
	if err != nil {
		return model.RuleSet{}, fmt.Errorf("规则集不存在: %s", id)
	}

	rules, fetchErr := rulesetdomain.FetchRules(ctx, set.URL, set.Kind)
	if fetchErr != nil {
		if ctx.Err() != nil {
			// Shutdown cancelled the download; not a rule set failure.
			return set, fetchErr
		}
		_ = s.store.SetRuleSetStatus(id, 0, "", fetchErr.Error())
		set.RuleCount = 0
		set.LastError = fetchErr.Error()
		return set, fetchErr
	}

	rulesetdomain.NameRules(rules, set.Name)
	for i := range rules {
		rules[i].Action = set.Action
		rules[i].Enabled = true
	}
	s.storeRules(id, rules)

	now := model.NowRFC3339()
	if err := s.store.SetRuleSetStatus(id, rulesetdomain.CountValues(rules), now, ""); err != nil {
		return model.RuleSet{}, err
	}
	updated, findErr := s.store.FindRuleSet(id)
	if findErr != nil {
		set.RuleCount = rulesetdomain.CountValues(rules)
		set.UpdatedAt = now
		set.LastError = ""
		return set, nil
	}
	return updated, nil
}

// UpdateAll refreshes every enabled rule set and returns how many succeeded.
// Failed downloads are collected into the returned error so one broken endpoint
// does not stop the others.
func (s *Service) UpdateAll() (int, error) {
	if s.store == nil {
		return 0, errors.New("规则集服务未初始化")
	}
	updated := 0
	failures := make([]string, 0)
	for _, set := range s.store.ListRuleSets() {
		if !set.Enabled {
			continue
		}
		if _, err := s.Update(set.ID); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", set.Name, err))
			continue
		}
		updated++
	}
	if len(failures) > 0 {
		return updated, fmt.Errorf("%d 个规则集更新失败: %s", len(failures), strings.Join(failures, "；"))
	}
	return updated, nil
}

// ExpandedRules returns the routing rules of every enabled rule set that has
// downloaded content, in the stored rule set order. The rules carry the action
// currently configured for their rule set so a Save takes effect immediately.
func (s *Service) ExpandedRules() []model.RoutingRule {
	if s.store == nil {
		return nil
	}
	sets := s.store.ListRuleSets()
	out := make([]model.RoutingRule, 0)
	for _, set := range sets {
		if !set.Enabled {
			continue
		}
		cached := s.cachedRules(set.ID)
		if len(cached) == 0 {
			continue
		}
		rules := append([]model.RoutingRule(nil), cached...)
		rulesetdomain.NameRules(rules, set.Name)
		for i := range rules {
			rules[i].Action = set.Action
			rules[i].Enabled = true
		}
		out = append(out, rules...)
	}
	return out
}

func (s *Service) storeRules(id string, rules []model.RoutingRule) {
	copied := append([]model.RoutingRule(nil), rules...)
	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string][]model.RoutingRule)
	}
	s.cache[id] = copied
	s.mu.Unlock()
}

func (s *Service) cachedRules(id string) []model.RoutingRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache[id]
}

// autoUpdateLoop refreshes due rule sets once a minute until the service
// context is cancelled.
func (s *Service) autoUpdateLoop(ctx context.Context) {
	ticker := time.NewTicker(autoUpdatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if s.runAutoUpdate(ctx, now) {
				emitRuleSetChanged()
			}
		}
	}
}

// startupRefresh repopulates the in-memory cache after the app starts.
func (s *Service) startupRefresh(ctx context.Context) {
	attempted := s.refreshNeverDownloaded(ctx)
	if s.runAutoUpdate(ctx, time.Now()) {
		attempted = true
	}
	if attempted {
		emitRuleSetChanged()
	}
}

// refreshNeverDownloaded fetches enabled rule sets that have no successful
// download yet so their rules are available right after start-up.
func (s *Service) refreshNeverDownloaded(ctx context.Context) bool {
	if s.store == nil || ctx.Err() != nil {
		return false
	}
	attempted := false
	for _, set := range s.store.ListRuleSets() {
		if ctx.Err() != nil {
			return attempted
		}
		if !set.Enabled || strings.TrimSpace(set.UpdatedAt) != "" {
			continue
		}
		attempted = true
		_, _ = s.update(ctx, set.ID)
	}
	return attempted
}

// runAutoUpdate performs one scheduling tick and reports whether any download
// was attempted. Download errors are recorded on the rule set; one failing
// endpoint never blocks the others.
func (s *Service) runAutoUpdate(ctx context.Context, now time.Time) bool {
	if s.store == nil {
		return false
	}
	due := dueRuleSets(s.store.ListRuleSets(), now)
	if len(due) == 0 {
		return false
	}
	for _, id := range due {
		if ctx.Err() != nil {
			return true
		}
		_, _ = s.update(ctx, id)
	}
	return true
}

// dueRuleSets returns the IDs of rule sets due for an automatic refresh at now.
// Rules:
//   - only Enabled rule sets with their own AutoUpdate switch on are considered;
//   - the effective interval is IntervalMinutes, or defaultIntervalMinutes when
//     the rule set has none;
//   - a rule set is due when UpdatedAt is empty (never downloaded) or when at
//     least the effective interval has elapsed since UpdatedAt;
//   - an unparsable UpdatedAt is treated as never downloaded so a hand-edited
//     data file cannot stop refreshes forever.
func dueRuleSets(sets []model.RuleSet, now time.Time) []string {
	due := make([]string, 0, len(sets))
	for _, set := range sets {
		if !set.Enabled || !set.AutoUpdate {
			continue
		}
		interval := set.IntervalMinutes
		if interval <= 0 {
			interval = defaultIntervalMinutes
		}
		if interval < minIntervalMinutes {
			interval = minIntervalMinutes
		}
		if strings.TrimSpace(set.UpdatedAt) == "" {
			due = append(due, set.ID)
			continue
		}
		updatedAt, err := time.Parse(time.RFC3339, set.UpdatedAt)
		if err != nil {
			due = append(due, set.ID)
			continue
		}
		if now.Sub(updatedAt) >= time.Duration(interval)*time.Minute {
			due = append(due, set.ID)
		}
	}
	return due
}

func emitRuleSetChanged() {
	if app := application.Get(); app != nil {
		app.Event.Emit(ruleSetChangedEvent)
	}
}
