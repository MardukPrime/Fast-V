package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/secure"
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
	// A pinned CDN edge is user data too: it must survive the refresh below.
	if _, err := st.SetNodeCdnIP("node-1", "104.22.0.7"); err != nil {
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
	if matched.CdnIP != "104.22.0.7" {
		t.Fatalf("pinned CDN edge lost on refresh: %+v", matched)
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

// fakeSecretCodec is an injectable stand-in for the platform codec (DPAPI on
// Windows). It encodes as "enc:v1:<base64("fake:"+plain)>" and records every
// call so tests can assert that the store protects exactly the sensitive
// settings fields, independent of the DPAPI availability of the test machine.
type fakeSecretCodec struct {
	protectErr   error
	unprotectErr error
	protected    []string
	unprotected  []string
}

func (f *fakeSecretCodec) protect(plain string) (string, error) {
	f.protected = append(f.protected, plain)
	if f.protectErr != nil {
		return "", f.protectErr
	}
	return secure.Prefix + base64.StdEncoding.EncodeToString([]byte("fake:"+plain)), nil
}

func (f *fakeSecretCodec) unprotect(value string) (string, error) {
	f.unprotected = append(f.unprotected, value)
	if f.unprotectErr != nil {
		return "", f.unprotectErr
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, secure.Prefix))
	if err != nil {
		return "", err
	}
	decoded := string(raw)
	if !strings.HasPrefix(decoded, "fake:") {
		return "", errors.New("fake codec: 密文已损坏")
	}
	return strings.TrimPrefix(decoded, "fake:"), nil
}

// useFakeSecretCodec installs codec for the current test and restores the real
// platform codec afterwards.
func useFakeSecretCodec(t *testing.T, codec *fakeSecretCodec) *fakeSecretCodec {
	t.Helper()
	previousProtect, previousUnprotect := secureProtect, secureUnprotect
	secureProtect, secureUnprotect = codec.protect, codec.unprotect
	t.Cleanup(func() {
		secureProtect, secureUnprotect = previousProtect, previousUnprotect
	})
	return codec
}

// decodeFakeSecret decodes a value produced by fakeSecretCodec.
func decodeFakeSecret(t *testing.T, value string) string {
	t.Helper()
	if !secure.IsEncrypted(value) {
		t.Fatalf("expected an encrypted value, got %q", value)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, secure.Prefix))
	if err != nil {
		t.Fatalf("decode encrypted value: %v", err)
	}
	decoded := string(raw)
	if !strings.HasPrefix(decoded, "fake:") {
		t.Fatalf("unexpected fake payload %q", decoded)
	}
	return strings.TrimPrefix(decoded, "fake:")
}

// readPersistedStoreData reads and decodes data.json so tests can inspect the
// persisted (encrypted) image instead of the in-memory plaintext.
func readPersistedStoreData(t *testing.T, path string) storeData {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read data.json: %v", err)
	}
	var data storeData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("decode data.json: %v", err)
	}
	return data
}

// writePersistedStoreData writes a data.json image directly, simulating files
// produced by older builds or damaged ones.
func writePersistedStoreData(t *testing.T, path string, data storeData) {
	t.Helper()
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("encode data.json: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write data.json: %v", err)
	}
}

