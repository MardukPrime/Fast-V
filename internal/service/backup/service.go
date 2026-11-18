package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	"github.com/MardukPrime/Fast-V/internal/webdav"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	// defaultWebDAVPath mirrors the store default and is used when an older
	// configuration has not persisted a WebDAV path yet.
	defaultWebDAVPath = "/FastV/fastv-backup.json"
	// webDAVTestTimeout bounds the complete connectivity probe.
	webDAVTestTimeout = 30 * time.Second
	// webDAVSyncTimeout bounds a full upload or download.
	webDAVSyncTimeout = 120 * time.Second

	// snapshotExportPrefix is the default file name prefix shown by the export
	// snapshot dialog.
	snapshotExportPrefix = "FastV-snapshot-"
	// snapshotExportSuffix is the extension of an exported snapshot backup.
	snapshotExportSuffix = ".json"
	// snapshotExportMaxLabelRunes bounds the label part of the default file name.
	// Labels may contain multi-byte characters, so the bound is in runes and
	// keeps the generated name below common file system limits.
	snapshotExportMaxLabelRunes = 64
)

var (
	// errFileDialogUnavailable is returned when the Wails application instance is
	// missing, so no native file dialog can be shown. This happens in unit tests
	// and before the application has started.
	errFileDialogUnavailable = errors.New("文件对话框不可用")
	// errSnapshotImportCancelled is returned when the user closes the open file
	// dialog without choosing a backup file, so the frontend can stay silent
	// instead of showing an error.
	errSnapshotImportCancelled = errors.New("已取消导入")
)

// Service exposes configuration export and restore to the frontend. The actual
// backup format is owned by the store package, which keeps settings,
// subscriptions, nodes, routing rules and the active node in one versioned JSON
// document.
type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) ServiceName() string {
	return "BackupService"
}

// Export returns the complete FastV configuration as indented JSON. The frontend
// uses the string to download or copy a backup file.
func (s *Service) Export() (string, error) {
	data, err := s.store.Export()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Import replaces the current configuration with the provided backup. Invalid
// input is rejected with a descriptive error and the existing configuration is
// left untouched.
func (s *Service) Import(data string) (model.BackupResult, error) {
	if strings.TrimSpace(data) == "" {
		return model.BackupResult{}, errors.New("备份内容为空")
	}
	result, err := s.store.Restore([]byte(data))
	if err != nil {
		return result, err
	}
	s.notifySettingsChanged()
	return result, nil
}

// CreateSnapshot stores the current configuration as a manually created local
// snapshot and returns its metadata for the snapshot list.
func (s *Service) CreateSnapshot(label string) (model.SnapshotInfo, error) {
	info, err := s.store.CreateSnapshot(label, false)
	if err != nil {
		return model.SnapshotInfo{}, err
	}
	return info, nil
}

// ListSnapshots returns the stored local snapshots, newest first. Damaged
// snapshot files are skipped by the store instead of failing the list.
func (s *Service) ListSnapshots() ([]model.SnapshotInfo, error) {
	snapshots, err := s.store.ListSnapshots()
	if err != nil {
		return nil, err
	}
	if snapshots == nil {
		snapshots = []model.SnapshotInfo{}
	}
	return snapshots, nil
}

// RestoreSnapshot rolls the configuration back to the given snapshot. The store
// takes a safety snapshot of the current configuration first, so the rollback
// itself can be undone. A successful rollback may contain different settings, so
// the settings:changed event is emitted exactly like Import does.
func (s *Service) RestoreSnapshot(id string) (model.BackupResult, error) {
	result, err := s.store.RestoreSnapshot(id)
	if err != nil {
		return result, err
	}
	s.notifySettingsChanged()
	return result, nil
}

// DeleteSnapshot removes one stored snapshot. The id is validated by the store,
// so only existing snapshot files inside the snapshot directory can be deleted.
func (s *Service) DeleteSnapshot(id string) error {
	return s.store.DeleteSnapshot(id)
}

// ExportSnapshot writes the backup document contained in a stored snapshot to a
// user chosen file. The native save dialog opens with a default name derived
// from the snapshot label ("FastV-snapshot-<label>.json"), filtered to JSON
// files. The exported file contains no snapshot metadata, so it is a normal
// local backup: it can be imported again through Import or the regular backup
// import. Cancelling the dialog returns ("", nil); a missing snapshot or a
// failed write is reported as a Chinese error.
func (s *Service) ExportSnapshot(id string) (string, error) {
	info, found, err := s.findSnapshot(id)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("快照不存在")
	}

	app := application.Get()
	if app == nil {
		return "", errFileDialogUnavailable
	}
	path, err := app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                "导出配置快照",
		Message:              "导出后可通过配置快照导入恢复当前配置",
		ButtonText:           "导出",
		Filename:             snapshotExportFilename(info),
		CanCreateDirectories: true,
		Filters:              jsonFileFilters(),
	}).PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("打开保存对话框失败: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		// The user cancelled the save dialog.
		return "", nil
	}
	if err := s.store.ExportSnapshotToPath(info.ID, path); err != nil {
		return "", fmt.Errorf("导出快照失败: %w", err)
	}
	return path, nil
}

