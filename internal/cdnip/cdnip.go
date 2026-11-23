// Package cdnip finds the fastest reachable CDN edge for a node that sits behind
// Cloudflare.
//
// The scan is deliberately offline-first: candidates come from Cloudflare's
// published anycast ranges, so it works without a subscription, a third-party
// API or an account. A candidate only counts when the edge answers a TLS
// handshake for the node's own host name and returns an HTTP response — an IP
// that merely accepts TCP is not serving the site and would make the node look
// broken once selected.
package cdnip

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// Cloudflare anycast ranges. Sampling one address per range keeps the scan short
// while still covering every block Cloudflare announces.
var anycastPrefixes = []string{
	"104.16.0.0/13",    // 104.16.0.0 - 104.23.255.255
	"104.24.0.0/14",    // 104.24.0.0 - 104.27.255.255
	"162.158.0.0/15",   // 162.158.0.0 - 162.159.255.255
	"172.64.0.0/13",    // 172.64.0.0 - 172.71.255.255
	"188.114.96.0/20",  // 188.114.96.0 - 188.114.111.255
	"197.234.240.0/22", // 197.234.240.0 - 197.234.243.255
	"198.41.128.0/17",  // 198.41.128.0 - 198.41.255.255
}

// wellKnownSeeds are always tested first so repeated scans stay comparable and a
// save from a previous session keeps working.
var wellKnownSeeds = []string{
	"104.16.0.1",
	"104.17.0.1",
	"104.18.0.1",
	"104.19.0.1",
	"104.20.0.1",
	"104.21.0.1",
	"104.22.0.1",
	"104.23.0.1",
	"104.24.0.1",
	"104.25.0.1",
	"104.26.0.1",
	"104.27.0.1",
	"162.159.0.1",
	"172.64.0.1",
	"172.66.0.1",
	"172.67.0.1",
	"188.114.96.1",
	"188.114.97.1",
	"197.234.240.1",
	"198.41.128.1",
}

// limits keep an accidental huge request from turning into a port scan.
const (
	maxCandidates   = 4000
	defaultCount    = 10
	defaultTimeout  = 3 * time.Second
	defaultWorkers  = 12
	maxWorkers      = 64
	probeUserAgent  = "FastV-cdn-scan"
	maxScanDuration = 3 * time.Minute
)

// blocks holds every /24 of the anycast ranges, the unit the sampler draws from.
var blocks = enumerateBlocks()

func enumerateBlocks() []netip.Prefix {
	var out []netip.Prefix
	for _, raw := range anycastPrefixes {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		base := prefix.Masked().Addr().As4()
		// Number of /24 blocks covered by this range.
		blockCount := 1 << (24 - prefix.Bits())
		value := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8
		for i := 0; i < blockCount; i++ {
			v := value + uint32(i)<<8
			addr := netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), 0})
			out = append(out, netip.PrefixFrom(addr, 24))
		}
	}
	return out
}

// Candidates returns the addresses a scan will test: the well-known seeds, the
// caller's own list, then sample random hosts drawn from the anycast ranges.
func Candidates(sample int, custom string) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(wellKnownSeeds)+sample)
	out := make([]netip.Addr, 0, len(wellKnownSeeds)+sample)
	add := func(addr netip.Addr) {
		if !addr.IsValid() || addr.IsUnspecified() {
			return
		}
		addr = addr.Unmap()
		if _, exists := seen[addr]; exists {
			return
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}

	for _, raw := range wellKnownSeeds {
		if addr, err := netip.ParseAddr(raw); err == nil {
			add(addr)
		}
	}
	for _, raw := range strings.FieldsFunc(custom, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	}) {
		if addr, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil {
			add(addr)
		}
	}
	if sample > 0 && len(blocks) > 0 {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i := 0; i < sample; i++ {
			block := blocks[rng.Intn(len(blocks))]
			base := block.Addr().As4()
			host := byte(1 + rng.Intn(254))
			add(netip.AddrFrom4([4]byte{base[0], base[1], base[2], host}))
		}
	}
	if len(out) > maxCandidates {
		out = out[:maxCandidates]
	}
	return out
}

