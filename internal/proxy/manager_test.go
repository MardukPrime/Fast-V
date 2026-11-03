package proxy

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

type fakeCoreProvider struct {
	info model.CoreInfo
}

func (f fakeCoreProvider) Info() model.CoreInfo {
	return f.info
}

func TestManagerStartAndStop(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	var apiPort int
	for {
		apiListener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatal(listenErr)
		}
		apiPort = apiListener.Addr().(*net.TCPAddr).Port
		_ = apiListener.Close()
		if apiPort != port {
			break
		}
	}

	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := store.DefaultSettings()
	settings.CoreType = "xray"
	settings.XrayLocalPort = port
	settings.XrayAPIEnabled = true
	settings.XrayAPIPort = apiPort
	settings.XrayLogLevel = "warning"
	settings.ProxyMode = "rule"
	settings.TestURL = "https://www.gstatic.com/generate_204"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	nodes := subdomain.BuildNodes("sub-1", []subdomain.ParsedNode{{
		Name: "Local Test", Protocol: "vless", Address: "example.com", Port: 443,
		Raw: "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls#Local",
	}})
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Local"}, nodes); err != nil {
		t.Fatal(err)
	}

	manager := NewManager(st, fakeCoreProvider{info: model.CoreInfo{
		Installed: true,
		Path:      binary,
		Status:    "ready",
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("start failed: %v logs=%+v", err, manager.Logs())
	}
	if state.Status != "running" {
		t.Fatalf("unexpected state: %+v", state)
	}
	conn, err := net.DialTimeout("tcp", state.LocalAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("local proxy port is not reachable: %v", err)
	}
	_ = conn.Close()

	stopped, err := manager.Stop()
	if err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("unexpected stopped state: %+v", stopped)
	}
}
