package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"errors"
	"fmt"
	"github.com/MardukPrime/Fast-V/internal/model"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const storeVersion = 4

// History retention limits. Traffic is aggregated per local calendar day and
// latency samples keep the newest entries; both are exposed by the services so
// the frontend clamps request parameters against the same limits.
const (
	// MaxTrafficHistoryDays is how many daily traffic aggregates are retained.
	MaxTrafficHistoryDays = 180
	// MaxLatencyHistorySamples is how many URL latency samples are retained.
	MaxLatencyHistorySamples = 300
	// localDateLayout is the day key used for daily traffic aggregation.
	localDateLayout = "2006-01-02"
)

// MaxTagLength is the maximum number of characters (runes) a node tag may
// contain.
const MaxTagLength = 24

// NormalizeTag trims surrounding whitespace and validates a user-defined node
// tag. It is the single source of truth for the tag rules, shared by the store,
// subscription refreshes and the node service:
//   - surrounding whitespace is trimmed;
//   - an empty result means "no tag" (clearing a tag);
//   - at most MaxTagLength characters (runes) are allowed;
//   - newlines and control characters are rejected.
func NormalizeTag(tag string) (string, error) {
	normalized := strings.TrimSpace(tag)
	if normalized == "" {
		return "", nil
	}
	for _, r := range normalized {
		if r == '\n' || r == '\r' || unicode.IsControl(r) {
			return "", errors.New("标签不能包含换行或控制字符")
		}
	}
	if utf8.RuneCountInString(normalized) > MaxTagLength {
		return "", fmt.Errorf("标签最多 %d 个字符", MaxTagLength)
	}
	return normalized, nil
}

// normalizeLoadedTag applies the tag rules to a record read from disk or a
// backup. Invalid persisted tags are dropped instead of breaking the load, and
// submit values are always validated on write.
func normalizeLoadedTag(tag string) string {
	normalized, err := NormalizeTag(tag)
	if err != nil {
		return ""
	}
	return normalized
}

// Aliases keep the storage package decoupled from frontend service packages.
type Settings = model.Settings
type Subscription = model.Subscription
type Node = model.Node

type storeData struct {
	Version        int                   `json:"version"`
	Settings       Settings              `json:"settings"`
	Subscriptions  []Subscription        `json:"subscriptions"`
	Nodes          []Node                `json:"nodes"`
	ActiveNodeID   string                `json:"activeNodeId"`
	RoutingRules   []model.RoutingRule   `json:"routingRules"`
	TrafficHistory []model.DailyTraffic  `json:"trafficHistory"`
	LatencyHistory []model.LatencySample `json:"latencyHistory"`
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
		Theme:                 "light",
		Language:              "zh-CN",
		LayoutMode:            "sidebar",
		DockTransparency:      28,
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
		XrayDNSStrategy:       "UseIP",
		XrayDNSServers:        "1.1.1.1,8.8.8.8",
		XrayDomainStrategy:    "IPIfNonMatch",
		XrayTunMTU:            1500,
		TestURL:               "https://www.gstatic.com/generate_204",
		XrayAPIEnabled:        false,
		XrayAPIPort:           10085,
		SystemProxyMode:       "system",
		TunMode:               false,
		AllowLan:              false,
		InboundAuthEnabled:    false,
		InboundUsername:       "",
		InboundPassword:       "",
		CloseToTray:           false,
		MinimizeToTray:        false,
		AutoCheckUpdate:       true,
		StatsEnabled:          true,

		AutoSelectEnabled:         false,
		AutoSelectIntervalMinutes: 10,
		AutoSelectToleranceMS:     80,
		FailoverEnabled:           false,
		LoadBalanceEnabled:        false,
		LoadBalanceMode:           "leastPing",
	}
}

