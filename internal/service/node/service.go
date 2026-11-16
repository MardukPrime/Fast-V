package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
	proxyruntime "github.com/MardukPrime/Fast-V/internal/proxy"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

// CorePathProvider reports the locally installed proxy core. NodeService only
// needs the executable path to spawn a temporary core for URL latency tests.
type CorePathProvider interface {
	Info() model.CoreInfo
}

// NodeService manages parsed nodes independently from their subscription source.
type NodeService struct {
	store *store.Store
	core  CorePathProvider
}

func New(store *store.Store, core CorePathProvider) *NodeService {
	return &NodeService{store: store, core: core}
}

func (s *NodeService) ServiceName() string {
	return "NodeService"
}

func (s *NodeService) List(filter model.NodeFilter) []model.NodeView {
	snapshot := s.store.Snapshot()
	subNames := make(map[string]string, len(snapshot.Subscriptions))
	for _, sub := range snapshot.Subscriptions {
		subNames[sub.ID] = sub.Name
	}

	search := strings.ToLower(strings.TrimSpace(filter.Search))
	protocol := strings.ToLower(strings.TrimSpace(filter.Protocol))
	nodes := append([]model.Node(nil), snapshot.Nodes...)
	// User-defined order (drag and drop) wins over subscription grouping; the
	// stable sort keeps insertion order for nodes that were never reordered.
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].Order < nodes[j].Order
	})
	out := make([]model.NodeView, 0, len(nodes))
	for _, item := range nodes {
		if filter.SubscriptionID != "" && item.SubscriptionID != filter.SubscriptionID {
			continue
		}
		if protocol != "" && strings.ToLower(item.Protocol) != protocol {
			continue
		}
		if search != "" {
			haystack := strings.ToLower(strings.Join([]string{
				item.Name,
				item.Protocol,
				item.Address,
				subNames[item.SubscriptionID],
			}, " "))
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		out = append(out, model.NodeViewFrom(item, subNames[item.SubscriptionID], snapshot.ActiveNodeID))
	}
	return out
}

func (s *NodeService) Active() (*model.NodeView, error) {
	item, ok := s.store.ActiveNode()
	if !ok {
		return nil, errors.New("no active node selected")
	}
	return s.view(item), nil
}

func (s *NodeService) SetActive(id string) (model.NodeView, error) {
	item, err := s.store.SetActiveNode(id)
	if err != nil {
		return model.NodeView{}, err
	}
	return *s.view(item), nil
}

func (s *NodeService) SetEnabled(id string, enabled bool) (model.NodeView, error) {
	item, err := s.store.SetNodeEnabled(id, enabled)
	if err != nil {
		return model.NodeView{}, err
	}
	return *s.view(item), nil
}

// SetFavorite marks or unmarks a node as favorite. Favorites survive
// subscription refreshes.
func (s *NodeService) SetFavorite(id string, favorite bool) (model.NodeView, error) {
	item, err := s.store.SetNodeFavorite(id, favorite)
	if err != nil {
		return model.NodeView{}, err
	}
	return *s.view(item), nil
}

// Reorder persists a drag-and-drop ordering for the node list.
func (s *NodeService) Reorder(ids []string) error {
	return s.store.ReorderNodes(ids)
}

