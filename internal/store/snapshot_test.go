package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// seedSnapshotConfiguration writes a small but complete configuration (settings
// plus one manual node) and returns its exported JSON so snapshot round trips
// can compare against exact bytes.
func seedSnapshotConfiguration(t *testing.T, st *Store) []byte {
	t.Helper()

	settings := st.Settings()
	settings.Theme = "dark"
	settings.XrayLocalPort = 20808
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	raw := "vless://11111111-2222-3333-4444-555555555555@snapshot.example.com:443?security=tls#snapshot"
	if err := st.AddManualNodes([]Node{{
		ID: "node-snapshot", Name: "快照节点", Protocol: "vless",
		Address: "snapshot.example.com", Port: 443, Raw: raw, Enabled: true,
	}}); err != nil {
		t.Fatalf("add manual node: %v", err)
	}
	exported, err := st.Export()
	if err != nil {
		t.Fatalf("export seeded configuration: %v", err)
	}
	return exported
}

func mustCreateSnapshot(t *testing.T, st *Store, label string, automatic bool) model.SnapshotInfo {
	t.Helper()
	info, err := st.CreateSnapshot(label, automatic)
	if err != nil {
		t.Fatalf("create snapshot %q: %v", label, err)
	}
	return info
}

func mustListSnapshots(t *testing.T, st *Store) []model.SnapshotInfo {
	t.Helper()
	snapshots, err := st.ListSnapshots()
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	return snapshots
}

func writeSnapshotDirFile(t *testing.T, st *Store, name, content string) {
	t.Helper()
	if err := os.MkdirAll(st.snapshotDir(), 0o700); err != nil {
		t.Fatalf("create snapshot dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(st.snapshotDir(), name), []byte(content), 0o600); err != nil {
		t.Fatalf("write snapshot file %s: %v", name, err)
	}
}

func snapshotDirFileCount(t *testing.T, st *Store) int {
	t.Helper()
	entries, err := os.ReadDir(st.snapshotDir())
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("read snapshot dir: %v", err)
	}
	return len(entries)
}