func defaultStoreData() storeData {
	return storeData{
		Version:        storeVersion,
		Settings:       DefaultSettings(),
		Subscriptions:  []Subscription{},
		Nodes:          []Node{},
		TrafficHistory: []model.DailyTraffic{},
		LatencyHistory: []model.LatencySample{},
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
	// v2 changed the default theme from dark to light. Installs that were still
	// carrying the old default are migrated once; an explicit choice made after
	// the upgrade is stored at v2 and is never touched again.
	if loaded.Version < 2 && loaded.Settings.Theme == "dark" {
		loaded.Settings.Theme = "light"
	}
	// v3 added live traffic statistics. Existing installs opt in automatically;
	// the field can be switched off again in settings.
	if loaded.Version < 3 {
		loaded.Settings.StatsEnabled = true
	}
	if loaded.Version < 4 {
		loaded.Settings.AutoCheckUpdate = true
	}
	if loaded.Version < storeVersion {
		loaded.Version = storeVersion
	}
	loaded.Settings = normalizeSettings(loaded.Settings, defaults.Settings)
	if loaded.Subscriptions == nil {
		loaded.Subscriptions = []Subscription{}
	}
	if loaded.Nodes == nil {
		loaded.Nodes = []Node{}
	}
	// History is optional data: repair and cap what was loaded so a hand-edited
	// or truncated file can never break the normal store contract.
	loaded.TrafficHistory = normalizeTrafficHistory(loaded.TrafficHistory)
	loaded.LatencyHistory = normalizeLatencyHistory(loaded.LatencyHistory)

	for i := range loaded.Nodes {
		// Tags are user data that must always satisfy the write-time rules, even
		// when the file was hand edited.
		loaded.Nodes[i].Tag = normalizeLoadedTag(loaded.Nodes[i].Tag)
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
	if value.SystemProxyMode == "" {
		value.SystemProxyMode = fallback.SystemProxyMode
	}
	if value.LayoutMode == "" {
		value.LayoutMode = fallback.LayoutMode
	}
	if value.DockTransparency == 0 {
		value.DockTransparency = fallback.DockTransparency
	}
	if value.AutoSelectIntervalMinutes <= 0 {
		value.AutoSelectIntervalMinutes = fallback.AutoSelectIntervalMinutes
	}
	if value.AutoSelectToleranceMS < 0 {
		value.AutoSelectToleranceMS = fallback.AutoSelectToleranceMS
	}
	if value.LoadBalanceMode == "" {
		value.LoadBalanceMode = fallback.LoadBalanceMode
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
	if value.XrayAPIPort == 0 {
		value.XrayAPIPort = fallback.XrayAPIPort
	}
	if value.TestURL == "" {
		value.TestURL = fallback.TestURL
	}
	if value.XrayDNSStrategy == "" {
		value.XrayDNSStrategy = fallback.XrayDNSStrategy
	}
	if value.XrayDNSServers == "" {
		value.XrayDNSServers = fallback.XrayDNSServers
	}
	if value.XrayDomainStrategy == "" {
		value.XrayDomainStrategy = fallback.XrayDomainStrategy
	}
	if value.XrayTunMTU == 0 {
		value.XrayTunMTU = fallback.XrayTunMTU
	}
	if value.XrayMuxConcurrency == 0 {
		value.XrayMuxConcurrency = fallback.XrayMuxConcurrency
	}
	return value
}

func (s *Store) Snapshot() storeData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() storeData {
	out := s.data
	out.Subscriptions = append([]Subscription{}, s.data.Subscriptions...)
	out.Nodes = append([]Node{}, s.data.Nodes...)
	out.RoutingRules = append([]model.RoutingRule{}, s.data.RoutingRules...)
	out.TrafficHistory = append([]model.DailyTraffic{}, s.data.TrafficHistory...)
	out.LatencyHistory = append([]model.LatencySample{}, s.data.LatencyHistory...)
	return out
}

// Export returns the complete persisted configuration (settings, subscriptions,
// nodes, routing rules and the active node) as versioned JSON. The output can be
// imported again on the same or another installation with Restore.
func (s *Store) Export() ([]byte, error) {
	s.mu.RLock()
	data := s.snapshotLocked()
	s.mu.RUnlock()

	data.Version = storeVersion
	return json.MarshalIndent(data, "", "  ")
}

// Restore replaces the persisted configuration with a JSON backup produced by
// Export. The backup is fully parsed and validated before anything is written,
// so corrupt or incompatible input never leaves the store partially modified.
// The returned summary counts the restored subscriptions, nodes and rules.
func (s *Store) Restore(data []byte) (model.BackupResult, error) {
	restored, err := parseBackup(data)
	if err != nil {
		return model.BackupResult{}, err
	}

	result := model.BackupResult{
		RestoredSubscriptions: len(restored.Subscriptions),
		RestoredNodes:         len(restored.Nodes),
		RestoredRules:         len(restored.RoutingRules),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.data
	s.data = restored
	if err := s.saveLocked(); err != nil {
		s.data = previous
		return model.BackupResult{}, fmt.Errorf("保存恢复的配置失败: %w", err)
	}
	return result, nil
}

// backupDocument is the raw backup shape used by Restore. The history sections
// are kept as raw JSON so a missing or malformed history can never make an
// otherwise valid configuration backup unrestorable; downtime data is optional
// and must never block a configuration import.
type backupDocument struct {
	Version        int                 `json:"version"`
	Settings       Settings            `json:"settings"`
	Subscriptions  []Subscription      `json:"subscriptions"`
	Nodes          []Node              `json:"nodes"`
	ActiveNodeID   string              `json:"activeNodeId"`
	RoutingRules   []model.RoutingRule `json:"routingRules"`
	TrafficHistory json.RawMessage     `json:"trafficHistory"`
	LatencyHistory json.RawMessage     `json:"latencyHistory"`
}

func parseBackup(data []byte) (storeData, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return storeData{}, errors.New("备份内容为空")
	}
	if trimmed == "null" {
		return storeData{}, errors.New("备份内容缺少配置数据")
	}

	var document backupDocument
	if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
		return storeData{}, fmt.Errorf("备份文件不是有效的 JSON: %w", err)
	}
	parsed := storeData{
		Version:        document.Version,
		Settings:       document.Settings,
		Subscriptions:  document.Subscriptions,
		Nodes:          document.Nodes,
		ActiveNodeID:   document.ActiveNodeID,
		RoutingRules:   document.RoutingRules,
		TrafficHistory: parseTrafficHistory(document.TrafficHistory),
		LatencyHistory: parseLatencyHistory(document.LatencyHistory),
	}
	if parsed.Version <= 0 {
		return storeData{}, errors.New("备份文件缺少有效的版本信息")
	}
	if parsed.Version > storeVersion {
		return storeData{}, fmt.Errorf("备份文件版本过新（%d），当前最高支持版本 %d", parsed.Version, storeVersion)
	}
	if parsed.Subscriptions == nil {
		parsed.Subscriptions = []Subscription{}
	}
	if parsed.Nodes == nil {
		parsed.Nodes = []Node{}
	}
	if parsed.RoutingRules == nil {
		parsed.RoutingRules = []model.RoutingRule{}
	}
	if err := validateBackup(parsed); err != nil {
		return storeData{}, err
	}

	// Reuse the normal load path so old backups are migrated and settings are
	// normalized exactly like data read from disk.
	restored := mergeStoreData(parsed)
	restored.Version = storeVersion
	normalizeRestoredData(&restored)
	return restored, nil
}

// parseTrafficHistory decodes the optional traffic history section of a backup.
// A missing, null or malformed section is treated as empty so damaged
// throughput data never blocks restoring the configuration itself.
func parseTrafficHistory(raw json.RawMessage) []model.DailyTraffic {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []model.DailyTraffic{}
	}
	var entries []model.DailyTraffic
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		return []model.DailyTraffic{}
	}
	return entries
}

