package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"errors"
	"github.com/MardukPrime/Fast-V/internal/model"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const storeVersion = 1

// Aliases keep the storage package decoupled from frontend service packages.
type Settings = model.Settings
type Subscription = model.Subscription
type Node = model.Node

type storeData struct {
	Version       int                 `json:"version"`
	Settings      Settings            `json:"settings"`
	Subscriptions []Subscription      `json:"subscriptions"`
	Nodes         []Node              `json:"nodes"`
	ActiveNodeID  string              `json:"activeNodeId"`
	RoutingRules  []model.RoutingRule `json:"routingRules"`
}

// Store persists subscriptions, nodes and settings in a user-local JSON file.
// It is intentionally small for the first dashboard/subscription milestone.
type Store struct {
	mu   sync.RWMutex
	path string
	data storeData
}

func DefaultSettings() Settings {
	return Settings{
		Theme:                 "dark",
		Language:              "zh-CN",
		AutoUpdate:            false,
		UpdateIntervalMinutes: 360,
		ProxyMode:             "rule",
		CoreAutoUpdate:        true,
		CoreType:              "xray",
		XrayLocalPort:         10808,
		XrayLogLevel:          "warning",
		XrayMuxEnabled:        false,
		XrayMuxConcurrency:    8,
		XraySniffing:          true,
		TestURL:               "https://www.gstatic.com/generate_204",
	}
}

func defaultStoreData() storeData {
	return storeData{
		Version:       storeVersion,
		Settings:      DefaultSettings(),
		Subscriptions: []Subscription{},
		Nodes:         []Node{},
	}
}

func NewStore() (*Store, error) {
	if dir := os.Getenv("FASTV_DATA_DIR"); dir != "" {
		return NewStoreAt(filepath.Join(dir, "data.json"))
	}

	candidates := make([]string, 0, 3)
	if configDir, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(configDir, "FastV", "data.json"))
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		candidates = append(candidates, filepath.Join(cacheDir, "FastV", "data.json"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, ".fastv-data", "data.json"))
	}

	var lastErr error
	for _, candidate := range candidates {
		store, err := NewStoreAt(candidate)
		if err == nil {
			return store, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no writable data directory available")
	}
	return nil, lastErr
}
func NewStoreAt(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	s := &Store{
		path: path,
		data: defaultStoreData(),
	}
	raw, err := os.ReadFile(s.path)
	if err == nil {
		var loaded storeData
		if err := json.Unmarshal(raw, &loaded); err != nil {
			return nil, err
		}
		s.data = mergeStoreData(loaded)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return s, nil
}

func mergeStoreData(loaded storeData) storeData {
	defaults := defaultStoreData()
	if loaded.Version == 0 {
		loaded.Version = storeVersion
	}
	loaded.Settings = normalizeSettings(loaded.Settings, defaults.Settings)
	if loaded.Subscriptions == nil {
		loaded.Subscriptions = []Subscription{}
	}
	if loaded.Nodes == nil {
		loaded.Nodes = []Node{}
	}

	for i := range loaded.Nodes {
		if loaded.Nodes[i].SourceKey == "" {
			key := loaded.Nodes[i].Raw
			if key == "" {
				key = loaded.Nodes[i].ID
			}
			loaded.Nodes[i].SourceKey = model.SourceKey(key)
			loaded.Nodes[i].Enabled = true
		}
	}

	for i := range loaded.Subscriptions {
		if loaded.Subscriptions[i].NodeCount == 0 {
			for _, node := range loaded.Nodes {
				if node.SubscriptionID == loaded.Subscriptions[i].ID {
					loaded.Subscriptions[i].NodeCount++
				}
			}
		}
	}
	return loaded
}
func normalizeSettings(value, fallback Settings) Settings {
	if value.Theme == "" {
		value.Theme = fallback.Theme
	}
	if value.Language == "" {
		value.Language = fallback.Language
	}
	if value.UpdateIntervalMinutes == 0 {
		value.UpdateIntervalMinutes = fallback.UpdateIntervalMinutes
	}
	if value.ProxyMode == "" {
		value.ProxyMode = fallback.ProxyMode
	}
	if value.CoreType == "" {
		value.CoreType = fallback.CoreType
	}
	if value.XrayLocalPort == 0 {
		value.XrayLocalPort = fallback.XrayLocalPort
	}
	if value.XrayLogLevel == "" {
		value.XrayLogLevel = fallback.XrayLogLevel
	}
	if value.TestURL == "" {
		value.TestURL = fallback.TestURL
	}
	if value.XrayMuxConcurrency == 0 {
		value.XrayMuxConcurrency = fallback.XrayMuxConcurrency
	}
	return value
}

func (s *Store) Snapshot() storeData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := s.data
	out.Subscriptions = append([]Subscription(nil), s.data.Subscriptions...)
	out.Nodes = append([]Node(nil), s.data.Nodes...)
	return out
}

