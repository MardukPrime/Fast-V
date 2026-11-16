package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func TestPickLocalPorts(t *testing.T) {
	ports, err := pickLocalPorts(5)
	if err != nil {
		t.Fatalf("pickLocalPorts failed: %v", err)
	}
	if len(ports) != 5 {
		t.Fatalf("expected 5 ports, got %d: %v", len(ports), ports)
	}
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port < urlTestPortStart || port > urlTestPortEnd {
			t.Fatalf("port %d outside the %d-%d test range", port, urlTestPortStart, urlTestPortEnd)
		}
		if _, ok := seen[port]; ok {
			t.Fatalf("port %d returned twice: %v", port, ports)
		}
		seen[port] = struct{}{}
	}
}

func TestPickLocalPortsInRangeRejectsOverflow(t *testing.T) {
	if _, err := pickLocalPortsInRange(5, 19000, 19003); err == nil {
		t.Fatal("expected an error when the port pool is too small")
	}
	if _, err := pickLocalPortsInRange(2, 19003, 19000); err == nil {
		t.Fatal("expected an error for an inverted port range")
	}
	if ports, err := pickLocalPortsInRange(0, 19000, 19999); err != nil || ports != nil {
		t.Fatalf("expected no ports and no error for a zero request, got %v %v", ports, err)
	}
}

func freedomCandidates(t *testing.T, names ...string) []urlTestCandidate {
	t.Helper()
	nodes := make([]model.Node, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, model.Node{
			ID:           name,
			Name:         name,
			Protocol:     "freedom",
			Enabled:      true,
			OutboundJSON: `{"protocol":"freedom"}`,
		})
	}
	candidates := prepareURLTestCandidates(nodes, model.Settings{})
	for _, candidate := range candidates {
		if candidate.err != nil {
			t.Fatalf("unexpected candidate error for %q: %v", candidate.node.Name, candidate.err)
		}
	}
	return candidates
}

