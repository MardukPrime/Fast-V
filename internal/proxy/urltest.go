package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
)

// URL latency testing runs one temporary Xray-core process with N SOCKS inbounds
// and N node outbounds. Every inbound is bound to its outbound through a routing
// rule, so a plain HTTP request through inbound i measures the real proxy chain
// latency of node i instead of only its TCP reachability.
const (
	urlTestDefaultTargetURL = "https://www.gstatic.com/generate_204"
	// urlTestProbeTimeout bounds one HTTP round trip through one node.
	urlTestProbeTimeout = 5 * time.Second
	// urlTestStartTimeout bounds how long the temporary core may take to listen.
	urlTestStartTimeout = 12 * time.Second
	// urlTestTotalTimeout bounds the complete batch including all core startups.
	urlTestTotalTimeout = 30 * time.Second
	// urlTestConcurrency bounds the number of simultaneously running probes.
	urlTestConcurrency = 16
	// urlTestMaxBatchSize bounds how many nodes share one temporary core process.
	urlTestMaxBatchSize = 128
	// urlTestPortStart / urlTestPortEnd delimit the loopback port pool used by
	// the temporary measurement core. The production inbound port is user
	// configured (10808 by default), so this range stays out of its way.
	urlTestPortStart = 19000
	urlTestPortEnd   = 19999
	// urlTestStopTimeout bounds the graceful shutdown of the temporary core.
	urlTestStopTimeout = 5 * time.Second
)

// AppendLog records a log entry in the manager's log stream. It lets services
// that drive the manager (for example the automatic node selection scheduler)
// surface their decisions in the same log panel as the captured core output.
func (m *Manager) AppendLog(level string, source string, message string) {
	m.appendLog(level, source, message)
}

// URLTestRequest configures one batch URL latency test.
type URLTestRequest struct {
	// CorePath is the Xray-core executable used for the temporary measurement
	// process. An empty path skips the process and reports an error per node.
	CorePath string
	// Nodes are the candidate nodes measured by this request.
	Nodes []model.Node
	// Settings supplies the outbound stream overrides (mux, sockopt, ...) so the
	// measured chain matches the configuration the proxy would actually use.
	Settings model.Settings
	// TargetURL is the HTTP(S) address fetched through every node. It falls back
	// to Settings.TestURL and then to the built-in generate_204 URL.
	TargetURL string
	// ProbeTimeout bounds one probe. Defaults to 5s.
	ProbeTimeout time.Duration
	// Concurrency bounds simultaneously running probes. Defaults to 16.
	Concurrency int
	// TotalTimeout bounds the whole request. Defaults to 30s.
	TotalTimeout time.Duration
	// MaxBatchSize bounds how many nodes share one temporary core. Defaults to 128.
	MaxBatchSize int
	// TempDir is the parent directory for the temporary measurement config and
	// core logs. Services pass a directory inside the application data dir so the
	// files stay next to the runtime config; an empty value uses the OS temp dir.
	TempDir string
}

// urlTestOptions is the normalized form of URLTestRequest.
type urlTestOptions struct {
	corePath     string
	settings     model.Settings
	targetURL    string
	probeTimeout time.Duration
	concurrency  int
	totalTimeout time.Duration
	maxBatchSize int
	tempDir      string
}

