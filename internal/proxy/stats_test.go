package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
	subdomain "github.com/MardukPrime/Fast-V/internal/subscription"
)

// Samples below were captured from the real binary
// (.fastv-data/cores/xray/xray.exe, Xray 26.11.9) with
// `xray api statsquery --server=127.0.0.1:<port> -pattern outbound>>>proxy>>>traffic>>>`.

const measuredStatsJSON = `{
    "stat": [
        {
            "name": "outbound>>>proxy>>>traffic>>>downlink",
            "value": 420
        },
        {
            "name": "outbound>>>proxy>>>traffic>>>uplink",
            "value": 237
        }
    ]
}
`

// measuredAllStatsJSON is the unfiltered response: inbound, api, direct and
// block counters must be ignored.
const measuredAllStatsJSON = `{
    "stat": [
        {
            "name": "inbound>>>local-in>>>traffic>>>downlink",
            "value": 456
        },
        {
            "name": "outbound>>>proxy>>>traffic>>>uplink",
            "value": 237
        },
        {
            "name": "inbound>>>api-in>>>traffic>>>uplink",
            "value": 750
        },
        {
            "name": "inbound>>>api-in>>>traffic>>>downlink",
            "value": 472
        },
        {
            "name": "outbound>>>proxy>>>traffic>>>downlink",
            "value": 420
        },
        {
            "name": "outbound>>>direct>>>traffic>>>uplink"
        },
        {
            "name": "outbound>>>direct>>>traffic>>>downlink"
        },
        {
            "name": "outbound>>>block>>>traffic>>>uplink"
        },
        {
            "name": "outbound>>>block>>>traffic>>>downlink"
        },
        {
            "name": "inbound>>>local-in>>>traffic>>>uplink",
            "value": 279
        }
    ]
}
`

// measuredEmptyStatsJSON is the response right after start: counters exist but
// the value field is omitted while it is still zero.
const measuredEmptyStatsJSON = `{
    "stat": [
        {
            "name": "outbound>>>proxy>>>traffic>>>uplink"
        },
        {
            "name": "outbound>>>proxy>>>traffic>>>downlink"
        }
    ]
}
`

const protobufTextStats = `stat: <
  name: "outbound>>>proxy>>>traffic>>>uplink"
  value: 237
>
stat: <
  name: "outbound>>>proxy>>>traffic>>>downlink"
  value: 420
>
`

const protobufNestedValueStats = `stat: <
  name: "outbound>>>proxy>>>traffic>>>uplink"
  value: <
    value: 1024
  >
>
stat: <
  name: "outbound>>>proxy>>>traffic>>>downlink"
  value: <
    value: 2048
  >
>
`

const inlineTextStats = `stat:<name:"outbound>>>proxy>>>traffic>>>uplink" value:1234> stat:<name:"outbound>>>proxy>>>traffic>>>downlink" value:5678>`

const tableTextStats = `outbound>>>proxy>>>traffic>>>uplink 111
outbound>>>proxy>>>traffic>>>downlink 222
`

// measuredBalancerStatsJSON is the shape produced in load balancing mode: one
// counter pair per node outbound. direct/block/api/dns outbounds must be ignored.
const measuredBalancerStatsJSON = `{
    "stat": [
        {"name": "outbound>>>balancer-node-0>>>traffic>>>uplink", "value": 1200},
        {"name": "outbound>>>balancer-node-0>>>traffic>>>downlink", "value": 3400},
        {"name": "outbound>>>balancer-node-1>>>traffic>>>uplink", "value": 800},
        {"name": "outbound>>>balancer-node-1>>>traffic>>>downlink", "value": 1600},
        {"name": "outbound>>>direct>>>traffic>>>uplink", "value": 9999},
        {"name": "outbound>>>direct>>>traffic>>>downlink", "value": 8888},
        {"name": "outbound>>>block>>>traffic>>>uplink", "value": 777},
        {"name": "outbound>>>block>>>traffic>>>downlink", "value": 666},
        {"name": "outbound>>>api>>>traffic>>>uplink", "value": 555},
        {"name": "outbound>>>api>>>traffic>>>downlink", "value": 444},
        {"name": "outbound>>>dns>>>traffic>>>uplink", "value": 333},
        {"name": "outbound>>>dns>>>traffic>>>downlink", "value": 222},
        {"name": "inbound>>>local-in>>>traffic>>>uplink", "value": 222},
        {"name": "inbound>>>local-in>>>traffic>>>downlink", "value": 111}
    ]
}
`

