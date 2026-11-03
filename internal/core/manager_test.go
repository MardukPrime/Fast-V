package core

import (
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

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"26.10.31", "26.10.31", 0},
		{"v26.10.31", "26.10.31", 0},
		{"26.10.32", "26.10.31", 1},
		{"26.9.1", "26.10.0", -1},
		{"25.3.6", "26.1.0", -1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.left, tc.right); got != tc.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tc.left, tc.right, got, tc.want)
		}
	}
}

func TestDigestMatches(t *testing.T) {
	actual := "2e04a9995a6f2ad1b28c0bb25c708ecb2695e43a1c9c7889c4b6ac2b48cf7a6a"
	if !digestMatches("sha256:"+actual, actual) {
		t.Fatal("expected digest to match")
	}
	if digestMatches("sha256:deadbeef", actual) {
		t.Fatal("expected digest mismatch")
	}
	if digestMatches("", actual) {
		t.Fatal("empty digest should not match")
	}
}

func TestFindReleaseAsset(t *testing.T) {
	name, err := coreAssetName()
	if err != nil {
		t.Skipf("unsupported test platform: %v", err)
	}
	release := &releaseInfo{
		TagName: "v26.10.31",
		Assets: []releaseAsset{
			{Name: name, BrowserDownloadURL: "https://example.com/xray.zip"},
		},
	}
	asset, err := findReleaseAsset(release)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Name != name {
		t.Fatalf("expected %s, got %s", name, asset.Name)
	}
}

func TestDownloadLatestFromLocalServer(t *testing.T) {
	fixture := filepath.Join("..", "..", ".tools", "xray", "xray.zip")
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

	manager := NewManager(t.TempDir())
	manager.releaseURL = server.URL + "/release"
	manager.client = server.Client()

	info, err := manager.DownloadLatest(context.Background())
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