func TestSnapshotCreateListRestoreDeleteRoundTrip(t *testing.T) {
	st := newTestStore(t)
	before := seedSnapshotConfiguration(t, st)

	created, err := st.CreateSnapshot("  发布前  ", false)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if created.ID == "" || !snapshotIDPattern.MatchString(created.ID) {
		t.Fatalf("snapshot id %q does not match the expected format", created.ID)
	}
	if created.Label != "发布前" {
		t.Fatalf("label was not trimmed: %q", created.Label)
	}
	if created.Automatic {
		t.Fatal("manually created snapshot must not be flagged automatic")
	}
	if created.Size <= 0 {
		t.Fatalf("snapshot size must be positive, got %d", created.Size)
	}
	if _, err := time.Parse(time.RFC3339Nano, created.CreatedAt); err != nil {
		t.Fatalf("createdAt %q is not RFC3339: %v", created.CreatedAt, err)
	}

	// The file layout is the public snapshot format: one JSON file per snapshot
	// with the metadata and the complete backup document embedded as raw JSON.
	path := filepath.Join(st.snapshotDir(), created.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}
	if int64(len(raw)) != created.Size {
		t.Fatalf("snapshot size = %d, file size = %d", created.Size, len(raw))
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("snapshot file is not valid JSON: %v", err)
	}
	for _, key := range []string{"id", "label", "createdAt", "automatic", "data"} {
		if _, ok := document[key]; !ok {
			t.Fatalf("snapshot file is missing %q: %s", key, raw)
		}
	}
	var record snapshotRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode snapshot record: %v", err)
	}
	if record.ID != created.ID || record.Label != created.Label || record.CreatedAt != created.CreatedAt || record.Automatic {
		t.Fatalf("snapshot record does not match its info: %+v vs %+v", record, created)
	}
	if !json.Valid(record.Data) {
		t.Fatal("embedded snapshot data is not valid JSON")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat snapshot file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("snapshot file permissions = %o, want 600", perm)
		}
	}

	listed := mustListSnapshots(t, st)
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed snapshot, got %d", len(listed))
	}
	if listed[0] != created {
		t.Fatalf("listed snapshot = %+v, want %+v", listed[0], created)
	}

	// Change the configuration, then roll back to the snapshot.
	settings := st.Settings()
	settings.Theme = "light"
	settings.XrayLocalPort = 10808
	settings.ConfigOverride = "// changed after the snapshot"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save changed settings: %v", err)
	}
	if err := st.DeleteNode("node-snapshot"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	mutated, err := st.Export()
	if err != nil {
		t.Fatalf("export mutated configuration: %v", err)
	}
	if bytes.Equal(mutated, before) {
		t.Fatal("test setup did not change the configuration")
	}

	result, err := st.RestoreSnapshot(created.ID)
	if err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
	if result.RestoredNodes != 1 {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after rollback: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("rollback did not restore the configuration\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if active := st.Settings(); active.Theme != "dark" || active.XrayLocalPort != 20808 || active.ConfigOverride != "" {
		t.Fatalf("settings were not rolled back: %+v", active)
	}
	nodes := st.ListNodes()
	if len(nodes) != 1 || nodes[0].ID != "node-snapshot" {
		t.Fatalf("nodes were not rolled back: %+v", nodes)
	}

	// Rolling back always leaves the safety snapshot in the list.
	listed = mustListSnapshots(t, st)
	if len(listed) != 2 {
		t.Fatalf("expected the safety snapshot and the manual one, got %d", len(listed))
	}
	if !listed[0].Automatic || listed[0].Label != snapshotPreRestoreLabel {
		t.Fatalf("newest snapshot is not the safety snapshot: %+v", listed[0])
	}
	if listed[1] != created {
		t.Fatalf("manual snapshot disappeared from the list: %+v", listed[1])
	}

	if err := st.DeleteSnapshot(created.ID); err != nil {
		t.Fatalf("delete snapshot: %v", err)
	}
	if listed = mustListSnapshots(t, st); len(listed) != 1 {
		t.Fatalf("expected 1 snapshot after delete, got %d", len(listed))
	}
	if err := st.DeleteSnapshot(created.ID); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("deleting a missing snapshot = %v, want a not-found error", err)
	}

	// Snapshots live next to data.json, so a reloaded store sees them again.
	reloaded, err := NewStoreAt(filepath.Join(st.DataDir(), "data.json"))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	listed = mustListSnapshots(t, reloaded)
	if len(listed) != 1 || !listed[0].Automatic {
		t.Fatalf("reloaded store lost its snapshots: %+v", listed)
	}
}

func TestSnapshotDefaultLabels(t *testing.T) {
	st := newTestStore(t)

	manual := mustCreateSnapshot(t, st, "   ", false)
	if manual.Label != snapshotManualLabel || manual.Automatic {
		t.Fatalf("unexpected manual defaults: %+v", manual)
	}
	automatic := mustCreateSnapshot(t, st, "", true)
	if automatic.Label != snapshotAutomaticLabel || !automatic.Automatic {
		t.Fatalf("unexpected automatic defaults: %+v", automatic)
	}
}

