package hotkey

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
)

func TestDefaultBindings(t *testing.T) {
	bindings := DefaultBindings()
	if len(bindings) != 3 {
		t.Fatalf("expected 3 default bindings, got %d", len(bindings))
	}

	want := []struct {
		id        int
		modifiers uint32
		key       uint32
	}{
		{ActionToggleProxy, platform.ModControl | platform.ModShift, 0x46}, // Ctrl+Shift+F
		{ActionCycleMode, platform.ModControl | platform.ModShift, 0x4D},   // Ctrl+Shift+M
		{ActionNextNode, platform.ModControl | platform.ModShift, 0x4E},    // Ctrl+Shift+N
	}

	seen := make(map[int]bool, len(bindings))
	for index, binding := range bindings {
		expected := want[index]
		if binding.ID != expected.id {
			t.Fatalf("binding %d: expected ID %d, got %d", index, expected.id, binding.ID)
		}
		if binding.Modifiers != expected.modifiers {
			t.Fatalf("binding %d: expected modifiers %#x, got %#x", index, expected.modifiers, binding.Modifiers)
		}
		if binding.Key != expected.key {
			t.Fatalf("binding %d: expected key %#x, got %#x", index, expected.key, binding.Key)
		}
		if binding.Modifiers&platform.ModAlt != 0 {
			t.Fatalf("binding %d: default bindings must not use Alt", index)
		}
		if seen[binding.ID] {
			t.Fatalf("duplicate binding ID %d", binding.ID)
		}
		seen[binding.ID] = true
	}
}

func TestDefaultBindingsAreIndependentCopies(t *testing.T) {
	first := DefaultBindings()
	first[0].ID = 99
	first[0].Key = 0

	second := DefaultBindings()
	if second[0].ID != ActionToggleProxy || second[0].Key != virtualKeyF {
		t.Fatalf("DefaultBindings must return a fresh copy, got %+v", second[0])
	}
}

func TestLabels(t *testing.T) {
	labels := Labels()
	want := map[int]string{
		ActionToggleProxy: "切换代理启停",
		ActionCycleMode:   "循环代理模式",
		ActionNextNode:    "切换到下一个启用节点",
	}
	if len(labels) != len(want) {
		t.Fatalf("expected %d labels, got %d: %v", len(want), len(labels), labels)
	}
	for id, expected := range want {
		if labels[id] != expected {
			t.Fatalf("label for action %d: expected %q, got %q", id, expected, labels[id])
		}
	}
	for _, binding := range DefaultBindings() {
		label := strings.TrimSpace(labels[binding.ID])
		if label == "" {
			t.Fatalf("default binding %d has no label", binding.ID)
		}
	}
}