func (req URLTestRequest) normalize() urlTestOptions {
	options := urlTestOptions{
		corePath:     strings.TrimSpace(req.CorePath),
		settings:     req.Settings,
		probeTimeout: req.ProbeTimeout,
		concurrency:  req.Concurrency,
		totalTimeout: req.TotalTimeout,
		maxBatchSize: req.MaxBatchSize,
		tempDir:      strings.TrimSpace(req.TempDir),
	}
	options.targetURL = strings.TrimSpace(req.TargetURL)
	if options.targetURL == "" {
		options.targetURL = strings.TrimSpace(req.Settings.TestURL)
	}
	if options.targetURL == "" {
		options.targetURL = urlTestDefaultTargetURL
	}
	if options.probeTimeout <= 0 {
		options.probeTimeout = urlTestProbeTimeout
	}
	if options.concurrency <= 0 {
		options.concurrency = urlTestConcurrency
	}
	if options.concurrency > urlTestMaxBatchSize {
		options.concurrency = urlTestMaxBatchSize
	}
	if options.totalTimeout <= 0 {
		options.totalTimeout = urlTestTotalTimeout
	}
	if options.maxBatchSize <= 0 {
		options.maxBatchSize = urlTestMaxBatchSize
	}
	return options
}

// URLTest measures the real proxy-chain latency of every node in req.Nodes by
// running a temporary Xray-core process with one SOCKS inbound per node. It
// always returns one result per requested node, in the same order.
func URLTest(ctx context.Context, req URLTestRequest) []model.NodeLatency {
	if ctx == nil {
		ctx = context.Background()
	}
	options := req.normalize()
	if len(req.Nodes) == 0 {
		return nil
	}
	if options.corePath == "" {
		return urlTestFailures(req.Nodes, "Xray-core 未安装，无法进行 URL 测速")
	}
	if _, err := parseHTTPTestURL(options.targetURL); err != nil {
		return urlTestFailures(req.Nodes, err.Error())
	}

	totalCtx, cancel := context.WithTimeout(ctx, options.totalTimeout)
	defer cancel()

	results := make([]model.NodeLatency, len(req.Nodes))
	candidates := prepareURLTestCandidates(req.Nodes, options.settings)
	runnable := make([]urlTestCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.err != nil {
			results[candidate.index] = urlTestError(candidate.node.ID, fmt.Sprintf("节点配置无效: %v", candidate.err))
			continue
		}
		runnable = append(runnable, candidate)
	}

	for start := 0; start < len(runnable); start += options.maxBatchSize {
		end := start + options.maxBatchSize
		if end > len(runnable) {
			end = len(runnable)
		}
		batch := runnable[start:end]
		if err := totalCtx.Err(); err != nil {
			for _, candidate := range batch {
				results[candidate.index] = urlTestError(candidate.node.ID, "测速超时")
			}
			continue
		}
		batchResults, err := runURLTestBatch(totalCtx, options, batch)
		if err != nil {
			message := "URL 测速失败: " + err.Error()
			for _, candidate := range batch {
				results[candidate.index] = urlTestError(candidate.node.ID, message)
			}
			continue
		}
		for i, candidate := range batch {
			results[candidate.index] = batchResults[i]
		}
	}
	return results
}

// urlTestCandidate is one node together with its generated outbound.
type urlTestCandidate struct {
	// index is the position of the node in the original request.
	index    int
	node     model.Node
	outbound map[string]any
	err      error
}

// prepareURLTestCandidates builds one outbound object per node. Nodes whose
// configuration cannot be converted are kept with an err so they can be reported
// individually instead of failing the whole batch.
func prepareURLTestCandidates(nodes []model.Node, settings model.Settings) []urlTestCandidate {
	candidates := make([]urlTestCandidate, 0, len(nodes))
	for index, node := range nodes {
		outbound, err := buildOutbound(node, settings)
		candidates = append(candidates, urlTestCandidate{
			index:    index,
			node:     node,
			outbound: outbound,
			err:      err,
		})
	}
	return candidates
}

