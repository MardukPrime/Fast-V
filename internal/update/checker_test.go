package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestChecker starts an httptest server with handler and returns a Checker
// pointing at it.
func newTestChecker(t *testing.T, handler http.HandlerFunc) *Checker {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Checker{APIURL: server.URL, Client: server.Client()}
}

// releaseJSON renders a GitHub release payload for the given tag.
func releaseJSON(tagName, body, htmlURL string) string {
	raw, err := json.Marshal(map[string]string{
		"tag_name": tagName,
		"body":     body,
		"html_url": htmlURL,
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestCheckReportsAvailableUpdate(t *testing.T) {
	const releaseURL = "https://github.com/MardukPrime/Fast-V/releases/tag/v0.2.0"
	var mu sync.Mutex
	var seenAccept, seenUserAgent, seenMethod string

	checker := newTestChecker(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenAccept = r.Header.Get("Accept")
		seenUserAgent = r.Header.Get("User-Agent")
		seenMethod = r.Method
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releaseJSON("v0.2.0", "## 更新内容\n- 修复若干问题\n", releaseURL)))
	})

	info, err := checker.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if info.Error != "" {
		t.Errorf("UpdateInfo.Error = %q, want empty", info.Error)
	}
	if info.CurrentVersion != "0.1.0" {
		t.Errorf("CurrentVersion = %q, want 0.1.0", info.CurrentVersion)
	}
	if info.LatestVersion != "0.2.0" {
		t.Errorf("LatestVersion = %q, want 0.2.0", info.LatestVersion)
	}
	if !info.Available {
		t.Error("Available = false, want true")
	}
	if !strings.Contains(info.Notes, "修复若干问题") {
		t.Errorf("Notes = %q, want it to contain the release body", info.Notes)
	}
	if info.URL != releaseURL {
		t.Errorf("URL = %q, want %q", info.URL, releaseURL)
	}
	if _, err := time.Parse(time.RFC3339, info.CheckedAt); err != nil {
		t.Errorf("CheckedAt = %q, want an RFC3339 timestamp: %v", info.CheckedAt, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if seenMethod != http.MethodGet {
		t.Errorf("request method = %q, want GET", seenMethod)
	}
	if seenAccept != "application/vnd.github+json" {
		t.Errorf("Accept header = %q, want application/vnd.github+json", seenAccept)
	}
	if !strings.HasPrefix(seenUserAgent, "FastV/") {
		t.Errorf("User-Agent = %q, want a FastV/... value", seenUserAgent)
	}
}

func TestCheckReportsUpToDate(t *testing.T) {
	checker := newTestChecker(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(releaseJSON("v0.1.0", "stable", "")))
	})

	info, err := checker.Check(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
	if info.Available {
		t.Error("Available = true, want false for the same version")
	}
	if info.LatestVersion != "0.1.0" {
		t.Errorf("LatestVersion = %q, want 0.1.0", info.LatestVersion)
	}
}

func TestCheckIgnoresPrereleaseSuffix(t *testing.T) {
	cases := []struct {
		name           string
		tag            string
		currentVersion string
		wantAvailable  bool
	}{
		{name: "newer prerelease", tag: "v0.2.0-beta.1", currentVersion: "0.1.0", wantAvailable: true},
		{name: "same version with prerelease", tag: "v0.2.0-rc.1", currentVersion: "0.2.0", wantAvailable: false},
		{name: "build metadata", tag: "v0.2.0+build.7", currentVersion: "0.2.0", wantAvailable: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checker := newTestChecker(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(releaseJSON(testCase.tag, "", "")))
			})
			info, err := checker.Check(context.Background(), testCase.currentVersion)
			if err != nil {
				t.Fatalf("Check() error = %v, want nil", err)
			}
			if info.Available != testCase.wantAvailable {
				t.Errorf("Available = %v, want %v", info.Available, testCase.wantAvailable)
			}
		})
	}
}

func TestCheckInvalidJSONReturnsInfoWithError(t *testing.T) {
	checker := newTestChecker(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": `))
	})

	info, err := checker.Check(context.Background(), "0.1.0")
	if err == nil {
		t.Fatal("Check() error = nil, want a decode error")
	}
	if info.Error == "" {
		t.Error("UpdateInfo.Error is empty, want the decode error")
	}
	if info.Available {
		t.Error("Available = true, want false on a failed check")
	}
	if info.CurrentVersion != "0.1.0" {
		t.Errorf("CurrentVersion = %q, want 0.1.0 even on failure", info.CurrentVersion)
	}
}