func (s *Store) update(fn func(*storeData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := fn(&s.data); err != nil {
		return err
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(s.path)
		if err := os.Rename(tmp, s.path); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DataDir() string {
	return filepath.Dir(s.path)
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Settings
}

func (s *Store) SaveSettings(settings Settings) error {
	return s.update(func(data *storeData) error {
		data.Settings = settings
		return nil
	})
}

func (s *Store) ListSubscriptions() []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Subscription(nil), s.data.Subscriptions...)
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out
}

func (s *Store) FindSubscription(id string) (Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.data.Subscriptions {
		if sub.ID == id {
			return sub, nil
		}
	}
	return Subscription{}, errors.New("subscription not found")
}

func (s *Store) AddSubscription(sub Subscription, nodes []Node) error {
	return s.update(func(data *storeData) error {
		sub.NodeCount = len(nodes)
		data.Subscriptions = append(data.Subscriptions, sub)
		data.Nodes = append(data.Nodes, nodes...)
		if data.ActiveNodeID == "" && len(nodes) > 0 {
			data.ActiveNodeID = nodes[0].ID
		}
		return nil
	})
}

func (s *Store) ReplaceSubscriptionNodes(id string, nodes []Node) error {
	return s.update(func(data *storeData) error {
		found := false
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID != id {
				continue
			}
			found = true
			data.Subscriptions[i].NodeCount = len(nodes)
			data.Subscriptions[i].LastError = ""
			data.Subscriptions[i].UpdatedAt = nowRFC3339()
			break
		}
		if !found {
			return errors.New("subscription not found")
		}

		oldBySource := make(map[string]Node)
		for _, node := range data.Nodes {
			if node.SubscriptionID == id && node.SourceKey != "" {
				oldBySource[node.SourceKey] = node
			}
		}
		for i := range nodes {
			if old, ok := oldBySource[nodes[i].SourceKey]; ok {
				nodes[i].ID = old.ID
				nodes[i].Enabled = old.Enabled
			}
		}

		filtered := make([]Node, 0, len(data.Nodes))
		for _, node := range data.Nodes {
			if node.SubscriptionID != id {
				filtered = append(filtered, node)
			}
		}
		data.Nodes = append(filtered, nodes...)

		activeExists := false
		firstEnabled := ""
		for _, node := range nodes {
			if node.ID == data.ActiveNodeID {
				activeExists = true
			}
			if firstEnabled == "" && node.Enabled {
				firstEnabled = node.ID
			}
		}
		if !activeExists {
			data.ActiveNodeID = firstEnabled
		}
		return nil
	})
}

func (s *Store) SetSubscriptionError(id string, message string) error {
	return s.update(func(data *storeData) error {
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID == id {
				data.Subscriptions[i].LastError = message
				return nil
			}
		}
		return errors.New("subscription not found")
	})
}

func (s *Store) DeleteSubscription(id string) error {
	return s.update(func(data *storeData) error {
		found := false
		subs := make([]Subscription, 0, len(data.Subscriptions))
		for _, sub := range data.Subscriptions {
			if sub.ID == id {
				found = true
				continue
			}
			subs = append(subs, sub)
		}
		if !found {
			return errors.New("subscription not found")
		}
		data.Subscriptions = subs

		nodes := make([]Node, 0, len(data.Nodes))
		removedActive := false
		for _, node := range data.Nodes {
			if node.SubscriptionID == id {
				if node.ID == data.ActiveNodeID {
					removedActive = true
				}
				continue
			}
			nodes = append(nodes, node)
		}
		data.Nodes = nodes
		if removedActive {
			data.ActiveNodeID = ""
		}
		return nil
	})
}

func (s *Store) NodesForSubscription(id string) []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Node, 0)
	for _, node := range s.data.Nodes {
		if node.SubscriptionID == id {
			out = append(out, node)
		}
	}
	return out
}

func (s *Store) ListNodes() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Node(nil), s.data.Nodes...)
}

func (s *Store) FindNode(id string) (Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, node := range s.data.Nodes {
		if node.ID == id {
			return node, nil
		}
	}
	return Node{}, errors.New("node not found")
}

