package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// GeoDataInfo returns the current geo asset files next to Xray-core.
func (m *Manager) GeoDataInfo() model.GeoDataInfo {
	info := model.GeoDataInfo{
		Directory: m.dir,
		Files:     []model.GeoDataFile{},
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return info
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".dat") {
			continue
		}
		file, err := entry.Info()
		if err != nil {
			continue
		}
		item := model.GeoDataFile{
			Name:      entry.Name(),
			Size:      file.Size(),
			UpdatedAt: file.ModTime().Format(time.RFC3339),
		}
		info.Files = append(info.Files, item)
		switch strings.ToLower(item.Name) {
		case "geoip.dat":
			fileCopy := item
			info.GeoIP = &fileCopy
		case "geosite.dat":
			fileCopy := item
			info.GeoSite = &fileCopy
		}
	}
	sort.Slice(info.Files, func(i, j int) bool { return info.Files[i].Name < info.Files[j].Name })
	return info
}

// UpdateGeoData downloads the latest Xray release and updates only *dat assets.
func (m *Manager) UpdateGeoData(ctx context.Context) (model.GeoDataInfo, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	release, err := m.fetchLatestRelease(ctx)
	if err != nil {
		return model.GeoDataInfo{}, err
	}
	asset, err := findReleaseAsset(release)
	if err != nil {
		return model.GeoDataInfo{}, err
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(m.dir), ".xray-geo-staging-")
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(m.dir), 0o700); mkErr != nil {
			return model.GeoDataInfo{}, mkErr
		}
		stagingDir, err = os.MkdirTemp(filepath.Dir(m.dir), ".xray-geo-staging-")
		if err != nil {
			return model.GeoDataInfo{}, err
		}
	}
	defer os.RemoveAll(stagingDir)

	archivePath := filepath.Join(stagingDir, "xray.zip")
	checksum, err := downloadFile(ctx, m.client, asset.BrowserDownloadURL, archivePath)
	if err != nil {
		return model.GeoDataInfo{}, err
	}
	if asset.Digest != "" && !digestMatches(asset.Digest, checksum) {
		return model.GeoDataInfo{}, fmt.Errorf("geo data checksum mismatch")
	}

	extractDir := filepath.Join(stagingDir, "extracted")
	if err := extractZip(archivePath, extractDir); err != nil {
		return model.GeoDataInfo{}, err
	}

	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return model.GeoDataInfo{}, err
	}
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return model.GeoDataInfo{}, err
	}
	copied := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".dat") {
			continue
		}
		source := filepath.Join(extractDir, entry.Name())
		target := filepath.Join(m.dir, entry.Name())
		if err := copyFileAtomic(source, target, 0o600); err != nil {
			return model.GeoDataInfo{}, err
		}
		copied++
	}
	if copied == 0 {
		return model.GeoDataInfo{}, fmt.Errorf("Xray archive contains no geo data files")
	}
	return m.GeoDataInfo(), nil
}