// buildURLTestConfig renders the temporary Xray configuration: one no-auth SOCKS
// inbound per node plus a routing rule that pins each inbound to its outbound.
func buildURLTestConfig(candidates []urlTestCandidate, ports []int) ([]byte, error) {
	if len(candidates) != len(ports) {
		return nil, fmt.Errorf("测速配置端口数量不匹配: %d 个节点, %d 个端口", len(candidates), len(ports))
	}
	inbounds := make([]any, 0, len(candidates))
	outbounds := make([]any, 0, len(candidates)+2)
	rules := make([]any, 0, len(candidates))
	for i, candidate := range candidates {
		if candidate.err != nil {
			return nil, fmt.Errorf("节点 %q 的 outbound 无效: %w", candidate.node.Name, candidate.err)
		}
		inboundTag := "in" + strconv.Itoa(i)
		outboundTag := "p" + strconv.Itoa(i)
		inbounds = append(inbounds, map[string]any{
			"tag":      inboundTag,
			"listen":   "127.0.0.1",
			"port":     ports[i],
			"protocol": "socks",
			"settings": map[string]any{
				"auth": "noauth",
				"udp":  false,
			},
		})
		// Copy the outbound so a caller-provided candidate map is never mutated.
		outbound := make(map[string]any, len(candidate.outbound)+1)
		for key, value := range candidate.outbound {
			outbound[key] = value
		}
		outbound["tag"] = outboundTag
		outbounds = append(outbounds, outbound)
		rules = append(rules, map[string]any{
			"type":        "field",
			"inboundTag":  []string{inboundTag},
			"outboundTag": outboundTag,
		})
	}
	outbounds = append(outbounds,
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	)
	config := map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing": map[string]any{
			// The routing rule only matches inbound tags, so no local DNS
			// resolution is wanted here; the node outbound resolves remotely.
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}
	return json.MarshalIndent(config, "", "  ")
}

// urlTestBatchGate serializes temporary measurement cores. Concurrent URL
// latency runs (scheduler plus a UI-triggered run, NodeService plus
// ProxyService) then never compete for the same loopback ports in the 19xxx
// pool. It is a channel instead of a mutex so a cancelled request can bail out
// while another batch is still running.
var urlTestBatchGate = make(chan struct{}, 1)

// runURLTestBatch starts one temporary core for the given candidates, probes
// every inbound concurrently and always terminates the process afterwards.
func runURLTestBatch(ctx context.Context, options urlTestOptions, candidates []urlTestCandidate) ([]model.NodeLatency, error) {
	select {
	case urlTestBatchGate <- struct{}{}:
		defer func() { <-urlTestBatchGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ports, err := pickLocalPorts(len(candidates))
	if err != nil {
		return nil, err
	}
	config, err := buildURLTestConfig(candidates, ports)
	if err != nil {
		return nil, err
	}
	dir, err := createURLTestTempDir(options.tempDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	configPath := filepath.Join(dir, "xray-urltest.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		return nil, fmt.Errorf("写入测速配置失败: %w", err)
	}

	cmd := exec.Command(options.corePath, "run", "-config", configPath)
	hideWindow(cmd)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+filepath.Dir(options.corePath))
	// Stdout and Stderr share one writer; os/exec serializes writes to the same
	// comparable writer, so this is race free.
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动临时 Xray-core 失败: %w", err)
	}

	var waitErr error
	var waitErrMu sync.Mutex
	exited := make(chan struct{})
	go func() {
		err := cmd.Wait()
		waitErrMu.Lock()
		waitErr = err
		waitErrMu.Unlock()
		close(exited)
	}()
	job, jobErr := newProcessJob()
	if jobErr == nil {
		if assignErr := job.Assign(cmd.Process); assignErr != nil {
			_ = job.Close()
			job = nil
		}
	}
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			select {
			case <-exited:
			case <-time.After(urlTestStopTimeout):
			}
			if job != nil {
				_ = job.Close()
			}
		})
	}
	// The deferred cleanup runs after the probes finished and before the temp
	// directory is removed (defers run LIFO).
	defer cleanup()

	addresses := make([]string, len(candidates))
	for i, port := range ports {
		addresses[i] = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	if err := waitURLTestPorts(ctx, exited, addresses, urlTestStartTimeout); err != nil {
		cleanup()
		waitErrMu.Lock()
		processErr := waitErr
		waitErrMu.Unlock()
		detail := summarizeCoreOutput(output.Bytes())
		if processErr != nil {
			return nil, fmt.Errorf("%w（内核退出: %v；输出: %s）", err, processErr, detail)
		}
		return nil, fmt.Errorf("%w（内核输出: %s）", err, detail)
	}

	results := make([]model.NodeLatency, len(candidates))
	semaphore := make(chan struct{}, options.concurrency)
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(index int, nodeID string, address string) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				results[index] = urlTestError(nodeID, "测速超时")
				return
			}
			defer func() { <-semaphore }()
			results[index] = probeNodeURL(ctx, nodeID, address, options.targetURL, options.probeTimeout)
		}(i, candidate.node.ID, addresses[i])
	}
	wg.Wait()
	return results, nil
}

