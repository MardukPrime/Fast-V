// Package nodegroup manages user-defined node groups: manual quick switching
// and URL-latency based automatic selection.
package nodegroup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// nodeGroupTypeManual groups nodes that are only switched to by hand.
	nodeGroupTypeManual = "manual"
	// nodeGroupTypeURLTest groups nodes whose lowest latency node is selected
	// automatically on the configured interval.
	nodeGroupTypeURLTest = "urltest"

	// minGroupIntervalMinutes and maxGroupIntervalMinutes bound the automatic
	// URL test interval of a group. An interval of 0 disables automatic testing.
	minGroupIntervalMinutes = 5
	maxGroupIntervalMinutes = 1440

	// autoTestCheckInterval is how often the background scheduler looks for
	// url-test groups that are due for a new measurement.
	autoTestCheckInterval = 60 * time.Second

	// groupsChangedEvent is broadcast after a scheduled measurement changed the
	// stored group data (measurement time and possibly the active node).
	groupsChangedEvent = "nodegroups:changed"
)

// switcher is the narrow slice of the proxy service used to switch the active
// node.
type switcher interface {
	SwitchNode(id string) (model.ProxyState, error)
	State() model.ProxyState
}

// tester is the narrow slice of the proxy service used to measure real
// proxy-chain URL latency.
type tester interface {
	TestURL(ids []string) []model.NodeLatency
}

// proxyService is the dependency the node group service needs from the proxy
// service. It is intentionally composed of two small interfaces so tests can
// fake switching and measuring independently.
type proxyService interface {
	switcher
	tester
}

// GroupTestResult is the outcome of one manual or scheduled group URL test: the
// selected node plus every per-node measurement that produced it.
type GroupTestResult struct {
	NodeID    string              `json:"nodeId"`
	NodeName  string              `json:"nodeName"`
	LatencyMS int                 `json:"latencyMs"`
	Results   []model.NodeLatency `json:"results"`
}

// Service is the Wails adapter for node group management.
type Service struct {
	store *store.Store
	proxy proxyService

	// testMu keeps URL test rounds single-flight so a scheduled round and a
	// UI-triggered round never measure and switch at the same time.
	testMu sync.Mutex

	lifecycleMu     sync.Mutex
	schedulerCancel context.CancelFunc
	schedulerDone   chan struct{}
}

// New builds the node group service on top of the persistent store and the
// proxy service it drives.
func New(st *store.Store, proxy proxyService) *Service {
	return &Service{store: st, proxy: proxy}
}

// ServiceName implements the Wails service name contract.
func (s *Service) ServiceName() string {
	return "NodeGroupService"
}

// List returns every stored group in user order.
func (s *Service) List() []model.NodeGroup {
	if s == nil || s.store == nil {
		return []model.NodeGroup{}
	}
	return s.store.ListNodeGroups()
}

// Save validates one group and persists it. A missing ID creates a new group,
// an existing ID updates it. Validation errors are returned in Chinese because
// they are shown to the user verbatim.
func (s *Service) Save(input model.NodeGroup) (model.NodeGroup, error) {
	if s == nil || s.store == nil {
		return model.NodeGroup{}, errors.New("节点分组服务未初始化")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return model.NodeGroup{}, errors.New("分组名称不能为空")
	}
	input.Type = strings.TrimSpace(input.Type)
	if input.Type != nodeGroupTypeManual && input.Type != nodeGroupTypeURLTest {
		return model.NodeGroup{}, errors.New("分组类型无效，仅支持 manual 或 urltest")
	}
	if input.IntervalMinutes != 0 &&
		(input.IntervalMinutes < minGroupIntervalMinutes || input.IntervalMinutes > maxGroupIntervalMinutes) {
		return model.NodeGroup{}, fmt.Errorf("测速间隔必须在 0 或 %d 到 %d 分钟之间",
			minGroupIntervalMinutes, maxGroupIntervalMinutes)
	}
	nodes := s.store.ListNodes()
	known := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		known[node.ID] = struct{}{}
	}
	// Copy the id slice before trimming so the caller's slice is never mutated.
	input.NodeIDs = append([]string(nil), input.NodeIDs...)
	for i, id := range input.NodeIDs {
		id = strings.TrimSpace(id)
		input.NodeIDs[i] = id
		if id == "" {
			continue
		}
		if _, ok := known[id]; !ok {
			return model.NodeGroup{}, fmt.Errorf("节点不存在: %s", id)
		}
	}
	input.TestURL = strings.TrimSpace(input.TestURL)
	if input.Type == nodeGroupTypeURLTest && input.TestURL == "" {
		// url-test groups fall back to the global latency test URL so a group
		// never silently measures against an empty target.
		input.TestURL = s.store.Settings().TestURL
	}
	return s.store.SaveNodeGroup(input)
}

