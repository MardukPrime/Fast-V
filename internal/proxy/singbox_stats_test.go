package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// TestSingBoxTrafficSamplerStreamsRates starts a fake Clash API traffic
// endpoint, pushes two rate frames and verifies that the sampler accumulates
// session totals and publishes non-zero rates.
func TestSingBoxTrafficSamplerStreamsRates(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/traffic") {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		frames := []map[string]int64{
			{"up": 1000, "down": 4000},
			{"up": 2000, "down": 8000},
		}
		for _, frame := range frames {
			payload, _ := json.Marshal(frame)
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
			time.Sleep(120 * time.Millisecond)
		}
		// Keep the socket open until the sampler is stopped.
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	// The sampler dials 127.0.0.1:<port>; rewrite the httptest host into a port.
	address := strings.TrimPrefix(server.URL, "http://")
	_, portText, _ := strings.Cut(address, ":")
	if portText == "" {
		t.Fatalf("unexpected server address %q", server.URL)
	}
	port := 0
	for _, char := range portText {
		if char < '0' || char > '9' {
			break
		}
		port = port*10 + int(char-'0')
	}

	samples := make(chan model.TrafficStats, 8)
	sampler := newSingBoxTrafficSampler(func(stats model.TrafficStats) {
		select {
		case samples <- stats:
		default:
		}
	})
	sampler.start(t.Context(), port)
	defer sampler.stop()

	deadline := time.After(5 * time.Second)
	var observed model.TrafficStats
	for {
		select {
		case stats := <-samples:
			if stats.TotalUplink >= 3000 && stats.TotalDownlink >= 12000 && stats.DownlinkRate > 0 {
				observed = stats
				goto done
			}
		case <-deadline:
			t.Fatalf("timed out waiting for traffic samples; latest totals: up=%d down=%d", observed.TotalUplink, observed.TotalDownlink)
		}
	}
done:
	if observed.UplinkRate <= 0 || observed.DownlinkRate <= 0 {
		t.Fatalf("rates not published: %+v", observed)
	}
	if observed.TotalUplink != 3000 || observed.TotalDownlink != 12000 {
		t.Fatalf("totals = %d/%d", observed.TotalUplink, observed.TotalDownlink)
	}
}
