package nodegroup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/model"
	proxyservice "github.com/MardukPrime/Fast-V/internal/service/proxy"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Compile-time check that the real proxy service satisfies the dependency this
// service expects; bootstrap wires the two together with New().
var _ proxyService = (*proxyservice.ProxyService)(nil)

// fakeProxy is a scriptable stand-in for the proxy service. It records switch
// and URL test calls so the service behavior can be asserted without starting a
// real core.
type fakeProxy struct {
	mu          sync.Mutex
	latencies   map[string]int64
	failures    map[string]string
	switchCalls []string
	state       model.ProxyState

	switchErr error
	testCalls [][]string
}

func newFakeProxy() *fakeProxy {
	return &fakeProxy{
		latencies: map[string]int64{},
		failures:  map[string]string{},
	}
}

func (f *fakeProxy) SwitchNode(id string) (model.ProxyState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.switchCalls = append(f.switchCalls, id)
	if f.switchErr != nil {
		return model.ProxyState{}, f.switchErr
	}
	state := f.state
	state.NodeID = id
	if state.Status == "" {
		state.Status = "running"
	}
	return state, nil
}

func (f *fakeProxy) State() model.ProxyState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakeProxy) TestURL(ids []string) []model.NodeLatency {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.testCalls = append(f.testCalls, append([]string(nil), ids...))
	results := make([]model.NodeLatency, 0, len(ids))
	for _, id := range ids {
		result := model.NodeLatency{NodeID: id, CheckedAt: "2026-01-01T00:00:00Z"}
		if message, ok := f.failures[id]; ok {
			result.Error = message
		} else {
			result.LatencyMS = f.latencies[id]
		}
		results = append(results, result)
	}
	return results
}

func (f *fakeProxy) switchedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.switchCalls...)
}

func (f *fakeProxy) testRounds() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, 0, len(f.testCalls))
	for _, call := range f.testCalls {
		out = append(out, append([]string(nil), call...))
	}
	return out
}

// newTestService builds a service backed by a fresh temporary store.
func newTestService(t *testing.T) (*Service, *fakeProxy, *store.Store) {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatalf("store.NewStoreAt() error = %v", err)
	}
	fake := newFakeProxy()
	return New(st, fake), fake, st
}

// addTestNodes seeds manual nodes with stable IDs and names.
func addTestNodes(t *testing.T, st *store.Store, ids ...string) {
	t.Helper()
	nodes := make([]store.Node, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, store.Node{
			ID:       id,
			Name:     "节点-" + id,
			Protocol: "socks",
			Raw:      "raw-" + id,
			Enabled:  true,
		})
	}
	if err := st.AddManualNodes(nodes); err != nil {
		t.Fatalf("AddManualNodes() error = %v", err)
	}
}

// seededStore writes a data file with the given groups and two nodes and loads
// it, which lets tests control UpdatedAt values that the normal store API always
// overwrites.
func seededStore(t *testing.T, groups ...model.NodeGroup) *store.Store {
	t.Helper()
	document := map[string]any{
		"version": 9,
		"nodes": []map[string]any{
			{"id": "n1", "name": "节点-n1"},
			{"id": "n2", "name": "节点-n2"},
		},
		"nodeGroups": groups,
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal seed document: %v", err)
	}
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write seed store: %v", err)
	}
	st, err := store.NewStoreAt(path)
	if err != nil {
		t.Fatalf("open seeded store: %v", err)
	}
	return st
}

// waitFor polls condition until it is true or timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestListReturnsEmptySlice(t *testing.T) {
	svc, _, _ := newTestService(t)
	groups := svc.List()
	if groups == nil {
		t.Fatal("List() = nil, want empty slice")
	}
	if len(groups) != 0 {
		t.Fatalf("List() = %v, want empty", groups)
	}
}

