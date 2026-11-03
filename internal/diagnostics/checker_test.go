package diagnostics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeDirectWithSpeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/trace":
			fmt.Fprint(w, "ip=203.0.113.9\nloc=JP\ncolo=NRT\ntls=TLSv1.3\nwarp=off\n")
		case r.URL.Path == "/ip":
			fmt.Fprint(w, `{"ip":"203.0.113.9"}`)
		case r.URL.Path == "/geo/203.0.113.9":
			fmt.Fprint(w, `{"success":true,"ip":"203.0.113.9","type":"IPv4","country":"Japan","country_code":"JP","region":"Tokyo","city":"Tokyo","connection":{"asn":64500,"org":"Example Network","isp":"Example ISP"},"timezone":{"id":"Asia/Tokyo"}}`)
		case r.URL.Path == "/speed":
			w.Header().Set("Content-Type", "application/octet-stream")
			fmt.Fprint(w, strings.Repeat("x", 1024))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	checker := NewChecker()
	checker.traceURL = server.URL + "/trace"
	checker.ipURL = server.URL + "/ip"
	checker.geoURL = server.URL + "/geo/%s"
	checker.speedURL = server.URL + "/speed?bytes=%d"
	checker.speedBytes = 1024
	checker.latencySamples = 2

	result, err := checker.Probe(context.Background(), "direct", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.IP != "203.0.113.9" || result.IPVersion != "IPv4" {
		t.Fatalf("unexpected IP result: %+v", result)
	}
	if result.Country != "Japan" || result.ASN != 64500 || result.ISP != "Example ISP" {
		t.Fatalf("unexpected geo result: %+v", result)
	}
	if result.Colo != "NRT" || result.TLS != "TLSv1.3" {
		t.Fatalf("unexpected trace result: %+v", result)
	}
	if result.LatencyMS < 0 || result.JitterMS < 0 {
		t.Fatalf("unexpected latency result: %+v", result)
	}
	if result.DownloadBytes != 1024 || result.DownloadMbps <= 0 {
		t.Fatalf("unexpected download result: %+v", result)
	}
}

func TestProbeFallsBackToIPService(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ip" {
			fmt.Fprint(w, `{"ip":"198.51.100.7"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	checker := NewChecker()
	checker.traceURL = server.URL + "/trace"
	checker.ipURL = server.URL + "/ip"
	checker.geoURL = server.URL + "/geo/%s"
	checker.latencySamples = 1

	result, err := checker.Probe(context.Background(), "direct", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.IP != "198.51.100.7" {
		t.Fatalf("unexpected fallback IP: %+v", result)
	}
}

func TestProbeProxyRequiresAddress(t *testing.T) {
	if _, err := NewChecker().Probe(context.Background(), "proxy", "", false); err == nil {
		t.Fatal("expected proxy address error")
	}
}