func TestSnapshotRestoreCreatesPreRestoreSnapshotThatCanBeRolledBack(t *testing.T) {
	st := newTestStore(t)
	configA := seedSnapshotConfiguration(t, st)

	manual, err := st.CreateSnapshot("配置 A", false)
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}

	// Move to configuration B.
	settings := st.Settings()
	settings.Theme = "light"
	settings.XrayLocalPort = 10808
	settings.ConfigOverride = "// configuration B"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save settings B: %v", err)
	}
	if err := st.DeleteNode("node-snapshot"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	configB, err := st.Export()
	if err != nil {
		t.Fatalf("export configuration B: %v", err)
	}

	if _, err := st.RestoreSnapshot(manual.ID); err != nil {
		t.Fatalf("restore configuration A: %v", err)
	}
	restoredA, err := st.Export()
	if err != nil {
		t.Fatalf("export after rollback: %v", err)
	}
	if !bytes.Equal(restoredA, configA) {
		t.Fatalf("rollback did not restore configuration A\nwant:\n%s\ngot:\n%s", configA, restoredA)
	}

	// The safety snapshot captured configuration B, so restoring it undoes the
	// rollback. That is the whole point of creating it automatically.
	snapshots := mustListSnapshots(t, st)
	if len(snapshots) != 2 {
		t.Fatalf("expected the manual and the safety snapshot, got %d", len(snapshots))
	}
	safety := snapshots[0]
	if safety.ID == manual.ID || !safety.Automatic || safety.Label != snapshotPreRestoreLabel {
		t.Fatalf("unexpected safety snapshot: %+v", safety)
	}
	if _, err := st.RestoreSnapshot(safety.ID); err != nil {
		t.Fatalf("restore safety snapshot: %v", err)
	}
	restoredB, err := st.Export()
	if err != nil {
		t.Fatalf("export after undoing the rollback: %v", err)
	}
	if !bytes.Equal(restoredB, configB) {
		t.Fatalf("undoing the rollback did not restore configuration B\nwant:\n%s\ngot:\n%s", configB, restoredB)
	}

	// Undoing the rollback took another safety snapshot; all three remain.
	snapshots = mustListSnapshots(t, st)
	if len(snapshots) != 3 {
		t.Fatalf("expected 3 snapshots after the undo, got %d: %+v", len(snapshots), snapshots)
	}
	foundManual := false
	for _, snapshot := range snapshots {
		if snapshot.ID == manual.ID {
			foundManual = true
		}
	}
	if !foundManual {
		t.Fatal("the restored manual snapshot must stay in the list")
	}
}

func TestSnapshotRetentionEvictsOldest(t *testing.T) {
	st := newTestStore(t)
	seedSnapshotConfiguration(t, st)

	labels := make([]string, 0, maxSnapshots+5)
	for i := 1; i <= maxSnapshots+5; i++ {
		label := fmt.Sprintf("快照-%02d", i)
		info := mustCreateSnapshot(t, st, label, false)
		if info.Label != label {
			t.Fatalf("snapshot %d label = %q, want %q", i, info.Label, label)
		}
		labels = append(labels, label)
		// Snapshot ids/ordering have second precision in the filename; a tiny
		// pause keeps creation order unambiguous for the retention assertion.
		time.Sleep(2 * time.Millisecond)
	}

	snapshots := mustListSnapshots(t, st)
	if len(snapshots) != maxSnapshots {
		t.Fatalf("expected %d retained snapshots, got %d", maxSnapshots, len(snapshots))
	}
	if count := st.SnapshotCount(); count != maxSnapshots {
		t.Fatalf("SnapshotCount = %d, want %d", count, maxSnapshots)
	}
	// Newest first: the last 20 labels survive in reverse creation order.
	for i := 0; i < maxSnapshots; i++ {
		want := labels[len(labels)-1-i]
		if snapshots[i].Label != want {
			t.Fatalf("snapshot %d = %q, want %q", i, snapshots[i].Label, want)
		}
	}
	for _, evicted := range labels[:5] {
		for _, snapshot := range snapshots {
			if snapshot.Label == evicted {
				t.Fatalf("oldest snapshot %q was not evicted", evicted)
			}
		}
	}
	if files := snapshotDirFileCount(t, st); files != maxSnapshots {
		t.Fatalf("snapshot directory holds %d files, want %d", files, maxSnapshots)
	}
}

