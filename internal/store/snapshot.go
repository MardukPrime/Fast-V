package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

const (
	// snapshotDirName is the sub directory of the store data directory that
	// keeps one JSON file per configuration snapshot.
	snapshotDirName = "snapshots"
	// maxSnapshots is the number of snapshot files that are retained. Creating
	// one more evicts the oldest file.
	maxSnapshots = 20
	// maxSnapshotDataSize bounds the complete backup document embedded in a
	// snapshot. Snapshots are configuration copies, so an oversized file is far
	// more likely to be corruption than real data.
	maxSnapshotDataSize = 8 << 20
	// maxSnapshotFileSize bounds a complete snapshot file. The embedded backup
	// document is re-indented by the JSON encoder, so the file on disk is a
	// little larger than the data it carries.
	maxSnapshotFileSize = maxSnapshotDataSize + 2<<20

	// Labels used when the caller does not provide one. The pre-restore label
	// marks the safety snapshot taken automatically before a rollback.
	snapshotManualLabel     = "手动快照"
	snapshotAutomaticLabel  = "自动快照"
	snapshotPreRestoreLabel = "回滚前快照"

	// snapshotIDTimeLayout is the timestamp part of a snapshot id.
	snapshotIDTimeLayout = "20060102-150405"
	// snapshotFileSuffix is used both for scanning and for building file paths.
	snapshotFileSuffix = ".json"
)