// parseLatencyHistory decodes the optional latency history section of a backup.
// Like the traffic section it degrades to an empty list instead of failing.
func parseLatencyHistory(raw json.RawMessage) []model.LatencySample {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []model.LatencySample{}
	}
	var samples []model.LatencySample
	if err := json.Unmarshal([]byte(trimmed), &samples); err != nil {
		return []model.LatencySample{}
	}
	return samples
}

func validateBackup(data storeData) error {
	subscriptionIDs := make(map[string]struct{}, len(data.Subscriptions))
	for i, sub := range data.Subscriptions {
		if strings.TrimSpace(sub.ID) == "" {
			return fmt.Errorf("备份文件中第 %d 个订阅缺少 ID", i+1)
		}
		if _, ok := subscriptionIDs[sub.ID]; ok {
			return fmt.Errorf("备份文件包含重复的订阅 ID: %s", sub.ID)
		}
		subscriptionIDs[sub.ID] = struct{}{}
	}

	nodeIDs := make(map[string]struct{}, len(data.Nodes))
	for i, node := range data.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("备份文件中第 %d 个节点缺少 ID", i+1)
		}
		if _, ok := nodeIDs[node.ID]; ok {
			return fmt.Errorf("备份文件包含重复的节点 ID: %s", node.ID)
		}
		nodeIDs[node.ID] = struct{}{}
		if node.SubscriptionID != "" {
			if _, ok := subscriptionIDs[node.SubscriptionID]; !ok {
				return fmt.Errorf("节点 %s 引用了不存在的订阅 %s", node.ID, node.SubscriptionID)
			}
		}
	}

	ruleIDs := make(map[string]struct{}, len(data.RoutingRules))
	for i, rule := range data.RoutingRules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("备份文件中第 %d 条路由规则缺少 ID", i+1)
		}
		if _, ok := ruleIDs[rule.ID]; ok {
			return fmt.Errorf("备份文件包含重复的路由规则 ID: %s", rule.ID)
		}
		ruleIDs[rule.ID] = struct{}{}
	}
	return nil
}