func TestSnapshotRejectsInvalidIDs(t *testing.T) {
	st := newTestStore(t)
	mustCreateSnapshot(t, st, "有效快照", false)

	outside := filepath.Join(st.DataDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("write sentinel file: %v", err)
	}

	invalid := []string{
		"",
		"   ",
		"..",
		"../data",
		"../outside",
		"snap-20260101-000000-aaaa/../../outside",
		`..\..\outside`,
		"snap-20260101-000000-aaaa.json",
		"snap-20260101-0000-abcd",
		"snap-20260101-000000-ABCD",
		"SNAP-20260101-000000-abcd",
		"snap-20260101-000000-abcd\x00",
		"snap-20260101-000000-zzzz",
		"junk",
		strings.Repeat("snap-", 40),
	}
	for _, id := range invalid {
		if _, err := st.RestoreSnapshot(id); err == nil || !strings.Contains(err.Error(), "无效") {
			t.Fatalf("RestoreSnapshot(%q) = %v, want an invalid id error", id, err)
		}
		if err := st.DeleteSnapshot(id); err == nil || !strings.Contains(err.Error(), "无效") {
			t.Fatalf("DeleteSnapshot(%q) = %v, want an invalid id error", id, err)
		}
	}

	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("a rejected id modified files outside the snapshot directory: %v", err)
	}

	// A well-formed but unknown id is reported as missing, not as invalid.
	for _, id := range []string{"snap-20260101-000000-9999", "snap-20990101-120000-0000"} {
		if _, err := st.RestoreSnapshot(id); err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("RestoreSnapshot(%q) = %v, want a not-found error", id, err)
		}
		if err := st.DeleteSnapshot(id); err == nil || !strings.Contains(err.Error(), "不存在") {
			t.Fatalf("DeleteSnapshot(%q) = %v, want a not-found error", id, err)
		}
	}
}

func TestSnapshotListSkipsCorruptedFiles(t *testing.T) {
	st := newTestStore(t)
	seedSnapshotConfiguration(t, st)
	valid := mustCreateSnapshot(t, st, "唯一有效快照", false)

	corrupted := map[string]string{
		// Truncated JSON.
		"snap-20260101-000000-aaaa.json": `{"id":`,
		// Empty file.
		"snap-20260101-000000-bbbb.json": "",
		// Complete metadata but no embedded configuration.
		"snap-20260101-000000-cccc.json": `{"id":"snap-20260101-000000-cccc","label":"缺少数据","createdAt":"2026-01-01T00:00:00Z","automatic":false}`,
		// Invalid creation timestamp.
		"snap-20260101-000000-dddd.json": `{"id":"snap-20260101-000000-dddd","label":"时间损坏","createdAt":"不是时间","automatic":false,"data":{"version":10}}`,
		// Embedded data is not a JSON object.
		"snap-20260101-000000-eeee.json": `{"id":"snap-20260101-000000-eeee","label":"数据损坏","createdAt":"2026-01-01T00:00:00Z","automatic":false,"data":"not-an-object"}`,
		// The id inside the file does not match its name.
		"snap-20260101-000000-ffff.json": `{"id":"snap-20260101-000000-1111","label":"ID 不一致","createdAt":"2026-01-01T00:00:00Z","automatic":false,"data":{"version":10}}`,
		// Unrelated files are ignored, including a leftover temporary file.
		"notes.txt":                             "not a snapshot",
		"junk.json":                             `{"id":"junk"}`,
		"snap-20260101-000000-aaaa.json.tmp":    `{"id":"snap-20260101-000000-aaaa"}`,
		"snap-20260101-000000-bbbb.json.backup": `{"id":"snap-20260101-000000-bbbb"}`,
	}
	for name, content := range corrupted {
		writeSnapshotDirFile(t, st, name, content)
	}

	snapshots := mustListSnapshots(t, st)
	if len(snapshots) != 1 || snapshots[0] != valid {
		t.Fatalf("list did not skip the damaged files: %+v", snapshots)
	}

	// Restoring a damaged file reports a Chinese error instead of touching the
	// configuration.
	before, err := st.Export()
	if err != nil {
		t.Fatalf("export before failed restore: %v", err)
	}
	for id := range map[string]struct{}{
		"snap-20260101-000000-aaaa": {},
		"snap-20260101-000000-bbbb": {},
		"snap-20260101-000000-cccc": {},
		"snap-20260101-000000-dddd": {},
		"snap-20260101-000000-eeee": {},
		"snap-20260101-000000-ffff": {},
	} {
		if _, err := st.RestoreSnapshot(id); err == nil || !strings.Contains(err.Error(), "损坏") {
			t.Fatalf("RestoreSnapshot(%q) = %v, want a damaged-snapshot error", id, err)
		}
	}
	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after failed restore: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a damaged snapshot modified the configuration")
	}
}