func TestBuildURLTestConfig(t *testing.T) {
	candidates := freedomCandidates(t, "Alpha", "Beta")
	ports := []int{19001, 19002}
	raw, err := buildURLTestConfig(candidates, ports)
	if err != nil {
		t.Fatalf("buildURLTestConfig failed: %v", err)
	}

	var config struct {
		Log struct {
			LogLevel string `json:"loglevel"`
		} `json:"log"`
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
		Routing   struct {
			DomainStrategy string           `json:"domainStrategy"`
			Rules          []map[string]any `json:"rules"`
		} `json:"routing"`
		API json.RawMessage `json:"api"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	if config.Log.LogLevel != "warning" {
		t.Fatalf("unexpected log level: %q", config.Log.LogLevel)
	}
	if len(config.Inbounds) != 2 {
		t.Fatalf("expected 2 inbounds, got %d: %+v", len(config.Inbounds), config.Inbounds)
	}
	for i, inbound := range config.Inbounds {
		if inbound["protocol"] != "socks" {
			t.Fatalf("inbound %d is not socks: %+v", i, inbound)
		}
		if inbound["listen"] != "127.0.0.1" {
			t.Fatalf("inbound %d is not loopback: %+v", i, inbound)
		}
		if port, _ := inbound["port"].(float64); int(port) != ports[i] {
			t.Fatalf("inbound %d has port %v, want %d", i, inbound["port"], ports[i])
		}
		settings, _ := inbound["settings"].(map[string]any)
		if settings["udp"] != false || settings["auth"] != "noauth" {
			t.Fatalf("inbound %d has unexpected settings: %+v", i, settings)
		}
	}
	if len(config.Outbounds) != 4 {
		t.Fatalf("expected 4 outbounds (2 nodes + direct + block), got %d: %+v", len(config.Outbounds), config.Outbounds)
	}
	for i := 0; i < 2; i++ {
		if config.Outbounds[i]["tag"] != "p"+strconv.Itoa(i) {
			t.Fatalf("outbound %d has tag %v, want p%d", i, config.Outbounds[i]["tag"], i)
		}
		if config.Outbounds[i]["protocol"] != "freedom" {
			t.Fatalf("outbound %d protocol = %v", i, config.Outbounds[i]["protocol"])
		}
	}
	if config.Outbounds[2]["tag"] != "direct" || config.Outbounds[3]["tag"] != "block" {
		t.Fatalf("missing direct/block outbounds: %+v", config.Outbounds[2:])
	}
	if config.Routing.DomainStrategy != "AsIs" {
		t.Fatalf("unexpected routing domain strategy: %q", config.Routing.DomainStrategy)
	}
	if len(config.Routing.Rules) != 2 {
		t.Fatalf("expected 2 routing rules, got %d: %+v", len(config.Routing.Rules), config.Routing.Rules)
	}
	for i, rule := range config.Routing.Rules {
		tags, _ := rule["inboundTag"].([]any)
		if len(tags) != 1 || tags[0] != "in"+strconv.Itoa(i) {
			t.Fatalf("rule %d has unexpected inboundTag: %+v", i, rule["inboundTag"])
		}
		if rule["outboundTag"] != "p"+strconv.Itoa(i) {
			t.Fatalf("rule %d has unexpected outboundTag: %v", i, rule["outboundTag"])
		}
	}
	if len(config.API) != 0 {
		t.Fatalf("temporary measurement config must not enable the API: %s", config.API)
	}

	if _, err := buildURLTestConfig(candidates, []int{19001}); err == nil {
		t.Fatal("expected a port count mismatch error")
	}
}

func TestPrepareURLTestCandidatesReportsInvalidConfig(t *testing.T) {
	nodes := []model.Node{
		{ID: "good", Name: "Good", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
		{ID: "broken", Name: "Broken", Enabled: true, OutboundJSON: `{not-json`},
	}
	candidates := prepareURLTestCandidates(nodes, model.Settings{})
	if len(candidates) != 2 {
		t.Fatalf("expected one candidate per node, got %d", len(candidates))
	}
	if candidates[0].err != nil {
		t.Fatalf("valid node reported an error: %v", candidates[0].err)
	}
	if candidates[1].err == nil {
		t.Fatal("invalid outbound JSON must be reported")
	}
	if candidates[1].index != 1 {
		t.Fatalf("candidate index = %d, want 1", candidates[1].index)
	}
}

func TestSelectURLTestNodes(t *testing.T) {
	nodes := []model.Node{
		{ID: "a", Name: "A", Enabled: true},
		{ID: "b", Name: "B", Enabled: false},
		{ID: "c", Name: "C", Enabled: true},
	}

	all := SelectURLTestNodes(nodes, nil)
	if len(all) != 2 || all[0].NodeID != "a" || all[1].NodeID != "c" {
		t.Fatalf("empty ids must select enabled nodes: %+v", all)
	}

	explicit := SelectURLTestNodes(nodes, []string{"c", "b", "missing", "c", " "})
	if len(explicit) != 3 {
		t.Fatalf("expected 3 deduplicated selections, got %d: %+v", len(explicit), explicit)
	}
	if explicit[0].NodeID != "c" || explicit[0].Error != "" {
		t.Fatalf("unexpected first selection: %+v", explicit[0])
	}
	if explicit[1].NodeID != "b" || explicit[1].Error != "节点未启用" {
		t.Fatalf("disabled node must report an error: %+v", explicit[1])
	}
	if explicit[2].NodeID != "missing" || explicit[2].Error != "节点不存在" {
		t.Fatalf("unknown node must report an error: %+v", explicit[2])
	}
}

func TestMergeURLTestResults(t *testing.T) {
	selections := []URLTestSelection{
		{NodeID: "a", Node: model.Node{ID: "a"}},
		{NodeID: "b", Error: "节点未启用"},
		{NodeID: "c", Node: model.Node{ID: "c"}},
	}
	tested := map[string]model.NodeLatency{
		"a": {NodeID: "a", LatencyMS: 42, CheckedAt: model.NowRFC3339()},
	}
	results := MergeURLTestResults(selections, tested)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].NodeID != "a" || results[0].LatencyMS != 42 || results[0].Error != "" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if results[1].NodeID != "b" || results[1].Error != "节点未启用" {
		t.Fatalf("unexpected second result: %+v", results[1])
	}
	if results[2].NodeID != "c" || results[2].Error != "测速结果缺失" {
		t.Fatalf("missing results must be reported: %+v", results[2])
	}
}

func TestURLTestWithoutCoreReportsErrors(t *testing.T) {
	nodes := []model.Node{
		{ID: "a", Name: "A", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
		{ID: "b", Name: "B", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
	}
	results := URLTest(context.Background(), URLTestRequest{Nodes: nodes})
	if len(results) != 2 {
		t.Fatalf("expected one result per node, got %d", len(results))
	}
	for i, result := range results {
		if result.NodeID != nodes[i].ID {
			t.Fatalf("result %d has node %q, want %q", i, result.NodeID, nodes[i].ID)
		}
		if !strings.Contains(result.Error, "Xray-core") {
			t.Fatalf("expected a missing-core error, got %+v", result)
		}
	}
}

func TestURLTestInvalidTargetURLReportsErrors(t *testing.T) {
	nodes := []model.Node{{ID: "a", Name: "A", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`}}
	results := URLTest(context.Background(), URLTestRequest{
		CorePath:  "xray.exe",
		Nodes:     nodes,
		TargetURL: "ftp://example.com/file",
	})
	if len(results) != 1 || !strings.Contains(results[0].Error, "http") {
		t.Fatalf("expected an invalid target URL error, got %+v", results)
	}
}

