package subscription

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func TestServiceAddUpdateDelete(t *testing.T) {
	content := "vless://00000000-0000-0000-0000-000000000003@example.com:443?security=tls#Node"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1024; download=2048; total=1048576; expire=1893456000")
		_, _ = fmt.Fprint(w, encoded)
	}))
	defer server.Close()

	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)

	sub, err := service.Add(model.SubscriptionInput{Name: "Test", URL: server.URL})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if sub.NodeCount != 1 {
		if sub.UploadBytes != 1024 || sub.DownloadBytes != 2048 || sub.TotalBytes != 1048576 || sub.ExpireAt != 1893456000 {
			t.Fatalf("unexpected subscription usage: %+v", sub)
		}
		t.Fatalf("expected 1 node, got %d", sub.NodeCount)
	}

	list := service.List()
	if len(list) != 1 || list[0].Name != "Test" {
		t.Fatalf("unexpected list: %+v", list)
	}

	nodes, err := service.Nodes(sub.ID)
	if err != nil {
		t.Fatalf("nodes failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Protocol != "vless" {
		t.Fatalf("unexpected nodes: %+v", nodes)
	}

	updated, err := service.Update(sub.ID)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if updated.NodeCount != 1 || updated.LastError != "" {
		if updated.UploadBytes != 1024 || updated.TotalBytes != 1048576 {
			t.Fatalf("usage was not retained after update: %+v", updated)
		}
		t.Fatalf("unexpected update result: %+v", updated)
	}

	if err := service.Delete(sub.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(service.List()) != 0 {
		t.Fatal("subscription was not deleted")
	}
}

func TestServiceAddRejectsInvalidURL(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)
	if _, err := service.Add(model.SubscriptionInput{URL: "not-a-url"}); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestServiceQRCode(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)
	subURL := "https://sub.example.com/api/v1/client/subscribe?token=secret-token"
	if err := st.AddSubscription(model.Subscription{ID: "sub-qr", Name: "QR Sub", URL: subURL}, nil); err != nil {
		t.Fatal(err)
	}

	dataURL, err := service.QRCode("sub-qr")
	if err != nil {
		t.Fatalf("qr code failed: %v", err)
	}
	assertPNGDataURL(t, dataURL, 320)

	if _, err := service.QRCode("missing-sub"); err == nil || !strings.Contains(err.Error(), "订阅") {
		t.Fatalf("expected a Chinese not-found error, got %v", err)
	}

	if err := st.AddSubscription(model.Subscription{ID: "sub-empty", Name: "Empty"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.QRCode("sub-empty"); err == nil || !strings.Contains(err.Error(), "订阅链接为空") {
		t.Fatalf("expected an empty-URL error, got %v", err)
	}
}

func TestServiceAddClashSubscription(t *testing.T) {
	content := `port: 7890
mode: rule
proxy-groups:
  - name: Proxy
    type: select
    proxies:
      - Clash VMess
      - Clash SS
proxies:
  - name: Clash VMess
    type: vmess
    server: vmess.example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000001
    alterId: 0
    cipher: auto
    tls: true
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: vmess.example.com
  - name: Clash SS
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: ss-password
  - name: Clash HY2
    type: hysteria2
    server: hy2.example.com
    port: 443
    password: hy2-password
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = fmt.Fprint(w, content)
	}))
	defer server.Close()

	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)
	sub, err := service.Add(model.SubscriptionInput{Name: "Clash Sub", URL: server.URL})
	if err != nil {
		t.Fatalf("add clash subscription failed: %v", err)
	}
	if sub.NodeCount != 2 {
		t.Fatalf("expected 2 importable nodes, got %d", sub.NodeCount)
	}
	nodes, err := service.Nodes(sub.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Protocol != "vmess" || nodes[1].Protocol != "shadowsocks" {
		t.Fatalf("unexpected clash nodes: %+v", nodes)
	}
	for _, node := range nodes {
		if node.Name == "" || node.Address == "" || node.Port <= 0 {
			t.Fatalf("incomplete clash node: %+v", node)
		}
	}

	// Refreshing the subscription keeps working through the Clash parser.
	updated, err := service.Update(sub.ID)
	if err != nil {
		t.Fatalf("update clash subscription failed: %v", err)
	}
	if updated.NodeCount != 2 || updated.LastError != "" {
		t.Fatalf("unexpected update result: %+v", updated)
	}
}

func assertPNGDataURL(t *testing.T, dataURL string, wantSize int) {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("unexpected data URL: %.48q", dataURL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataURL, prefix))
	if err != nil {
		t.Fatalf("data URL is not valid base64: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("data URL is not a PNG image: %v", err)
	}
	if img.Bounds().Dx() != wantSize || img.Bounds().Dy() != wantSize {
		t.Fatalf("expected a %dx%d PNG, got %v", wantSize, wantSize, img.Bounds())
	}
}

func TestServiceAddSetsAutoUpdateDefaults(t *testing.T) {
	content := "vless://00000000-0000-0000-0000-000000000003@example.com:443?security=tls#Node"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, encoded)
	}))
	defer server.Close()

	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(st)

	added, err := service.Add(model.SubscriptionInput{Name: "Defaults", URL: server.URL})
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	// New subscriptions opt into automatic updates on the global schedule and
	// use the default FastV User-Agent until the user customises them.
	if !added.AutoUpdate || added.UpdateIntervalMinutes != 0 || added.UserAgent != "" {
		t.Fatalf("unexpected default options: %+v", added)
	}
	stored, err := st.FindSubscription(added.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.AutoUpdate || stored.UpdateIntervalMinutes != 0 || stored.UserAgent != "" {
		t.Fatalf("default options were not persisted: %+v", stored)
	}
}

func TestServiceSetOptions(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSubscription(model.Subscription{ID: "sub-options", Name: "Options", URL: "https://sub.example.com/api", Enabled: true, AutoUpdate: true}, nil); err != nil {
		t.Fatal(err)
	}
	service := New(st)

	view, err := service.SetOptions("sub-options", false, 60, "  ClashforWindows/0.20.39  ")
	if err != nil {
		t.Fatalf("set options failed: %v", err)
	}
	if view.AutoUpdate || view.UpdateIntervalMinutes != 60 || view.UserAgent != "ClashforWindows/0.20.39" {
		t.Fatalf("unexpected options view: %+v", view)
	}

	// The settings are persisted and echoed back through List().
	list := service.List()
	if len(list) != 1 {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list[0].AutoUpdate || list[0].UpdateIntervalMinutes != 60 || list[0].UserAgent != "ClashforWindows/0.20.39" {
		t.Fatalf("options were not persisted: %+v", list[0])
	}

	// The interval boundary values are accepted.
	for _, interval := range []int{15, 1440} {
		view, err := service.SetOptions("sub-options", true, interval, "Agent/1.0")
		if err != nil {
			t.Fatalf("interval %d should be accepted: %v", interval, err)
		}
		if view.UpdateIntervalMinutes != interval {
			t.Fatalf("expected interval %d to be echoed back, got %+v", interval, view)
		}
	}

	// Zero minutes and an empty User-Agent fall back to the global defaults.
	view, err = service.SetOptions("sub-options", true, 0, "")
	if err != nil {
		t.Fatalf("reset options failed: %v", err)
	}
	if !view.AutoUpdate || view.UpdateIntervalMinutes != 0 || view.UserAgent != "" {
		t.Fatalf("unexpected reset options: %+v", view)
	}
}

func TestServiceSetOptionsValidation(t *testing.T) {
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSubscription(model.Subscription{ID: "sub-validate", Name: "Validate", URL: "https://sub.example.com/api", Enabled: true, AutoUpdate: true}, nil); err != nil {
		t.Fatal(err)
	}
	service := New(st)

	if _, err := service.SetOptions("sub-validate", true, 14, ""); err == nil || !strings.Contains(err.Error(), "更新间隔必须在 15 到 1440 分钟之间") {
		t.Fatalf("expected the Chinese interval range error, got %v", err)
	}
	if _, err := service.SetOptions("sub-validate", true, 1441, ""); err == nil || !strings.Contains(err.Error(), "更新间隔") {
		t.Fatalf("expected the interval range error, got %v", err)
	}
	if _, err := service.SetOptions("sub-validate", true, -5, ""); err == nil || !strings.Contains(err.Error(), "更新间隔") {
		t.Fatalf("expected the interval range error for negative values, got %v", err)
	}
	if _, err := service.SetOptions("sub-validate", true, 0, "Agent/1.0\r\nX-Injected: bad"); err == nil || !strings.Contains(err.Error(), "User-Agent") {
		t.Fatalf("expected the User-Agent newline error, got %v", err)
	}
	tooLong := strings.Repeat("a", maxSubscriptionUserAgentLength+1)
	if _, err := service.SetOptions("sub-validate", true, 0, tooLong); err == nil || !strings.Contains(err.Error(), "User-Agent") {
		t.Fatalf("expected the User-Agent length error, got %v", err)
	}
	if _, err := service.SetOptions("missing-sub", true, 0, ""); err == nil || !strings.Contains(err.Error(), "订阅不存在") {
		t.Fatalf("expected the Chinese not-found error, got %v", err)
	}

	// Rejected calls must not modify the stored options.
	stored, err := st.FindSubscription("sub-validate")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.AutoUpdate || stored.UpdateIntervalMinutes != 0 || stored.UserAgent != "" {
		t.Fatalf("rejected options leaked into the store: %+v", stored)
	}
}

func TestServiceUpdateUsesSubscriptionUserAgent(t *testing.T) {
	content := "vless://00000000-0000-0000-0000-000000000003@example.com:443?security=tls#Node"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	agents := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.Header.Get("User-Agent")
		_, _ = fmt.Fprint(w, encoded)
	}))
	defer server.Close()

	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSubscription(model.Subscription{ID: "sub-agent", Name: "Agent", URL: server.URL, Enabled: true, AutoUpdate: true, UserAgent: "ClashforWindows/0.20.39"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSubscription(model.Subscription{ID: "sub-default-agent", Name: "DefaultAgent", URL: server.URL, Enabled: true, AutoUpdate: true}, nil); err != nil {
		t.Fatal(err)
	}
	service := New(st)

	updated, err := service.Update("sub-agent")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if got := <-agents; got != "ClashforWindows/0.20.39" {
		t.Fatalf("expected the subscription User-Agent, got %q", got)
	}
	if updated.UserAgent != "ClashforWindows/0.20.39" {
		t.Fatalf("unexpected view User-Agent: %+v", updated)
	}

	if _, err := service.Update("sub-default-agent"); err != nil {
		t.Fatalf("update with the default agent failed: %v", err)
	}
	if got := <-agents; !strings.Contains(got, "FastV/") {
		t.Fatalf("expected the default FastV User-Agent, got %q", got)
	}
}

func TestDueSubscriptions(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	sub := func(id string, enabled, autoUpdate bool, interval int, updatedAt string) model.Subscription {
		return model.Subscription{ID: id, Enabled: enabled, AutoUpdate: autoUpdate, UpdateIntervalMinutes: interval, UpdatedAt: updatedAt}
	}

	tests := []struct {
		name     string
		settings model.Settings
		subs     []model.Subscription
		want     []string
	}{
		{
			name:     "global auto update off skips everything",
			settings: model.Settings{AutoUpdate: false, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("due", true, true, 0, "")},
			want:     nil,
		},
		{
			name:     "subscription auto update off is skipped",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("off", true, false, 0, "")},
			want:     nil,
		},
		{
			name:     "disabled subscription is skipped",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("disabled", false, true, 0, "")},
			want:     nil,
		},
		{
			name:     "global interval elapsed is due",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("elapsed", true, true, 0, ago(61*time.Minute))},
			want:     []string{"elapsed"},
		},
		{
			name:     "global interval not elapsed is not due",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("fresh", true, true, 0, ago(59*time.Minute))},
			want:     nil,
		},
		{
			name:     "empty updated at is due",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("never", true, true, 0, "")},
			want:     []string{"never"},
		},
		{
			name:     "per subscription interval overrides the global interval",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 600},
			subs: []model.Subscription{
				sub("own-interval-due", true, true, 30, ago(31*time.Minute)),
				sub("own-interval-fresh", true, true, 30, ago(29*time.Minute)),
				sub("global-not-due", true, true, 0, ago(59*time.Minute)),
			},
			want: []string{"own-interval-due"},
		},
		{
			name:     "zero global interval falls back to the default",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 0},
			subs: []model.Subscription{
				sub("older-than-default", true, true, 0, ago(7*time.Hour)),
				sub("younger-than-default", true, true, 0, ago(5*time.Hour)),
			},
			want: []string{"older-than-default"},
		},
		{
			name:     "invalid updated at does not block refreshes forever",
			settings: model.Settings{AutoUpdate: true, UpdateIntervalMinutes: 60},
			subs:     []model.Subscription{sub("broken", true, true, 0, "not-a-timestamp")},
			want:     []string{"broken"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dueSubscriptions(tc.subs, tc.settings, now)
			if len(got) != len(tc.want) {
				t.Fatalf("expected due %v, got %v", tc.want, got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("expected due %v, got %v", tc.want, got)
				}
			}
		})
	}
}