// TestSaveProtectsSensitiveSettings verifies the core save/load contract: the
// persisted document only carries protected values while the in-memory model
// (and therefore every service) keeps the plaintext.
func TestSaveProtectsSensitiveSettings(t *testing.T) {
	codec := useFakeSecretCodec(t, &fakeSecretCodec{})
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}

	settings := st.Settings()
	settings.WebDavUsername = "dav-user"
	settings.WebDavPassword = "dav-secret"
	settings.InboundUsername = "inbound-user"
	settings.InboundPassword = "inbound-秘密"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}

	// The live model is plaintext.
	live := st.Settings()
	if live.WebDavPassword != "dav-secret" || live.InboundPassword != "inbound-秘密" {
		t.Fatalf("in-memory settings must keep plaintext secrets, got %+v", live)
	}

	// data.json only carries protected values and never the plaintext.
	persisted := readPersistedStoreData(t, path)
	if got := persisted.Settings.WebDavPassword; !secure.IsEncrypted(got) || got == "dav-secret" {
		t.Fatalf("webdavPassword persisted as %q, want an encrypted value", got)
	}
	if got := persisted.Settings.InboundPassword; !secure.IsEncrypted(got) || got == "inbound-秘密" {
		t.Fatalf("inboundPassword persisted as %q, want an encrypted value", got)
	}
	if got := decodeFakeSecret(t, persisted.Settings.WebDavPassword); got != "dav-secret" {
		t.Fatalf("decoded webdavPassword = %q, want %q", got, "dav-secret")
	}
	if got := decodeFakeSecret(t, persisted.Settings.InboundPassword); got != "inbound-秘密" {
		t.Fatalf("decoded inboundPassword = %q, want %q", got, "inbound-秘密")
	}
	// Only the two passwords are protected; usernames and the rest are untouched.
	if persisted.Settings.WebDavUsername != "dav-user" || persisted.Settings.InboundUsername != "inbound-user" {
		t.Fatalf("non-sensitive fields must stay plaintext: %+v", persisted.Settings)
	}
	wantProtected := []string{"dav-secret", "inbound-秘密"}
	if len(codec.protected) != len(wantProtected) {
		t.Fatalf("protect called with %v, want exactly %v", codec.protected, wantProtected)
	}
	for i := range wantProtected {
		if codec.protected[i] != wantProtected[i] {
			t.Fatalf("protect call %d = %q, want %q", i, codec.protected[i], wantProtected[i])
		}
	}

	// A fresh store transparently decrypts the persisted document.
	reopened, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("reopening store failed: %v", err)
	}
	if got := reopened.Settings().WebDavPassword; got != "dav-secret" {
		t.Fatalf("reloaded webdavPassword = %q, want %q", got, "dav-secret")
	}
	if got := reopened.Settings().InboundPassword; got != "inbound-秘密" {
		t.Fatalf("reloaded inboundPassword = %q, want %q", got, "inbound-秘密")
	}
	if len(codec.unprotected) != 2 {
		t.Fatalf("unprotect called %d times, want 2: %v", len(codec.unprotected), codec.unprotected)
	}
	// The reload rewrites the file, which must stay encrypted.
	if got := readPersistedStoreData(t, path).Settings.WebDavPassword; !secure.IsEncrypted(got) {
		t.Fatalf("data.json lost its protected form after reload: %q", got)
	}
}

// TestLoadLegacyPlaintextData verifies backward compatibility: data.json files
// written before encryption existed load unchanged and are upgraded on the next
// save without a decrypt attempt.
func TestLoadLegacyPlaintextData(t *testing.T) {
	codec := useFakeSecretCodec(t, &fakeSecretCodec{})
	path := filepath.Join(t.TempDir(), "data.json")
	writePersistedStoreData(t, path, storeData{
		Version: storeVersion,
		Settings: Settings{
			Theme:           "dark",
			WebDavUsername:  "dav-user",
			WebDavPassword:  "legacy-dav",
			InboundUsername: "inbound-user",
			InboundPassword: "legacy-inbound",
		},
	})

	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("loading a legacy plaintext file failed: %v", err)
	}
	if got := st.Settings().WebDavPassword; got != "legacy-dav" {
		t.Fatalf("legacy webdavPassword = %q, want %q", got, "legacy-dav")
	}
	if got := st.Settings().InboundPassword; got != "legacy-inbound" {
		t.Fatalf("legacy inboundPassword = %q, want %q", got, "legacy-inbound")
	}
	if got := st.Settings().Theme; got != "dark" {
		t.Fatalf("unrelated settings must survive the load, got theme %q", got)
	}
	if len(codec.unprotected) != 0 {
		t.Fatalf("legacy plaintext must not be treated as ciphertext, unprotect calls: %v", codec.unprotected)
	}
	// NewStoreAt saves after loading, so the file is upgraded to the protected
	// form even without an explicit settings change.
	persisted := readPersistedStoreData(t, path)
	if got := persisted.Settings.WebDavPassword; !secure.IsEncrypted(got) {
		t.Fatalf("legacy file was not upgraded to a protected value: %q", got)
	}
}

