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