var (
	// snapshotIDPattern is the whitelist for snapshot identifiers. It guards
	// every filesystem access: only ids produced by newSnapshotID may be used as
	// a file name, so user-provided values can never traverse outside the
	// snapshot directory.
	snapshotIDPattern = regexp.MustCompile(`^snap-[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

	// snapshotMu serializes snapshot file operations. The store lock only covers
	// the data document; snapshot files live outside it and are guarded here so
	// concurrent creates cannot race the retention pruning.
	snapshotMu sync.Mutex

	// errSnapshotTooLarge is returned when a snapshot exceeds the data bound and
	// is only handled internally (list skips such a file, restore reports it).
	errSnapshotTooLarge = errors.New("快照文件超过大小上限")
)

// snapshotRecord is the on-disk format of one snapshot: the metadata shown in
// the UI plus the complete exported configuration document as raw JSON.
type snapshotRecord struct {
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	CreatedAt string          `json:"createdAt"`
	Automatic bool            `json:"automatic"`
	Data      json.RawMessage `json:"data"`
}

// CreateSnapshot exports the current configuration and stores it as a new local
// snapshot file. An empty label falls back to a descriptive default, and the
// number of retained snapshots is capped at maxSnapshots (the oldest file is
// removed first).
func (s *Store) CreateSnapshot(label string, automatic bool) (model.SnapshotInfo, error) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()
	return s.createSnapshotLocked(label, automatic, "")
}

// createSnapshotLocked implements CreateSnapshot for callers that already hold
// snapshotMu. protectedID is never evicted: a rollback keeps the snapshot it
// restores even when taking the safety snapshot pushes the file count over the
// retention limit.
func (s *Store) createSnapshotLocked(label string, automatic bool, protectedID string) (model.SnapshotInfo, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		if automatic {
			label = snapshotAutomaticLabel
		} else {
			label = snapshotManualLabel
		}
	}

	data, err := s.Export()
	if err != nil {
		return model.SnapshotInfo{}, fmt.Errorf("导出当前配置失败: %w", err)
	}
	if len(data) > maxSnapshotDataSize {
		return model.SnapshotInfo{}, fmt.Errorf("当前配置数据超过 %d MB，无法创建快照", maxSnapshotDataSize>>20)
	}

	dir := s.snapshotDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return model.SnapshotInfo{}, fmt.Errorf("创建快照目录失败: %w", err)
	}

	now := time.Now()
	createdAt := now.Format(time.RFC3339Nano)
	record := snapshotRecord{
		Label:     label,
		CreatedAt: createdAt,
		Automatic: automatic,
		Data:      json.RawMessage(append([]byte(nil), data...)),
	}

	// The id carries a one-second timestamp plus four random hex digits. Retry
	// the (astronomically unlikely) collision while snapshotMu is held.
	id := ""
	for attempt := 0; attempt < 16; attempt++ {
		candidate := newSnapshotID(now)
		if _, statErr := os.Stat(snapshotPath(dir, candidate)); errors.Is(statErr, os.ErrNotExist) {
			id = candidate
			break
		}
	}
	if id == "" {
		return model.SnapshotInfo{}, errors.New("生成快照 ID 失败")
	}
	record.ID = id

	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return model.SnapshotInfo{}, fmt.Errorf("生成快照内容失败: %w", err)
	}
	if len(raw) > maxSnapshotFileSize {
		return model.SnapshotInfo{}, errSnapshotTooLarge
	}

	path := snapshotPath(dir, id)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return model.SnapshotInfo{}, fmt.Errorf("写入快照文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return model.SnapshotInfo{}, fmt.Errorf("保存快照文件失败: %w", err)
	}

	info := model.SnapshotInfo{
		ID:        id,
		Label:     label,
		CreatedAt: createdAt,
		Size:      int64(len(raw)),
		Automatic: automatic,
	}
	if err := pruneSnapshots(dir, protectedID); err != nil {
		// The snapshot itself was written successfully; report the retention
		// failure so the caller can surface it instead of hiding it.
		return info, err
	}
	return info, nil
}

// ListSnapshots returns the available snapshots ordered by creation time
// (newest first). Damaged, oversized or incomplete files are skipped instead of
// failing the whole list so one bad file can never hide the remaining history.
func (s *Store) ListSnapshots() ([]model.SnapshotInfo, error) {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	dir := s.snapshotDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []model.SnapshotInfo{}, nil
		}
		return nil, fmt.Errorf("读取快照目录失败: %w", err)
	}

	type snapshotItem struct {
		info      model.SnapshotInfo
		createdAt time.Time
	}
	items := make([]snapshotItem, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, snapshotFileSuffix) {
			continue
		}
		id := strings.TrimSuffix(name, snapshotFileSuffix)
		if !snapshotIDPattern.MatchString(id) {
			continue
		}
		raw, err := readSnapshotFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		record, createdAt, err := parseSnapshotFile(raw, id)
		if err != nil {
			continue
		}
		items = append(items, snapshotItem{
			info: model.SnapshotInfo{
				ID:        record.ID,
				Label:     record.Label,
				CreatedAt: record.CreatedAt,
				Size:      int64(len(raw)),
				Automatic: record.Automatic,
			},
			createdAt: createdAt,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if !items[i].createdAt.Equal(items[j].createdAt) {
			return items[i].createdAt.After(items[j].createdAt)
		}
		return items[i].info.ID > items[j].info.ID
	})

	out := make([]model.SnapshotInfo, 0, len(items))
	for _, item := range items {
		out = append(out, item.info)
	}
	return out, nil
}

// RestoreSnapshot rolls the configuration back to the given snapshot. Before
// anything is written it automatically creates a safety snapshot of the current
// configuration ("回滚前快照"), so a rollback can always be undone. Unknown or
// damaged snapshots are rejected with a descriptive error and leave the current
// configuration untouched.
func (s *Store) RestoreSnapshot(id string) (model.BackupResult, error) {
	id = strings.TrimSpace(id)
	if !snapshotIDPattern.MatchString(id) {
		return model.BackupResult{}, errors.New("快照 ID 无效")
	}

	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	raw, err := readSnapshotFile(snapshotPath(s.snapshotDir(), id))
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return model.BackupResult{}, errors.New("快照不存在")
		case errors.Is(err, errSnapshotTooLarge):
			return model.BackupResult{}, errors.New("快照文件超过大小上限，无法回滚")
		default:
			return model.BackupResult{}, fmt.Errorf("读取快照失败: %w", err)
		}
	}

	record, _, err := parseSnapshotFile(raw, id)
	if err != nil {
		if errors.Is(err, errSnapshotTooLarge) {
			return model.BackupResult{}, fmt.Errorf("快照内容超过 %d MB 上限，无法回滚", maxSnapshotDataSize>>20)
		}
		return model.BackupResult{}, fmt.Errorf("快照文件已损坏，无法回滚: %w", err)
	}
	// Validate the embedded backup before taking the safety snapshot: a damaged
	// file must not add a pointless snapshot to the history.
	if _, err := parseBackup(record.Data); err != nil {
		return model.BackupResult{}, fmt.Errorf("快照文件已损坏，无法回滚: %w", err)
	}

	// The undo path must exist before the restore can change anything.
	if _, err := s.createSnapshotLocked(snapshotPreRestoreLabel, true, id); err != nil {
		return model.BackupResult{}, fmt.Errorf("创建回滚前快照失败: %w", err)
	}

	result, err := s.Restore(record.Data)
	if err != nil {
		return result, err
	}
	return result, nil
}

// DeleteSnapshot removes one snapshot file. Only whitelisted ids are accepted,
// so a crafted id can never delete a file outside the snapshot directory.
func (s *Store) DeleteSnapshot(id string) error {
	id = strings.TrimSpace(id)
	if !snapshotIDPattern.MatchString(id) {
		return errors.New("快照 ID 无效")
	}

	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	err := os.Remove(snapshotPath(s.snapshotDir(), id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("快照不存在")
		}
		return fmt.Errorf("删除快照失败: %w", err)
	}
	return nil
}

// SnapshotCount returns how many valid snapshots are currently stored. It is a
// convenience for callers that only need the number, not the metadata.
func (s *Store) SnapshotCount() int {
	snapshots, err := s.ListSnapshots()
	if err != nil {
		return 0
	}
	return len(snapshots)
}

// snapshotDir is the directory holding the snapshot files of this store.
func (s *Store) snapshotDir() string {
	return filepath.Join(s.DataDir(), snapshotDirName)
}

// snapshotPath builds the file path of a snapshot. Callers must validate the id
// against snapshotIDPattern first.
func snapshotPath(dir, id string) string {
	return filepath.Join(dir, id+snapshotFileSuffix)
}

// newSnapshotID returns an id of the form "snap-YYYYMMDD-HHMMSS-xxxx", where the
// last part is four random hex digits so ids stay unique within one second.
func newSnapshotID(now time.Time) string {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand only fails when the OS source is unavailable; fall back to
		// the nanosecond part so an id is still generated.
		return "snap-" + now.Format(snapshotIDTimeLayout) + "-" + fmt.Sprintf("%04x", uint16(now.Nanosecond()))
	}
	return "snap-" + now.Format(snapshotIDTimeLayout) + "-" + hex.EncodeToString(buf)
}

// snapshotFileEntry is the cheap metadata used for retention pruning.
type snapshotFileEntry struct {
	id       string
	path     string
	modified time.Time
}

// listSnapshotFiles returns the snapshot files of a directory. Only regular
// files whose name is a whitelisted snapshot id are considered, so unrelated or
// tampered names are never touched by the retention logic.
func listSnapshotFiles(dir string) ([]snapshotFileEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]snapshotFileEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, snapshotFileSuffix) {
			continue
		}
		id := strings.TrimSuffix(name, snapshotFileSuffix)
		if !snapshotIDPattern.MatchString(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, snapshotFileEntry{
			id:       id,
			path:     filepath.Join(dir, name),
			modified: info.ModTime(),
		})
	}
	return files, nil
}

// pruneSnapshots deletes the oldest snapshot files until at most maxSnapshots
// remain. Modification time is creation order on a normal data directory, which
// keeps pruning cheap: it never has to read (potentially multi-megabyte) files.
func pruneSnapshots(dir, protectedID string) error {
	files, err := listSnapshotFiles(dir)
	if err != nil {
		return fmt.Errorf("读取快照目录失败: %w", err)
	}
	if len(files) <= maxSnapshots {
		return nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].modified.Equal(files[j].modified) {
			return files[i].id < files[j].id
		}
		return files[i].modified.Before(files[j].modified)
	})

	excess := len(files) - maxSnapshots
	for _, file := range files {
		if excess == 0 {
			break
		}
		if file.id == protectedID {
			continue
		}
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("清理旧快照失败: %w", err)
		}
		excess--
	}
	return nil
}

// readSnapshotFile reads one snapshot file with a hard size bound, so a corrupt
// or malicious file can never exhaust memory through the list or restore path.
func readSnapshotFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxSnapshotFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSnapshotFileSize {
		return nil, errSnapshotTooLarge
	}
	return raw, nil
}

// parseSnapshotFile decodes and validates one snapshot file. The filename-derived
// id must match the id inside the file, and the embedded data must be a JSON
// object within the size limit; anything else is treated as a damaged snapshot.
func parseSnapshotFile(raw []byte, expectedID string) (snapshotRecord, time.Time, error) {
	var record snapshotRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return snapshotRecord{}, time.Time{}, err
	}
	if record.ID != expectedID {
		return snapshotRecord{}, time.Time{}, errors.New("快照 ID 与文件名不一致")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return snapshotRecord{}, time.Time{}, errors.New("快照创建时间无效")
	}
	if strings.TrimSpace(record.Label) == "" {
		return snapshotRecord{}, time.Time{}, errors.New("快照缺少标签")
	}
	if len(record.Data) == 0 {
		return snapshotRecord{}, time.Time{}, errors.New("快照缺少配置数据")
	}
	if len(record.Data) > maxSnapshotDataSize {
		return snapshotRecord{}, time.Time{}, errSnapshotTooLarge
	}
	trimmed := bytes.TrimSpace(record.Data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(record.Data) {
		return snapshotRecord{}, time.Time{}, errors.New("快照配置数据无效")
	}
	return record, createdAt, nil
}