// Scan tests every candidate and returns the fastest edges that serve host.
func Scan(ctx context.Context, request model.CdnScanRequest) (model.CdnScanResult, error) {
	host := strings.TrimSpace(request.Host)
	if host == "" {
		return model.CdnScanResult{}, errors.New("缺少节点域名，无法指定 Host")
	}
	port := request.Port
	if port <= 0 || port > 65535 {
		port = 443
	}
	timeout := time.Duration(request.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	workers := request.Concurrency
	if workers <= 0 {
		workers = defaultWorkers
	}
	if workers > maxWorkers {
		workers = maxWorkers
	}
	count := request.Count
	if count <= 0 {
		count = defaultCount
	}
	if count > 200 {
		count = 200
	}

	candidates := Candidates(request.Sample, request.CustomIPs)
	if len(candidates) == 0 {
		return model.CdnScanResult{}, errors.New("没有可测试的候选地址")
	}

	ctx, cancel := context.WithTimeout(ctx, maxScanDuration)
	defer cancel()

	started := time.Now()
	hits := make([]model.CdnScanHit, 0, len(candidates))
	var mu sync.Mutex
	failures := 0

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		wg.Add(1)
		go func(ip netip.Addr) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			hit, ok := probe(ctx, ip, host, port, timeout)
			mu.Lock()
			if ok {
				hits = append(hits, hit)
			} else {
				failures++
			}
			mu.Unlock()
		}(candidate)
	}
	wg.Wait()

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].LatencyMS == hits[j].LatencyMS {
			return hits[i].IP < hits[j].IP
		}
		return hits[i].LatencyMS < hits[j].LatencyMS
	})
	reachable := len(hits)
	if len(hits) > count {
		hits = hits[:count]
	}

	result := model.CdnScanResult{
		Host:      host,
		Tested:    len(candidates),
		Reachable: reachable,
		Hits:      hits,
		ElapsedMS: time.Since(started).Milliseconds(),
	}
	if reachable == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf(
			"%d 个候选地址都无法在 %s 内完成 TLS 握手，检查节点域名、端口或当前网络", len(candidates), timeout))
	} else if failures > 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("%d 个候选不可用，已排除", failures))
	}
	return result, nil
}

// probe measures the TCP connect time and then proves the edge serves host.
func probe(ctx context.Context, ip netip.Addr, host string, port int, timeout time.Duration) (model.CdnScanHit, bool) {
	address := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	dialer := net.Dialer{Timeout: timeout}
	dialStarted := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return model.CdnScanHit{}, false
	}
	connectLatency := time.Since(dialStarted)
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return model.CdnScanHit{}, false
	}

	var stream net.Conn = conn
	secure := port != 80
	if secure {
		tlsConn := tls.Client(conn, &tls.Config{
			// The certificate is irrelevant here: SNI routing is what decides
			// whether this edge serves the host, and the payload never leaves
			// the scan.
			ServerName:         host,
			InsecureSkipVerify: true, //nolint:gosec // scan probe, no data exchanged
			NextProtos:         []string{"http/1.1"},
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return model.CdnScanHit{}, false
		}
		stream = tlsConn
	}

	request := "HEAD / HTTP/1.1\r\nHost: " + host + "\r\nUser-Agent: " + probeUserAgent + "\r\nConnection: close\r\n\r\n"
	if _, err := stream.Write([]byte(request)); err != nil {
		return model.CdnScanHit{}, false
	}
	reader := bufio.NewReader(stream)
	line, err := reader.ReadString('\n')
	if err != nil {
		return model.CdnScanHit{}, false
	}
	status := parseStatusLine(line)
	if status == 0 {
		return model.CdnScanHit{}, false
	}
	return model.CdnScanHit{
		IP:        ip.String(),
		LatencyMS: connectLatency.Milliseconds(),
		Status:    status,
		TLSOk:     secure,
	}, true
}

// parseStatusLine reads the code out of "HTTP/1.1 200 OK".
func parseStatusLine(line string) int {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "HTTP/") {
		return 0
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil || code < 100 || code > 599 {
		return 0
	}
	return code
}