// protobufTextBalancerStats is the protobuf text form of the same load balancing
// counters.
const protobufTextBalancerStats = `stat: <
  name: "outbound>>>balancer-node-0>>>traffic>>>uplink"
  value: 100
>
stat: <
  name: "outbound>>>balancer-node-1>>>traffic>>>uplink"
  value: 200
>
stat: <
  name: "outbound>>>balancer-node-0>>>traffic>>>downlink"
  value: 300
>
stat: <
  name: "outbound>>>balancer-node-1>>>traffic>>>downlink"
  value: 400
>
stat: <
  name: "outbound>>>direct>>>traffic>>>uplink"
  value: 999
>
`

// onlyIgnoredOutboundStatsJSON contains only outbound counters that must never be
// counted as proxy traffic (single-node mode has no proxy counter here).
const onlyIgnoredOutboundStatsJSON = `{
    "stat": [
        {"name": "outbound>>>direct>>>traffic>>>uplink", "value": 123},
        {"name": "outbound>>>block>>>traffic>>>downlink", "value": 456},
        {"name": "outbound>>>api>>>traffic>>>uplink", "value": 789}
    ]
}
`

func TestParseStatsOutputJSON(t *testing.T) {
	cases := []struct {
		name     string
		output   string
		uplink   int64
		downlink int64
	}{
		{"measured", measuredStatsJSON, 237, 420},
		{"all counters", measuredAllStatsJSON, 237, 420},
		{"zero counters omit value", measuredEmptyStatsJSON, 0, 0},
		{"string values", `{"stat":[{"name":"outbound>>>proxy>>>traffic>>>uplink","value":"1234"},{"name":"outbound>>>proxy>>>traffic>>>downlink","value":"5678"}]}`, 1234, 5678},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uplink, downlink, err := parseStatsOutput([]byte(tc.output))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if uplink != tc.uplink || downlink != tc.downlink {
				t.Fatalf("got uplink=%d downlink=%d, want uplink=%d downlink=%d", uplink, downlink, tc.uplink, tc.downlink)
			}
		})
	}
}

func TestParseStatsOutputProtobufText(t *testing.T) {
	cases := []struct {
		name     string
		output   string
		uplink   int64
		downlink int64
	}{
		{"multiline", protobufTextStats, 237, 420},
		{"nested value", protobufNestedValueStats, 1024, 2048},
		{"inline", inlineTextStats, 1234, 5678},
		{"table columns", tableTextStats, 111, 222},
		{"only downlink", "stat: <\n  name: \"outbound>>>proxy>>>traffic>>>downlink\"\n  value: 420\n>\n", 0, 420},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uplink, downlink, err := parseStatsOutput([]byte(tc.output))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if uplink != tc.uplink || downlink != tc.downlink {
				t.Fatalf("got uplink=%d downlink=%d, want uplink=%d downlink=%d", uplink, downlink, tc.uplink, tc.downlink)
			}
		})
	}
}

func TestParseStatsOutputErrors(t *testing.T) {
	for _, output := range []string{
		"",
		"boom",
		`{"stat":[]}`,
		`{"unexpected":true}`,
		"stat: <>\n",
		onlyIgnoredOutboundStatsJSON,
	} {
		if _, _, err := parseStatsOutput([]byte(output)); err == nil {
			t.Fatalf("expected an error for %q", output)
		}
	}
}