func TestSnapshotRejectsOversizedConfiguration(t *testing.T) {
	st := newTestStore(t)

	// A configuration above the 8 MB snapshot bound is rejected up front.
	settings := st.Settings()
	settings.ConfigOverride = strings.Repeat("x", maxSnapshotDataSize+1024)
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save oversized settings: %v", err)
	}
	if _, err := st.CreateSnapshot("过大", false); err == nil || !strings.Contains(err.Error(), "8 MB") {
		t.Fatalf("CreateSnapshot with oversized data = %v, want a size error", err)
	}

	// Restore the store to a small configuration and create one valid snapshot.
	settings.ConfigOverride = ""
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save small settings: %v", err)
	}
	valid := mustCreateSnapshot(t, st, "有效", false)

	// A stored file whose data exceeds the bound is skipped by the list and
	// refused by the restore path instead of exhausting memory.
	huge := strings.Repeat("y", maxSnapshotDataSize+1024)
	content := fmt.Sprintf(
		`{"id":"snap-20260101-000000-aaaa","label":"过大","createdAt":"2026-01-01T00:00:00Z","automatic":true,"data":{"version":10,"settings":{"configOverride":%q}}}`,
		huge,
	)
	writeSnapshotDirFile(t, st, "snap-20260101-000000-aaaa.json", content)

	snapshots := mustListSnapshots(t, st)
	if len(snapshots) != 1 || snapshots[0] != valid {
		t.Fatalf("an oversized snapshot file must be skipped: %+v", snapshots)
	}
	if _, err := st.RestoreSnapshot("snap-20260101-000000-aaaa"); err == nil || !strings.Contains(err.Error(), "MB") {
		t.Fatalf("RestoreSnapshot with oversized data = %v, want a size error", err)
	}
	if count := st.SnapshotCount(); count != 1 {
		t.Fatalf("SnapshotCount = %d, want 1", count)
	}
}

// writeSnapshotBackupFile writes a complete backup document to a temporary file
// and returns its path. It mimics a file produced by "export local backup".
func writeSnapshotBackupFile(t *testing.T, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fastv-backup.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write backup file: %v", err)
	}
	return path
}