// normalizeRestoredData repairs fields that are derived from other records
// (node counts and the active node reference) so a hand-edited backup cannot
// leave the store in an inconsistent state.
func normalizeRestoredData(data *storeData) {
	counts := make(map[string]int, len(data.Subscriptions))
	for _, node := range data.Nodes {
		if node.SubscriptionID == "" {
			continue
		}
		counts[node.SubscriptionID]++
	}
	for i := range data.Subscriptions {
		data.Subscriptions[i].NodeCount = counts[data.Subscriptions[i].ID]
	}

	if data.ActiveNodeID == "" {
		return
	}
	active, ok := findNodeByID(data.Nodes, data.ActiveNodeID)
	if !ok || !active.Enabled {
		data.ActiveNodeID = ""
	}
}

func findNodeByID(nodes []Node, id string) (Node, bool) {
	for _, node := range nodes {
		if node.ID == id {
			return node, true
		}
	}
	return Node{}, false
}

// normalizeTrafficHistory repairs a traffic history slice loaded from disk or a
// backup: entries without a usable date are dropped, duplicate days are summed,
// the result is sorted by date and only the newest MaxTrafficHistoryDays days are
// kept. It is idempotent for data produced by AddTrafficBytes, so Export/Restore
// round trips stay stable.
func normalizeTrafficHistory(entries []model.DailyTraffic) []model.DailyTraffic {
	out := []model.DailyTraffic{}
	if len(entries) == 0 {
		return out
	}
	byDate := make(map[string]model.DailyTraffic, len(entries))
	for _, entry := range entries {
		date := normalizeLocalDate(entry.Date)
		if date == "" {
			continue
		}
		current := byDate[date]
		current.Date = date
		if entry.UplinkBytes > 0 {
			current.UplinkBytes += entry.UplinkBytes
		}
		if entry.DownlinkBytes > 0 {
			current.DownlinkBytes += entry.DownlinkBytes
		}
		byDate[date] = current
	}
	if len(byDate) == 0 {
		return out
	}
	dates := make([]string, 0, len(byDate))
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	if len(dates) > MaxTrafficHistoryDays {
		dates = dates[len(dates)-MaxTrafficHistoryDays:]
	}
	out = make([]model.DailyTraffic, 0, len(dates))
	for _, date := range dates {
		out = append(out, byDate[date])
	}
	return out
}