// Delete removes one group.
func (s *Service) Delete(id string) error {
	if s == nil || s.store == nil {
		return errors.New("节点分组服务未初始化")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("分组 ID 不能为空")
	}
	if _, err := s.store.FindNodeGroup(id); err != nil {
		return errors.New("分组不存在")
	}
	return s.store.DeleteNodeGroup(id)
}

// Apply switches the active node to one node of the group. Both manual and
// url-test groups allow a manual pick; url-test groups only choose a node
// automatically on their own schedule.
func (s *Service) Apply(groupID, nodeID string) (model.ProxyState, error) {
	if s == nil || s.store == nil || s.proxy == nil {
		return model.ProxyState{}, errors.New("节点分组服务未初始化")
	}
	group, err := s.store.FindNodeGroup(strings.TrimSpace(groupID))
	if err != nil {
		return model.ProxyState{}, errors.New("分组不存在")
	}
	nodeID = strings.TrimSpace(nodeID)
	if !containsNode(group.NodeIDs, nodeID) {
		return model.ProxyState{}, fmt.Errorf("节点不在分组内: %s", nodeID)
	}
	return s.proxy.SwitchNode(nodeID)
}

// TestGroup measures every node of the group with a real URL latency test,
// switches to the fastest reachable node and returns the measurements.
func (s *Service) TestGroup(groupID string) (GroupTestResult, error) {
	if s == nil || s.store == nil || s.proxy == nil {
		return GroupTestResult{}, errors.New("节点分组服务未初始化")
	}
	group, err := s.store.FindNodeGroup(strings.TrimSpace(groupID))
	if err != nil {
		return GroupTestResult{}, errors.New("分组不存在")
	}
	if len(group.NodeIDs) == 0 {
		// An empty id list would mean "every enabled node" to the proxy service,
		// so an empty group must fail before measuring anything.
		return GroupTestResult{}, errors.New("分组内没有可用节点")
	}
	ids := append([]string(nil), group.NodeIDs...)

	s.testMu.Lock()
	defer s.testMu.Unlock()
	results := append([]model.NodeLatency(nil), s.proxy.TestURL(ids)...)
	best, ok := bestLatency(results)
	if !ok {
		return GroupTestResult{Results: results}, errors.New("分组内没有可用节点")
	}
	result := GroupTestResult{
		NodeID:    best.NodeID,
		NodeName:  s.nodeName(best.NodeID),
		LatencyMS: int(best.LatencyMS),
		Results:   results,
	}
	if _, err := s.proxy.SwitchNode(best.NodeID); err != nil {
		return result, fmt.Errorf("切换节点失败: %w", err)
	}
	return result, nil
}

// ServiceStartup starts the background scheduler that periodically re-measures
// url-test groups. The scheduler exits when the lifecycle context is cancelled
// (ServiceShutdown) and never outlives it.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.schedulerCancel != nil {
		return nil
	}
	schedulerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.schedulerCancel = cancel
	s.schedulerDone = done
	go func() {
		defer close(done)
		s.autoTestLoop(schedulerCtx)
	}()
	return nil
}

