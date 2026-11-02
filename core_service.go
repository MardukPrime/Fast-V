package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const xrayReleaseAPI = "https://api.github.com/repos/XTLS/Xray-core/releases/latest"

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type coreVersionRecord struct {
	Version     string `json:"version"`
	AssetName   string `json:"assetName"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installedAt"`
}

// CoreService detects, downloads and installs the Xray-core binary.
type CoreService struct {
	mu         sync.RWMutex
	opMu       sync.Mutex
	store      *Store
	dir        string
	client     *http.Client
	releaseURL string
	info       CoreInfo
}

func NewCoreService(store *Store) *CoreService {
	dir := filepath.Join(filepath.Dir(store.path), "cores", "xray")
	service := &CoreService{
		store: store,
		dir:   dir,
		client: &http.Client{
			Timeout: 15 * time.Minute,
		},
		info: CoreInfo{
			Path:       filepath.Join(dir, coreBinaryName()),
			Status:     "not_installed",
			Message:    "尚未安装 Xray-core",
			AutoUpdate: store.Settings().CoreAutoUpdate,
		},
	}
	service.releaseURL = xrayReleaseAPI
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	service.detectInstalled(ctx)
	return service
}
func (s *CoreService) ServiceName() string {
	return "CoreService"
}

func (s *CoreService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.detectInstalled(ctx)
	if s.store.Settings().CoreAutoUpdate {
		go func() {
			select {
			case <-time.After(3 * time.Second):
				_, _ = s.AutoUpdate()
			case <-ctx.Done():
				return
			}
		}()
	}
	return nil
}

func (s *CoreService) Info() CoreInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info := s.info
	info.AutoUpdate = s.store.Settings().CoreAutoUpdate
	return info
}

func (s *CoreService) CheckLatest() (CoreInfo, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	info := s.Info()
	info.Status = "checking"
	info.Message = "正在检查 Xray-core 更新"
	s.setInfo(info)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	release, err := s.fetchLatestRelease(ctx)
	if err != nil {
		info.Status = "error"
		info.Message = err.Error()
		s.setInfo(info)
		return info, err
	}

	info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	info.CheckedAt = nowRFC3339()
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
	s.setInfo(info)
	return info, nil
}

func (s *CoreService) DownloadLatest() (CoreInfo, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	info := s.Info()
	info.Status = "downloading"
	info.Message = "正在下载 Xray-core"
	s.setInfo(info)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	release, err := s.fetchLatestRelease(ctx)
	if err != nil {
		return s.fail(info, err)
	}

	asset, err := findReleaseAsset(release)
	if err != nil {
		return s.fail(info, err)
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(s.dir), ".xray-staging-")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(s.dir), 0o700); mkErr != nil {
			return s.fail(info, mkErr)
		}
		stagingDir, err = os.MkdirTemp(filepath.Dir(s.dir), ".xray-staging-")
		if err != nil {
			return s.fail(info, err)
		}
	}
	defer os.RemoveAll(stagingDir)

	archivePath := filepath.Join(stagingDir, "xray.zip")
	checksum, err := downloadFile(ctx, s.client, asset.BrowserDownloadURL, archivePath)
	if err != nil {
		return s.fail(info, err)
	}
	if asset.Digest != "" && !digestMatches(asset.Digest, checksum) {
		return s.fail(info, fmt.Errorf("download checksum mismatch"))
	}

	extractDir := filepath.Join(stagingDir, "extracted")
	if err := extractZip(archivePath, extractDir); err != nil {
		return s.fail(info, err)
	}

	stagedBinary := filepath.Join(extractDir, coreBinaryName())
	version, err := readCoreVersion(ctx, stagedBinary)
	if err != nil {
		return s.fail(info, fmt.Errorf("downloaded core failed validation: %w", err))
	}

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return s.fail(info, err)
	}
	if err := installCoreFiles(extractDir, s.dir); err != nil {
		return s.fail(info, err)
	}
	if err := writeCoreVersionRecord(s.dir, coreVersionRecord{
		Version:     version,
		AssetName:   asset.Name,
		SHA256:      checksum,
		InstalledAt: nowRFC3339(),
	}); err != nil {
		return s.fail(info, err)
	}

	info = s.detectInstalled(ctx)
	info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	info.AssetName = asset.Name
	info.CheckedAt = nowRFC3339()
	info.Status = "ready"
	info.Message = "Xray-core 已更新到 " + version
	s.setInfo(info)
	return info, nil
}

func (s *CoreService) AutoUpdate() (CoreUpdateResult, error) {
	info, err := s.CheckLatest()
	if err != nil {
		return CoreUpdateResult{Info: info, Message: err.Error()}, err
	}
	if info.Installed && compareVersions(info.Version, info.LatestVersion) >= 0 {
		return CoreUpdateResult{
			Updated: false,
			Info:    info,
			Message: "Xray-core 已是最新版本",
		}, nil
	}

	updated, err := s.DownloadLatest()
	if err != nil {
		return CoreUpdateResult{Info: updated, Message: err.Error()}, err
	}
	return CoreUpdateResult{
		Updated: true,
		Info:    updated,
		Message: "Xray-core 已更新到 " + updated.Version,
	}, nil
}

