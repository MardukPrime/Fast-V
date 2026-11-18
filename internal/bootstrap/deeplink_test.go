package bootstrap

import (
	"errors"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/deeplink"
)

// fakeDeepLinkImporter records the requests the handler applies and returns a
// canned outcome or error, so handler tests never touch the real services.
type fakeDeepLinkImporter struct {
	requests []deeplink.Request
	outcome  deepLinkOutcome
	err      error
}

func (f *fakeDeepLinkImporter) Import(request deeplink.Request) (deepLinkOutcome, error) {
	f.requests = append(f.requests, request)
	return f.outcome, f.err
}

func TestHandleDeepLinkImportsSubscription(t *testing.T) {
	importer := &fakeDeepLinkImporter{outcome: deepLinkOutcome{Name: "demo", Message: "订阅「demo」已导入"}}
	raw := "fastv://import?url=https%3A%2F%2Fexample.com%2Fsub%3Ftoken%3Dabc"
	if err := handleDeepLink(raw, importer); err != nil {
		t.Fatalf("handleDeepLink(%q) returned error: %v", raw, err)
	}
	if len(importer.requests) != 1 {
		t.Fatalf("importer called %d times, want 1", len(importer.requests))
	}
	request := importer.requests[0]
	if request.Kind != deeplink.KindSubscription {
		t.Fatalf("request.Kind = %q, want %q", request.Kind, deeplink.KindSubscription)
	}
	if request.URL != "https://example.com/sub?token=abc" {
		t.Fatalf("request.URL = %q, want the decoded subscription URL", request.URL)
	}
}

func TestHandleDeepLinkImportsNode(t *testing.T) {
	importer := &fakeDeepLinkImporter{outcome: deepLinkOutcome{Name: "node-one", Message: "节点「node-one」已导入"}}
	raw := "fastv://node?link=vless%3A%2F%2Fuuid%40example.com%3A443%23node-one"
	if err := handleDeepLink(raw, importer); err != nil {
		t.Fatalf("handleDeepLink(%q) returned error: %v", raw, err)
	}
	if len(importer.requests) != 1 {
		t.Fatalf("importer called %d times, want 1", len(importer.requests))
	}
	request := importer.requests[0]
	if request.Kind != deeplink.KindNode {
		t.Fatalf("request.Kind = %q, want %q", request.Kind, deeplink.KindNode)
	}
	if request.URL != "vless://uuid@example.com:443#node-one" {
		t.Fatalf("request.URL = %q, want the decoded share link", request.URL)
	}
}

func TestHandleDeepLinkRejectsInvalidLinkWithoutImporting(t *testing.T) {
	importer := &fakeDeepLinkImporter{}
	if err := handleDeepLink("https://example.com/sub", importer); err == nil {
		t.Fatal("handleDeepLink accepted a plain http URL")
	}
	if len(importer.requests) != 0 {
		t.Fatalf("importer was called %d times for an invalid link", len(importer.requests))
	}
}

func TestHandleDeepLinkPropagatesImportError(t *testing.T) {
	want := errors.New("订阅下载失败")
	importer := &fakeDeepLinkImporter{err: want}
	err := handleDeepLink("fastv://node?link=vless%3A%2F%2Fuuid%40example.com%3A443", importer)
	if !errors.Is(err, want) {
		t.Fatalf("handleDeepLink error = %v, want %v", err, want)
	}
}

func TestHandleDeepLinkWithNilImporter(t *testing.T) {
	if err := handleDeepLink("fastv://import?url=https%3A%2F%2Fexample.com%2Fsub", nil); err == nil {
		t.Fatal("handleDeepLink accepted a nil importer")
	}
}

// TestHandleDeepLinkWithoutServices makes sure the exported entry point is safe
// before any Wails application or service exists (unit test environment).
func TestHandleDeepLinkWithoutServices(t *testing.T) {
	if err := HandleDeepLink("fastv://import?url=https%3A%2F%2Fexample.com%2Fsub", nil, nil); err == nil {
		t.Fatal("HandleDeepLink accepted nil services")
	}
	if err := HandleDeepLink("fastv://node?link=vless%3A%2F%2Fuuid%40example.com%3A443", nil, nil); err == nil {
		t.Fatal("HandleDeepLink accepted nil services for a node link")
	}
	if err := HandleDeepLink("", nil, nil); err == nil {
		t.Fatal("HandleDeepLink accepted an empty link")
	}
}

func TestParseDeepLinkArgs(t *testing.T) {
	args := []string{
		`C:\Program Files\FastV\FastV.exe`,
		"--debug",
		"fastv://import?url=https%3A%2F%2Fexample.com%2Fsub",
		`"fastv://node?link=trojan%3A%2F%2Fsecret%40example.com%3A443%23node"`,
		"fastv://import?url=https%3A%2F%2Fexample.com%2Fsub", // duplicate of the link above
		"vless://uuid@example.com:443#raw",
	}
	requests := parseDeepLinkArgs(args)
	if len(requests) != 3 {
		t.Fatalf("parseDeepLinkArgs returned %d requests, want 3: %+v", len(requests), requests)
	}
	if requests[0].Kind != deeplink.KindSubscription || requests[0].URL != "https://example.com/sub" {
		t.Fatalf("requests[0] = %+v, want the subscription link", requests[0])
	}
	if requests[1].Kind != deeplink.KindNode || requests[1].URL != "trojan://secret@example.com:443#node" {
		t.Fatalf("requests[1] = %+v, want the quoted node link", requests[1])
	}
	if requests[2].Kind != deeplink.KindNode || requests[2].URL != "vless://uuid@example.com:443#raw" {
		t.Fatalf("requests[2] = %+v, want the raw share link", requests[2])
	}
}

func TestParseDeepLinkArgsIgnoresNonLinks(t *testing.T) {
	requests := parseDeepLinkArgs([]string{
		`C:\Apps\FastV.exe`,
		"--debug",
		"",
		"https://example.com/sub",
		"not a link",
	})
	if len(requests) != 0 {
		t.Fatalf("parseDeepLinkArgs returned %d requests for non-link arguments: %+v", len(requests), requests)
	}
}

// TestSecondInstanceLaunchWithoutWindowIsSafe covers the case where a second
// instance appears before the main window has been created: the callback must
// not panic and must ignore arguments that are not deep links.
func TestSecondInstanceLaunchWithoutWindowIsSafe(t *testing.T) {
	integration := newDeepLinkIntegration(nil, nil)
	integration.onSecondInstanceLaunch(application.SecondInstanceData{
		Args: []string{`C:\Apps\FastV.exe`},
	})
}