// ImportLinks parses pasted share links (multi-line text, clipboard content or a
// whole base64 subscription) and stores them as manual nodes. Links that are
// already known are skipped, unusable lines are reported in Errors.
func (s *NodeService) ImportLinks(text string) (model.NodeImportResult, error) {
	result := model.NodeImportResult{
		Nodes:  make([]model.NodeView, 0),
		Errors: make([]string, 0),
	}
	if strings.TrimSpace(text) == "" {
		return result, errors.New("分享链接内容不能为空")
	}

	parsed, issues := subdomain.ParseLinksWithIssues(text)
	result.Errors = append(result.Errors, issues...)
	if len(parsed) == 0 {
		return result, nil
	}

	known := make(map[string]struct{})
	for _, item := range s.store.ListNodes() {
		if item.SourceKey != "" {
			known[item.SourceKey] = struct{}{}
		}
	}

	created := make([]model.Node, 0, len(parsed))
	for _, item := range parsed {
		raw := strings.TrimSpace(item.Raw)
		if raw == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("节点 %q 缺少分享链接", item.Name))
			continue
		}
		sourceKey := model.SourceKey(raw)
		if _, ok := known[sourceKey]; ok {
			result.Skipped++
			continue
		}
		known[sourceKey] = struct{}{}
		created = append(created, model.Node{
			ID:             model.NewID(),
			SubscriptionID: "",
			Name:           item.Name,
			Protocol:       item.Protocol,
			Address:        item.Address,
			Port:           item.Port,
			Raw:            raw,
			Enabled:        true,
			Favorite:       false,
			SourceKey:      sourceKey,
		})
	}
	if len(created) == 0 {
		return result, nil
	}
	if err := s.store.AddManualNodes(created); err != nil {
		return result, err
	}

	result.Imported = len(created)
	for _, item := range created {
		node, err := s.store.FindNode(item.ID)
		if err != nil {
			continue
		}
		result.Nodes = append(result.Nodes, *s.view(node))
	}
	return result, nil
}

// ExportLinks returns share links for the requested node ids (all nodes when ids
// is empty). The original raw link is reused when it still matches the node
// protocol, otherwise it is rebuilt from the extracted node configuration.
func (s *NodeService) ExportLinks(ids []string) ([]model.NodeLink, error) {
	nodes := s.store.ListNodes()
	if len(ids) > 0 {
		byID := make(map[string]model.Node, len(nodes))
		for _, item := range nodes {
			byID[item.ID] = item
		}
		selected := make([]model.Node, 0, len(ids))
		for _, id := range ids {
			if item, ok := byID[id]; ok {
				selected = append(selected, item)
			}
		}
		nodes = selected
	}

	links := make([]model.NodeLink, 0, len(nodes))
	for _, item := range nodes {
		link, err := exportNodeLink(item)
		if err != nil {
			return nil, err
		}
		links = append(links, model.NodeLink{ID: item.ID, Name: item.Name, Link: link})
	}
	return links, nil
}

func exportNodeLink(item model.Node) (string, error) {
	raw := strings.TrimSpace(item.Raw)
	if raw != "" && linkSchemeMatches(raw, item.Protocol) {
		return raw, nil
	}
	cfg, err := nodeconfig.Extract(raw, item.Protocol)
	if err != nil {
		return "", fmt.Errorf("节点 %q 的分享链接无法导出: %w", item.Name, err)
	}
	cfg.Name = item.Name
	link, err := nodeconfig.BuildRaw(cfg)
	if err != nil {
		return "", fmt.Errorf("节点 %q 的分享链接无法生成: %w", item.Name, err)
	}
	return link, nil
}

func linkSchemeMatches(raw, protocol string) bool {
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return false
	}
	return normalizeLinkProtocol(scheme) == normalizeLinkProtocol(protocol)
}

func normalizeLinkProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "ss" {
		return "shadowsocks"
	}
	return value
}

func (s *NodeService) Config(id string) (model.NodeConfig, error) {
	item, err := s.store.FindNode(id)
	if err != nil {
		return model.NodeConfig{}, err
	}
	cfg, err := nodeconfig.Extract(item.Raw, item.Protocol)
	if err != nil {
		return model.NodeConfig{}, err
	}
	cfg.ID = item.ID
	cfg.Name = item.Name
	cfg.OutboundJSON = item.OutboundJSON
	return cfg, nil
}

func (s *NodeService) Preview(input model.NodeConfig) (model.NodeConfigPreview, error) {
	raw, err := nodeconfig.BuildRaw(input)
	if err != nil {
		return model.NodeConfigPreview{}, err
	}
	outbound, err := nodeconfig.BuildOutbound(input)
	if err != nil {
		return model.NodeConfigPreview{}, err
	}
	rawJSON, err := json.MarshalIndent(outbound, "", "  ")
	if err != nil {
		return model.NodeConfigPreview{}, err
	}
	return model.NodeConfigPreview{RawLink: raw, OutboundJSON: string(rawJSON)}, nil
}

