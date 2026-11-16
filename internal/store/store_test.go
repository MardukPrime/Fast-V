package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func TestMergeStoreDataMigratesLegacyDarkTheme(t *testing.T) {
	migrated := mergeStoreData(storeData{
		Version:  1,
		Settings: Settings{Theme: "dark"},
	})

	if migrated.Settings.Theme != "light" {
		t.Fatalf("expected legacy dark theme to migrate to light, got %q", migrated.Settings.Theme)
	}
	if migrated.Version != storeVersion {
		t.Fatalf("expected version %d after migration, got %d", storeVersion, migrated.Version)
	}
}

func TestMergeStoreDataKeepsExplicitTheme(t *testing.T) {
	kept := mergeStoreData(storeData{
		Version:  storeVersion,
		Settings: Settings{Theme: "dark"},
	})

	if kept.Settings.Theme != "dark" {
		t.Fatalf("expected an explicit theme to be preserved, got %q", kept.Settings.Theme)
	}
}

func TestDefaultSettingsUseLightTheme(t *testing.T) {
	if got := DefaultSettings().Theme; got != "light" {
		t.Fatalf("expected light default theme, got %q", got)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestNormalizeTag(t *testing.T) {
	maxTag := strings.Repeat("标", MaxTagLength)
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "empty clears", input: "", want: ""},
		{name: "whitespace only clears", input: "   ", want: ""},
		{name: "trims surrounding spaces", input: "  香港  ", want: "香港"},
		{name: "keeps inner spaces", input: "香港 01", want: "香港 01"},
		{name: "max length allowed", input: maxTag, want: maxTag},
		{name: "trims before length check", input: " " + maxTag + " ", want: maxTag},
		{name: "too long", input: maxTag + "标", wantErr: "标签最多 24 个字符"},
		{name: "newline", input: "香港\n节点", wantErr: "标签不能包含换行或控制字符"},
		{name: "carriage return", input: "a\rb", wantErr: "标签不能包含换行或控制字符"},
		{name: "tab", input: "a\tb", wantErr: "标签不能包含换行或控制字符"},
		{name: "nul", input: "a\x00b", wantErr: "标签不能包含换行或控制字符"},
		{name: "del", input: "a\x7fb", wantErr: "标签不能包含换行或控制字符"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := NormalizeTag(testCase.input)
			if testCase.wantErr != "" {
				if err == nil {
					t.Fatalf("NormalizeTag(%q) = %q, want an error", testCase.input, got)
				}
				if !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("NormalizeTag(%q) error = %q, want it to contain %q", testCase.input, err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeTag(%q) error = %v", testCase.input, err)
			}
			if got != testCase.want {
				t.Fatalf("NormalizeTag(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestSetNodeTag(t *testing.T) {
	st := newTestStore(t)
	if err := st.AddManualNodes([]Node{
		{ID: "node-1", Name: "One", Protocol: "vless", Raw: "vless://one", Enabled: true},
		{ID: "node-2", Name: "Two", Protocol: "trojan", Raw: "trojan://two", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := st.SetNodeTag("node-1", "  香港  ")
	if err != nil {
		t.Fatalf("SetNodeTag failed: %v", err)
	}
	if updated.Tag != "香港" || updated.ID != "node-1" {
		t.Fatalf("unexpected updated node: %+v", updated)
	}

	// Invalid tags are rejected and leave the stored tag untouched.
	for _, invalid := range []string{
		strings.Repeat("标", MaxTagLength+1),
		"香港\n节点",
		"香港\x00节点",
	} {
		if _, err := st.SetNodeTag("node-1", invalid); err == nil {
			t.Fatalf("expected tag %q to be rejected", invalid)
		}
	}
	if node, err := st.FindNode("node-1"); err != nil || node.Tag != "香港" {
		t.Fatalf("rejected tag changed the stored value: %+v (err=%v)", node, err)
	}

	// The trimming result is persisted like any other node field.
	reopened, err := NewStoreAt(st.path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := reopened.FindNode("node-1")
	if err != nil {
		t.Fatal(err)
	}
	if node.Tag != "香港" {
		t.Fatalf("expected tag to persist across reload, got %q", node.Tag)
	}

	// An empty (or whitespace-only) tag clears the tag.
	cleared, err := reopened.SetNodeTag("node-1", "   ")
	if err != nil {
		t.Fatalf("SetNodeTag clear failed: %v", err)
	}
	if cleared.Tag != "" {
		t.Fatalf("expected cleared tag, got %q", cleared.Tag)
	}

	if _, err := reopened.SetNodeTag("missing-node", "香港"); err == nil {
		t.Fatal("expected an error for an unknown node id")
	}
}

func TestReplaceSubscriptionNodesPreservesTags(t *testing.T) {
	st := newTestStore(t)
	if err := st.AddSubscription(Subscription{ID: "sub-1", Name: "Sub", URL: "https://example.com/sub"}, []Node{
		{
			ID: "node-1", SubscriptionID: "sub-1", Name: "Old One", Protocol: "vless",
			Raw: "vless://one", SourceKey: model.SourceKey("vless://one"), Enabled: true,
			// A tag in the payload must not be trusted: tags are user data.
			Tag: "payload-tag",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if node, err := st.FindNode("node-1"); err != nil || node.Tag != "" {
		t.Fatalf("new subscription nodes must start without a tag: %+v (err=%v)", node, err)
	}
	if _, err := st.SetNodeTag("node-1", "美国"); err != nil {
		t.Fatal(err)
	}

	// Refresh: the same SourceKey keeps identity, a new SourceKey is a new node.
	if err := st.ReplaceSubscriptionNodes("sub-1", []Node{
		{
			ID: "node-2", SubscriptionID: "sub-1", Name: "New One", Protocol: "vless",
			Raw: "vless://one", SourceKey: model.SourceKey("vless://one"), Enabled: true,
		},
		{
			ID: "node-3", SubscriptionID: "sub-1", Name: "Two", Protocol: "trojan",
			Raw: "trojan://two", SourceKey: model.SourceKey("trojan://two"), Enabled: true,
			Tag: "payload-tag",
		},
	}); err != nil {
		t.Fatal(err)
	}

	matched, err := st.FindNode("node-1")
	if err != nil {
		t.Fatalf("matched node lost its identity: %v", err)
	}
	if matched.Name != "New One" || matched.Tag != "美国" {
		t.Fatalf("expected a refreshed name with a preserved tag, got %+v", matched)
	}
	added, err := st.FindNode("node-3")
	if err != nil {
		t.Fatal(err)
	}
	if added.Tag != "" {
		t.Fatalf("a brand new subscription node must not carry a payload tag, got %q", added.Tag)
	}
	if _, err := st.FindNode("node-2"); err == nil {
		t.Fatal("matched node kept the payload id instead of the stable node id")
	}
}

func TestListTagsDeduplicatesAndSorts(t *testing.T) {
	st := newTestStore(t)
	if err := st.AddManualNodes([]Node{
		{ID: "n1", Name: "1", Raw: "a", Enabled: true, Tag: "us"},
		{ID: "n2", Name: "2", Raw: "b", Enabled: true, Tag: "jp"},
		{ID: "n3", Name: "3", Raw: "c", Enabled: true, Tag: "us"},
		{ID: "n4", Name: "4", Raw: "d", Enabled: true},
		{ID: "n5", Name: "5", Raw: "e", Enabled: true, Tag: "  香港  "},
	}); err != nil {
		t.Fatal(err)
	}

	tags := st.ListTags()
	want := []string{"jp", "us", "香港"}
	if len(tags) != len(want) {
		t.Fatalf("expected tags %v, got %v", want, tags)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("expected sorted tags %v, got %v", want, tags)
		}
	}

	// Clearing both "us" tags removes the duplicate from the dictionary.
	if _, err := st.SetNodeTag("n1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetNodeTag("n3", ""); err != nil {
		t.Fatal(err)
	}
	if tags := st.ListTags(); len(tags) != 2 || tags[0] != "jp" || tags[1] != "香港" {
		t.Fatalf("expected only the remaining tags, got %v", tags)
	}
}

func TestMergeStoreDataDropsInvalidLoadedTags(t *testing.T) {
	loaded := mergeStoreData(storeData{
		Version: storeVersion,
		Nodes: []Node{
			{ID: "n1", Raw: "a", Tag: "  美国  "},
			{ID: "n2", Raw: "b", Tag: "bad\nline"},
			{ID: "n3", Raw: "c", Tag: strings.Repeat("标", MaxTagLength+1)},
		},
	})

	if got := loaded.Nodes[0].Tag; got != "美国" {
		t.Fatalf("expected a loaded tag to be trimmed, got %q", got)
	}
	if got := loaded.Nodes[1].Tag; got != "" {
		t.Fatalf("expected a loaded newline tag to be dropped, got %q", got)
	}
	if got := loaded.Nodes[2].Tag; got != "" {
		t.Fatalf("expected an oversized loaded tag to be dropped, got %q", got)
	}
}