func TestURLTestNodesReportsMissingCoreAndSkippedNodes(t *testing.T) {
	nodes := []model.Node{
		{ID: "enabled", Name: "Enabled", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
		{ID: "disabled", Name: "Disabled", Enabled: false},
	}
	results := URLTestNodes(context.Background(), nil, nodes, model.Settings{}, nil, "")
	if len(results) != 1 || results[0].NodeID != "enabled" {
		t.Fatalf("expected only the enabled node, got %+v", results)
	}
	if !strings.Contains(results[0].Error, "Xray-core") {
		t.Fatalf("expected a missing-core error, got %+v", results[0])
	}

	explicit := URLTestNodes(context.Background(), nil, nodes, model.Settings{}, []string{"disabled"}, "")
	if len(explicit) != 1 || explicit[0].Error != "节点未启用" {
		t.Fatalf("unexpected explicit selection results: %+v", explicit)
	}

	empty := URLTestNodes(context.Background(), nil, nil, model.Settings{}, nil, "")
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected an empty non-nil result slice, got %#v", empty)
	}
}

func TestChooseBestNode(t *testing.T) {
	results := []model.NodeLatency{
		{NodeID: "fast", LatencyMS: 40},
		{NodeID: "current", LatencyMS: 100},
		{NodeID: "slow", LatencyMS: 200},
		{NodeID: "failed", Error: "timeout"},
	}

	cases := []struct {
		name           string
		results        []model.NodeLatency
		currentID      string
		currentLatency int64
		toleranceMS    int
		want           string
	}{
		{
			name:           "switches when the candidate is faster than the tolerance",
			results:        results,
			currentID:      "slow",
			currentLatency: 200,
			toleranceMS:    80,
			want:           "fast",
		},
		{
			name:           "keeps the current node inside the tolerance",
			results:        []model.NodeLatency{{NodeID: "fast", LatencyMS: 90}, {NodeID: "current", LatencyMS: 100}},
			currentID:      "current",
			currentLatency: 100,
			toleranceMS:    80,
			want:           "",
		},
		{
			name:           "switches when the current node is not part of the round",
			results:        results,
			currentID:      "gone",
			currentLatency: 0,
			toleranceMS:    80,
			want:           "fast",
		},
		{
			name:           "uses a known current latency for an unmeasured node",
			results:        []model.NodeLatency{{NodeID: "fast", LatencyMS: 95}},
			currentID:      "unmeasured",
			currentLatency: 100,
			toleranceMS:    80,
			want:           "",
		},
		{
			name:           "keeps the only successful node",
			results:        []model.NodeLatency{{NodeID: "current", LatencyMS: 20}, {NodeID: "failed", Error: "timeout"}},
			currentID:      "current",
			currentLatency: 20,
			toleranceMS:    10,
			want:           "",
		},
		{
			name:           "no usable result",
			results:        []model.NodeLatency{{NodeID: "failed", Error: "timeout"}},
			currentID:      "current",
			currentLatency: 0,
			toleranceMS:    80,
			want:           "",
		},
		{
			name:           "zero tolerance still requires a faster node",
			results:        []model.NodeLatency{{NodeID: "fast", LatencyMS: 40}, {NodeID: "current", LatencyMS: 100}},
			currentID:      "current",
			currentLatency: 100,
			toleranceMS:    0,
			want:           "fast",
		},
		{
			name:           "negative tolerance is clamped",
			results:        []model.NodeLatency{{NodeID: "fast", LatencyMS: 40}, {NodeID: "current", LatencyMS: 41}},
			currentID:      "current",
			currentLatency: 41,
			toleranceMS:    -5,
			want:           "fast",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := ChooseBestNode(testCase.results, testCase.currentID, testCase.currentLatency, testCase.toleranceMS)
			if got != testCase.want {
				t.Fatalf("ChooseBestNode() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestChooseFailoverNode(t *testing.T) {
	enabled := []model.Node{
		{ID: "a", Enabled: true},
		{ID: "b", Enabled: true},
		{ID: "c", Enabled: true},
	}
	results := []model.NodeLatency{
		{NodeID: "a", LatencyMS: 30},
		{NodeID: "b", LatencyMS: 10},
		{NodeID: "current", LatencyMS: 5},
	}

	if got := ChooseFailoverNode(results, enabled, "current"); got != "b" {
		t.Fatalf("expected the fastest measured alternative, got %q", got)
	}

	disabledResults := []model.NodeLatency{{NodeID: "disabled", LatencyMS: 1}}
	disabledNodes := append(append([]model.Node(nil), enabled...), model.Node{ID: "disabled", Enabled: false})
	if got := ChooseFailoverNode(disabledResults, disabledNodes, "a"); got != "b" {
		t.Fatalf("disabled nodes must not be selected, got %q", got)
	}

	// No usable latency data: fall back to the next enabled node in list order.
	if got := ChooseFailoverNode(nil, enabled, "a"); got != "b" {
		t.Fatalf("expected the next enabled node, got %q", got)
	}
	if got := ChooseFailoverNode(nil, enabled, "c"); got != "a" {
		t.Fatalf("expected wrap-around, got %q", got)
	}
	if got := ChooseFailoverNode(nil, enabled, "unknown"); got != "a" {
		t.Fatalf("expected the first enabled node for an unknown current node, got %q", got)
	}
	if got := ChooseFailoverNode(nil, []model.Node{{ID: "only", Enabled: true}}, "only"); got != "" {
		t.Fatalf("expected no failover target, got %q", got)
	}
	if got := ChooseFailoverNode(nil, nil, "current"); got != "" {
		t.Fatalf("expected no failover target for an empty node list, got %q", got)
	}
}

func TestNextEnabledNodeID(t *testing.T) {
	nodes := []model.Node{
		{ID: "a", Enabled: true},
		{ID: "b", Enabled: false},
		{ID: "c", Enabled: true},
		{ID: "d", Enabled: true},
	}
	if got := NextEnabledNodeID(nodes, "a"); got != "c" {
		t.Fatalf("NextEnabledNodeID(a) = %q, want c", got)
	}
	if got := NextEnabledNodeID(nodes, "d"); got != "a" {
		t.Fatalf("NextEnabledNodeID(d) = %q, want a", got)
	}
	if got := NextEnabledNodeID(nodes, "missing"); got != "a" {
		t.Fatalf("NextEnabledNodeID(missing) = %q, want a", got)
	}
	if got := NextEnabledNodeID([]model.Node{{ID: "only", Enabled: true}}, "only"); got != "" {
		t.Fatalf("expected no next node, got %q", got)
	}
}

// startFakeSOCKS5 starts a minimal SOCKS5 CONNECT server. dialTarget resolves a
// successful CONNECT request; reject sends the given reply code instead.
func startFakeSOCKS5(t *testing.T, dialTarget func(target string) (net.Conn, error), reject byte) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start fake SOCKS5 server: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveFakeSOCKS5(conn, dialTarget, reject)
		}
	}()
	return listener.Addr().String()
}

func serveFakeSOCKS5(conn net.Conn, dialTarget func(target string) (net.Conn, error), reject byte) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	host := ""
	switch header[3] {
	case 0x01:
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		buf := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		host = string(buf)
	case 0x04:
		buf := make([]byte, 16)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}
		host = net.IP(buf).String()
	default:
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(portBytes[0])<<8|int(portBytes[1])))
	if reject != 0 {
		_, _ = conn.Write([]byte{0x05, reject, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	targetConn, err := dialTarget(target)
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer targetConn.Close()
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(targetConn, conn)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(conn, targetConn)
		done <- struct{}{}
	}()
	<-done
}

func TestHTTPLatencyThroughSOCKS5(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	serverAddress := strings.TrimPrefix(server.URL, "http://")

	var recordedTarget string
	targets := make(chan string, 1)
	proxyAddress := startFakeSOCKS5(t, func(target string) (net.Conn, error) {
		select {
		case targets <- target:
		default:
		}
		return net.Dial("tcp", serverAddress)
	}, 0)

	// Domain targets must be forwarded as SOCKS5 domain names (ATYP 0x03); the
	// fake proxy routes them to the local test server regardless of DNS.
	targetURL := "http://speedtest.invalid/probe"
	latency, err := httpLatencyThroughSOCKS5(context.Background(), proxyAddress, targetURL, 5*time.Second)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if latency < 1 {
		t.Fatalf("expected a positive latency, got %d", latency)
	}
	select {
	case recordedTarget = <-targets:
	default:
	}
	if recordedTarget != "speedtest.invalid:80" {
		t.Fatalf("expected the domain to be forwarded to the proxy, got %q", recordedTarget)
	}

	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer statusServer.Close()
	statusAddress := strings.TrimPrefix(statusServer.URL, "http://")
	statusProxy := startFakeSOCKS5(t, func(target string) (net.Conn, error) {
		return net.Dial("tcp", statusAddress)
	}, 0)
	if _, err := httpLatencyThroughSOCKS5(context.Background(), statusProxy, "http://status.invalid/", 5*time.Second); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("expected an HTTP 500 error, got %v", err)
	}
}

