package main

import "testing"

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
