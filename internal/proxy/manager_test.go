package proxy

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	settings.SystemProxyMode = "off"
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

// loadBalanceParsedNodes returns two enabled VLESS nodes for load balancing tests.
func loadBalanceParsedNodes() []subdomain.ParsedNode {
	return []subdomain.ParsedNode{
		{
			Name: "Balance One", Protocol: "vless", Address: "example.com", Port: 443,
			Raw: "vless://00000000-0000-0000-0000-000000000021@example.com:443?security=tls#BalanceOne",
		},
		{
			Name: "Balance Two", Protocol: "vless", Address: "example.org", Port: 443,
			Raw: "vless://00000000-0000-0000-0000-000000000022@example.org:443?security=tls#BalanceTwo",
		},
	}
}

// loadBalanceTestSettings returns load balancing settings bound to the given
// local proxy and API ports.
func loadBalanceTestSettings(localPort int, apiPort int) model.Settings {
	settings := store.DefaultSettings()
	settings.CoreType = "xray"
	settings.SystemProxyMode = "off"
	settings.XrayLocalPort = localPort
	settings.XrayAPIEnabled = false
	settings.StatsEnabled = true
	settings.XrayAPIPort = apiPort
	settings.XrayLogLevel = "warning"
	settings.ProxyMode = "rule"
	settings.TestURL = "https://www.gstatic.com/generate_204"
	settings.LoadBalanceEnabled = true
	settings.LoadBalanceMode = "leastPing"
	return settings
}

// newLoadBalanceTestManager stores settings plus the requested number of enabled
// nodes and returns a manager wired to the local Xray binary.
func newLoadBalanceTestManager(t *testing.T, binary string, settings model.Settings, nodeCount int) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	nodes := subdomain.BuildNodes("sub-1", loadBalanceParsedNodes()[:nodeCount])
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Balance"}, nodes); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(st, fakeCoreProvider{info: model.CoreInfo{
		Installed: true,
		Path:      binary,
		Status:    "ready",
	}})
	return manager, st
}

func managerLogsContain(manager *Manager, fragment string) bool {
	for _, entry := range manager.Logs() {
		if strings.Contains(entry.Message, fragment) {
			return true
		}
	}
	return false
}

// TestManagerStartLoadBalance starts the proxy with two enabled nodes and load
// balancing enabled. The generated configuration must use the balancer and the
// state must describe the balancer instead of a single node.
func TestManagerStartLoadBalance(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	ports := freeDistinctTCPPorts(t, 2)
	manager, st := newLoadBalanceTestManager(t, binary, loadBalanceTestSettings(ports[0], ports[1]), 2)
	samples := make(chan model.TrafficStats, 64)
	manager.SetOnTraffic(func(stats model.TrafficStats) {
		select {
		case samples <- stats:
		default:
		}
	})
	t.Cleanup(func() { _, _ = manager.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	state, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("load balancing start failed: %v logs=%+v", err, manager.Logs())
	}
	if state.Status != "running" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.NodeID != "" || state.NodeProtocol != "" || state.NodeAddress != "" || state.NodePort != 0 {
		t.Fatalf("load balancing must not report a single node: %+v", state)
	}
	if state.NodeName != "负载均衡（2 个节点）" {
		t.Fatalf("unexpected load balancing node label: %q", state.NodeName)
	}
	if state.LocalAddr == "" || state.Inbound != "SOCKS5 / HTTP" {
		t.Fatalf("inbound/local address changed: %+v", state)
	}
	conn, err := net.DialTimeout("tcp", state.LocalAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("local proxy port is not reachable: %v", err)
	}
	_ = conn.Close()

	raw, err := os.ReadFile(filepath.Join(st.DataDir(), "runtime", "xray-config.json"))
	if err != nil {
		t.Fatalf("generated config is missing: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	tags := make([]string, 0, 4)
	outbounds, _ := config["outbounds"].([]any)
	for _, entry := range outbounds {
		if outbound, ok := entry.(map[string]any); ok {
			tags = append(tags, outbound["tag"].(string))
		}
	}
	wantTags := []string{"balancer-node-0", "balancer-node-1", "direct", "block"}
	if len(tags) != len(wantTags) {
		t.Fatalf("unexpected outbound tags: %+v", tags)
	}
	for i, want := range wantTags {
		if tags[i] != want {
			t.Fatalf("outbound %d: expected tag %q, got %+v", i, want, tags)
		}
	}
	routing, _ := config["routing"].(map[string]any)
	if _, ok := routing["balancers"]; !ok {
		t.Fatalf("generated config has no balancer: %+v", routing)
	}
	if _, ok := config["observatory"]; !ok {
		t.Fatalf("generated config has no observatory: %+v", config)
	}
	if !managerLogsContain(manager, "负载均衡已启用") {
		t.Fatalf("load balancing start was not logged: %+v", manager.Logs())
	}

	// The live traffic sampler sums the balancer-node outbound counters.
	deadline := time.After(15 * time.Second)
	lastError := ""
	for waiting := true; waiting; {
		select {
		case stats := <-samples:
			if stats.Error != "" {
				lastError = stats.Error
				continue
			}
			if !stats.Running {
				continue
			}
			waiting = false
		case <-deadline:
			t.Fatalf("timed out waiting for a load balancing traffic sample (last error: %q)", lastError)
		}
	}

	if _, err := manager.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

// TestManagerLoadBalanceFallsBackToSingleNode verifies that requesting load
// balancing with fewer than two enabled nodes keeps the previous single-node
// behavior and explains the fallback in the log.
func TestManagerLoadBalanceFallsBackToSingleNode(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	ports := freeDistinctTCPPorts(t, 2)
	settings := loadBalanceTestSettings(ports[0], ports[1])
	settings.LoadBalanceMode = "roundRobin"
	manager, st := newLoadBalanceTestManager(t, binary, settings, 1)
	t.Cleanup(func() { _, _ = manager.Stop() })

	active, ok := st.ActiveNode()
	if !ok {
		t.Fatal("expected an active node")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	state, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("fallback start failed: %v logs=%+v", err, manager.Logs())
	}
	if state.Status != "running" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.NodeID != active.ID || state.NodeName != active.Name {
		t.Fatalf("expected the single node state, got %+v", state)
	}
	if !managerLogsContain(manager, "负载均衡需要至少 2 个启用节点") {
		t.Fatalf("the fallback reason was not logged: %+v", manager.Logs())
	}

	raw, err := os.ReadFile(filepath.Join(st.DataDir(), "runtime", "xray-config.json"))
	if err != nil {
		t.Fatalf("generated config is missing: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	if _, ok := config["observatory"]; ok {
		t.Fatalf("single-node fallback must not add an observatory: %+v", config)
	}

	if _, err := manager.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}