func TestSnapshotExportToPath(t *testing.T) {
	st := newTestStore(t)
	before := seedSnapshotConfiguration(t, st)
	created := mustCreateSnapshot(t, st, "导出测试", false)

	// The exported file must contain exactly the backup document embedded in the
	// snapshot, without the snapshot metadata.
	snapshotRaw, err := os.ReadFile(filepath.Join(st.snapshotDir(), created.ID+snapshotFileSuffix))
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}
	var record snapshotRecord
	if err := json.Unmarshal(snapshotRaw, &record); err != nil {
		t.Fatalf("decode snapshot record: %v", err)
	}

	exportPath := filepath.Join(t.TempDir(), "exported.json")
	// Surrounding whitespace in the path is stripped before the check.
	if err := st.ExportSnapshotToPath("  "+created.ID+"  ", "  "+exportPath+"  "); err != nil {
		t.Fatalf("ExportSnapshotToPath: %v", err)
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if !bytes.Equal(exported, record.Data) {
		t.Fatalf("exported file does not equal the embedded snapshot data\nexported:\n%s\nsnapshot data:\n%s", exported, record.Data)
	}

	var document map[string]json.RawMessage
	if err := json.Unmarshal(exported, &document); err != nil {
		t.Fatalf("exported file is not valid JSON: %v", err)
	}
	for _, key := range []string{"version", "settings", "subscriptions", "nodes"} {
		if _, ok := document[key]; !ok {
			t.Fatalf("exported file is missing the backup section %q: %s", key, exported)
		}
	}
	for _, key := range []string{"id", "label", "createdAt", "automatic", "data"} {
		if _, ok := document[key]; ok {
			t.Fatalf("exported file must not contain snapshot metadata %q", key)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(exportPath)
		if err != nil {
			t.Fatalf("stat exported file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("exported file permissions = %o, want 600", perm)
		}
	}

	// The exported file is a regular backup: restoring it must bring the
	// snapshot's configuration back byte for byte.
	settings := st.Settings()
	settings.Theme = "light"
	settings.XrayLocalPort = 10808
	settings.ConfigOverride = "// changed after the export"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save changed settings: %v", err)
	}
	if err := st.DeleteNode("node-snapshot"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	if _, err := st.Restore(exported); err != nil {
		t.Fatalf("restore exported backup: %v", err)
	}
	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after restore: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("restoring the exported file did not restore the snapshot configuration\nwant:\n%s\ngot:\n%s", before, after)
	}
}

func TestSnapshotExportToPathRejectsInvalidInput(t *testing.T) {
	st := newTestStore(t)
	seedSnapshotConfiguration(t, st)
	created := mustCreateSnapshot(t, st, "有效快照", false)

	dir := t.TempDir()
	target := filepath.Join(dir, "exported.json")

	invalidIDs := []string{
		"",
		"   ",
		"..",
		"../data",
		"snap-20260101-000000-zzzz",
		"snap-20260101-000000-aaaa.json",
		created.ID + "/../../outside",
	}
	for _, id := range invalidIDs {
		if err := st.ExportSnapshotToPath(id, target); err == nil || !strings.Contains(err.Error(), "无效") {
			t.Fatalf("ExportSnapshotToPath(%q) = %v, want an invalid id error", id, err)
		}
	}

	// A well-formed but unknown id is reported as missing.
	if err := st.ExportSnapshotToPath("snap-20260101-000000-9999", target); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("ExportSnapshotToPath with a missing id = %v, want a not-found error", err)
	}

	// Empty and relative export paths are rejected before anything is written.
	if err := st.ExportSnapshotToPath(created.ID, "   "); err == nil || !strings.Contains(err.Error(), "路径") {
		t.Fatalf("ExportSnapshotToPath with an empty path = %v, want a path error", err)
	}
	if err := st.ExportSnapshotToPath(created.ID, "relative-backup.json"); err == nil || !strings.Contains(err.Error(), "绝对") {
		t.Fatalf("ExportSnapshotToPath with a relative path = %v, want an absolute path error", err)
	}

	// A missing parent directory is reported instead of being created silently.
	missingParent := filepath.Join(dir, "missing", "exported.json")
	if err := st.ExportSnapshotToPath(created.ID, missingParent); err == nil || !strings.Contains(err.Error(), "写入") {
		t.Fatalf("ExportSnapshotToPath with a missing parent = %v, want a write error", err)
	}

	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a rejected export created %s: %v", target, err)
	}
}

