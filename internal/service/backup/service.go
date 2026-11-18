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
