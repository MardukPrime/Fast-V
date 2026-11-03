package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

const xrayReleaseAPI = "https://api.github.com/repos/XTLS/Xray-core/releases/latest"

// Manager detects, downloads and installs the Xray-core binary.
type Manager struct {
	mu         sync.RWMutex
	opMu       sync.Mutex
	dir        string
	client     *http.Client
	releaseURL string
	info       model.CoreInfo
	autoUpdate bool
	onChange   func(model.CoreInfo)
}

func NewManager(dataDir string) *Manager {
	dir := filepath.Join(dataDir, "cores", "xray")
	return &Manager{
		dir: dir,
		client: &http.Client{
			Timeout: 15 * time.Minute,
		},
		releaseURL: xrayReleaseAPI,
		info: model.CoreInfo{
			Path:    filepath.Join(dir, coreBinaryName()),
			Status:  "not_installed",
			Message: "尚未安装 Xray-core",
		},
	}
}

func (m *Manager) SetAutoUpdate(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoUpdate = enabled
	m.info.AutoUpdate = enabled
}

func (m *Manager) SetOnChange(callback func(model.CoreInfo)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = callback
}

func (m *Manager) Info() model.CoreInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info := m.info
	info.AutoUpdate = m.autoUpdate
	return info
}

func (m *Manager) DetectInstalled(ctx context.Context) model.CoreInfo {
	info := m.Info()
	info.Path = filepath.Join(m.dir, coreBinaryName())
	info.AutoUpdate = m.autoUpdate

	if _, err := os.Stat(info.Path); err != nil {
		info.Installed = false
		info.Version = ""
		info.Status = "not_installed"
		info.Message = "尚未安装 Xray-core"
		m.setInfo(info)
		return info
	}

	version, err := readCoreVersion(ctx, info.Path)
	if err != nil {
		info.Installed = true
		info.Status = "error"
		info.Message = err.Error()
		m.setInfo(info)
		return info
	}

	info.Installed = true
	info.Version = version
	info.Status = "ready"
	info.Message = "Xray-core 已就绪"
	m.setInfo(info)
	return info
}

func (m *Manager) CheckLatest(ctx context.Context) (model.CoreInfo, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.checkLatest(ctx)
}

func (m *Manager) checkLatest(ctx context.Context) (model.CoreInfo, error) {
	info := m.Info()
	info.Status = "checking"
	info.Message = "正在检查 Xray-core 更新"
	m.setInfo(info)

	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		info.Status = "error"
		info.Message = err.Error()
		m.setInfo(info)
		return info, err
	}

	info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	info.CheckedAt = model.NowRFC3339()
	switch {
	case !info.Installed:
		info.Status = "not_installed"
		info.Message = "Xray-core 尚未安装，可下载最新版本"
	case compareVersions(info.Version, info.LatestVersion) >= 0:
		info.Status = "up_to_date"
		info.Message = "Xray-core 已是最新版本"
	default:
		info.Status = "update_available"
		info.Message = "发现 Xray-core 新版本"
	}
	m.setInfo(info)
	return info, nil
}

func (m *Manager) DownloadLatest(ctx context.Context) (model.CoreInfo, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.downloadLatest(ctx)
}

func (m *Manager) downloadLatest(ctx context.Context) (model.CoreInfo, error) {
	info := m.Info()
	info.Status = "downloading"
	info.Message = "正在下载 Xray-core"
	m.setInfo(info)

	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		return m.fail(info, err)
	}

	asset, err := findReleaseAsset(release)
	if err != nil {
		return m.fail(info, err)
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(m.dir), ".xray-staging-")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(m.dir), 0o700); mkErr != nil {
			return m.fail(info, mkErr)
		}
		stagingDir, err = os.MkdirTemp(filepath.Dir(m.dir), ".xray-staging-")
		if err != nil {
			return m.fail(info, err)
		}
	}
	defer os.RemoveAll(stagingDir)

	archivePath := filepath.Join(stagingDir, "xray.zip")
	checksum, err := downloadFile(ctx, m.client, asset.BrowserDownloadURL, archivePath)
	if err != nil {
		return m.fail(info, err)
	}
	if asset.Digest != "" && !digestMatches(asset.Digest, checksum) {
		return m.fail(info, fmt.Errorf("download checksum mismatch"))
	}

	extractDir := filepath.Join(stagingDir, "extracted")
	if err := extractZip(archivePath, extractDir); err != nil {
		return m.fail(info, err)
	}

	stagedBinary := filepath.Join(extractDir, coreBinaryName())
	version, err := readCoreVersion(ctx, stagedBinary)
	if err != nil {
		return m.fail(info, fmt.Errorf("downloaded core failed validation: %w", err))
	}

	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return m.fail(info, err)
	}
	if err := installCoreFiles(extractDir, m.dir); err != nil {
		return m.fail(info, err)
	}
	if err := writeCoreVersionRecord(m.dir, coreVersionRecord{
		Version:     version,
		AssetName:   asset.Name,
		SHA256:      checksum,
		InstalledAt: model.NowRFC3339(),
	}); err != nil {
		return m.fail(info, err)
	}

	info = m.DetectInstalled(ctx)
	info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	info.AssetName = asset.Name
	info.CheckedAt = model.NowRFC3339()
	info.Status = "ready"
	info.Message = "Xray-core 已更新到 " + version
	m.setInfo(info)
	return info, nil
}

func (m *Manager) AutoUpdate(ctx context.Context) (model.CoreUpdateResult, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	info, err := m.checkLatest(ctx)
	if err != nil {
		return model.CoreUpdateResult{Info: info, Message: err.Error()}, err
	}
	if info.Installed && compareVersions(info.Version, info.LatestVersion) >= 0 {
		return model.CoreUpdateResult{
			Updated: false,
			Info:    info,
			Message: "Xray-core 已是最新版本",
		}, nil
	}

	updated, err := m.downloadLatest(ctx)
	if err != nil {
		return model.CoreUpdateResult{Info: updated, Message: err.Error()}, err
	}
	return model.CoreUpdateResult{
		Updated: true,
		Info:    updated,
		Message: "Xray-core 已更新到 " + updated.Version,
	}, nil
}

func (m *Manager) fetchLatestRelease(ctx context.Context) (*releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.releaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to check Xray-core release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Xray-core release check returned HTTP %d", resp.StatusCode)
	}

	var release releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to decode Xray-core release: %w", err)
	}
	if release.TagName == "" {
		return nil, errors.New("Xray-core release has no tag")
	}
	return &release, nil
}

func (m *Manager) setInfo(info model.CoreInfo) {
	m.mu.Lock()
	m.info = info
	callback := m.onChange
	m.mu.Unlock()
	if callback != nil {
		callback(info)
	}
}

func (m *Manager) fail(info model.CoreInfo, err error) (model.CoreInfo, error) {
	info.Status = "error"
	info.Message = err.Error()
	m.setInfo(info)
	return info, err
}