func TestSnapshotExportRejectsDamagedSnapshot(t *testing.T) {
	st := newTestStore(t)
	// Valid snapshot metadata, but the embedded document is not a usable backup.
	writeSnapshotDirFile(t, st, "snap-20260101-000000-aaaa.json",
		`{"id":"snap-20260101-000000-aaaa","label":"坏数据","createdAt":"2026-01-01T00:00:00Z","automatic":false,"data":{"version":0}}`)

	target := filepath.Join(t.TempDir(), "exported.json")
	if err := st.ExportSnapshotToPath("snap-20260101-000000-aaaa", target); err == nil || !strings.Contains(err.Error(), "损坏") {
		t.Fatalf("ExportSnapshotToPath with damaged data = %v, want a damaged-snapshot error", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a damaged snapshot produced an export file: %v", err)
	}
}

func TestSnapshotImportFromPath(t *testing.T) {
	st := newTestStore(t)
	before := seedSnapshotConfiguration(t, st)
	// A trailing newline is what a hand-saved backup file typically has; import
	// must accept it like the regular backup import does.
	backupPath := writeSnapshotBackupFile(t, append(append([]byte(nil), before...), '\n'))

	// Move the live configuration away from the backup content so the rollback
	// below proves the imported snapshot really carries the document.
	settings := st.Settings()
	settings.Theme = "light"
	settings.XrayLocalPort = 10808
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("save changed settings: %v", err)
	}
	if err := st.DeleteNode("node-snapshot"); err != nil {
		t.Fatalf("delete node: %v", err)
	}

	info, err := st.ImportSnapshotFromPath("  "+backupPath+"  ", "  ", true)
	if err != nil {
		t.Fatalf("ImportSnapshotFromPath: %v", err)
	}
	if !snapshotIDPattern.MatchString(info.ID) {
		t.Fatalf("imported snapshot id %q does not match the snapshot format", info.ID)
	}
	if info.Label != snapshotImportLabel {
		t.Fatalf("imported snapshot label = %q, want %q", info.Label, snapshotImportLabel)
	}
	if !info.Automatic {
		t.Fatal("imported snapshot did not keep the automatic flag")
	}
	if info.Size <= 0 {
		t.Fatalf("imported snapshot size must be positive, got %d", info.Size)
	}
	if _, err := time.Parse(time.RFC3339Nano, info.CreatedAt); err != nil {
		t.Fatalf("imported createdAt %q is not RFC3339: %v", info.CreatedAt, err)
	}

	// The created file uses the normal snapshot format and metadata.
	raw, err := os.ReadFile(filepath.Join(st.snapshotDir(), info.ID+snapshotFileSuffix))
	if err != nil {
		t.Fatalf("read imported snapshot file: %v", err)
	}
	if int64(len(raw)) != info.Size {
		t.Fatalf("imported snapshot size = %d, file size = %d", info.Size, len(raw))
	}
	var record snapshotRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode imported snapshot record: %v", err)
	}
	if record.ID != info.ID || record.Label != info.Label || record.CreatedAt != info.CreatedAt || record.Automatic != info.Automatic {
		t.Fatalf("imported snapshot record does not match its info: %+v vs %+v", record, info)
	}
	if !json.Valid(record.Data) {
		t.Fatal("imported snapshot data is not valid JSON")
	}
	if _, err := parseBackup(record.Data); err != nil {
		t.Fatalf("imported snapshot data is not a valid backup: %v", err)
	}

	// The imported snapshot is listed and can be rolled back.
	listed := mustListSnapshots(t, st)
	if len(listed) != 1 || listed[0] != info {
		t.Fatalf("ListSnapshots after import = %+v, want [%+v]", listed, info)
	}
	if _, err := st.RestoreSnapshot(info.ID); err != nil {
		t.Fatalf("restore imported snapshot: %v", err)
	}
	after, err := st.Export()
	if err != nil {
		t.Fatalf("export after restoring the imported snapshot: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("restoring the imported snapshot did not restore the backup content\nwant:\n%s\ngot:\n%s", before, after)
	}
}

func TestSnapshotImportUsesProvidedLabel(t *testing.T) {
	st := newTestStore(t)
	backupPath := writeSnapshotBackupFile(t, seedSnapshotConfiguration(t, st))

	info, err := st.ImportSnapshotFromPath(backupPath, "  我的导入  ", false)
	if err != nil {
		t.Fatalf("ImportSnapshotFromPath: %v", err)
	}
	if info.Label != "我的导入" {
		t.Fatalf("imported snapshot label = %q, want %q", info.Label, "我的导入")
	}
	if info.Automatic {
		t.Fatal("a manual import must not be flagged automatic")
	}
	listed := mustListSnapshots(t, st)
	if len(listed) != 1 || listed[0].Label != "我的导入" || listed[0].Automatic {
		t.Fatalf("imported snapshot is not listed correctly: %+v", listed)
	}
}