// TestLoadCorruptCiphertextClearsField verifies the failure policy: a value that
// cannot be decrypted must never be used as a password, but it must also not
// abort loading the rest of the configuration.
func TestLoadCorruptCiphertextClearsField(t *testing.T) {
	useFakeSecretCodec(t, &fakeSecretCodec{})
	path := filepath.Join(t.TempDir(), "data.json")
	writePersistedStoreData(t, path, storeData{
		Version: storeVersion,
		Settings: Settings{
			Theme:           "dark",
			WebDavURL:       "https://dav.example.com",
			WebDavUsername:  "dav-user",
			WebDavPassword:  secure.Prefix + base64.StdEncoding.EncodeToString([]byte("garbage")),
			InboundUsername: "inbound-user",
			InboundPassword: secure.Prefix + "!!!not-base64!!!",
		},
	})

	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("a damaged secret must not block loading: %v", err)
	}
	settings := st.Settings()
	if settings.WebDavPassword != "" {
		t.Fatalf("unreadable webdavPassword must be cleared, got %q", settings.WebDavPassword)
	}
	if settings.InboundPassword != "" {
		t.Fatalf("unreadable inboundPassword must be cleared, got %q", settings.InboundPassword)
	}
	if settings.WebDavURL != "https://dav.example.com" ||
		settings.WebDavUsername != "dav-user" ||
		settings.InboundUsername != "inbound-user" ||
		settings.Theme != "dark" {
		t.Fatalf("unrelated settings must load normally, got %+v", settings)
	}
	persisted := readPersistedStoreData(t, path)
	if persisted.Settings.WebDavPassword != "" || persisted.Settings.InboundPassword != "" {
		t.Fatalf("cleared secrets must be persisted as empty, got %+v", persisted.Settings)
	}
}

// TestSaveFallsBackToPlaintextWhenProtectFails verifies that a protection
// failure never blocks saving: the value is stored as plaintext instead and the
// in-memory plaintext is unchanged.
func TestSaveFallsBackToPlaintextWhenProtectFails(t *testing.T) {
	codec := useFakeSecretCodec(t, &fakeSecretCodec{protectErr: errors.New("DPAPI 不可用")})
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}

	settings := st.Settings()
	settings.WebDavPassword = "fallback-dav"
	settings.InboundPassword = "fallback-inbound"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save must not fail when protection fails: %v", err)
	}
	if got := st.Settings().WebDavPassword; got != "fallback-dav" {
		t.Fatalf("in-memory plaintext changed to %q", got)
	}
	persisted := readPersistedStoreData(t, path)
	if persisted.Settings.WebDavPassword != "fallback-dav" {
		t.Fatalf("expected plaintext fallback, persisted %q", persisted.Settings.WebDavPassword)
	}
	if persisted.Settings.InboundPassword != "fallback-inbound" {
		t.Fatalf("expected plaintext fallback, persisted %q", persisted.Settings.InboundPassword)
	}
	if len(codec.protected) != 2 {
		t.Fatalf("protect called %d times, want 2: %v", len(codec.protected), codec.protected)
	}
}

// TestLoadKeepsValuesWhenCodecUnsupported verifies the non-Windows contract on
// the store level: ErrUnsupported must preserve what is on disk instead of
// clearing it.
func TestLoadKeepsValuesWhenCodecUnsupported(t *testing.T) {
	t.Run("plaintext stays plaintext", func(t *testing.T) {
		useFakeSecretCodec(t, &fakeSecretCodec{
			protectErr:   secure.ErrUnsupported,
			unprotectErr: secure.ErrUnsupported,
		})
		path := filepath.Join(t.TempDir(), "data.json")
		writePersistedStoreData(t, path, storeData{
			Version: storeVersion,
			Settings: Settings{
				WebDavPassword:  "plain-dav",
				InboundPassword: "plain-inbound",
			},
		})

		st, err := NewStoreAt(path)
		if err != nil {
			t.Fatalf("loading without a protection backend failed: %v", err)
		}
		if got := st.Settings().WebDavPassword; got != "plain-dav" {
			t.Fatalf("plaintext webdavPassword = %q, want it preserved", got)
		}
		if got := st.Settings().InboundPassword; got != "plain-inbound" {
			t.Fatalf("plaintext inboundPassword = %q, want it preserved", got)
		}
		persisted := readPersistedStoreData(t, path)
		if persisted.Settings.WebDavPassword != "plain-dav" {
			t.Fatalf("persisted webdavPassword = %q, want the plaintext fallback", persisted.Settings.WebDavPassword)
		}
	})

	t.Run("prefixed value is kept untouched", func(t *testing.T) {
		useFakeSecretCodec(t, &fakeSecretCodec{
			protectErr:   secure.ErrUnsupported,
			unprotectErr: secure.ErrUnsupported,
		})
		encrypted := secure.Prefix + base64.StdEncoding.EncodeToString([]byte("foreign blob"))
		path := filepath.Join(t.TempDir(), "data.json")
		writePersistedStoreData(t, path, storeData{
			Version:  storeVersion,
			Settings: Settings{WebDavPassword: encrypted},
		})

		st, err := NewStoreAt(path)
		if err != nil {
			t.Fatalf("loading a foreign protected value failed: %v", err)
		}
		if got := st.Settings().WebDavPassword; got != encrypted {
			t.Fatalf("webdavPassword = %q, want the original value %q kept", got, encrypted)
		}
	})
}