func (s *NodeService) SaveConfig(input model.NodeConfig) (model.NodeView, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Protocol = strings.ToLower(strings.TrimSpace(input.Protocol))
	input.Address = strings.TrimSpace(input.Address)
	input.OutboundJSON = strings.TrimSpace(input.OutboundJSON)
	if input.Name == "" {
		input.Name = input.Address
	}
	raw, err := nodeconfig.BuildRaw(input)
	if err != nil {
		return model.NodeView{}, err
	}
	if input.OutboundJSON != "" {
		var outbound map[string]any
		if err := json.Unmarshal([]byte(input.OutboundJSON), &outbound); err != nil {
			return model.NodeView{}, errors.New("Xray outbound JSON 格式无效")
		}
		if len(outbound) == 0 {
			return model.NodeView{}, errors.New("Xray outbound JSON 不能为空对象")
		}
		protocol, _ := outbound["protocol"].(string)
		if strings.TrimSpace(protocol) == "" {
			return model.NodeView{}, errors.New("Xray outbound JSON 必须包含 protocol")
		}
	}
	item, err := s.store.UpdateNode(input.ID, input.Name, input.Protocol, input.Address, input.Port, raw, input.OutboundJSON)
	if err != nil {
		return model.NodeView{}, err
	}
	return *s.view(item), nil
}
func (s *NodeService) Delete(id string) error {
	return s.store.DeleteNode(id)
}

func (s *NodeService) TestLatency(ids []string) []model.NodeLatency {
	nodes := s.store.ListNodes()
	if len(ids) > 0 {
		wanted := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			wanted[id] = struct{}{}
		}
		filtered := make([]model.Node, 0, len(ids))
		for _, item := range nodes {
			if _, ok := wanted[item.ID]; ok {
				filtered = append(filtered, item)
			}
		}
		nodes = filtered
	}

	results := make([]model.NodeLatency, len(nodes))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	for i, item := range nodes {
		wg.Add(1)
		go func(index int, target model.Node) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index] = testNodeLatency(target)
		}(i, item)
	}
	wg.Wait()
	return results
}

// urlLatencyTestBudget bounds a URL latency request triggered by the UI. It is
// slightly larger than the batch timeout inside the proxy package so the batch
// itself reports the timeout per node.
const urlLatencyTestBudget = 35 * time.Second

// TestURL measures the real proxy-chain latency of the requested nodes: a
// temporary Xray-core process is started with one SOCKS inbound per node and the
// configured test URL is fetched through each node in turn. An empty ids list
// tests every enabled node; unknown or disabled nodes are reported individually
// in Error instead of being skipped.
func (s *NodeService) TestURL(ids []string) []model.NodeLatency {
	ctx, cancel := context.WithTimeout(context.Background(), urlLatencyTestBudget)
	defer cancel()
	// Keep the temporary measurement config next to the normal runtime config so
	// it never depends on a writable OS temp directory.
	tempDir := filepath.Join(s.store.DataDir(), "runtime")
	return proxyruntime.URLTestNodes(ctx, s.core, s.store.ListNodes(), s.store.Settings(), ids, tempDir)
}

func (s *NodeService) view(item model.Node) *model.NodeView {
	subName := ""
	if sub, err := s.store.FindSubscription(item.SubscriptionID); err == nil {
		subName = sub.Name
	}
	activeID := s.store.Snapshot().ActiveNodeID
	view := model.NodeViewFrom(item, subName, activeID)
	return &view
}

func testNodeLatency(item model.Node) model.NodeLatency {
	result := model.NodeLatency{
		NodeID:    item.ID,
		CheckedAt: model.NowRFC3339(),
	}
	address := net.JoinHostPort(item.Address, strconv.Itoa(item.Port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	_ = conn.Close()
	result.LatencyMS = time.Since(start).Milliseconds()
	if result.LatencyMS == 0 {
		result.LatencyMS = 1
	}
	return result
}