func TestValidateBindings(t *testing.T) {
	if err := ValidateBindings(DefaultBindings()); err != nil {
		t.Fatalf("default bindings must be valid: %v", err)
	}

	if err := ValidateBindings(nil); err == nil || !strings.Contains(err.Error(), "没有可注册的全局快捷键") {
		t.Fatalf("expected an empty-binding error, got %v", err)
	}

	cases := []struct {
		name     string
		bindings []platform.HotkeyBinding
		want     string
	}{
		{
			name:     "zero ID",
			bindings: []platform.HotkeyBinding{{ID: 0, Modifiers: platform.ModControl, Key: virtualKeyF}},
			want:     "ID 不能为空",
		},
		{
			name:     "negative ID",
			bindings: []platform.HotkeyBinding{{ID: -1, Modifiers: platform.ModControl, Key: virtualKeyF}},
			want:     "ID 不能为空",
		},
		{
			name: "duplicate ID",
			bindings: []platform.HotkeyBinding{
				{ID: 7, Modifiers: platform.ModControl, Key: virtualKeyF},
				{ID: 7, Modifiers: platform.ModControl, Key: virtualKeyM},
			},
			want: "ID 重复",
		},
		{
			name:     "missing modifiers",
			bindings: []platform.HotkeyBinding{{ID: 1, Key: virtualKeyF}},
			want:     "缺少修饰键",
		},
		{
			name:     "missing key",
			bindings: []platform.HotkeyBinding{{ID: 1, Modifiers: platform.ModControl}},
			want:     "缺少按键",
		},
	}
	for _, tc := range cases {
		err := ValidateBindings(tc.bindings)
		if err == nil {
			t.Fatalf("%s: expected a validation error", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

// Run must reject a broken table before touching the platform registration
// (which would otherwise try to own real system-wide shortcuts).
func TestRunRejectsInvalidInput(t *testing.T) {
	handler := func(int) {}
	if err := Run(context.Background(), nil, handler); err == nil {
		t.Fatal("expected an error for an empty binding table")
	}
	if err := Run(context.Background(), DefaultBindings(), nil); err == nil || !strings.Contains(err.Error(), "处理函数不能为空") {
		t.Fatalf("expected a handler error, got %v", err)
	}
	duplicated := []platform.HotkeyBinding{
		{ID: 1, Modifiers: platform.ModControl, Key: virtualKeyF},
		{ID: 1, Modifiers: platform.ModControl, Key: virtualKeyM},
	}
	if err := Run(context.Background(), duplicated, handler); err == nil || !strings.Contains(err.Error(), "ID 重复") {
		t.Fatalf("expected a duplicate-ID error, got %v", err)
	}
}

// TestRunRegistersAndStops exercises the real platform registration together
// with the WM_QUIT teardown path. It skips when the environment cannot own the
// shortcut (for example when another program already registered it), because
// that is an environmental limitation rather than a code bug.
func TestRunRegistersAndStops(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("global hotkeys are only supported on Windows")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	// A shortcut the application does not use, so the test never competes with a
	// real FastV instance possibly running in the background.
	bindings := []platform.HotkeyBinding{{
		ID:        101,
		Modifiers: platform.ModControl | platform.ModAlt,
		Key:       0x7B, // VK_F12
	}}
	go func() {
		done <- Run(ctx, bindings, func(int) {})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Skipf("environment cannot register global hotkeys: %v", err)
		}
		t.Fatal("Run returned before the context was cancelled")
	case <-time.After(250 * time.Millisecond):
		// Still blocked: the shortcut was registered and the message loop runs.
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Skipf("environment cannot register global hotkeys: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after the context was cancelled")
	}
}

func TestProxyModes(t *testing.T) {
	modes := ProxyModes()
	want := []string{"rule", "global", "direct"}
	if len(modes) != len(want) {
		t.Fatalf("unexpected proxy modes: %v", modes)
	}
	for index := range want {
		if modes[index] != want[index] {
			t.Fatalf("mode %d: expected %q, got %q", index, want[index], modes[index])
		}
	}

	// The returned slice must not expose the internal cycle.
	modes[0] = "hacked"
	if ProxyModes()[0] != "rule" {
		t.Fatal("ProxyModes must return a copy")
	}
}

func TestNextProxyMode(t *testing.T) {
	cases := []struct {
		current string
		want    string
	}{
		{"rule", "global"},
		{"global", "direct"},
		{"direct", "rule"},
		{"  global  ", "direct"},
		{"", "rule"},
		{"unknown", "rule"},
	}
	for _, tc := range cases {
		if got := NextProxyMode(tc.current); got != tc.want {
			t.Fatalf("NextProxyMode(%q): expected %q, got %q", tc.current, tc.want, got)
		}
	}
}

func TestProxyModeLabel(t *testing.T) {
	cases := map[string]string{
		"rule":    "规则分流",
		"global":  "全局代理",
		"direct":  "全部直连",
		" global": "全局代理",
		"":        "规则分流",
		"custom":  "规则分流",
	}
	for mode, want := range cases {
		if got := ProxyModeLabel(mode); got != want {
			t.Fatalf("ProxyModeLabel(%q): expected %q, got %q", mode, want, got)
		}
	}
}

func TestNextEnabledNode(t *testing.T) {
	nodes := []model.Node{
		{ID: "a", Name: "节点 A", Enabled: true},
		{ID: "b", Name: "节点 B", Enabled: false},
		{ID: "c", Name: "节点 C", Enabled: true},
		{ID: "d", Name: "节点 D", Enabled: true},
	}

	cases := []struct {
		name     string
		activeID string
		wantID   string
	}{
		{"next in order", "a", "c"},
		{"skip disabled nodes", "c", "d"},
		{"wrap around", "d", "a"},
		{"unknown active falls back to first", "missing", "a"},
		{"disabled active falls back to first", "b", "a"},
		{"empty active falls back to first", "", "a"},
	}
	for _, tc := range cases {
		next, ok := NextEnabledNode(nodes, tc.activeID)
		if !ok {
			t.Fatalf("%s: expected an enabled node", tc.name)
		}
		if next.ID != tc.wantID {
			t.Fatalf("%s: expected node %q, got %q", tc.name, tc.wantID, next.ID)
		}
	}

	// A single enabled node cycles onto itself; callers detect that case and
	// skip the restart.
	single := []model.Node{{ID: "solo", Enabled: true}, {ID: "off", Enabled: false}}
	next, ok := NextEnabledNode(single, "solo")
	if !ok || next.ID != "solo" {
		t.Fatalf("single enabled node must cycle onto itself, got %+v ok=%v", next, ok)
	}

	if _, ok := NextEnabledNode(nil, "a"); ok {
		t.Fatal("expected no node for an empty list")
	}
	onlyDisabled := []model.Node{{ID: "x", Enabled: false}, {ID: "y", Enabled: false}}
	if _, ok := NextEnabledNode(onlyDisabled, "x"); ok {
		t.Fatal("expected no node when every node is disabled")
	}
}