func TestSaveCreatesAndUpdatesGroup(t *testing.T) {
	svc, _, st := newTestService(t)
	addTestNodes(t, st, "n1", "n2")

	saved, err := svc.Save(model.NodeGroup{
		Name:    "  亚太  ",
		Type:    "manual",
		NodeIDs: []string{"n1", "n2"},
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.ID == "" {
		t.Fatal("Save() returned empty ID")
	}
	if saved.Name != "亚太" {
		t.Fatalf("Save() name = %q, want %q", saved.Name, "亚太")
	}
	if saved.UpdatedAt == "" {
		t.Fatal("Save() did not set UpdatedAt")
	}
	if len(saved.NodeIDs) != 2 {
		t.Fatalf("Save() nodeIDs = %v, want 2 entries", saved.NodeIDs)
	}

	saved.Name = "亚太 2"
	updated, err := svc.Save(saved)
	if err != nil {
		t.Fatalf("Save(existing) error = %v", err)
	}
	if updated.ID != saved.ID || updated.Name != "亚太 2" {
		t.Fatalf("Save(existing) = %+v, want ID %q name %q", updated, saved.ID, "亚太 2")
	}
	if groups := svc.List(); len(groups) != 1 {
		t.Fatalf("List() has %d groups after update, want 1", len(groups))
	}
}

func TestSaveValidation(t *testing.T) {
	cases := []struct {
		name    string
		input   model.NodeGroup
		wantErr string
	}{
		{
			name:    "empty name",
			input:   model.NodeGroup{Type: "manual", NodeIDs: []string{"n1"}},
			wantErr: "分组名称不能为空",
		},
		{
			name:    "unknown type",
			input:   model.NodeGroup{Name: "g", Type: "auto", NodeIDs: []string{"n1"}},
			wantErr: "分组类型",
		},
		{
			name:    "empty type",
			input:   model.NodeGroup{Name: "g", NodeIDs: []string{"n1"}},
			wantErr: "分组类型",
		},
		{
			name:    "interval too small",
			input:   model.NodeGroup{Name: "g", Type: "urltest", NodeIDs: []string{"n1"}, IntervalMinutes: 4},
			wantErr: "测速间隔",
		},
		{
			name:    "interval too large",
			input:   model.NodeGroup{Name: "g", Type: "urltest", NodeIDs: []string{"n1"}, IntervalMinutes: 1441},
			wantErr: "测速间隔",
		},
		{
			name:    "unknown node",
			input:   model.NodeGroup{Name: "g", Type: "manual", NodeIDs: []string{"ghost"}},
			wantErr: "节点不存在: ghost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, st := newTestService(t)
			addTestNodes(t, st, "n1")
			_, err := svc.Save(tc.input)
			if err == nil {
				t.Fatalf("Save(%+v) error = nil, want %q", tc.input, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Save(%+v) error = %q, want it to contain %q", tc.input, err.Error(), tc.wantErr)
			}
			if groups := svc.List(); len(groups) != 0 {
				t.Fatalf("invalid group was persisted: %v", groups)
			}
		})
	}
}

func TestSaveAcceptsIntervalBoundaries(t *testing.T) {
	for _, interval := range []int{0, 5, 1440} {
		t.Run(fmt.Sprintf("interval_%d", interval), func(t *testing.T) {
			svc, _, st := newTestService(t)
			addTestNodes(t, st, "n1")
			saved, err := svc.Save(model.NodeGroup{
				Name:            "g",
				Type:            "urltest",
				NodeIDs:         []string{"n1"},
				IntervalMinutes: interval,
			})
			if err != nil {
				t.Fatalf("Save(interval=%d) error = %v", interval, err)
			}
			if saved.IntervalMinutes != interval {
				t.Fatalf("IntervalMinutes = %d, want %d", saved.IntervalMinutes, interval)
			}
		})
	}
}

func TestSaveURLTestFallsBackToSettingsTestURL(t *testing.T) {
	svc, _, st := newTestService(t)
	addTestNodes(t, st, "n1")

	settings := st.Settings()
	settings.TestURL = "https://example.com/generate_204"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings() error = %v", err)
	}

	auto, err := svc.Save(model.NodeGroup{
		Name:            "auto",
		Type:            "urltest",
		NodeIDs:         []string{"n1"},
		IntervalMinutes: 10,
	})
	if err != nil {
		t.Fatalf("Save(urltest) error = %v", err)
	}
	if auto.TestURL != settings.TestURL {
		t.Fatalf("urltest TestURL = %q, want fallback %q", auto.TestURL, settings.TestURL)
	}

	manual, err := svc.Save(model.NodeGroup{Name: "manual", Type: "manual", NodeIDs: []string{"n1"}})
	if err != nil {
		t.Fatalf("Save(manual) error = %v", err)
	}
	if manual.TestURL != "" {
		t.Fatalf("manual TestURL = %q, want empty", manual.TestURL)
	}
}

func TestDelete(t *testing.T) {
	svc, _, st := newTestService(t)
	addTestNodes(t, st, "n1")
	group, err := svc.Save(model.NodeGroup{Name: "g", Type: "manual", NodeIDs: []string{"n1"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := svc.Delete(group.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if groups := svc.List(); len(groups) != 0 {
		t.Fatalf("List() = %v after Delete, want empty", groups)
	}
	if err := svc.Delete(group.ID); err == nil || !strings.Contains(err.Error(), "分组不存在") {
		t.Fatalf("Delete(missing) error = %v, want 分组不存在", err)
	}
}

func TestApplySwitchesNodeInsideGroup(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1", "n2", "n3")
	group, err := svc.Save(model.NodeGroup{Name: "g", Type: "manual", NodeIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	state, err := svc.Apply(group.ID, "n2")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if state.NodeID != "n2" || state.Status != "running" {
		t.Fatalf("Apply() state = %+v, want running node n2", state)
	}
	if got := fake.switchedIDs(); !reflect.DeepEqual(got, []string{"n2"}) {
		t.Fatalf("SwitchNode calls = %v, want [n2]", got)
	}

	if _, err := svc.Apply(group.ID, "n3"); err == nil || !strings.Contains(err.Error(), "节点不在分组内") {
		t.Fatalf("Apply(node outside group) error = %v, want 节点不在分组内", err)
	}
	if _, err := svc.Apply("missing", "n1"); err == nil || !strings.Contains(err.Error(), "分组不存在") {
		t.Fatalf("Apply(missing group) error = %v, want 分组不存在", err)
	}
}

func TestApplyAllowsManualPickInURLTestGroup(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1", "n2")
	group, err := svc.Save(model.NodeGroup{
		Name:            "auto",
		Type:            "urltest",
		NodeIDs:         []string{"n1", "n2"},
		IntervalMinutes: 10,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := svc.Apply(group.ID, "n1"); err != nil {
		t.Fatalf("Apply(urltest group) error = %v", err)
	}
	if got := fake.switchedIDs(); !reflect.DeepEqual(got, []string{"n1"}) {
		t.Fatalf("SwitchNode calls = %v, want [n1]", got)
	}
}

func TestApplyPropagatesSwitchError(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1")
	group, err := svc.Save(model.NodeGroup{Name: "g", Type: "manual", NodeIDs: []string{"n1"}})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	fake.switchErr = errors.New("proxy busy")
	if _, err := svc.Apply(group.ID, "n1"); err == nil || !strings.Contains(err.Error(), "proxy busy") {
		t.Fatalf("Apply() error = %v, want proxy busy", err)
	}
}

func TestTestGroupSelectsFastestNode(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1", "n2", "n3")
	group, err := svc.Save(model.NodeGroup{
		Name:            "auto",
		Type:            "urltest",
		NodeIDs:         []string{"n1", "n2", "n3"},
		IntervalMinutes: 10,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	fake.latencies["n1"] = 120
	fake.latencies["n2"] = 45
	fake.failures["n3"] = "timeout"

	result, err := svc.TestGroup(group.ID)
	if err != nil {
		t.Fatalf("TestGroup() error = %v", err)
	}
	if result.NodeID != "n2" {
		t.Fatalf("TestGroup() nodeID = %q, want n2", result.NodeID)
	}
	if result.NodeName != "节点-n2" {
		t.Fatalf("TestGroup() nodeName = %q, want 节点-n2", result.NodeName)
	}
	if result.LatencyMS != 45 {
		t.Fatalf("TestGroup() latency = %d, want 45", result.LatencyMS)
	}
	if len(result.Results) != 3 {
		t.Fatalf("TestGroup() results = %v, want 3 entries", result.Results)
	}
	if got := fake.testRounds(); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"n1", "n2", "n3"}) {
		t.Fatalf("TestURL calls = %v, want one round over the group nodes", got)
	}
	if got := fake.switchedIDs(); !reflect.DeepEqual(got, []string{"n2"}) {
		t.Fatalf("SwitchNode calls = %v, want [n2]", got)
	}
}

func TestTestGroupAllNodesUnreachable(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1", "n2")
	group, err := svc.Save(model.NodeGroup{
		Name:            "auto",
		Type:            "urltest",
		NodeIDs:         []string{"n1", "n2"},
		IntervalMinutes: 10,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	fake.failures["n1"] = "timeout"
	fake.failures["n2"] = "connection refused"

	result, err := svc.TestGroup(group.ID)
	if err == nil || !strings.Contains(err.Error(), "分组内没有可用节点") {
		t.Fatalf("TestGroup() error = %v, want 分组内没有可用节点", err)
	}
	if len(result.Results) != 2 {
		t.Fatalf("TestGroup() results = %v, want both measurements", result.Results)
	}
	if got := fake.switchedIDs(); len(got) != 0 {
		t.Fatalf("SwitchNode calls = %v, want none", got)
	}
}

func TestTestGroupEmptyGroupDoesNotTestEverything(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1", "n2")
	group, err := svc.Save(model.NodeGroup{
		Name:            "empty",
		Type:            "urltest",
		IntervalMinutes: 10,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := svc.TestGroup(group.ID); err == nil || !strings.Contains(err.Error(), "分组内没有可用节点") {
		t.Fatalf("TestGroup(empty) error = %v, want 分组内没有可用节点", err)
	}
	if got := fake.testRounds(); len(got) != 0 {
		t.Fatalf("TestURL calls = %v, want none for an empty group", got)
	}
}

func TestTestGroupReportsSwitchFailure(t *testing.T) {
	svc, fake, st := newTestService(t)
	addTestNodes(t, st, "n1")
	group, err := svc.Save(model.NodeGroup{
		Name:            "auto",
		Type:            "urltest",
		NodeIDs:         []string{"n1"},
		IntervalMinutes: 10,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	fake.latencies["n1"] = 30
	fake.switchErr = errors.New("proxy busy")

	result, err := svc.TestGroup(group.ID)
	if err == nil || !strings.Contains(err.Error(), "切换节点失败") {
		t.Fatalf("TestGroup() error = %v, want 切换节点失败", err)
	}
	if result.NodeID != "n1" || result.LatencyMS != 30 {
		t.Fatalf("TestGroup() result = %+v, want the selected node kept in the result", result)
	}
}

func TestTestGroupUnknownGroup(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.TestGroup("missing"); err == nil || !strings.Contains(err.Error(), "分组不存在") {
		t.Fatalf("TestGroup(missing) error = %v, want 分组不存在", err)
	}
}

func TestGroupDue(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	stamp := func(offset time.Duration) string {
		return now.Add(offset).Format(time.RFC3339)
	}
	cases := []struct {
		name  string
		group model.NodeGroup
		want  bool
	}{
		{
			name:  "manual group is never due",
			group: model.NodeGroup{Type: "manual", IntervalMinutes: 60, UpdatedAt: ""},
			want:  false,
		},
		{
			name:  "disabled interval is never due",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 0, UpdatedAt: ""},
			want:  false,
		},
		{
			name:  "never tested is due",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 30},
			want:  true,
		},
		{
			name:  "unparsable timestamp is due",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 30, UpdatedAt: "yesterday"},
			want:  true,
		},
		{
			name:  "not due before interval elapses",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 30, UpdatedAt: stamp(-29 * time.Minute)},
			want:  false,
		},
		{
			name:  "exactly due at interval",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 30, UpdatedAt: stamp(-30 * time.Minute)},
			want:  true,
		},
		{
			name:  "overdue is due",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 30, UpdatedAt: stamp(-3 * time.Hour)},
			want:  true,
		},
		{
			name:  "timestamp in the future is not due",
			group: model.NodeGroup{Type: "urltest", IntervalMinutes: 30, UpdatedAt: stamp(5 * time.Minute)},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := groupDue(tc.group, now); got != tc.want {
				t.Fatalf("groupDue(%+v) = %v, want %v", tc.group, got, tc.want)
			}
		})
	}
}

func TestBestLatency(t *testing.T) {
	cases := []struct {
		name    string
		results []model.NodeLatency
		wantID  string
		wantOK  bool
	}{
		{name: "empty", results: nil, wantOK: false},
		{
			name: "all failed",
			results: []model.NodeLatency{
				{NodeID: "a", Error: "timeout"},
				{NodeID: "b", LatencyMS: 0},
			},
			wantOK: false,
		},
		{
			name: "zero latency ignored",
			results: []model.NodeLatency{
				{NodeID: "a", LatencyMS: 0},
				{NodeID: "b", LatencyMS: 12},
			},
			wantID: "b",
			wantOK: true,
		},
		{
			name: "fastest wins regardless of order",
			results: []model.NodeLatency{
				{NodeID: "a", LatencyMS: 120},
				{NodeID: "b", LatencyMS: 45},
				{NodeID: "c", LatencyMS: 80},
			},
			wantID: "b",
			wantOK: true,
		},
		{
			name: "tie keeps first entry",
			results: []model.NodeLatency{
				{NodeID: "a", LatencyMS: 40},
				{NodeID: "b", LatencyMS: 40},
			},
			wantID: "a",
			wantOK: true,
		},
		{
			name: "failed fast node does not win",
			results: []model.NodeLatency{
				{NodeID: "a", LatencyMS: 5, Error: "tls handshake"},
				{NodeID: "b", LatencyMS: 60},
			},
			wantID: "b",
			wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := bestLatency(tc.results)
			if ok != tc.wantOK {
				t.Fatalf("bestLatency() ok = %v, want %v", ok, tc.wantOK)
			}
			if got.NodeID != tc.wantID {
				t.Fatalf("bestLatency() nodeID = %q, want %q", got.NodeID, tc.wantID)
			}
		})
	}
}

func TestCheckDueGroupsMeasuresOnlyDueURLTestGroups(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour).Format(time.RFC3339)
	recent := now.Format(time.RFC3339)
	st := seededStore(t,
		model.NodeGroup{ID: "g-due", Name: "到期", Type: "urltest", NodeIDs: []string{"n1", "n2"}, IntervalMinutes: 5, UpdatedAt: old},
		model.NodeGroup{ID: "g-recent", Name: "未到期", Type: "urltest", NodeIDs: []string{"n1"}, IntervalMinutes: 5, UpdatedAt: recent},
		model.NodeGroup{ID: "g-manual", Name: "手动", Type: "manual", NodeIDs: []string{"n1"}, IntervalMinutes: 5, UpdatedAt: old},
		model.NodeGroup{ID: "g-disabled", Name: "关闭", Type: "urltest", NodeIDs: []string{"n1"}, IntervalMinutes: 0, UpdatedAt: old},
	)
	fake := newFakeProxy()
	fake.latencies["n1"] = 80
	fake.latencies["n2"] = 30
	svc := New(st, fake)

	svc.checkDueGroups(context.Background())

	rounds := fake.testRounds()
	if len(rounds) != 1 {
		t.Fatalf("TestURL rounds = %v, want exactly the due group", rounds)
	}
	if !reflect.DeepEqual(rounds[0], []string{"n1", "n2"}) {
		t.Fatalf("TestURL ids = %v, want [n1 n2]", rounds[0])
	}
	if got := fake.switchedIDs(); !reflect.DeepEqual(got, []string{"n2"}) {
		t.Fatalf("SwitchNode calls = %v, want [n2]", got)
	}

	due, err := st.FindNodeGroup("g-due")
	if err != nil {
		t.Fatalf("FindNodeGroup(g-due) error = %v", err)
	}
	if due.UpdatedAt == old {
		t.Fatal("due group UpdatedAt was not refreshed")
	}
	if parsed, err := time.Parse(time.RFC3339, due.UpdatedAt); err != nil || time.Since(parsed) > time.Minute {
		t.Fatalf("due group UpdatedAt = %q, want a fresh timestamp", due.UpdatedAt)
	}
	notDue, err := st.FindNodeGroup("g-recent")
	if err != nil {
		t.Fatalf("FindNodeGroup(g-recent) error = %v", err)
	}
	if notDue.UpdatedAt != recent {
		t.Fatalf("not-due group UpdatedAt = %q, want untouched %q", notDue.UpdatedAt, recent)
	}
	manual, err := st.FindNodeGroup("g-manual")
	if err != nil {
		t.Fatalf("FindNodeGroup(g-manual) error = %v", err)
	}
	if manual.UpdatedAt != old {
		t.Fatalf("manual group UpdatedAt = %q, want untouched %q", manual.UpdatedAt, old)
	}

	// A second check right away must not measure the group again.
	svc.checkDueGroups(context.Background())
	if rounds := fake.testRounds(); len(rounds) != 1 {
		t.Fatalf("TestURL rounds after second check = %v, want 1", rounds)
	}
}

func TestCheckDueGroupsRecordsAttemptWhenMeasurementFails(t *testing.T) {
	old := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	st := seededStore(t,
		model.NodeGroup{ID: "g-fail", Name: "失败", Type: "urltest", NodeIDs: []string{"n1"}, IntervalMinutes: 5, UpdatedAt: old},
	)
	fake := newFakeProxy()
	fake.failures["n1"] = "timeout"
	svc := New(st, fake)

	svc.checkDueGroups(context.Background())

	if rounds := fake.testRounds(); len(rounds) != 1 {
		t.Fatalf("TestURL rounds = %v, want 1", rounds)
	}
	if got := fake.switchedIDs(); len(got) != 0 {
		t.Fatalf("SwitchNode calls = %v, want none", got)
	}
	group, err := st.FindNodeGroup("g-fail")
	if err != nil {
		t.Fatalf("FindNodeGroup(g-fail) error = %v", err)
	}
	if group.UpdatedAt == old {
		t.Fatal("failed measurement did not advance UpdatedAt, the group would be retested every tick")
	}
	svc.checkDueGroups(context.Background())
	if rounds := fake.testRounds(); len(rounds) != 1 {
		t.Fatalf("TestURL rounds after second check = %v, want 1", rounds)
	}
}

func TestServiceStartupMeasuresNeverTestedGroupsImmediately(t *testing.T) {
	st := seededStore(t,
		model.NodeGroup{ID: "g-auto", Name: "自动", Type: "urltest", NodeIDs: []string{"n1"}, IntervalMinutes: 5},
	)
	fake := newFakeProxy()
	fake.latencies["n1"] = 20
	svc := New(st, fake)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatalf("ServiceStartup() error = %v", err)
	}
	defer func() {
		if err := svc.ServiceShutdown(); err != nil {
			t.Errorf("ServiceShutdown() error = %v", err)
		}
	}()

	waitFor(t, 3*time.Second, func() bool {
		group, err := st.FindNodeGroup("g-auto")
		if err != nil {
			return false
		}
		parsed, err := time.Parse(time.RFC3339, group.UpdatedAt)
		return err == nil && time.Since(parsed) < time.Minute
	}, "the never-tested url-test group to be measured at startup")

	if rounds := fake.testRounds(); len(rounds) != 1 {
		t.Fatalf("TestURL rounds = %v, want 1", rounds)
	}
	if got := fake.switchedIDs(); !reflect.DeepEqual(got, []string{"n1"}) {
		t.Fatalf("SwitchNode calls = %v, want [n1]", got)
	}
}

func TestServiceStartupAndShutdownAreIdempotent(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := svc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatalf("first ServiceStartup() error = %v", err)
	}
	if err := svc.ServiceStartup(ctx, application.ServiceOptions{}); err != nil {
		t.Fatalf("second ServiceStartup() error = %v", err)
	}

	cancel()
	done := make(chan error, 1)
	go func() {
		done <- svc.ServiceShutdown()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServiceShutdown() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServiceShutdown() did not return after context cancellation")
	}
	if err := svc.ServiceShutdown(); err != nil {
		t.Fatalf("second ServiceShutdown() error = %v", err)
	}
}