func TestParsedStatsOutputSumsLoadBalanceOutbounds(t *testing.T) {
	uplink, downlink, err := parseStatsOutput([]byte(measuredBalancerStatsJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uplink != 2000 || downlink != 5000 {
		t.Fatalf("balancer outbound counters must be summed: got uplink=%d downlink=%d", uplink, downlink)
	}
}

func TestParsedTextStatsSumsLoadBalanceOutbounds(t *testing.T) {
	uplink, downlink, err := parseStatsOutput([]byte(protobufTextBalancerStats))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uplink != 300 || downlink != 700 {
		t.Fatalf("balancer text counters must be summed: got uplink=%d downlink=%d", uplink, downlink)
	}
}

func TestCountedOutboundTag(t *testing.T) {
	cases := map[string]bool{
		"proxy":            true,
		"balancer-node-0":  true,
		"balancer-node-12": true,
		"direct":           false,
		"block":            false,
		"api":              false,
		"dns":              false,
		" balancer-node-3": true,
		"balancer-node":    false,
		"":                 false,
	}
	for tag, want := range cases {
		if got := countedOutboundTag(tag); got != want {
			t.Fatalf("countedOutboundTag(%q) = %v, want %v", tag, got, want)
		}
	}
}

func TestTrafficMeterComputesRatesAndTotals(t *testing.T) {
	var meter trafficMeter
	base := time.Unix(1_700_000_000, 0)

	first := meter.advance(1000, 2000, base)
	if first.UplinkBytes != 0 || first.DownlinkBytes != 0 || first.UplinkRate != 0 || first.DownlinkRate != 0 {
		t.Fatalf("first sample must only establish the baseline: %+v", first)
	}
	if first.TotalUplink != 1000 || first.TotalDownlink != 2000 {
		t.Fatalf("first sample totals: %+v", first)
	}

	second := meter.advance(4000, 7000, base.Add(2*time.Second))
	if second.UplinkBytes != 3000 || second.DownlinkBytes != 5000 {
		t.Fatalf("unexpected deltas: %+v", second)
	}
	if second.UplinkRate != 1500 || second.DownlinkRate != 2500 {
		t.Fatalf("unexpected rates: %+v", second)
	}
	if second.TotalUplink != 4000 || second.TotalDownlink != 7000 {
		t.Fatalf("unexpected totals: %+v", second)
	}

	// A shorter/longer actual window must be reflected in the rate.
	third := meter.advance(4500, 7000, base.Add(3*time.Second))
	if third.UplinkRate != 500 || third.DownlinkRate != 0 {
		t.Fatalf("unexpected rate for a 1s window: %+v", third)
	}
}

func TestTrafficMeterHandlesCounterResetAndStalledClock(t *testing.T) {
	var meter trafficMeter
	base := time.Unix(1_700_000_000, 0)
	meter.advance(5000, 5000, base)

	reset := meter.advance(300, 200, base.Add(2*time.Second))
	if reset.UplinkBytes != 0 || reset.DownlinkBytes != 0 {
		t.Fatalf("counter reset must not yield negative deltas: %+v", reset)
	}
	if reset.TotalUplink != 5000 || reset.TotalDownlink != 5000 {
		t.Fatalf("totals must not shrink on counter reset: %+v", reset)
	}

	stalled := meter.advance(1300, 1200, base.Add(2*time.Second))
	if stalled.UplinkRate != 0 || stalled.DownlinkRate != 0 {
		t.Fatalf("zero elapsed must not divide by zero: %+v", stalled)
	}
	if stalled.UplinkBytes != 1000 || stalled.DownlinkBytes != 1000 {
		t.Fatalf("deltas must still be reported: %+v", stalled)
	}
}

func TestTrafficSamplerPublishesRatesAndErrors(t *testing.T) {
	samples := make(chan model.TrafficStats, 64)
	var queries int32
	sampler := newTrafficSampler(func(stats model.TrafficStats) {
		select {
		case samples <- stats:
		default:
		}
	})
	sampler.interval = 10 * time.Millisecond
	sampler.firstDelay = 5 * time.Millisecond
	sampler.query = func(ctx context.Context) ([]byte, error) {
		switch atomic.AddInt32(&queries, 1) {
		case 1:
			return nil, errors.New("statsquery failed: failed to dial 127.0.0.1:10085")
		case 2:
			return []byte(measuredStatsJSON), nil
		default:
			return []byte(`{"stat":[{"name":"outbound>>>proxy>>>traffic>>>uplink","value":1237},{"name":"outbound>>>proxy>>>traffic>>>downlink","value":2420}]}`), nil
		}
	}
	sampler.start(context.Background(), "xray", 10085)
	defer sampler.stop()

	var sawError bool
	var baseline, advanced model.TrafficStats
	deadline := time.After(5 * time.Second)
	for baseline.TotalUplink == 0 || advanced.TotalUplink == 0 {
		select {
		case stats := <-samples:
			if stats.Error != "" {
				sawError = true
				continue
			}
			switch {
			case stats.TotalUplink == 237 && stats.TotalDownlink == 420 && baseline.TotalUplink == 0:
				baseline = stats
			case stats.TotalUplink > 237:
				advanced = stats
			}
		case <-deadline:
			t.Fatalf("timed out waiting for traffic samples; errorSeen=%v baseline=%+v advanced=%+v", sawError, baseline, advanced)
		}
	}

	if !sawError {
		t.Fatal("an unreachable API must be reported through the Error field")
	}
	if baseline.UplinkRate != 0 || baseline.DownlinkRate != 0 {
		t.Fatalf("baseline must not report a rate: %+v", baseline)
	}
	if !baseline.Running || baseline.UpdatedAt == "" {
		t.Fatalf("baseline must be marked as running with a timestamp: %+v", baseline)
	}
	if advanced.Error != "" {
		t.Fatalf("successful sample must clear the error: %+v", advanced)
	}
	if advanced.UplinkBytes != 1000 || advanced.DownlinkBytes != 2000 {
		t.Fatalf("unexpected deltas: %+v", advanced)
	}
	if advanced.TotalUplink != 1237 || advanced.TotalDownlink != 2420 {
		t.Fatalf("unexpected totals: %+v", advanced)
	}
	if advanced.UplinkRate <= 0 || advanced.DownlinkRate <= 0 {
		t.Fatalf("expected positive rates: %+v", advanced)
	}
}

func TestTrafficSamplerStopCancelsInFlightQuery(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	sampler := newTrafficSampler(nil)
	sampler.interval = 10 * time.Millisecond
	sampler.firstDelay = time.Millisecond
	sampler.query = func(ctx context.Context) ([]byte, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	sampler.start(context.Background(), "xray", 10085)
	<-started

	stopped := make(chan struct{})
	go func() {
		sampler.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not cancel the in-flight statsquery")
	}

	// Idempotent stop and no cancellation noise published as a traffic error.
	sampler.stop()
	if stats := sampler.stats(); stats.Error != "" {
		t.Fatalf("shutdown cancellation must not be reported as a traffic error: %+v", stats)
	}
}

func TestTrafficSamplerStopBeforeStart(t *testing.T) {
	sampler := newTrafficSampler(nil)
	sampler.stop()
	sampler.start(context.Background(), "xray", 10085)
	if stats := sampler.stats(); stats.Error != "" {
		t.Fatalf("unexpected traffic state: %+v", stats)
	}
	sampler.stop()
}

// TestTrafficSamplerConcurrentStartStop exercises the start/stop ordering guards
// that keep the manager from leaking a sampling goroutine when Start and Stop
// race with each other.
func TestTrafficSamplerConcurrentStartStop(t *testing.T) {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for iteration := 0; iteration < 25; iteration++ {
			sampler := newTrafficSampler(nil)
			sampler.firstDelay = time.Millisecond
			sampler.interval = time.Millisecond
			sampler.query = func(ctx context.Context) ([]byte, error) {
				return []byte(measuredStatsJSON), nil
			}
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				sampler.start(context.Background(), "xray", 10085)
			}()
			go func() {
				defer wg.Done()
				sampler.stop()
			}()
			wg.Wait()
			sampler.stop()                                     // repeated stop must be a no-op
			sampler.start(context.Background(), "xray", 10085) // must not resurrect
			sampler.stop()
		}
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent start/stop deadlocked")
	}
}

func TestManagerTrafficIsZeroWhenNotRunning(t *testing.T) {
	manager := NewManager(nil, nil)
	if stats := manager.Traffic(); stats.Running || stats.TotalUplink != 0 || stats.TotalDownlink != 0 {
		t.Fatalf("stopped manager must report zero traffic: %+v", stats)
	}
}

// TestStatsQueryAgainstRealXray exercises the full sampler path (exec, JSON
// parsing, rates) against a real Xray-core instance with a freedom outbound
// tagged "proxy" and a local SOCKS inbound.
func TestStatsQueryAgainstRealXray(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}

	usedPorts := freeDistinctTCPPorts(t, 2)
	apiPort := usedPorts[0]
	socksPort := usedPorts[1]

	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echoListener.Close() })
	go func() {
		for {
			conn, acceptErr := echoListener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(conn)
		}
	}()

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"tag": "local-in", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks",
				"settings": map[string]any{"auth": "noauth", "udp": true},
			},
			map[string]any{
				"tag": "api-in", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door",
				"settings": map[string]any{"address": "127.0.0.1"},
			},
		},
		"outbounds": []any{
			map[string]any{"tag": "proxy", "protocol": "freedom"},
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "IPIfNonMatch",
			"rules": []any{
				map[string]any{"type": "field", "inboundTag": []string{"api-in"}, "outboundTag": "api"},
			},
		},
		"api":   map[string]any{"tag": "api", "services": []string{"HandlerService", "LoggerService", "StatsService", "RoutingService"}},
		"stats": map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"handshake": 4, "connIdle": 300, "uplinkOnly": 2, "downlinkOnly": 5,
					"statsUserUplink": true, "statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsInboundUplink": true, "statsInboundDownlink": true,
				"statsOutboundUplink": true, "statsOutboundDownlink": true,
			},
		},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "stats-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(binary, "run", "-config", configPath)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(binary))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start xray: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		if t.Failed() {
			t.Logf("xray stderr: %s", stderr.String())
		}
	})

	// waitForPort dialing the API inbound creates a few api-in bytes, which the
	// sampler pattern intentionally ignores.
	if err := waitForPort(context.Background(), net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)), 10*time.Second); err != nil {
		t.Fatalf("xray API did not become ready: %v (stderr=%s)", err, stderr.String())
	}
	if err := waitForPort(context.Background(), net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), 10*time.Second); err != nil {
		t.Fatalf("xray SOCKS inbound did not become ready: %v", err)
	}

	// Generate real through-proxy traffic: SOCKS -> freedom("proxy") -> local echo.
	conn := dialSOCKS5(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), echoListener.Addr().String())
	payload := bytes.Repeat([]byte("fastv-traffic-probe-"), 24)
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("failed to write through SOCKS: %v", err)
	}
	echoed := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, echoed); err != nil {
		t.Fatalf("failed to read through SOCKS: %v", err)
	}
	if !bytes.Equal(echoed, payload) {
		t.Fatal("echoed payload does not match")
	}
	_ = conn.Close()
	time.Sleep(500 * time.Millisecond)

	sampler := newTrafficSampler(nil)
	sampler.corePath = binary
	sampler.apiPort = apiPort
	queryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	output, err := sampler.execQuery(queryCtx)
	cancel()
	if err != nil {
		t.Fatalf("statsquery against the real core failed: %v (stderr=%s)", err, stderr.String())
	}
	if !bytes.Contains(output, []byte(`"stat"`)) {
		t.Logf("statsquery output: %s", output)
	}
	uplink, downlink, err := parseStatsOutput(output)
	if err != nil {
		t.Fatalf("failed to parse real statsquery output: %v\noutput: %s", err, output)
	}
	if uplink <= 0 || downlink <= 0 {
		t.Fatalf("expected non-zero outbound traffic, got uplink=%d downlink=%d\noutput: %s", uplink, downlink, output)
	}

	// And the full sampling loop must publish those counters.
	samples := make(chan model.TrafficStats, 16)
	loopSampler := newTrafficSampler(func(stats model.TrafficStats) {
		select {
		case samples <- stats:
		default:
		}
	})
	loopSampler.firstDelay = 100 * time.Millisecond
	loopSampler.start(context.Background(), binary, apiPort)

	var snapshot model.TrafficStats
	deadline := time.After(8 * time.Second)
