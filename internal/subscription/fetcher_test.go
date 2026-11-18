package subscription

import (
	"context"
	"io"
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

// newTestForwardProxy starts a minimal HTTP forward proxy for the fetcher
// tests. It relays each request to the absolute URL from the request line and
// stamps a marker header on the outbound request so tests can prove the fetch
// really went through the proxy instead of dialing the target directly.
func newTestForwardProxy(t *testing.T) *httptest.Server {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.IsAbs() {
			http.Error(w, "expected an absolute request URI", http.StatusBadRequest)
			return
		}
		targetURL := *r.URL
		outbound := r.Clone(r.Context())
		outbound.URL = &targetURL
		outbound.RequestURI = ""
		outbound.Header.Set("X-FastV-Test-Proxy", "1")

		response, err := http.DefaultTransport.RoundTrip(outbound)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

func TestFetchWithMetaProxyRoutesThroughHTTPProxy(t *testing.T) {
	const userAgent = "ProxyAgent/2.0"
	headers := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=1893456000")
		_, _ = w.Write([]byte("subscription-body"))
	}))
	defer target.Close()
	proxy := newTestForwardProxy(t)

	result, err := FetchWithMetaProxy(context.Background(), target.URL+"/sub", userAgent, proxy.URL)
	if err != nil {
		t.Fatalf("fetch through proxy failed: %v", err)
	}
	got := <-headers
	if got.Get("X-FastV-Test-Proxy") != "1" {
		t.Fatal("target request did not pass through the test proxy")
	}
	if got.Get("User-Agent") != userAgent {
		t.Fatalf("expected User-Agent %q, got %q", userAgent, got.Get("User-Agent"))
	}
	if result.Content != "subscription-body" {
		t.Fatalf("unexpected body: %q", result.Content)
	}
	if !result.UserInfo.Present || result.UserInfo.TotalBytes != 3 {
		t.Fatalf("unexpected user info: %+v", result.UserInfo)
	}
}

func TestFetchWithMetaProxyWithoutProxyFetchesDirectly(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-FastV-Test-Proxy") != "" {
			t.Error("direct fetch unexpectedly used the test proxy")
		}
		_, _ = w.Write([]byte("subscription-body"))
	}))
	defer target.Close()

	result, err := FetchWithMetaProxy(context.Background(), target.URL, "", "   ")
	if err != nil {
		t.Fatalf("fetch without proxy failed: %v", err)
	}
	if result.Content != "subscription-body" {
		t.Fatalf("unexpected body: %q", result.Content)
	}
}

func TestFetchWithMetaProxyRejectsInvalidProxyURL(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("target must not be contacted with an invalid proxy URL")
	}))
	defer target.Close()

	for _, proxyURL := range []string{"127.0.0.1:1080", "://bad"} {
		if _, err := FetchWithMetaProxy(context.Background(), target.URL, "", proxyURL); err == nil {
			t.Fatalf("expected an invalid proxy URL error for %q", proxyURL)
		}
	}
}

func TestFetchWithMetaProxyReturnsHTTPErrorStatus(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer target.Close()
	proxy := newTestForwardProxy(t)

	_, err := FetchWithMetaProxy(context.Background(), target.URL, "", proxy.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected an HTTP 404 error, got %v", err)
	}
}