func TestSnapshotImportRejectsInvalidFiles(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.ImportSnapshotFromPath("", "", false); err == nil || !strings.Contains(err.Error(), "路径") {
		t.Fatalf("ImportSnapshotFromPath with an empty path = %v, want a path error", err)
	}
	dir := t.TempDir()
	if _, err := st.ImportSnapshotFromPath(filepath.Join(dir, "missing.json"), "", false); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("ImportSnapshotFromPath with a missing file = %v, want a not-found error", err)
	}

	invalid := map[string]string{
		"empty.json":      "",
		"null.json":       "null",
		"broken.json":     `{"version":`,
		"array.json":      `[]`,
		"not-backup.json": `{"hello":"world"}`,
		"newer.json":      `{"version":9999}`,
	}
	for name, content := range invalid {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write invalid import file %s: %v", name, err)
		}
		if _, err := st.ImportSnapshotFromPath(path, "", false); err == nil || !strings.Contains(err.Error(), "备份") {
			t.Fatalf("ImportSnapshotFromPath(%s) = %v, want an invalid backup error", name, err)
		}
	}

	// A file above the snapshot data bound is rejected before it is parsed.
	huge := filepath.Join(dir, "huge.json")
	hugeContent := fmt.Sprintf(`{"version":%d,"settings":{"configOverride":%q}}`, storeVersion, strings.Repeat("x", maxSnapshotDataSize+1024))
	if err := os.WriteFile(huge, []byte(hugeContent), 0o600); err != nil {
		t.Fatalf("write oversized import file: %v", err)
	}
	if _, err := st.ImportSnapshotFromPath(huge, "", false); err == nil || !strings.Contains(err.Error(), "MB") {
		t.Fatalf("ImportSnapshotFromPath with an oversized file = %v, want a size error", err)
	}

	// A directory can never be a backup file.
	if _, err := st.ImportSnapshotFromPath(dir, "", false); err == nil || !strings.Contains(err.Error(), "读取") {
		t.Fatalf("ImportSnapshotFromPath with a directory = %v, want a read error", err)
	}

	// None of the rejected imports may leave a snapshot behind.
	if count := st.SnapshotCount(); count != 0 {
		t.Fatalf("rejected imports created %d snapshots, want 0", count)
	}
	if files := snapshotDirFileCount(t, st); files != 0 {
		t.Fatalf("rejected imports left %d files in the snapshot directory", files)
	}
}

func TestSnapshotImportAppliesRetention(t *testing.T) {
	st := newTestStore(t)
	backupPath := writeSnapshotBackupFile(t, seedSnapshotConfiguration(t, st))

	labels := make([]string, 0, maxSnapshots)
	for i := 1; i <= maxSnapshots; i++ {
		label := fmt.Sprintf("导入前-%02d", i)
		mustCreateSnapshot(t, st, label, false)
		labels = append(labels, label)
		// Snapshot ordering uses second-precision ids and file modification
		// times; a tiny pause keeps the creation order unambiguous.
		time.Sleep(2 * time.Millisecond)
	}
	if count := st.SnapshotCount(); count != maxSnapshots {
		t.Fatalf("SnapshotCount before import = %d, want %d", count, maxSnapshots)
	}

	info, err := st.ImportSnapshotFromPath(backupPath, "导入的最旧快照", false)
	if err != nil {
		t.Fatalf("ImportSnapshotFromPath: %v", err)
	}
	if count := st.SnapshotCount(); count != maxSnapshots {
		t.Fatalf("SnapshotCount after import = %d, want %d", count, maxSnapshots)
	}
	if files := snapshotDirFileCount(t, st); files != maxSnapshots {
		t.Fatalf("snapshot directory holds %d files after import, want %d", files, maxSnapshots)
	}

	snapshots := mustListSnapshots(t, st)
	if len(snapshots) != maxSnapshots {
		t.Fatalf("expected %d snapshots, got %d", maxSnapshots, len(snapshots))
	}
	if snapshots[0].ID != info.ID {
		t.Fatalf("newest snapshot after import = %+v, want %+v", snapshots[0], info)
	}
	for _, snapshot := range snapshots {
		if snapshot.Label == labels[0] {
			t.Fatalf("oldest snapshot %q was not evicted by the imported snapshot", labels[0])
		}
	}
}
