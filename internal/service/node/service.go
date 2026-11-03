package node

import (
	"encoding/json"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/nodeconfig"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// NodeService manages parsed nodes independently from their subscription source.
type NodeService struct {
	store *store.Store
}

func New(store *store.Store) *NodeService {
	return &NodeService{store: store}
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
	out := make([]model.NodeView, 0, len(snapshot.Nodes))
	for _, item := range snapshot.Nodes {
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

	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		if out[i].SubscriptionName != out[j].SubscriptionName {
			return out[i].SubscriptionName < out[j].SubscriptionName
		}
		return out[i].Name < out[j].Name
	})
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
