package proxy

import (
	"context"
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
	"github.com/MardukPrime/Fast-V/internal/store"
)

type CoreInfoProvider interface {
	Info() model.CoreInfo
}

// Manager owns the Xray process and the local proxy state.
type Manager struct {
	mu            sync.RWMutex
	store         *store.Store
	core          CoreInfoProvider
	cmd           *exec.Cmd
	done          chan struct{}
	job           *processJob
	state         model.ProxyState
	onStateChange func(model.ProxyState)
	tunCleanup    func() error
}

func NewManager(st *store.Store, coreProvider CoreInfoProvider) *Manager {
	return &Manager{
		store: st,
		core:  coreProvider,
		state: model.ProxyState{Status: "stopped"},
	}
}

func (m *Manager) SetOnStateChange(callback func(model.ProxyState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStateChange = callback
}

func (m *Manager) State() model.ProxyState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Manager) Start(ctx context.Context) (model.ProxyState, error) {
	m.mu.Lock()
	if m.cmd != nil {
		state := m.state
		m.mu.Unlock()
		return state, nil
	}
	node, ok := m.store.ActiveNode()
	if !ok {
		m.mu.Unlock()
		return m.fail("no active node selected")
	}
	settings := m.store.Settings()
	if settings.CoreType != "xray" {
		m.mu.Unlock()
		return m.fail("only Xray-core is currently supported")
	}
	coreInfo := m.core.Info()
	if !coreInfo.Installed || coreInfo.Path == "" {
		m.mu.Unlock()
		return m.fail("Xray-core is not installed")
	}
	if settings.TunMode {
		if err := preflightTun(coreInfo.Path); err != nil {
			m.mu.Unlock()
			return m.fail(err.Error())
		}
	}

	config, err := BuildConfig(node, settings)
	if err != nil {
		m.mu.Unlock()
		return m.fail(err.Error())
	}
	runtimeDir := filepath.Join(m.store.DataDir(), "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		m.mu.Unlock()
		return m.fail(err.Error())
	}
	configPath := filepath.Join(runtimeDir, "xray-config.json")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		m.mu.Unlock()
		return m.fail(err.Error())
	}

	subscriptionName := ""
	if sub, err := m.store.FindSubscription(node.SubscriptionID); err == nil {
		subscriptionName = sub.Name
	}
	resolvedIP := resolveNodeHost(ctx, node.Address)
	if settings.TunMode && resolvedIP == "" {
		m.mu.Unlock()
		return m.fail("TUN 模式要求节点服务器地址能够解析为 IP")
	}
	inbound := "SOCKS5 / HTTP"
	if settings.TunMode {
		inbound = "TUN + SOCKS5 / HTTP"
	}
	state := model.ProxyState{
		Status:           "starting",
		NodeID:           node.ID,
		NodeName:         node.Name,
		NodeProtocol:     node.Protocol,
		NodeAddress:      node.Address,
		NodePort:         node.Port,
		NodeSubscription: subscriptionName,
		ResolvedIP:       resolvedIP,
		ProxyMode:        settings.ProxyMode,
		Inbound:          inbound,
		LocalAddr:        net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.XrayLocalPort)),
	}
	m.state = state
	m.mu.Unlock()
	m.emit(state)

	testCmd := exec.CommandContext(ctx, coreInfo.Path, "run", "-test", "-config", configPath)
	hideWindow(testCmd)
	if output, err := testCmd.CombinedOutput(); err != nil {
		return m.fail(fmt.Sprintf("Xray config validation failed: %s", strings.TrimSpace(string(output))))
	}

	cmd := exec.Command(coreInfo.Path, "run", "-config", configPath)
	hideWindow(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return m.fail(fmt.Sprintf("failed to start Xray-core: %v", err))
	}

	job, err := newProcessJob()
	if err != nil {
		_ = cmd.Process.Kill()
		return m.fail(fmt.Sprintf("failed to create process job: %v", err))
	}
	if err := job.Assign(cmd.Process); err != nil {
		_ = job.Close()
		_ = cmd.Process.Kill()
		return m.fail(fmt.Sprintf("failed to assign process job: %v", err))
	}

	done := make(chan struct{})
	m.mu.Lock()
	m.cmd = cmd
	m.done = done
	m.job = job
	m.state.PID = cmd.Process.Pid
	m.state.StartedAt = model.NowRFC3339()
	m.mu.Unlock()

	go m.monitor(cmd, done, job)

	if err := waitForPort(ctx, state.LocalAddr, 12*time.Second); err != nil {
		_ = cmd.Process.Kill()
		<-done
		_ = job.Close()
		return m.fail(fmt.Sprintf("Xray-core did not become ready: %v", err))
	}

	if settings.TunMode {
		cleanup, routeErr := setupTunRoutes(tunInterfaceName, resolvedIP)
		if routeErr != nil {
			_ = cmd.Process.Kill()
			<-done
			_ = job.Close()
			return m.fail(fmt.Sprintf("TUN 路由配置失败: %v", routeErr))
		}
		m.mu.Lock()
		m.tunCleanup = cleanup
		m.mu.Unlock()
	}

	m.mu.Lock()
	m.state.Status = "running"
	state = m.state
	m.mu.Unlock()
	m.emit(state)
	return state, nil
}
func (m *Manager) Stop() (model.ProxyState, error) {
	m.mu.Lock()
	if m.cmd == nil {
		m.state = model.ProxyState{Status: "stopped"}
		state := m.state
		m.mu.Unlock()
		if cleanup := m.takeTunCleanup(); cleanup != nil {
			_ = cleanup()
		}
		m.emit(state)
		return state, nil
	}
	cmd := m.cmd
	done := m.done
	job := m.job
	m.state.Status = "stopping"
	m.mu.Unlock()
	m.emit(m.State())
	if cleanup := m.takeTunCleanup(); cleanup != nil {
		_ = cleanup()
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	if job != nil {
		_ = job.Close()
	}

	m.mu.Lock()
	m.cmd = nil
	m.done = nil
	m.job = nil
	m.state = model.ProxyState{Status: "stopped"}
	state := m.state
	m.mu.Unlock()
	m.emit(state)
	return state, nil
}
func (m *Manager) Test(settings model.Settings) (model.ProxyTestResult, error) {
	state := m.State()
	if state.Status != "running" || state.LocalAddr == "" {
		return model.ProxyTestResult{}, errors.New("proxy is not running")
	}
	target := strings.TrimSpace(settings.TestURL)
	if target == "" {
		target = "https://www.gstatic.com/generate_204"
	}
	proxyURL, err := url.Parse("http://" + state.LocalAddr)
	if err != nil {
		return model.ProxyTestResult{}, err
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Get(target)
	latency := time.Since(start).Milliseconds()
	if latency == 0 {
		latency = 1
	}
	result := model.ProxyTestResult{
		TargetURL: target,
		LatencyMS: latency,
		CheckedAt: model.NowRFC3339(),
	}
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 400
	if !result.Success {
		result.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
		return result, errors.New(result.Error)
	}
	return result, nil
}

func (m *Manager) monitor(cmd *exec.Cmd, done chan struct{}, job *processJob) {
	err := cmd.Wait()
	close(done)
	_ = job.Close()
	if cleanup := m.takeTunCleanup(); cleanup != nil {
		_ = cleanup()
	}

	m.mu.Lock()
	if m.cmd == cmd {
		m.cmd = nil
		m.done = nil
		m.job = nil
		if m.state.Status != "stopping" {
			if err != nil {
				m.state.Status = "error"
				m.state.Error = err.Error()
			} else {
				m.state.Status = "stopped"
			}
		}
	}
	state := m.state
	m.mu.Unlock()
	m.emit(state)
}

func (m *Manager) takeTunCleanup() func() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cleanup := m.tunCleanup
	m.tunCleanup = nil
	return cleanup
}
func (m *Manager) fail(message string) (model.ProxyState, error) {
	m.mu.Lock()
	m.state = model.ProxyState{Status: "error", Error: message}
	state := m.state
	m.mu.Unlock()
	m.emit(state)
	return state, errors.New(message)
}

func (m *Manager) emit(state model.ProxyState) {
	m.mu.RLock()
	callback := m.onStateChange
	m.mu.RUnlock()
	if callback != nil {
		callback(state)
	}
}

func waitForPort(ctx context.Context, address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("timeout waiting for local proxy port")
}

func resolveNodeHost(ctx context.Context, address string) string {
	if parsed := net.ParseIP(strings.TrimSpace(address)); parsed != nil {
		return parsed.String()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(lookupCtx, address)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0]
}