// createURLTestTempDir creates the temporary directory for one measurement
// batch. A configured parent (the application runtime directory) keeps the
// temporary files next to the normal runtime config.
func createURLTestTempDir(parent string) (string, error) {
	if parent == "" {
		dir, err := os.MkdirTemp("", "fastv-urltest-")
		if err != nil {
			return "", fmt.Errorf("创建测速临时目录失败: %w", err)
		}
		return dir, nil
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("创建测速临时目录失败: %w", err)
	}
	dir, err := os.MkdirTemp(parent, "fastv-urltest-")
	if err != nil {
		return "", fmt.Errorf("创建测速临时目录失败: %w", err)
	}
	return dir, nil
}

// waitURLTestPorts waits until every temporary SOCKS inbound accepts TCP
// connections. It bails out early when the core process exits or the context is
// cancelled.
func waitURLTestPorts(ctx context.Context, exited <-chan struct{}, addresses []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ready := 0
		for _, address := range addresses {
			if portListening(address) {
				ready++
			}
		}
		if ready == len(addresses) {
			return nil
		}
		select {
		case <-exited:
			return errors.New("临时 Xray-core 提前退出")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待临时 Xray-core 端口就绪超时（%d/%d 个入站就绪）", ready, len(addresses))
		}
	}
}

// portListening reports whether a loopback TCP listener accepts connections.
func portListening(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// probeNodeURL fetches the test URL through one temporary SOCKS inbound and
// converts the round trip into a NodeLatency result.
func probeNodeURL(ctx context.Context, nodeID string, proxyAddress string, targetURL string, timeout time.Duration) model.NodeLatency {
	result := model.NodeLatency{NodeID: nodeID, CheckedAt: model.NowRFC3339()}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	latency, err := httpLatencyThroughSOCKS5(probeCtx, proxyAddress, targetURL, timeout)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.LatencyMS = latency
	return result
}

// httpLatencyThroughSOCKS5 performs one GET request through a SOCKS5 proxy and
// returns the elapsed time until the response headers arrive.
func httpLatencyThroughSOCKS5(ctx context.Context, proxyAddress string, targetURL string, timeout time.Duration) (int64, error) {
	target, err := parseHTTPTestURL(targetURL)
	if err != nil {
		return 0, err
	}
	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, network string, address string) (net.Conn, error) {
			return dialSOCKS5Tunnel(dialCtx, proxyAddress, address, timeout)
		},
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("测速请求构造失败: %w", err)
	}
	start := time.Now()
	response, err := client.Do(request)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		if ctx.Err() != nil {
			return 0, errors.New("测速超时")
		}
		return 0, fmt.Errorf("请求失败: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return 0, fmt.Errorf("测试地址返回 HTTP %d", response.StatusCode)
	}
	if latency <= 0 {
		latency = 1
	}
	return latency, nil
}