// normalizeLatencyHistory repairs a latency sample slice loaded from disk or a
// backup: whitespace is trimmed, negative latencies are clamped, samples without
// any node reference are dropped and only the newest MaxLatencyHistorySamples
// samples are kept in their original order.
func normalizeLatencyHistory(samples []model.LatencySample) []model.LatencySample {
	out := make([]model.LatencySample, 0, len(samples))
	for _, sample := range samples {
		sample.NodeID = strings.TrimSpace(sample.NodeID)
		sample.NodeName = strings.TrimSpace(sample.NodeName)
		if sample.NodeID == "" && sample.NodeName == "" {
			continue
		}
		if sample.LatencyMS < 0 {
			sample.LatencyMS = 0
		}
		out = append(out, sample)
	}
	if len(out) > MaxLatencyHistorySamples {
		out = append([]model.LatencySample(nil), out[len(out)-MaxLatencyHistorySamples:]...)
	}
	return out
}

// normalizeLocalDate validates and canonicalizes a "YYYY-MM-DD" day key.
func normalizeLocalDate(value string) string {
	parsed, err := time.Parse(localDateLayout, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.Format(localDateLayout)
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
		nextOrder := nextNodeOrder(data.Nodes)
		for i := range nodes {
			// Tags are user data owned by the node, never by the subscription
			// payload, so newly added subscription nodes always start without one.
			nodes[i].Tag = ""
			if nodes[i].Order == 0 {
				nodes[i].Order = nextOrder
				nextOrder++
			}
		}
		sub.NodeCount = len(nodes)
		data.Subscriptions = append(data.Subscriptions, sub)
		data.Nodes = append(data.Nodes, nodes...)
		if data.ActiveNodeID == "" && len(nodes) > 0 {
			data.ActiveNodeID = nodes[0].ID
		}
		return nil
	})
}

// AddManualNodes appends nodes created by pasting share links. Manual nodes are
// not owned by any subscription, so subscription refreshes never remove them.
func (s *Store) AddManualNodes(nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	// Imported nodes normally carry no tag; a caller-provided tag is normalized
	// with the same rules as SetNodeTag so invalid tags never reach the store.
	for i := range nodes {
		normalized, err := NormalizeTag(nodes[i].Tag)
		if err != nil {
			return err
		}
		nodes[i].Tag = normalized
	}
	return s.update(func(data *storeData) error {
		nextOrder := nextNodeOrder(data.Nodes)
		for i := range nodes {
			if nodes[i].ID == "" {
				nodes[i].ID = newID()
			}
			if nodes[i].SourceKey == "" {
				key := nodes[i].Raw
				if key == "" {
					key = nodes[i].ID
				}
				nodes[i].SourceKey = model.SourceKey(key)
			}
			if nodes[i].Order == 0 {
				nodes[i].Order = nextOrder
				nextOrder++
			}
		}
		data.Nodes = append(data.Nodes, nodes...)
		if data.ActiveNodeID == "" {
			for _, node := range nodes {
				if node.Enabled {
					data.ActiveNodeID = node.ID
					break
				}
			}
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
		nextOrder := nextNodeOrder(data.Nodes)
		for i := range nodes {
			if old, ok := oldBySource[nodes[i].SourceKey]; ok {
				nodes[i].ID = old.ID
				nodes[i].Enabled = old.Enabled
				// Favorites, the user-defined order and tags belong to the node,
				// not to the subscription payload, so they survive every refresh.
				nodes[i].Favorite = old.Favorite
				nodes[i].Order = old.Order
				nodes[i].Tag = old.Tag
				continue
			}
			// A node that appears for the first time has no user data yet.
			nodes[i].Tag = ""
			nodes[i].Order = nextOrder
			nextOrder++
		}

		filtered := make([]Node, 0, len(data.Nodes))
		for _, node := range data.Nodes {
			// Manual nodes (no subscription) and nodes of other subscriptions are
			// never touched by a refresh.
			if node.SubscriptionID == "" || node.SubscriptionID != id {
				filtered = append(filtered, node)
			}
		}
		data.Nodes = append(filtered, nodes...)

		activeExists := false
		for _, node := range data.Nodes {
			if node.ID == data.ActiveNodeID {
				activeExists = true
				break
			}
		}
		if !activeExists {
			firstEnabled := ""
			for _, node := range nodes {
				if node.Enabled {
					firstEnabled = node.ID
					break
				}
			}
			data.ActiveNodeID = firstEnabled
		}
		return nil
	})
}

func (s *Store) SetSubscriptionUsage(id string, uploadBytes, downloadBytes, totalBytes, expireAt int64, updatedAt string) error {
	return s.update(func(data *storeData) error {
		for i := range data.Subscriptions {
			if data.Subscriptions[i].ID == id {
				data.Subscriptions[i].UploadBytes = uploadBytes
				data.Subscriptions[i].DownloadBytes = downloadBytes
				data.Subscriptions[i].TotalBytes = totalBytes
				data.Subscriptions[i].ExpireAt = expireAt
				data.Subscriptions[i].UsageUpdatedAt = updatedAt
				return nil
			}
		}
		return errors.New("subscription not found")
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

// ListNodes returns all nodes in user-defined order. Nodes with the same order
// keep their previous relative position (sort is stable).
func (s *Store) ListNodes() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]Node(nil), s.data.Nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})
	return out
}

