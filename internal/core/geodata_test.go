package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testGeoZip(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte("test-" + name))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestUpdateGeoDataFromLocalServer(t *testing.T) {
	zipData := testGeoZip(t)
	assetName, err := coreAssetName()
	if err != nil {
		t.Skipf("unsupported platform: %v", err)
	}
	sum := sha256.Sum256(zipData)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v26.10.31",
				"assets": []map[string]any{{
					"name":                 assetName,
					"browser_download_url": server.URL + "/xray.zip",
					"size":                 len(zipData),
					"digest":               digest,
				}},
			})
		case "/xray.zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(zipData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dataDir := t.TempDir()
	manager := NewManager(dataDir)
	manager.releaseURL = server.URL + "/release"
	manager.client = server.Client()

	info, err := manager.UpdateGeoData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.GeoIP == nil || info.GeoSite == nil {
		t.Fatalf("missing geo files: %+v", info)
	}
	if info.GeoIP.Size == 0 || info.GeoSite.Size == 0 {
		t.Fatalf("geo files are empty: %+v", info)
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		if _, err := os.Stat(filepath.Join(dataDir, "cores", "xray", name)); err != nil {
			t.Fatalf("missing installed geo file %s: %v", name, err)
		}
	}
}

func TestGeoDataInfoEmpty(t *testing.T) {
	info := NewManager(t.TempDir()).GeoDataInfo()
	if len(info.Files) != 0 || info.GeoIP != nil || info.GeoSite != nil {
		t.Fatalf("unexpected empty geo info: %+v", info)
	}
}