// dialSOCKS5Tunnel dials the local SOCKS5 inbound and performs an unauthenticated
// CONNECT for targetAddress. The handshake is intentionally minimal: only the
// no-auth method and the CONNECT command are supported.
func dialSOCKS5Tunnel(ctx context.Context, proxyAddress string, targetAddress string, timeout time.Duration) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return nil, fmt.Errorf("无效的目标地址 %q: %w", targetAddress, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("无效的目标端口 %q", portText)
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return nil, fmt.Errorf("连接本地 SOCKS5 入站失败: %w", err)
	}
	aborted := true
	defer func() {
		if aborted {
			_ = conn.Close()
		}
	}()

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil, fmt.Errorf("SOCKS5 握手失败: %w", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return nil, fmt.Errorf("SOCKS5 握手响应读取失败: %w", err)
	}
	if greeting[0] != 0x05 {
		return nil, fmt.Errorf("SOCKS5 版本不支持: 0x%02x", greeting[0])
	}
	if greeting[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 代理不接受无认证方式: 0x%02x", greeting[1])
	}

	request := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			request = append(request, 0x01)
			request = append(request, ip4...)
		} else {
			request = append(request, 0x04)
			request = append(request, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return nil, fmt.Errorf("无效的目标主机名 %q", host)
		}
		request = append(request, 0x03, byte(len(host)))
		request = append(request, host...)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("SOCKS5 CONNECT 请求发送失败: %w", err)
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("SOCKS5 CONNECT 响应读取失败: %w", err)
	}
	if header[0] != 0x05 {
		return nil, fmt.Errorf("SOCKS5 CONNECT 响应版本不支持: 0x%02x", header[0])
	}
	if header[1] != 0x00 {
		return nil, fmt.Errorf("SOCKS5 代理拒绝连接 (rep=0x%02x)", header[1])
	}
	switch header[3] {
	case 0x01:
		if _, err := io.ReadFull(conn, make([]byte, 4)); err != nil {
			return nil, fmt.Errorf("SOCKS5 绑定地址读取失败: %w", err)
		}
	case 0x04:
		if _, err := io.ReadFull(conn, make([]byte, 16)); err != nil {
			return nil, fmt.Errorf("SOCKS5 绑定地址读取失败: %w", err)
		}
	case 0x03:
		length := make([]byte, 1)
		if _, err := io.ReadFull(conn, length); err != nil {
			return nil, fmt.Errorf("SOCKS5 绑定域名长度读取失败: %w", err)
		}
		if _, err := io.ReadFull(conn, make([]byte, int(length[0]))); err != nil {
			return nil, fmt.Errorf("SOCKS5 绑定域名读取失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("SOCKS5 绑定地址类型不支持: 0x%02x", header[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, 2)); err != nil {
		return nil, fmt.Errorf("SOCKS5 绑定端口读取失败: %w", err)
	}

	// The HTTP client owns the connection from here on; a stale deadline would
	// abort larger responses.
	_ = conn.SetDeadline(time.Time{})
	aborted = false
	return conn, nil
}

// pickLocalPorts reserves count distinct free loopback ports in the URL-test port
// pool (19000-19999).
func pickLocalPorts(count int) ([]int, error) {
	return pickLocalPortsInRange(count, urlTestPortStart, urlTestPortEnd)
}

// pickLocalPortsInRange probes the given loopback port range and returns the
// first count ports that can be bound. Every candidate is released immediately;
// the temporary core binds them a moment later.
func pickLocalPortsInRange(count int, start int, end int) ([]int, error) {
	if count <= 0 {
		return nil, nil
	}
	if start < 1 || end > 65535 || end < start {
		return nil, fmt.Errorf("无效的端口范围 %d-%d", start, end)
	}
	capacity := end - start + 1
	if count > capacity {
		return nil, fmt.Errorf("需要 %d 个测速端口，但端口池只有 %d 个", count, capacity)
	}
	ports := make([]int, 0, count)
	for port := start; port <= end && len(ports) < count; port++ {
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		if err := listener.Close(); err != nil {
			continue
		}
		ports = append(ports, port)
	}
	if len(ports) < count {
		return nil, fmt.Errorf("只找到 %d 个可用本地端口（需要 %d 个），请稍后重试", len(ports), count)
	}
	return ports, nil
}

// parseHTTPTestURL validates the HTTP(S) URL probed through every node.
func parseHTTPTestURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("测速地址无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("测速地址必须是 http 或 https")
	}
	if parsed.Hostname() == "" {
		return nil, errors.New("测速地址缺少主机名")
	}
	return parsed, nil
}