// ListTags returns the distinct non-empty tags of all nodes sorted in ascending
// (byte) order. The dictionary is what the UI offers as tag filters.
func (s *Store) ListTags() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]struct{}, len(s.data.Nodes))
	tags := make([]string, 0, len(s.data.Nodes))
	for _, node := range s.data.Nodes {
		if node.Tag == "" {
			continue
		}
		if _, ok := seen[node.Tag]; ok {
			continue
		}
		seen[node.Tag] = struct{}{}
		tags = append(tags, node.Tag)
	}
	sort.Strings(tags)
	return tags
}

// nextNodeOrder returns the order value to use for the next appended node.
func nextNodeOrder(nodes []Node) int {
	next := 0
	for _, node := range nodes {
		if node.Order >= next {
			next = node.Order + 1
		}
	}
	return next
}

// ReorderNodes applies a drag-and-drop order: nodes in ids come first in the
// given order, nodes missing from ids keep their previous relative order at the
// end. Unknown ids are ignored.
func (s *Store) ReorderNodes(ids []string) error {
	return s.update(func(data *storeData) error {
		byID := make(map[string]Node, len(data.Nodes))
		for _, node := range data.Nodes {
			byID[node.ID] = node
		}
		ordered := make([]Node, 0, len(data.Nodes))
		seen := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			node, ok := byID[id]
			if !ok {
				continue
			}
			seen[id] = struct{}{}
			ordered = append(ordered, node)
		}
		remaining := append([]Node(nil), data.Nodes...)
		sort.SliceStable(remaining, func(i, j int) bool {
			return remaining[i].Order < remaining[j].Order
		})
		for _, node := range remaining {
			if _, ok := seen[node.ID]; !ok {
				ordered = append(ordered, node)
			}
		}
		for i := range ordered {
			ordered[i].Order = i
		}
		data.Nodes = ordered
		return nil
	})
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

// SetNodeFavorite marks or unmarks a node as favorite. Favorites are kept by
// subscription refreshes.
func (s *Store) SetNodeFavorite(id string, favorite bool) (Node, error) {
	var updated Node
	err := s.update(func(data *storeData) error {
		for i := range data.Nodes {
			if data.Nodes[i].ID != id {
				continue
			}
			data.Nodes[i].Favorite = favorite
			updated = data.Nodes[i]
			return nil
		}
		return errors.New("node not found")
	})
	return updated, err
}