waitLoop:
	for {
		select {
		case stats := <-samples:
			if stats.Error != "" {
				t.Logf("sampler error (tolerated): %s", stats.Error)
				continue
			}
			snapshot = stats
			break waitLoop
		case <-deadline:
			loopSampler.stop()
			t.Fatal("timed out waiting for a live traffic snapshot")
		}
	}
	loopSampler.stop()
	if !snapshot.Running || snapshot.TotalUplink <= 0 || snapshot.TotalDownlink <= 0 {
		t.Fatalf("unexpected live snapshot: %+v", snapshot)
	}
	if snapshot.UpdatedAt == "" {
		t.Fatalf("snapshot must carry a timestamp: %+v", snapshot)
	}
}

// TestBalancerStatsQueryAgainstRealXray exercises load balancing mode against a
// real core: the echo traffic is routed through a balancer to one of two
// "balancer-node-<index>" freedom outbounds, and the sampler must sum those
// counters (while the direct/block/api counters stay ignored).
func TestBalancerStatsQueryAgainstRealXray(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	ports := freeDistinctTCPPorts(t, 2)
	socksPort, apiPort := ports[0], ports[1]

	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echoListener.Close() })
	go func() {
		for {
			conn, acceptErr := echoListener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(conn)
		}
	}()

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"tag": "local-in", "listen": "127.0.0.1", "port": socksPort, "protocol": "socks",
				"settings": map[string]any{"auth": "noauth", "udp": true},
			},
			map[string]any{
				"tag": "api-in", "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door",
				"settings": map[string]any{"address": "127.0.0.1"},
			},
		},
		"outbounds": []any{
			map[string]any{"tag": "balancer-node-0", "protocol": "freedom"},
			map[string]any{"tag": "balancer-node-1", "protocol": "freedom"},
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"balancers": []any{map[string]any{
				"tag": "proxy", "selector": []string{"balancer-node-"},
				"strategy": map[string]any{"type": "roundRobin"},
			}},
			"rules": []any{
				map[string]any{"type": "field", "inboundTag": []string{"api-in"}, "outboundTag": "api"},
				map[string]any{"type": "field", "network": "tcp,udp", "balancerTag": "proxy"},
			},
		},
		"api":   map[string]any{"tag": "api", "services": []string{"HandlerService", "LoggerService", "StatsService", "RoutingService"}},
		"stats": map[string]any{},
		"policy": map[string]any{
			"system": map[string]any{
				"statsInboundUplink": true, "statsInboundDownlink": true,
				"statsOutboundUplink": true, "statsOutboundDownlink": true,
			},
		},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "balancer-stats-config.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-config", configPath)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(binary))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start xray: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		if t.Failed() {
			t.Logf("xray stderr: %s", stderr.String())
		}
	})
	if err := waitForPort(context.Background(), net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)), 10*time.Second); err != nil {
		t.Fatalf("xray API did not become ready: %v (stderr=%s)", err, stderr.String())
	}
	if err := waitForPort(context.Background(), net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), 10*time.Second); err != nil {
		t.Fatalf("xray SOCKS inbound did not become ready: %v", err)
	}

	// Two round trips make roundRobin use both node outbounds.
	for i := 0; i < 2; i++ {
		conn := dialSOCKS5(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), echoListener.Addr().String())
		payload := bytes.Repeat([]byte("fastv-balancer-traffic-"), 32)
		if _, err := conn.Write(payload); err != nil {
			t.Fatalf("round %d: failed to write through SOCKS: %v", i, err)
		}
		echoed := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, echoed); err != nil {
			t.Fatalf("round %d: failed to read through SOCKS: %v", i, err)
		}
		if !bytes.Equal(echoed, payload) {
			t.Fatalf("round %d: echoed payload does not match", i)
		}
		_ = conn.Close()
	}
	time.Sleep(500 * time.Millisecond)

	sampler := newTrafficSampler(nil)
	sampler.corePath = binary
	sampler.apiPort = apiPort
	queryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	output, err := sampler.execQuery(queryCtx)
	cancel()
	if err != nil {
		t.Fatalf("statsquery against the load balancing core failed: %v (stderr=%s)", err, stderr.String())
	}
	if !bytes.Contains(output, []byte("balancer-node-")) {
		t.Fatalf("statsquery output has no balancer-node counters: %s", output)
	}
	uplink, downlink, err := parseStatsOutput(output)
	if err != nil {
		t.Fatalf("failed to parse balancer statsquery output: %v\noutput: %s", err, output)
	}
	if uplink <= 0 || downlink <= 0 {
		t.Fatalf("expected non-zero balancer traffic, got uplink=%d downlink=%d\noutput: %s", uplink, downlink, output)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate a port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func freeDistinctTCPPorts(t *testing.T, count int) []int {
	t.Helper()
	ports := make([]int, 0, count)
	seen := make(map[int]bool, count)
	for len(ports) < count {
		port := freeTCPPort(t)
		if seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

type statsCoreProvider struct {
	path string
}

func (p statsCoreProvider) Info() model.CoreInfo {
	return model.CoreInfo{Installed: p.path != "", Path: p.path, Status: "ready"}
}

// TestManagerTrafficSamplerLifecycle verifies the manager wiring against a real
// core: the sampler starts with the proxy, publishes snapshots, is stopped and
// cleared on Stop, and does not leak or restart unexpectedly on a restart.
func TestManagerTrafficSamplerLifecycle(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	ports := freeDistinctTCPPorts(t, 2)
	manager := newTrafficTestManager(t, binary, trafficTestSettings(ports))
	samples := make(chan model.TrafficStats, 128)
	manager.SetOnTraffic(func(stats model.TrafficStats) {
		select {
		case samples <- stats:
		default:
		}
	})
	t.Cleanup(func() { _, _ = manager.Stop() })

	startProxy := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		state, err := manager.Start(ctx)
		if err != nil {
			t.Fatalf("start failed: %v logs=%+v", err, manager.Logs())
		}
		if state.Status != "running" {
			t.Fatalf("unexpected state: %+v", state)
		}
	}
	waitRunningSample := func() model.TrafficStats {
		t.Helper()
		deadline := time.After(10 * time.Second)
		lastError := ""
		for {
			select {
			case stats := <-samples:
				if stats.Error != "" {
					lastError = stats.Error
					continue
				}
				if !stats.Running {
					continue
				}
				return stats
			case <-deadline:
				t.Fatalf("timed out waiting for a traffic snapshot (last error: %q)", lastError)
				return model.TrafficStats{}
			}
		}
	}
	waitClearedSample := func() {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case stats := <-samples:
				if !stats.Running && stats.TotalUplink == 0 && stats.TotalDownlink == 0 {
					return
				}
			case <-deadline:
				t.Fatal("timed out waiting for the cleared traffic snapshot")
			}
		}
	}

	startProxy()
	first := waitRunningSample()
	if first.TotalUplink != 0 || first.TotalDownlink != 0 {
		t.Fatalf("expected zero counters right after start: %+v", first)
	}
	if live := manager.Traffic(); !live.Running {
		t.Fatalf("manager.Traffic() must be running while the proxy runs: %+v", live)
	}

	if _, err := manager.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if stats := manager.Traffic(); stats.Running || stats.TotalUplink != 0 {
		t.Fatalf("traffic runtime state must be cleared on stop: %+v", stats)
	}
	waitClearedSample()

	// A restart must not duplicate or leak the sampling goroutine.
	startProxy()
	waitRunningSample()
	if _, err := manager.Stop(); err != nil {
		t.Fatalf("second stop failed: %v", err)
	}
	waitClearedSample()
	time.Sleep(300 * time.Millisecond)
	for {
		select {
		case stats := <-samples:
			if stats.Running {
				t.Fatalf("sampler still publishing after stop: %+v", stats)
			}
		default:
			return
		}
	}
}