// summarizeCoreOutput returns the tail of the captured core output for errors.
func summarizeCoreOutput(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "无输出"
	}
	const limit = 600
	if len(text) > limit {
		return "..." + text[len(text)-limit:]
	}
	return text
}

// URLTestSelection is one requested node. Error is set when the node cannot be
// measured (unknown id or disabled node).
type URLTestSelection struct {
	NodeID string
	Node   model.Node
	Error  string
}

// SelectURLTestNodes resolves the requested ids against the stored nodes. An
// empty ids list selects every enabled node; explicit ids keep the requested
// order and report unknown or disabled nodes as Error selections.
func SelectURLTestNodes(nodes []model.Node, ids []string) []URLTestSelection {
	if len(ids) == 0 {
		selections := make([]URLTestSelection, 0, len(nodes))
		for _, node := range nodes {
			if !node.Enabled {
				continue
			}
			selections = append(selections, URLTestSelection{NodeID: node.ID, Node: node})
		}
		return selections
	}
	byID := make(map[string]model.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	selections := make([]URLTestSelection, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		node, ok := byID[id]
		if !ok {
			selections = append(selections, URLTestSelection{NodeID: id, Error: "节点不存在"})
			continue
		}
		if !node.Enabled {
			selections = append(selections, URLTestSelection{NodeID: id, Node: node, Error: "节点未启用"})
			continue
		}
		selections = append(selections, URLTestSelection{NodeID: id, Node: node})
	}
	return selections
}

// MergeURLTestResults restores the requested order and turns skipped selections
// into error results.
func MergeURLTestResults(selections []URLTestSelection, tested map[string]model.NodeLatency) []model.NodeLatency {
	checkedAt := model.NowRFC3339()
	results := make([]model.NodeLatency, 0, len(selections))
	for _, selection := range selections {
		if selection.Error != "" {
			results = append(results, model.NodeLatency{NodeID: selection.NodeID, Error: selection.Error, CheckedAt: checkedAt})
			continue
		}
		result, ok := tested[selection.NodeID]
		if !ok {
			results = append(results, model.NodeLatency{NodeID: selection.NodeID, Error: "测速结果缺失", CheckedAt: checkedAt})
			continue
		}
		results = append(results, result)
	}
	return results
}

// URLTestNodes is the high-level entry point used by the Wails services. It
// resolves the requested ids (empty means every enabled node), measures the
// candidates through a temporary core and returns one result per requested node.
func URLTestNodes(ctx context.Context, core CoreInfoProvider, nodes []model.Node, settings model.Settings, ids []string, tempDir string) []model.NodeLatency {
	selections := SelectURLTestNodes(nodes, ids)
	if len(selections) == 0 {
		return []model.NodeLatency{}
	}
	corePath := ""
	if core != nil {
		if xrayProvider, ok := core.(interface{ XrayInfo() model.CoreInfo }); ok {
			corePath = strings.TrimSpace(xrayProvider.XrayInfo().Path)
		} else {
			corePath = strings.TrimSpace(core.Info().Path)
		}
	}
	candidates := make([]model.Node, 0, len(selections))
	for _, selection := range selections {
		if selection.Error == "" {
			candidates = append(candidates, selection.Node)
		}
	}
	tested := make(map[string]model.NodeLatency, len(candidates))
	if len(candidates) > 0 {
		if corePath == "" {
			for _, node := range candidates {
				tested[node.ID] = urlTestError(node.ID, "Xray-core 未安装，无法进行 URL 测速")
			}
		} else {
			results := URLTest(ctx, URLTestRequest{
				CorePath: corePath,
				Nodes:    candidates,
				Settings: settings,
				TempDir:  tempDir,
			})
			for _, result := range results {
				tested[result.NodeID] = result
			}
		}
	}
	return MergeURLTestResults(selections, tested)
}

func urlTestError(nodeID string, message string) model.NodeLatency {
	return model.NodeLatency{NodeID: nodeID, Error: message, CheckedAt: model.NowRFC3339()}
}

func urlTestFailures(nodes []model.Node, message string) []model.NodeLatency {
	results := make([]model.NodeLatency, len(nodes))
	checkedAt := model.NowRFC3339()
	for i, node := range nodes {
		results[i] = model.NodeLatency{NodeID: node.ID, Error: message, CheckedAt: checkedAt}
	}
	return results
}

// ChooseBestNode returns the node id that should replace the current node after
// a URL latency round, or "" when the current node should be kept.
//
// A candidate is only eligible when its probe succeeded. The switch also has to
// beat the current latency by at least toleranceMS milliseconds so measurement
// noise does not make the active node flap. currentLatency may be 0 when the
// current node was not part of the round (for example when it is disabled); a
// successful candidate is then selected without a tolerance comparison.
func ChooseBestNode(results []model.NodeLatency, currentID string, currentLatency int64, toleranceMS int) string {
	if toleranceMS < 0 {
		toleranceMS = 0
	}
	best := model.NodeLatency{}
	for _, result := range results {
		if result.NodeID == "" || result.Error != "" || result.LatencyMS <= 0 {
			continue
		}
		if best.NodeID == "" || result.LatencyMS < best.LatencyMS {
			best = result
		}
	}
	if best.NodeID == "" || best.NodeID == currentID {
		return ""
	}
	if currentLatency <= 0 {
		return best.NodeID
	}
	if best.LatencyMS >= currentLatency {
		return ""
	}
	if currentLatency-best.LatencyMS < int64(toleranceMS) {
		return ""
	}
	return best.NodeID
}

// ChooseFailoverNode returns the node id to switch to after the current node
// failed its health check. A successful latency result is preferred; when no
// result is usable the next enabled node in list order is returned. It returns
// "" when no alternative node exists.
func ChooseFailoverNode(results []model.NodeLatency, enabled []model.Node, currentID string) string {
	enabledSet := make(map[string]struct{}, len(enabled))
	for _, node := range enabled {
		if node.Enabled {
			enabledSet[node.ID] = struct{}{}
		}
	}
	bestID := ""
	var bestLatency int64
	for _, result := range results {
		if result.NodeID == "" || result.NodeID == currentID {
			continue
		}
		if result.Error != "" || result.LatencyMS <= 0 {
			continue
		}
		if len(enabledSet) > 0 {
			if _, ok := enabledSet[result.NodeID]; !ok {
				continue
			}
		}
		if bestID == "" || result.LatencyMS < bestLatency {
			bestID = result.NodeID
			bestLatency = result.LatencyMS
		}
	}
	if bestID != "" {
		return bestID
	}
	return NextEnabledNodeID(enabled, currentID)
}

// NextEnabledNodeID returns the next enabled node after currentID in list order,
// wrapping around at the end. It returns "" when no other enabled node exists.
func NextEnabledNodeID(nodes []model.Node, currentID string) string {
	enabled := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Enabled {
			enabled = append(enabled, node)
		}
	}
	if len(enabled) <= 1 {
		return ""
	}
	currentIndex := -1
	for i, node := range enabled {
		if node.ID == currentID {
			currentIndex = i
			break
		}
	}
	if currentIndex < 0 {
		return enabled[0].ID
	}
	next := (currentIndex + 1) % len(enabled)
	if next == currentIndex {
		return ""
	}
	return enabled[next].ID
}