func (s *Store) SetNodeEnabled(id string, enabled bool) (Node, error) {
	var updated Node
	err := s.update(func(data *storeData) error {
		for i := range data.Nodes {
			if data.Nodes[i].ID != id {
				continue
			}
			data.Nodes[i].Enabled = enabled
			updated = data.Nodes[i]
			if !enabled && data.ActiveNodeID == id {
				data.ActiveNodeID = ""
			}
			return nil
		}
		return errors.New("node not found")
	})
	return updated, err
}

func (s *Store) ListRoutingRules() []model.RoutingRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]model.RoutingRule(nil), s.data.RoutingRules...)
}

func (s *Store) SaveRoutingRule(rule model.RoutingRule) (model.RoutingRule, error) {
	var saved model.RoutingRule
	err := s.update(func(data *storeData) error {
		if rule.ID == "" {
			rule.ID = model.NewID()
			if !rule.Enabled {
				rule.Enabled = true
			}
		}
		for i := range data.RoutingRules {
			if data.RoutingRules[i].ID == rule.ID {
				data.RoutingRules[i] = rule
				saved = rule
				return nil
			}
		}
		data.RoutingRules = append(data.RoutingRules, rule)
		saved = rule
		return nil
	})
	return saved, err
}

func (s *Store) DeleteRoutingRule(id string) error {
	return s.update(func(data *storeData) error {
		found := false
		rules := make([]model.RoutingRule, 0, len(data.RoutingRules))
		for _, rule := range data.RoutingRules {
			if rule.ID == id {
				found = true
				continue
			}
			rules = append(rules, rule)
		}
		if !found {
			return errors.New("routing rule not found")
		}
		data.RoutingRules = rules
		return nil
	})
}

func (s *Store) ReorderRoutingRules(ids []string) error {
	return s.update(func(data *storeData) error {
		byID := make(map[string]model.RoutingRule, len(data.RoutingRules))
		for _, rule := range data.RoutingRules {
			byID[rule.ID] = rule
		}
		ordered := make([]model.RoutingRule, 0, len(data.RoutingRules))
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if rule, ok := byID[id]; ok {
				ordered = append(ordered, rule)
				seen[id] = struct{}{}
			}
		}
		for _, rule := range data.RoutingRules {
			if _, ok := seen[rule.ID]; !ok {
				ordered = append(ordered, rule)
			}
		}
		data.RoutingRules = ordered
		return nil
	})
}

func (s *Store) ReplaceRoutingRules(rules []model.RoutingRule) error {
	return s.update(func(data *storeData) error {
		data.RoutingRules = append([]model.RoutingRule(nil), rules...)
		return nil
	})
}
func (s *Store) UpdateNode(id string, name string, protocol string, address string, port int, raw string, outboundJSON string) (Node, error) {
	var updated Node
	err := s.update(func(data *storeData) error {
		for i := range data.Nodes {
			if data.Nodes[i].ID != id {
				continue
			}
			data.Nodes[i].Name = name
			data.Nodes[i].Protocol = protocol
			data.Nodes[i].Address = address
			data.Nodes[i].Port = port
			data.Nodes[i].Raw = raw
			data.Nodes[i].OutboundJSON = outboundJSON
			updated = data.Nodes[i]
			return nil
		}
		return errors.New("node not found")
	})
	return updated, err
}
func (s *Store) DeleteNode(id string) error {
	return s.update(func(data *storeData) error {
		removedSubID := ""
		nodes := make([]Node, 0, len(data.Nodes))
		for _, node := range data.Nodes {
			if node.ID == id {
				removedSubID = node.SubscriptionID
				continue
			}
			nodes = append(nodes, node)
		}
		if removedSubID == "" {
			return errors.New("node not found")
		}
		data.Nodes = nodes
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID == removedSubID {
				data.Subscriptions[i].NodeCount--
				break
			}
		}
		if data.ActiveNodeID == id {
			data.ActiveNodeID = ""
		}
		return nil
	})
}

func (s *Store) SetActiveNode(id string) (Node, error) {
	var active Node
	err := s.update(func(data *storeData) error {
		for _, node := range data.Nodes {
			if node.ID != id {
				continue
			}
			if !node.Enabled {
				return errors.New("node is disabled")
			}
			data.ActiveNodeID = id
			active = node
			return nil
		}
		return errors.New("node not found")
	})
	return active, err
}

func (s *Store) ActiveNode() (Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, node := range s.data.Nodes {
		if node.ID == s.data.ActiveNodeID {
			return node, true
		}
	}
	return Node{}, false
}
func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
