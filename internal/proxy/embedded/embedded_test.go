//go:build embeddedcore

package embedded

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// TestEmbeddedCoreServesSocks starts the real Xray-core in-process with a SOCKS
// inbound and a freedom outbound, then fetches a local HTTP server through that
// SOCKS proxy. This is the desktop verification for the Android Phase 2 M2
// milestone: the exact same library will host the embedded core on Android.
func TestEmbeddedCoreServesSocks(t *testing.T) {
	// Local origin server.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen origin: %v", err)
	}
	defer listener.Close()
	originURL := fmt.Sprintf("http://%s/", listener.Addr().String())
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "embedded-core-ok")
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	// Reserve a SOCKS port, then release it for Xray to bind.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve socks port: %v", err)
	}
	socksPort := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	config := []byte(fmt.Sprintf(`{
		"log": {"loglevel": "warning"},
		"inbounds": [{
			"listen": "127.0.0.1",
			"port": %d,
			"protocol": "socks",
			"settings": {"auth": "noauth", "udp": false}
		}],
		"outbounds": [{"protocol": "freedom", "settings": {}}]
	}`, socksPort))

	instance, err := Start(config)
	if err != nil {
		t.Fatalf("start embedded core: %v", err)
	}
	defer instance.Close()

	socksAddr := "127.0.0.1:" + strconv.Itoa(socksPort)
	// Wait for the inbound to accept connections.
	var dialer proxy.Dialer
	deadline := time.Now().Add(15 * time.Second)
	for {
		if dialer, err = proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct); err == nil {
			conn, dialErr := dialer.Dial("tcp", listener.Addr().String())
			if dialErr == nil {
				_ = conn.Close()
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("embedded SOCKS inbound did not become ready: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		},
	}
	response, err := client.Get(originURL)
	if err != nil {
		t.Fatalf("request through embedded SOCKS failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(body) != "embedded-core-ok" {
		t.Fatalf("unexpected body %q", string(body))
	}
}