func trafficTestSettings(ports []int) model.Settings {
	settings := store.DefaultSettings()
	settings.CoreType = "xray"
	settings.SystemProxyMode = "off"
	settings.TunMode = false
	settings.XrayLocalPort = ports[0]
	settings.XrayAPIPort = ports[1]
	settings.XrayAPIEnabled = false
	settings.StatsEnabled = true
	settings.XrayLogLevel = "warning"
	settings.ProxyMode = "rule"
	settings.TestURL = "https://www.gstatic.com/generate_204"
	return settings
}

func newTrafficTestManager(t *testing.T, binary string, settings model.Settings) *Manager {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	nodes := subdomain.BuildNodes("sub-1", []subdomain.ParsedNode{{
		Name: "Traffic Test", Protocol: "vless", Address: "example.com", Port: 443,
		Raw: "vless://00000000-0000-0000-0000-000000000002@example.com:443?security=tls#Traffic",
	}})
	if err := st.AddSubscription(model.Subscription{ID: "sub-1", Name: "Traffic"}, nodes); err != nil {
		t.Fatal(err)
	}
	return NewManager(st, statsCoreProvider{path: binary})
}

// TestManagerPortConflictSkipsStats ensures a local proxy port equal to the API
// port degrades to "no live traffic" instead of generating an invalid Xray
// config that cannot bind.
func TestManagerPortConflictSkipsStats(t *testing.T) {
	binary := filepath.Join("..", "..", ".fastv-data", "cores", "xray", "xray.exe")
	if _, err := os.Stat(binary); err != nil {
		t.Skip("local Xray binary not available")
	}
	localPort := freeDistinctTCPPorts(t, 1)[0]
	settings := trafficTestSettings([]int{localPort, localPort})
	manager := newTrafficTestManager(t, binary, settings)
	samples := make(chan model.TrafficStats, 16)
	manager.SetOnTraffic(func(stats model.TrafficStats) {
		select {
		case samples <- stats:
		default:
		}
	})
	t.Cleanup(func() { _, _ = manager.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	state, err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("proxy must still start when stats are skipped: %v logs=%+v", err, manager.Logs())
	}
	if state.Status != "running" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if stats := manager.Traffic(); stats.Running {
		t.Fatalf("stats must be disabled on a port conflict: %+v", stats)
	}
	time.Sleep(700 * time.Millisecond)
	select {
	case stats := <-samples:
		t.Fatalf("unexpected traffic sample when stats are skipped: %+v", stats)
	default:
	}
	if _, err := manager.Stop(); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}

// TestManagerExplicitAPIWithPortConflictFails verifies an explicitly requested
// management API still fails fast with a clear message.
func TestManagerExplicitAPIWithPortConflictFails(t *testing.T) {
	localPort := freeDistinctTCPPorts(t, 1)[0]
	settings := trafficTestSettings([]int{localPort, localPort})
	settings.XrayAPIEnabled = true
	manager := newTrafficTestManager(t, "xray.exe", settings)
	t.Cleanup(func() { _, _ = manager.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := manager.Start(ctx); err == nil {
		t.Fatal("expected Start to fail when the explicit API port conflicts with the local port")
	} else if !strings.Contains(err.Error(), "API 端口") {
		t.Fatalf("expected a clear API port error, got %v", err)
	}
	if state := manager.State(); state.Status != "error" {
		t.Fatalf("unexpected state after failure: %+v", state)
	}
}

// dialSOCKS5 performs a minimal SOCKS5 CONNECT handshake without third-party
// dependencies.
func dialSOCKS5(t *testing.T, proxyAddr, targetAddr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", proxyAddr, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial SOCKS proxy: %v", err)
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		t.Fatalf("SOCKS greeting failed: %v", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil || greeting[0] != 0x05 || greeting[1] != 0x00 {
		conn.Close()
		t.Fatalf("unexpected SOCKS greeting: %v %v", greeting, err)
	}

	host, portText, err := net.SplitHostPort(targetAddr)
	if err != nil {
		conn.Close()
		t.Fatalf("invalid target address: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		conn.Close()
		t.Fatalf("invalid target port: %v", err)
	}
	request := []byte{0x05, 0x01, 0x00, 0x01}
	if ip := net.ParseIP(host).To4(); ip != nil {
		request = append(request, ip...)
	} else {
		conn.Close()
		t.Fatalf("target %q is not an IPv4 address", host)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err := conn.Write(request); err != nil {
		conn.Close()
		t.Fatalf("SOCKS connect request failed: %v", err)
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		conn.Close()
		t.Fatalf("SOCKS connect reply failed: %v", err)
	}
	if header[1] != 0x00 {
		conn.Close()
		t.Fatalf("SOCKS connect rejected with code %d", header[1])
	}
	addressLength := 0
	switch header[3] {
	case 0x01:
		addressLength = 4
	case 0x04:
		addressLength = 16
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			conn.Close()
			t.Fatalf("failed to read SOCKS bound address: %v", err)
		}
		addressLength = int(length[0])
	default:
		conn.Close()
		t.Fatalf("unexpected SOCKS address type %d", header[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, addressLength+2)); err != nil {
		conn.Close()
		t.Fatalf("failed to read SOCKS bound port: %v", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		t.Fatalf("failed to clear connection deadline: %v", err)
	}
	return conn
}