func (s *CoreService) detectInstalled(ctx context.Context) CoreInfo {
	info := s.Info()
	info.Path = filepath.Join(s.dir, coreBinaryName())
	info.AutoUpdate = s.store.Settings().CoreAutoUpdate

	if _, err := os.Stat(info.Path); err != nil {
		info.Installed = false
		info.Version = ""
		info.Status = "not_installed"
		info.Message = "尚未安装 Xray-core"
		s.setInfo(info)
		return info
	}

	version, err := readCoreVersion(ctx, info.Path)
	if err != nil {
		info.Installed = true
		info.Status = "error"
		info.Message = err.Error()
		s.setInfo(info)
		return info
	}

	info.Installed = true
	info.Version = version
	info.Status = "ready"
	info.Message = "Xray-core 已就绪"
	s.setInfo(info)
	return info
}

func (s *CoreService) fetchLatestRelease(ctx context.Context) (*releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.releaseURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.client.Do(req)
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

func (s *CoreService) setInfo(info CoreInfo) {
	s.mu.Lock()
	s.info = info
	s.mu.Unlock()
	if app := application.Get(); app != nil {
		app.Event.Emit("core:state", info)
	}
}

func (s *CoreService) fail(info CoreInfo, err error) (CoreInfo, error) {
	info.Status = "error"
	info.Message = err.Error()
	s.setInfo(info)
	return info, err
}

func coreBinaryName() string {
	if runtime.GOOS == "windows" {
		return "xray.exe"
	}
	return "xray"
}

func findReleaseAsset(release *releaseInfo) (releaseAsset, error) {
	wanted, err := coreAssetName()
	if err != nil {
		return releaseAsset{}, err
	}
	for _, asset := range release.Assets {
		if asset.Name == wanted {
			return asset, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("Xray-core release has no asset %s", wanted)
}

func coreAssetName() (string, error) {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-windows-64.zip", nil
		case "arm64":
			return "Xray-windows-arm64-v8a.zip", nil
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-linux-64.zip", nil
		case "arm64":
			return "Xray-linux-arm64-v8a.zip", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-macos-64.zip", nil
		case "arm64":
			return "Xray-macos-arm64-v8a.zip", nil
		}
	}
	return "", fmt.Errorf("unsupported platform for Xray-core: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func readCoreVersion(ctx context.Context, binary string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	output, err := exec.CommandContext(commandCtx, binary, "version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to read Xray-core version: %w", err)
	}
	line := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	fields := strings.Fields(line)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "Xray") {
		return strings.TrimPrefix(fields[1], "v"), nil
	}
	return "", fmt.Errorf("unexpected Xray-core version output: %s", line)
}

func compareVersions(a, b string) int {
	left := parseVersionParts(a)
	right := parseVersionParts(b)
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	for i := 0; i < length; i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l > r {
			return 1
		}
		if l < r {
			return -1
		}
	}
	return 0
}

func parseVersionParts(value string) []int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		number := 0
		for _, char := range part {
			if char < '0' || char > '9' {
				break
			}
			number = number*10 + int(char-'0')
		}
		out = append(out, number)
	}
	return out
}

func downloadFile(ctx context.Context, client *http.Client, url string, destination string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "FastV/0.1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download Xray-core: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Xray-core download returned HTTP %d", resp.StatusCode)
	}

	file, err := os.Create(destination)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), resp.Body); err != nil {
		return "", fmt.Errorf("failed to save Xray-core download: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestMatches(digest string, actual string) bool {
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(digest, "sha256:") {
		return strings.EqualFold(strings.TrimPrefix(digest, "sha256:"), actual)
	}
	return false
}

func extractZip(archivePath string, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}

	for _, file := range reader.File {
		name := filepath.Clean(file.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(destination, name)
		cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(target), cleanDestination) {
			return fmt.Errorf("invalid archive path: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o600
		}
		dest, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(dest, source)
		source.Close()
		dest.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func installCoreFiles(sourceDir string, destinationDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(destinationDir, entry.Name())
		mode := os.FileMode(0o600)
		if entry.Name() == coreBinaryName() {
			mode = 0o755
		}
		if err := copyFileAtomic(source, target, mode); err != nil {
			return err
		}
	}
	return nil
}

func copyFileAtomic(source string, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	temp := destination + ".new"
	output, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, destination); err != nil {
		_ = os.Remove(destination)
		if err := os.Rename(temp, destination); err != nil {
			_ = os.Remove(temp)
			return err
		}
	}
	return nil
}

func writeCoreVersionRecord(directory string, record coreVersionRecord) error {
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "version.json"), raw, 0o600)
}