// ImportSnapshot asks the user to choose a local backup file and stores its
// content as a new local snapshot. The file is validated by the store before it
// appears in the snapshot list, and an empty label falls back to the store
// default "导入快照". Cancelling the dialog returns (SnapshotInfo{}, 已取消导入)
// so the frontend can distinguish a cancelled choice from a real failure.
func (s *Service) ImportSnapshot(label string) (model.SnapshotInfo, error) {
	app := application.Get()
	if app == nil {
		return model.SnapshotInfo{}, errFileDialogUnavailable
	}
	path, err := app.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "导入配置快照",
		Message:              "选择一个 FastV 备份文件（*.json）",
		ButtonText:           "导入",
		CanChooseFiles:       true,
		CanChooseDirectories: false,
		AllowsOtherFileTypes: false,
		Filters:              jsonFileFilters(),
	}).PromptForSingleSelection()
	if err != nil {
		return model.SnapshotInfo{}, fmt.Errorf("打开文件选择对话框失败: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		// The user cancelled the open dialog.
		return model.SnapshotInfo{}, errSnapshotImportCancelled
	}
	info, err := s.store.ImportSnapshotFromPath(path, label, false)
	if err != nil {
		return model.SnapshotInfo{}, fmt.Errorf("导入快照失败: %w", err)
	}
	return info, nil
}

// findSnapshot looks a snapshot up by id. The list is sorted and validated by
// the store, so the service can derive the default export file name from the
// label without reading the snapshot file format itself.
func (s *Service) findSnapshot(id string) (model.SnapshotInfo, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.SnapshotInfo{}, false, nil
	}
	snapshots, err := s.store.ListSnapshots()
	if err != nil {
		return model.SnapshotInfo{}, false, fmt.Errorf("读取快照列表失败: %w", err)
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == id {
			return snapshot, true, nil
		}
	}
	return model.SnapshotInfo{}, false, nil
}

// snapshotExportFilename builds the default file name of an exported snapshot.
// The label is user supplied, so characters that are invalid on Windows and
// control characters are removed and the label part is length bounded.
func snapshotExportFilename(info model.SnapshotInfo) string {
	label := strings.TrimSpace(info.Label)
	if label == "" {
		label = info.ID
	}
	label = sanitizeSnapshotFileLabel(label)
	if label == "" {
		label = info.ID
	}
	return snapshotExportPrefix + label + snapshotExportSuffix
}

// sanitizeSnapshotFileLabel strips path separators, Windows reserved characters
// and control characters from a label, trims dots and spaces from both ends and
// truncates it to snapshotExportMaxLabelRunes runes.
func sanitizeSnapshotFileLabel(label string) string {
	runes := []rune(label)
	if len(runes) > snapshotExportMaxLabelRunes {
		runes = runes[:snapshotExportMaxLabelRunes]
	}
	var builder strings.Builder
	for _, r := range runes {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			continue
		}
		builder.WriteRune(r)
	}
	return strings.Trim(builder.String(), " .")
}

// jsonFileFilters returns the JSON file filter used by both snapshot dialogs.
func jsonFileFilters() []application.FileFilter {
	return []application.FileFilter{{
		DisplayName: "JSON 文件 (*.json)",
		Pattern:     "*.json",
	}}
}

// notifySettingsChanged tells the rest of the app (tray, global hotkeys, top
// bar) that the restored configuration may contain different settings. Restore
// bypasses SettingsService, so the event is emitted here.
func (s *Service) notifySettingsChanged() {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit("settings:changed", s.store.Settings())
}

// webDAVClient builds a WebDAV client from the persisted settings and returns
// the remote backup path to use. An unconfigured server is reported with a
// message the settings page can show directly.
func (s *Service) webDAVClient() (*webdav.Client, string, error) {
	settings := s.store.Settings()
	baseURL := strings.TrimSpace(settings.WebDavURL)
	if baseURL == "" {
		return nil, "", errors.New("请先配置 WebDAV 地址")
	}
	remotePath := strings.TrimSpace(settings.WebDavPath)
	if remotePath == "" {
		remotePath = defaultWebDAVPath
	}
	return webdav.New(baseURL, settings.WebDavUsername, settings.WebDavPassword), remotePath, nil
}

// WebDAVTest verifies that the configured WebDAV server is reachable and that
// the account may write the backup. A temporary probe file is uploaded next to
// the configured backup path and removed again.
func (s *Service) WebDAVTest() error {
	client, remotePath, err := s.webDAVClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), webDAVTestTimeout)
	defer cancel()
	return client.TestPath(ctx, remotePath)
}

// WebDAVUpload exports the complete configuration and stores it on the
// configured WebDAV server. It returns the absolute remote URL of the uploaded
// backup so the frontend can display where the data was written.
func (s *Service) WebDAVUpload() (string, error) {
	client, remotePath, err := s.webDAVClient()
	if err != nil {
		return "", err
	}

	data, err := s.store.Export()
	if err != nil {
		return "", fmt.Errorf("导出备份失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), webDAVSyncTimeout)
	defer cancel()
	if err := client.Upload(ctx, remotePath, data); err != nil {
		return "", err
	}
	return client.RemoteURL(remotePath)
}

// WebDAVDownload downloads the backup from the configured WebDAV server and
// restores it. The backup is validated by Restore before anything is written,
// so a damaged remote file never leaves the configuration partially restored.
func (s *Service) WebDAVDownload() (model.BackupResult, error) {
	client, remotePath, err := s.webDAVClient()
	if err != nil {
		return model.BackupResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), webDAVSyncTimeout)
	defer cancel()
	data, err := client.Download(ctx, remotePath)
	if err != nil {
		return model.BackupResult{}, err
	}
	result, err := s.store.Restore(data)
	if err != nil {
		return result, err
	}
	s.notifySettingsChanged()
	return result, nil
}