// SetNodeTag sets or clears (empty tag) the user-defined tag of a node. Tags are
// kept by subscription refreshes like favorites and the user-defined order.
func (s *Store) SetNodeTag(id string, tag string) (Node, error) {
	normalized, err := NormalizeTag(tag)
	if err != nil {
		return Node{}, err
	}
	var updated Node
	err = s.update(func(data *storeData) error {
		for i := range data.Nodes {
			if data.Nodes[i].ID != id {
				continue
			}
			data.Nodes[i].Tag = normalized
			updated = data.Nodes[i]
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
		found := false
		removedSubID := ""
		nodes := make([]Node, 0, len(data.Nodes))
		for _, node := range data.Nodes {
			if node.ID == id {
				found = true
				removedSubID = node.SubscriptionID
				continue
			}
			nodes = append(nodes, node)
		}
		if !found {
			return errors.New("node not found")
		}
		data.Nodes = nodes
		if removedSubID != "" {
			for i := range data.Subscriptions {
				if data.Subscriptions[i].ID == removedSubID {
					data.Subscriptions[i].NodeCount--
					break
				}
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

// AddTrafficBytes adds one traffic delta to the aggregate of the local calendar
// day that contains at. Deltas of the same day are summed, days are kept in
// ascending order and only the newest MaxTrafficHistoryDays days are retained.
// Zero deltas are ignored so a flush with nothing to record does not touch disk.
func (s *Store) AddTrafficBytes(uplink, downlink int64, at time.Time) error {
	if uplink < 0 {
		uplink = 0
	}
	if downlink < 0 {
		downlink = 0
	}
	if uplink == 0 && downlink == 0 {
		return nil
	}
	date := trafficDateKey(at)
	return s.update(func(data *storeData) error {
		data.TrafficHistory = addTrafficBytes(data.TrafficHistory, date, uplink, downlink)
		return nil
	})
}

// trafficDateKey returns the local "YYYY-MM-DD" key of at; the zero time is
// treated as now.
func trafficDateKey(at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	return at.In(time.Local).Format(localDateLayout)
}

// addTrafficBytes upserts one day entry into an ascending history slice and
// trims days beyond the retention limit.
func addTrafficBytes(entries []model.DailyTraffic, date string, uplink, downlink int64) []model.DailyTraffic {
	index := sort.Search(len(entries), func(i int) bool { return entries[i].Date >= date })
	if index < len(entries) && entries[index].Date == date {
		entries[index].UplinkBytes += uplink
		entries[index].DownlinkBytes += downlink
	} else {
		entries = append(entries, model.DailyTraffic{})
		copy(entries[index+1:], entries[index:])
		entries[index] = model.DailyTraffic{Date: date, UplinkBytes: uplink, DownlinkBytes: downlink}
	}
	if len(entries) > MaxTrafficHistoryDays {
		entries = append([]model.DailyTraffic(nil), entries[len(entries)-MaxTrafficHistoryDays:]...)
	}
	return entries
}

// TrafficHistory returns up to days of the newest daily traffic aggregates in
// ascending date order. days <= 0 (or larger than the stored history) returns
// everything that is retained. The result is always a fresh copy.
func (s *Store) TrafficHistory(days int) []model.DailyTraffic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.data.TrafficHistory
	if days <= 0 || days > len(entries) {
		days = len(entries)
	}
	out := make([]model.DailyTraffic, days)
	copy(out, entries[len(entries)-days:])
	return out
}

// AddLatencySamples appends latency samples in one batch write. The newest
// MaxLatencyHistorySamples samples are retained and the input order (newest
// last) is preserved.
func (s *Store) AddLatencySamples(samples []model.LatencySample) error {
	normalized := normalizeLatencyHistory(samples)
	if len(normalized) == 0 {
		return nil
	}
	return s.update(func(data *storeData) error {
		combined := make([]model.LatencySample, 0, len(data.LatencyHistory)+len(normalized))
		combined = append(combined, data.LatencyHistory...)
		combined = append(combined, normalized...)
		if len(combined) > MaxLatencyHistorySamples {
			combined = append([]model.LatencySample(nil), combined[len(combined)-MaxLatencyHistorySamples:]...)
		}
		data.LatencyHistory = combined
		return nil
	})
}

// LatencyHistory returns up to limit of the newest latency samples in their
// recorded order (chronological, newest last). limit <= 0 returns everything that
// is retained. The result is always a fresh copy.
func (s *Store) LatencyHistory(limit int) []model.LatencySample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.data.LatencyHistory
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}
	out := make([]model.LatencySample, limit)
	copy(out, entries[len(entries)-limit:])
	return out
}

// ClearHistory removes all recorded traffic and latency history in one write.
func (s *Store) ClearHistory() error {
	return s.update(func(data *storeData) error {
		data.TrafficHistory = []model.DailyTraffic{}
		data.LatencyHistory = []model.LatencySample{}
		return nil
	})
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
