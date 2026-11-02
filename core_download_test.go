package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCoreDownloadFromLocalServer(t *testing.T) {
	fixture := filepath.Join(".tools", "xray", "xray.zip")
	zipData, err := os.ReadFile(fixture)
	if err != nil {
		t.Skipf("local Xray archive fixture not available: %v", err)
	}

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

	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewCoreService(store)
	service.releaseURL = server.URL + "/release"
	service.client = server.Client()

	info, err := service.DownloadLatest()
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !info.Installed || info.Version == "" {
		t.Fatalf("unexpected core info: %+v", info)
	}
	if _, err := os.Stat(info.Path); err != nil {
		t.Fatalf("core binary not installed: %v", err)
	}
	if info.LatestVersion != "26.10.31" {
		t.Fatalf("unexpected latest version: %s", info.LatestVersion)
	}
}