// ServiceShutdown stops the background scheduler and waits for it to exit.
func (s *Service) ServiceShutdown() error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.Lock()
	cancel, done := s.schedulerCancel, s.schedulerDone
	s.schedulerCancel, s.schedulerDone = nil, nil
	s.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// autoTestLoop checks the stored groups once immediately so groups that were
// never measured (or whose interval elapsed while the application was closed)
// are refreshed without waiting for the first tick, then keeps checking every
// autoTestCheckInterval.
func (s *Service) autoTestLoop(ctx context.Context) {
	s.checkDueGroups(ctx)
	ticker := time.NewTicker(autoTestCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			s.checkDueGroups(ctx)
		}
	}
}

// checkDueGroups measures every url-test group that is due at the current time
// and broadcasts the updated group list once after any group was saved.
func (s *Service) checkDueGroups(ctx context.Context) {
	if s == nil || s.store == nil || s.proxy == nil {
		return
	}
	now := time.Now()
	changed := false
	for _, group := range s.store.ListNodeGroups() {
		if ctx.Err() != nil {
			return
		}
		if s.runScheduledTest(ctx, group, now) {
			changed = true
		}
	}
	if changed {
		s.emitGroups()
	}
}

// runScheduledTest measures one group when it is due and stores the attempt
// time. The attempt time is recorded even when the measurement finds no usable
// node so a failing group is retried on its configured interval instead of on
// every scheduler tick. It reports whether the stored group was updated.
func (s *Service) runScheduledTest(ctx context.Context, group model.NodeGroup, now time.Time) bool {
	if !groupDue(group, now) {
		return false
	}
	// A failed round still consumed the interval; the error is intentionally
	// ignored here because there is no UI to report it to from this loop.
	_, _ = s.TestGroup(group.ID)
	if ctx.Err() != nil {
		return false
	}
	// Re-read the group so a concurrent edit is not overwritten by stale data.
	current, err := s.store.FindNodeGroup(group.ID)
	if err != nil {
		// The group was deleted while it was being measured.
		return false
	}
	if _, err := s.store.SaveNodeGroup(current); err != nil {
		return false
	}
	return true
}

// emitGroups broadcasts the current group list after a scheduled measurement.
// It is a no-op outside a running Wails application so tests stay safe.
func (s *Service) emitGroups() {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(groupsChangedEvent, s.List())
}

// nodeName resolves the display name of a node; unknown nodes yield an empty
// name instead of failing the whole measurement.
func (s *Service) nodeName(id string) string {
	for _, node := range s.store.ListNodes() {
		if node.ID == id {
			return node.Name
		}
	}
	return ""
}

// groupDue reports whether a group needs a new automatic measurement at now.
// Manual groups and groups with automatic testing disabled are never due. A
// url-test group that was never measured (or whose timestamp cannot be parsed)
// is due immediately; otherwise the interval is measured from UpdatedAt.
func groupDue(group model.NodeGroup, now time.Time) bool {
	if group.Type != nodeGroupTypeURLTest || group.IntervalMinutes <= 0 {
		return false
	}
	raw := strings.TrimSpace(group.UpdatedAt)
	if raw == "" {
		return true
	}
	updated, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true
	}
	return !now.Before(updated.Add(time.Duration(group.IntervalMinutes) * time.Minute))
}

// bestLatency picks the fastest successful measurement. Failed entries (error
// text) and non-positive latencies are ignored; ties keep the first entry.
func bestLatency(results []model.NodeLatency) (model.NodeLatency, bool) {
	var best model.NodeLatency
	found := false
	for _, result := range results {
		if result.LatencyMS <= 0 || strings.TrimSpace(result.Error) != "" {
			continue
		}
		if !found || result.LatencyMS < best.LatencyMS {
			best = result
			found = true
		}
	}
	return best, found
}

// containsNode reports whether id is one of ids.
func containsNode(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
