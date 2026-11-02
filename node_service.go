package main

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NodeService manages parsed nodes independently from their subscription source.
type NodeService struct {
	store *Store
}

func NewNodeService(store *Store) *NodeService {
	return &NodeService{store: store}
}

func (s *NodeService) ServiceName() string {
	return "NodeService"
}

func (s *NodeService) List(filter NodeFilter) []NodeView {
	snapshot := s.store.snapshot()
	subNames := make(map[string]string, len(snapshot.Subscriptions))
	for _, sub := range snapshot.Subscriptions {
		subNames[sub.ID] = sub.Name
	}

	search := strings.ToLower(strings.TrimSpace(filter.Search))
	protocol := strings.ToLower(strings.TrimSpace(filter.Protocol))
	out := make([]NodeView, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		if filter.SubscriptionID != "" && node.SubscriptionID != filter.SubscriptionID {
			continue
		}
		if protocol != "" && strings.ToLower(node.Protocol) != protocol {
			continue
		}
		if search != "" {
			haystack := strings.ToLower(strings.Join([]string{
				node.Name,
				node.Protocol,
				node.Address,
				subNames[node.SubscriptionID],
			}, " "))
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		out = append(out, toNodeView(node, subNames[node.SubscriptionID], snapshot.ActiveNodeID))
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

func (s *NodeService) Active() (*NodeView, error) {
	node, ok := s.store.ActiveNode()
	if !ok {
		return nil, errors.New("no active node selected")
	}
	return s.view(node), nil
}

func (s *NodeService) SetActive(id string) (NodeView, error) {
	node, err := s.store.SetActiveNode(id)
	if err != nil {
		return NodeView{}, err
	}
	return *s.view(node), nil
}

func (s *NodeService) SetEnabled(id string, enabled bool) (NodeView, error) {
	node, err := s.store.SetNodeEnabled(id, enabled)
	if err != nil {
		return NodeView{}, err
	}
	return *s.view(node), nil
}

func (s *NodeService) Delete(id string) error {
	return s.store.DeleteNode(id)
}

func (s *NodeService) TestLatency(ids []string) []NodeLatency {
	nodes := s.store.ListNodes()
	if len(ids) > 0 {
		wanted := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			wanted[id] = struct{}{}
		}
		filtered := make([]Node, 0, len(ids))
		for _, node := range nodes {
			if _, ok := wanted[node.ID]; ok {
				filtered = append(filtered, node)
			}
		}
		nodes = filtered
	}

	results := make([]NodeLatency, len(nodes))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	for i, node := range nodes {
		wg.Add(1)
		go func(index int, item Node) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index] = testNodeLatency(item)
		}(i, node)
	}
	wg.Wait()
	return results
}

func (s *NodeService) view(node Node) *NodeView {
	subName := ""
	if sub, err := s.store.FindSubscription(node.SubscriptionID); err == nil {
		subName = sub.Name
	}
	activeID := s.store.snapshot().ActiveNodeID
	view := toNodeView(node, subName, activeID)
	return &view
}

func testNodeLatency(node Node) NodeLatency {
	result := NodeLatency{
		NodeID:    node.ID,
		CheckedAt: nowRFC3339(),
	}
	address := net.JoinHostPort(node.Address, strconv.Itoa(node.Port))
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
