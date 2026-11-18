package ruleset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchRulesSuccess(t *testing.T) {
	userAgents := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgents <- r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("# a rule set\nexample.com\n1.2.3.4\n"))
	}))
	defer server.Close()

	rules, err := FetchRules(context.Background(), server.URL, KindCombined)
	if err != nil {
		t.Fatalf("FetchRules() error = %v", err)
	}
	if got := collectValues(rules, "domain_suffix"); !equalStrings(got, []string{"example.com"}) {
		t.Fatalf("domain values = %v", got)
	}
	if got := collectValues(rules, "ip"); !equalStrings(got, []string{"1.2.3.4"}) {
		t.Fatalf("ip values = %v", got)
	}
	userAgent := <-userAgents
	if !strings.HasPrefix(userAgent, "FastV/") {
		t.Fatalf("User-Agent = %q, want FastV/<version>", userAgent)
	}
}

func TestFetchRulesHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	rules, err := FetchRules(context.Background(), server.URL, KindCombined)
	if err == nil {
		t.Fatalf("FetchRules() = %v, want HTTP error", rules)
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("error = %q, want it to mention HTTP 404", err)
	}
}

func TestFetchRulesRejectsOversizeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxResponseBytes+1024)))
	}))
	defer server.Close()

	rules, err := FetchRules(context.Background(), server.URL, KindCombined)
	if err == nil {
		t.Fatalf("FetchRules() = %v, want oversize error", len(rules))
	}
	if !strings.Contains(err.Error(), "4 MB") {
		t.Fatalf("error = %q, want it to mention the 4 MB limit", err)
	}
}

func TestFetchRulesEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("   \n\n  "))
	}))
	defer server.Close()

	if _, err := FetchRules(context.Background(), server.URL, KindCombined); err == nil || err.Error() != "规则集内容为空" {
		t.Fatalf("FetchRules() error = %v, want %q", err, "规则集内容为空")
	}
}

func TestFetchRulesInvalidURL(t *testing.T) {
	if _, err := FetchRules(context.Background(), "://invalid", KindCombined); err == nil {
		t.Fatal("FetchRules() error = nil, want invalid URL error")
	}
}

func TestFetchRulesCancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("example.com\n"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FetchRules(ctx, server.URL, KindCombined); err == nil {
		t.Fatal("FetchRules() error = nil, want cancelled context error")
	}
}