func TestCheckHTTPErrorReturnsInfoWithError(t *testing.T) {
	checker := newTestChecker(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusInternalServerError)
	})

	info, err := checker.Check(context.Background(), "0.1.0")
	if err == nil {
		t.Fatal("Check() error = nil, want an HTTP error")
	}
	if !strings.Contains(info.Error, "500") {
		t.Errorf("UpdateInfo.Error = %q, want it to mention HTTP 500", info.Error)
	}
	if info.Available {
		t.Error("Available = true, want false on a failed check")
	}
}

func TestCheckMissingTagReturnsError(t *testing.T) {
	checker := newTestChecker(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"","body":"no tag"}`))
	})

	info, err := checker.Check(context.Background(), "0.1.0")
	if err == nil {
		t.Fatal("Check() error = nil, want a missing-tag error")
	}
	if !strings.Contains(info.Error, "tag_name") {
		t.Errorf("UpdateInfo.Error = %q, want it to mention tag_name", info.Error)
	}
}

func TestCheckNetworkFailureReturnsInfoWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	apiURL := server.URL
	server.Close()

	checker := &Checker{APIURL: apiURL, Client: &http.Client{Timeout: 2 * time.Second}}
	info, err := checker.Check(context.Background(), "0.1.0")
	if err == nil {
		t.Fatal("Check() error = nil, want a network error")
	}
	if info.Error == "" {
		t.Error("UpdateInfo.Error is empty, want the network error")
	}
}

func TestCheckUsesDefaultClientWhenNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(releaseJSON("v0.1.1", "", "")))
	}))
	t.Cleanup(server.Close)

	checker := &Checker{APIURL: server.URL}
	info, err := checker.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatalf("Check() with nil Client error = %v, want nil", err)
	}
	if !info.Available {
		t.Error("Available = false, want true")
	}
}

func TestCheckContextCancelledReturnsError(t *testing.T) {
	checker := newTestChecker(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	info, err := checker.Check(ctx, "0.1.0")
	if err == nil {
		t.Fatal("Check() error = nil, want a context error")
	}
	if info.Error == "" {
		t.Error("UpdateInfo.Error is empty, want the context error")
	}
}

func TestDefaultAPIURLPointsAtFastV(t *testing.T) {
	const want = "https://api.github.com/repos/MardukPrime/Fast-V/releases/latest"
	if DefaultAPIURL != want {
		t.Errorf("DefaultAPIURL = %q, want %q", DefaultAPIURL, want)
	}
	if New().APIURL != want {
		t.Errorf("New().APIURL = %q, want %q", New().APIURL, want)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "1.2.3", b: "1.2.3", want: 0},
		{name: "v prefix equal", a: "v1.2.3", b: "1.2.3", want: 0},
		{name: "v prefix newer", a: "v0.2.0", b: "0.1.0", want: 1},
		{name: "v prefix older", a: "0.1.0", b: "v0.2.0", want: -1},
		{name: "missing segment", a: "1.2", b: "1.2.0", want: 0},
		{name: "extra zero segment", a: "1.2.0.0", b: "1.2", want: 0},
		{name: "patch bump", a: "1.2.10", b: "1.2.9", want: 1},
		{name: "minor bump", a: "1.3.0", b: "1.2.99", want: 1},
		{name: "numeric not lexical", a: "10.0.0", b: "9.9.9", want: 1},
		{name: "extra segment newer", a: "1.2.3.1", b: "1.2.3", want: 1},
		{name: "prerelease ignored newer", a: "1.2.3-beta.1", b: "1.2.3", want: 0},
		{name: "prerelease ignored equal", a: "v1.2.3-rc.2", b: "1.2.3", want: 0},
		{name: "build metadata ignored", a: "1.2.3+build.5", b: "1.2.3", want: 0},
		{name: "empty current", a: "0.0.1", b: "", want: 1},
		{name: "dev version", a: "0.0.1", b: "dev", want: 1},
		{name: "both unparsable", a: "dev", b: "", want: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := CompareVersions(testCase.a, testCase.b); got != testCase.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", testCase.a, testCase.b, got, testCase.want)
			}
		})
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v1.2.3":  "1.2.3",
		"V1.2.3":  "1.2.3",
		" 0.1.0 ": "0.1.0",
		"1.2.3":   "1.2.3",
		"":        "",
	}
	for input, want := range cases {
		if got := normalizeVersion(input); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}
