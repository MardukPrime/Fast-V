package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A UTF-8 BOM from a Windows editor must not abort startup: the load would fail
// and take the whole app down with it.
func TestNewStoreAtToleratesUTF8BOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	body := `{"version":12,"settings":{"theme":"dark"},"nodes":[{"id":"n1","name":"bom node","protocol":"vless"}],"activeNodeId":"n1"}`
	if err := os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt with BOM: %v", err)
	}
	if notice := st.LoadNotice(); notice != "" {
		t.Fatalf("unexpected load notice: %q", notice)
	}
	if got := len(st.Snapshot().Nodes); got != 1 {
		t.Fatalf("nodes = %d, want 1", got)
	}
	if got := st.Snapshot().ActiveNodeID; got != "n1" {
		t.Fatalf("activeNodeID = %q, want n1", got)
	}
}

// A hand-edited or truncated file must keep the app usable and preserve the
// original file instead of silently overwriting it.
func TestNewStoreAtQuarantinesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"version":12,"nodes":[`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	st, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt on corrupt file: %v", err)
	}
	notice := st.LoadNotice()
	if notice == "" {
		t.Fatal("expected a load notice for a corrupt file")
	}
	if !strings.Contains(notice, ".corrupt-") {
		t.Fatalf("notice should name the backup file, got %q", notice)
	}

	backups, err := filepath.Glob(path + ".corrupt-*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want exactly one", backups)
	}
	raw, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(raw) != `{"version":12,"nodes":[` {
		t.Fatalf("backup content changed: %q", string(raw))
	}
	// The store must still be usable and have written a fresh file.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected a fresh store file: %v", err)
	}
	if got := len(st.Snapshot().Nodes); got != 0 {
		t.Fatalf("nodes = %d, want 0 after fallback", got)
	}
}