func TestDialSOCKS5TunnelRejected(t *testing.T) {
	proxyAddress := startFakeSOCKS5(t, func(target string) (net.Conn, error) {
		return nil, io.ErrClosedPipe
	}, 0x05)
	_, err := dialSOCKS5Tunnel(context.Background(), proxyAddress, "127.0.0.1:1", 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("expected a rejected CONNECT error, got %v", err)
	}

	rejectAddress := startFakeSOCKS5(t, nil, 0x02)
	if _, err := dialSOCKS5Tunnel(context.Background(), rejectAddress, "127.0.0.1:1", 2*time.Second); err == nil {
		t.Fatal("expected an error for a refused SOCKS5 connection")
	}
}

// realXrayBinary returns the workspace core used by integration style tests. The
// test is skipped when the binary is not available.
func realXrayBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	return binary
}

func TestURLTestWithRealCore(t *testing.T) {
	binary := realXrayBinary(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	nodes := []model.Node{
		{ID: "n-freedom", Name: "Freedom", Protocol: "freedom", Enabled: true, OutboundJSON: `{"protocol":"freedom"}`},
		{ID: "n-blackhole", Name: "Blackhole", Protocol: "freedom", Enabled: true, OutboundJSON: `{"protocol":"blackhole"}`},
	}
	results := URLTest(context.Background(), URLTestRequest{
		CorePath:     binary,
		Nodes:        nodes,
		Settings:     model.Settings{TestURL: server.URL},
		ProbeTimeout: 2 * time.Second,
		TempDir:      t.TempDir(),
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	byID := make(map[string]model.NodeLatency, len(results))
	for _, result := range results {
		byID[result.NodeID] = result
	}
	freedom, ok := byID["n-freedom"]
	if !ok {
		t.Fatalf("missing freedom result: %+v", results)
	}
	if freedom.Error != "" {
		t.Fatalf("freedom probe failed: %+v", freedom)
	}
	if freedom.LatencyMS <= 0 {
		t.Fatalf("expected a positive latency, got %+v", freedom)
	}
	blackhole, ok := byID["n-blackhole"]
	if !ok {
		t.Fatalf("missing blackhole result: %+v", results)
	}
	if blackhole.Error == "" {
		t.Fatalf("blackhole node must fail its probe: %+v", blackhole)
	}
	if blackhole.LatencyMS != 0 {
		t.Fatalf("failed probe must not report a latency: %+v", blackhole)
	}

	// MaxBatchSize smaller than the node count exercises the batching path with
	// one temporary core per node.
	batched := URLTest(context.Background(), URLTestRequest{
		CorePath:     binary,
		Nodes:        nodes,
		Settings:     model.Settings{TestURL: server.URL},
		ProbeTimeout: 2 * time.Second,
		MaxBatchSize: 1,
		TempDir:      t.TempDir(),
	})
	if len(batched) != 2 || batched[0].Error != "" || batched[0].LatencyMS <= 0 {
		t.Fatalf("batched run failed: %+v", batched)
	}
}
