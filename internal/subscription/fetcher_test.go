package subscription

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseUserInfo(t *testing.T) {
	info := ParseUserInfo("upload=1073741824; download=2147483648; total=10737418240; expire=1893456000")
	if !info.Present {
		t.Fatal("expected user info to be present")
	}
	if info.UploadBytes != 1073741824 || info.DownloadBytes != 2147483648 || info.TotalBytes != 10737418240 {
		t.Fatalf("unexpected traffic info: %+v", info)
	}
	if info.ExpireAt != 1893456000 {
		t.Fatalf("unexpected expiry: %+v", info)
	}
}

func TestParseUserInfoExpireMilliseconds(t *testing.T) {
	info := ParseUserInfo("expire=1893456000000")
	if info.ExpireAt != 1893456000 {
		t.Fatalf("expected milliseconds to be normalized: %+v", info)
	}
}

func TestParseUserInfoMissing(t *testing.T) {
	info := ParseUserInfo("")
	if info.Present {
		t.Fatalf("empty header should not be present: %+v", info)
	}
}

func TestDefaultUserAgent(t *testing.T) {
	agent := DefaultUserAgent()
	if !strings.HasPrefix(agent, "FastV/") {
		t.Fatalf("expected the default User-Agent to identify FastV, got %q", agent)
	}
	if strings.TrimSpace(strings.TrimPrefix(agent, "FastV/")) == "" {
		t.Fatalf("expected the default User-Agent to include a version, got %q", agent)
	}
}

func TestFetchWithMetaOptionsCustomUserAgent(t *testing.T) {
	const customUserAgent = "ClashforWindows/0.20.39"
	agents := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.Header.Get("User-Agent")
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=1893456000")
		_, _ = w.Write([]byte("subscription-body"))
	}))
	defer server.Close()

	result, err := FetchWithMetaOptions(context.Background(), server.URL, customUserAgent)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if got := <-agents; got != customUserAgent {
		t.Fatalf("expected custom User-Agent %q, got %q", customUserAgent, got)
	}
	if result.Content != "subscription-body" {
		t.Fatalf("unexpected body: %q", result.Content)
	}
	if !result.UserInfo.Present || result.UserInfo.TotalBytes != 3 {
		t.Fatalf("unexpected user info: %+v", result.UserInfo)
	}
}

func TestFetchWithMetaOptionsDefaultUserAgent(t *testing.T) {
	agents := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("subscription-body"))
	}))
	defer server.Close()

	if _, err := FetchWithMeta(context.Background(), server.URL); err != nil {
		t.Fatalf("fetch with meta failed: %v", err)
	}
	if got := <-agents; !strings.Contains(got, "FastV/") {
		t.Fatalf("expected the default User-Agent to contain FastV/, got %q", got)
	}

	// An empty or whitespace-only custom value must fall back to the default.
	content, err := FetchWithMetaOptions(context.Background(), server.URL, "   ")
	if err != nil {
		t.Fatalf("fetch with options failed: %v", err)
	}
	if content.Content != "subscription-body" {
		t.Fatalf("unexpected body: %q", content.Content)
	}
	if got := <-agents; !strings.Contains(got, "FastV/") {
		t.Fatalf("expected the default User-Agent fallback, got %q", got)
	}
}

func TestFetchWithMetaOptionsTrimsCustomUserAgent(t *testing.T) {
	agents := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("subscription-body"))
	}))
	defer server.Close()

	if _, err := FetchWithMetaOptions(context.Background(), server.URL, "  CustomAgent/1.0  "); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if got := <-agents; got != "CustomAgent/1.0" {
		t.Fatalf("expected the trimmed User-Agent, got %q", got)
	}
}
