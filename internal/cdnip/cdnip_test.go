package cdnip

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

func TestParseStatusLine(t *testing.T) {
	cases := map[string]int{
		"HTTP/1.1 200 OK\r\n":        200,
		"HTTP/1.1 403 Forbidden\r\n": 403,
		"HTTP/2 200\r\n":             200,
		"":                           0,
		"garbage":                    0,
		"HTTP/1.1 abc\r\n":           0,
	}
	for line, want := range cases {
		if got := parseStatusLine(line); got != want {
			t.Fatalf("parseStatusLine(%q) = %d, want %d", line, got, want)
		}
	}
}

func TestCandidatesKeepsSeedsAndCustom(t *testing.T) {
	candidates := Candidates(0, "1.2.3.4, not-an-ip\n 5.6.7.8 ")
	seen := make(map[netip.Addr]bool, len(candidates))
	for _, addr := range candidates {
		seen[addr] = true
	}
	for _, want := range []string{"1.2.3.4", "5.6.7.8", wellKnownSeeds[0]} {
		addr := netip.MustParseAddr(want)
		if !seen[addr] {
			t.Fatalf("candidate %s missing from %v", want, candidates)
		}
	}
	if len(candidates) != len(wellKnownSeeds)+2 {
		t.Fatalf("expected seeds plus two custom addresses, got %d", len(candidates))
	}
}

func TestCandidatesSampleStaysInsideAnycastRanges(t *testing.T) {
	prefixes := make([]netip.Prefix, 0, len(anycastPrefixes))
	for _, raw := range anycastPrefixes {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		prefixes = append(prefixes, prefix)
	}
	for _, addr := range Candidates(200, "") {
		found := false
		for _, prefix := range prefixes {
			if prefix.Contains(addr) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("sampled address %s is outside the anycast ranges", addr)
		}
	}
}

func TestEnumerateBlocksCoverage(t *testing.T) {
	if len(blocks) < 1000 {
		t.Fatalf("expected thousands of /24 blocks, got %d", len(blocks))
	}
	want := netip.MustParsePrefix("104.16.0.0/24")
	found := false
	for _, block := range blocks {
		if block == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("block %s missing from the enumeration", want)
	}
}

func TestScanRejectsMissingHost(t *testing.T) {
	if _, err := Scan(context.Background(), model.CdnScanRequest{}); err == nil {
		t.Fatal("expected an error when the host is empty")
	}
}

// TestProbeAcceptsServingEdge drives the probe against a local TLS server that
// answers any request, which is exactly what a Cloudflare edge does for a
// configured host.
func TestProbeAcceptsServingEdge(t *testing.T) {
	certificate := selfSignedCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer conn.Close()
				buf := make([]byte, 1024)
				_, _ = conn.Read(buf)
				_, _ = conn.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))
			}()
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	hit, ok := probe(context.Background(), netip.MustParseAddr("127.0.0.1"), "example.com", port, 2*time.Second)
	if !ok {
		t.Fatal("probe rejected a serving edge")
	}
	if hit.Status != 204 {
		t.Fatalf("unexpected status %d", hit.Status)
	}
	if !hit.TLSOk {
		t.Fatal("expected the probe to report a TLS handshake")
	}
	_ = strconv.Itoa(port)
}

// TestProbeRejectsClosedPort makes sure a plain TCP failure is reported as
// unusable instead of being counted as a candidate.
func TestProbeRejectsClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	if _, ok := probe(context.Background(), netip.MustParseAddr("127.0.0.1"), "example.com", port, 500*time.Millisecond); ok {
		t.Fatal("probe accepted a closed port")
	}
}

func selfSignedCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fastv-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	return certificate
}
