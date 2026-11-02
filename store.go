package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const storeVersion = 1

type storeData struct {
	Version       int            `json:"version"`
	Settings      Settings       `json:"settings"`
	Subscriptions []Subscription `json:"subscriptions"`
	Nodes         []Node         `json:"nodes"`
}

// Store persists subscriptions, nodes and settings in a user-local JSON file.
// It is intentionally small for the first dashboard/subscription milestone.
type Store struct {
	mu   sync.RWMutex
	path string
	data storeData
}

func defaultSettings() Settings {
	return Settings{
		Theme:                 "dark",
		Language:              "zh-CN",
		AutoUpdate:            false,
		UpdateIntervalMinutes: 360,
		ProxyMode:             "rule",
	}
}

func defaultStoreData() storeData {
	return storeData{
		Version:       storeVersion,
		Settings:      defaultSettings(),
		Subscriptions: []Subscription{},
		Nodes:         []Node{},
	}
}

func NewStore() (*Store, error) {
	if dir := os.Getenv("FASTV_DATA_DIR"); dir != "" {
		return newStoreAt(filepath.Join(dir, "data.json"))
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
		store, err := newStoreAt(candidate)
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
func newStoreAt(path string) (*Store, error) {
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
	return value
}

func (s *Store) snapshot() storeData {
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
		filtered := data.Nodes[:0]
		for _, node := range data.Nodes {
			if node.SubscriptionID != id {
				filtered = append(filtered, node)
			}
		}
		data.Nodes = append(filtered, nodes...)
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
		subs := data.Subscriptions[:0]
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
		nodes := data.Nodes[:0]
		for _, node := range data.Nodes {
			if node.SubscriptionID != id {
				nodes = append(nodes, node)
			}
		}
		data.Nodes = nodes
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