// TestExportBackupIsPlaintextAndRestoreReencrypts verifies the documented
// backup semantics: Export stays portable plaintext JSON, while Restore writes
// the protected form again on its next save.
func TestExportBackupIsPlaintextAndRestoreReencrypts(t *testing.T) {
	useFakeSecretCodec(t, &fakeSecretCodec{})
	st, err := NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := st.Settings()
	settings.WebDavPassword = "backup-dav"
	settings.InboundPassword = "backup-inbound"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	exported, err := st.Export()
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	document := string(exported)
	if !strings.Contains(document, "backup-dav") || !strings.Contains(document, "backup-inbound") {
		t.Fatalf("export must keep plaintext credentials for portable backups")
	}

	targetPath := filepath.Join(t.TempDir(), "restore.json")
	target, err := NewStoreAt(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Restore(exported); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if got := target.Settings().WebDavPassword; got != "backup-dav" {
		t.Fatalf("restored webdavPassword = %q, want %q", got, "backup-dav")
	}
	if got := target.Settings().InboundPassword; got != "backup-inbound" {
		t.Fatalf("restored inboundPassword = %q, want %q", got, "backup-inbound")
	}
	persisted := readPersistedStoreData(t, targetPath)
	if got := persisted.Settings.WebDavPassword; !secure.IsEncrypted(got) {
		t.Fatalf("restored configuration must be persisted protected, got %q", got)
	}
	if got := decodeFakeSecret(t, persisted.Settings.InboundPassword); got != "backup-inbound" {
		t.Fatalf("persisted inboundPassword decodes to %q", got)
	}
}

// TestRealDPAPIProtectsPersistedSettings is the end-to-end integration test on
// a Windows runner: real DPAPI protects the file and another store instance can
// decrypt it. It is skipped on platforms without a protection backend.
func TestRealDPAPIProtectsPersistedSettings(t *testing.T) {
	if !secure.Supported() {
		t.Skipf("DPAPI only exists on Windows (running on %s)", runtime.GOOS)
	}
	path := filepath.Join(t.TempDir(), "data.json")
	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := st.Settings()
	settings.WebDavPassword = "真实-webdav-密码"
	settings.InboundPassword = "real-inbound-p@ss"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings failed: %v", err)
	}

	persisted := readPersistedStoreData(t, path)
	if got := persisted.Settings.WebDavPassword; !secure.IsEncrypted(got) || got == "真实-webdav-密码" {
		t.Fatalf("webdavPassword persisted as %q, want a DPAPI encrypted value", got)
	}
	plain, err := secure.UnprotectString(persisted.Settings.WebDavPassword)
	if err != nil || plain != "真实-webdav-密码" {
		t.Fatalf("direct DPAPI unprotect = %q, %v", plain, err)
	}

	reopened, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("reopening store failed: %v", err)
	}
	if got := reopened.Settings().WebDavPassword; got != "真实-webdav-密码" {
		t.Fatalf("reloaded webdavPassword = %q", got)
	}
	if got := reopened.Settings().InboundPassword; got != "real-inbound-p@ss" {
		t.Fatalf("reloaded inboundPassword = %q", got)
	}
	// The snapshot/backup path uses the in-memory model, so it stays plaintext.
	exported, err := reopened.Export()
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if !strings.Contains(string(exported), "真实-webdav-密码") {
		t.Fatalf("export must be a portable plaintext backup")
	}
}
