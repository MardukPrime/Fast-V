// Package selfcheck implements the FastV diagnostics self-check engine.
//
// A Checker collects indicators from the application state (settings, installed
// core, proxy process, persisted subscriptions, geo assets, network reachability
// and data directory writability) and turns them into a fixed, ordered list of
// model.CheckResult values for the diagnostics page.
//
// Every dependency is injected through a small interface or a function field so
// the engine can be unit tested without a real Xray-core, without network access
// and without a writable data directory. A single failing or even panicking
// dependency can never abort the whole run: each check is executed separately
// and converted into an error result instead.
package selfcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/proxy"
)

// CheckResult status values. They are part of the frontend contract.
const (
	StatusOK    = "ok"
	StatusWarn  = "warn"
	StatusError = "error"
)

// Stable CheckResult IDs in their fixed execution order. The frontend and the
// tests rely on these identifiers.
const (
	CheckCoreInstalled  = "core-installed"
	CheckCoreConfig     = "core-config"
	CheckLocalPort      = "local-port"
	CheckAPIPort        = "api-port"
	CheckTunPrivilege   = "tun-privilege"
	CheckSystemProxy    = "system-proxy"
	CheckNetworkTestURL = "network-test-url"
	CheckGeoData        = "geodata"
	CheckDataWritable   = "data-writable"
	CheckSubscriptions  = "subscriptions"
)

const (
	// coreConfigTestTimeout bounds the "xray run -test" validation of the
	// generated configuration.
	coreConfigTestTimeout = 10 * time.Second
	// networkTestTimeout bounds the direct reachability probe of settings.TestURL.
	networkTestTimeout = 5 * time.Second
	// configOutputLimit caps how much of the core validation output is embedded
	// in the result detail.
	configOutputLimit = 600
	// geoIPFileName and geoSiteFileName are the geo assets Xray loads from
	// XRAY_LOCATION_ASSET.
	geoIPFileName   = "geoip.dat"
	geoSiteFileName = "geosite.dat"
)

// SettingsProvider supplies the current application settings.
type SettingsProvider interface {
	Settings() model.Settings
}

// CoreProvider supplies the installed Xray-core state.
type CoreProvider interface {
	Info() model.CoreInfo
}

// ProxyProvider supplies the local proxy process state.
type ProxyProvider interface {
	State() model.ProxyState
}

// NodeProvider supplies the persisted active node. It is optional; without it
// the configuration check reports a warning instead of validating a config.
type NodeProvider interface {
	ActiveNode() (model.Node, bool)
}

// SubscriptionProvider supplies the persisted subscriptions. Optional.
type SubscriptionProvider interface {
	ListSubscriptions() []model.Subscription
}

// RoutingRuleProvider supplies the persisted routing rules so the validated
// configuration matches the one the proxy manager would generate at runtime.
// Optional.
type RoutingRuleProvider interface {
	ListRoutingRules() []model.RoutingRule
}

// StoreProvider is the aggregated provider set implemented by *store.Store.
// NewChecker accepts it so bootstrap only needs one value for the read models.
type StoreProvider interface {
	SettingsProvider
	NodeProvider
	SubscriptionProvider
	RoutingRuleProvider
	DataDir() string
}

// Checker runs the diagnostics self-checks.
//
// The zero value is valid and safe to run: missing providers are reported as
// warnings or errors instead of panicking. Production code should use
// NewChecker, which fills in the platform default dependencies.
type Checker struct {
	// Store provides the current settings; it is required by most checks.
	Store SettingsProvider
	// Core provides the installed Xray-core state.
	Core CoreProvider
	// Proxy provides the local proxy process state.
	Proxy ProxyProvider
	// Nodes provides the active node used to build the validation config.
	Nodes NodeProvider
	// Subscriptions provides persisted subscriptions for the last check.
	Subscriptions SubscriptionProvider
	// Rules provides persisted routing rules for the configuration check.
	Rules RoutingRuleProvider

	// Elevated reports whether the process runs with administrator rights. When
	// nil the privilege cannot be verified and TUN mode reports a warning.
	Elevated func() bool
	// DataDir is the application data directory: it is checked for writability
	// and receives the temporary configuration used for core validation.
	DataDir string
	// HTTPClient is used by the test-URL check. When nil, a client with a 5s
	// timeout is created for the check.
	HTTPClient *http.Client
	// TestConfig overrides the real Xray configuration validation. It exists for
	// tests; the default implementation writes a temporary configuration and
	// executes "<core> run -test -config <file>".
	TestConfig func(ctx context.Context) error
	// GeoDir overrides the directory containing geoip.dat/geosite.dat. It
	// defaults to the directory of the Xray-core binary.
	GeoDir string
	// Now is the clock used for generated timestamps. Defaults to time.Now.
	Now func() time.Time
	// PortAvailable probes whether a local TCP port can be bound. When nil the
	// default implementation listens on 127.0.0.1:<port> with net.Listen and
	// closes the listener again.
	PortAvailable func(port int) error
}

// NewChecker builds a Checker for the real application wiring. It fills in the
// production defaults (elevation detection, clock) and is the constructor used
// by bootstrap.
func NewChecker(store StoreProvider, core CoreProvider, proxyProvider ProxyProvider) *Checker {
	checker := &Checker{
		Core:     core,
		Proxy:    proxyProvider,
		Elevated: platform.IsElevated,
		Now:      time.Now,
	}
	if store != nil {
		checker.Store = store
		checker.Nodes = store
		checker.Subscriptions = store
		checker.Rules = store
		checker.DataDir = store.DataDir()
	}
	return checker
}

// Run executes every check in the fixed order and always returns exactly one
// result per check. A panicking dependency is converted into an error result, so
// one broken check can never abort the whole run.
func (c *Checker) Run(ctx context.Context) []model.CheckResult {
	if ctx == nil {
		ctx = context.Background()
	}
	definitions := c.checks()
	results := make([]model.CheckResult, 0, len(definitions))
	for _, definition := range definitions {
		results = append(results, runCheck(ctx, definition))
	}
	return results
}

// checkDefinition describes one self-check: its stable identity and the function
// producing the status/detail/hint triple.
type checkDefinition struct {
	id   string
	name string
	run  func(context.Context) (status string, detail string, hint string)
}

// checks returns the complete check list in the fixed execution order.
func (c *Checker) checks() []checkDefinition {
	return []checkDefinition{
		{id: CheckCoreInstalled, name: "Xray-core 安装", run: c.checkCoreInstalled},
		{id: CheckCoreConfig, name: "Xray 配置校验", run: c.checkCoreConfig},
		{id: CheckLocalPort, name: "本地代理端口", run: c.checkLocalPort},
		{id: CheckAPIPort, name: "统计 / API 端口", run: c.checkAPIPort},
		{id: CheckTunPrivilege, name: "TUN 模式权限", run: c.checkTunPrivilege},
		{id: CheckSystemProxy, name: "Windows 系统代理", run: c.checkSystemProxy},
		{id: CheckNetworkTestURL, name: "网络测试地址", run: c.checkNetworkTestURL},
		{id: CheckGeoData, name: "Geo 数据文件", run: c.checkGeoData},
		{id: CheckDataWritable, name: "数据目录写入", run: c.checkDataWritable},
		{id: CheckSubscriptions, name: "订阅状态", run: c.checkSubscriptions},
	}
}

// runCheck executes one check and converts panics into a normal error result.
// The named return value guarantees that a recovered panic still yields the
// correct ID and name for the frontend.
func runCheck(ctx context.Context, definition checkDefinition) (result model.CheckResult) {
	result = model.CheckResult{ID: definition.id, Name: definition.name}
	defer func() {
		if recovered := recover(); recovered != nil {
			result.Status = StatusError
			result.Detail = fmt.Sprintf("检查执行异常: %v", recovered)
			result.Hint = "请重启应用后重试；若问题持续出现请导出日志排查"
		}
	}()

	status, detail, hint := definition.run(ctx)
	result.Status = normalizeStatus(status)
	result.Detail = detail
	result.Hint = hint
	return
}

// normalizeStatus keeps the frontend contract strict: anything unexpected is
// reported as an error instead of leaking an unknown status string.
func normalizeStatus(status string) string {
	switch status {
	case StatusOK, StatusWarn, StatusError:
		return status
	default:
		return StatusError
	}
}

// checkCoreInstalled reports whether Xray-core is installed and usable.
func (c *Checker) checkCoreInstalled(_ context.Context) (string, string, string) {
	info := c.coreInfo()
	if !info.Installed {
		detail := "未检测到 Xray-core"
		if message := strings.TrimSpace(info.Message); message != "" {
			detail += ": " + message
		}
		return StatusError, detail, "前往「设置 → 内核」下载并安装 Xray-core 后重试"
	}
	if info.Status == "error" {
		detail := "Xray-core 已安装，但状态异常"
		if message := strings.TrimSpace(info.Message); message != "" {
			detail += ": " + message
		}
		return StatusError, detail, "重新下载安装 Xray-core，或检查内核文件是否完整"
	}

	version := strings.TrimSpace(info.Version)
	if version == "" {
		version = "未知版本"
	}
	detail := "Xray-core 已安装，版本 " + version
	if path := strings.TrimSpace(info.Path); path != "" {
		detail += "（" + path + "）"
	}
	return StatusOK, detail, ""
}

// checkCoreConfig validates the configuration generated for the active node.
//
// The real implementation mirrors what the proxy manager does at start time:
// build the configuration with proxy.BuildConfig, write it to a temporary file
// and run "<core> run -test". The TestConfig injection point replaces the whole
// external part in tests.
func (c *Checker) checkCoreConfig(ctx context.Context) (string, string, string) {
	node, ok := c.activeNode()
	if !ok {
		return StatusWarn, "未选择激活节点，无法生成校验用的 Xray 配置", "在「服务器」页选择一个可用节点作为激活节点后重试"
	}
	nodeName := describeNode(node)

	if c.TestConfig != nil {
		if err := c.TestConfig(ctx); err != nil {
			return StatusError, "Xray 配置校验失败: " + err.Error(), "检查当前节点参数（地址、端口、协议、UUID/密码）后重试，或切换到其他节点"
		}
		return StatusOK, "Xray 配置校验通过（节点：" + nodeName + "）", ""
	}

	core := c.coreInfo()
	corePath := strings.TrimSpace(core.Path)
	if corePath == "" {
		return StatusError, "未找到 Xray-core 可执行文件，无法校验配置", "前往「设置 → 内核」下载并安装 Xray-core 后重试"
	}
	if info, err := os.Stat(corePath); err != nil || info.IsDir() {
		return StatusError, "Xray-core 可执行文件不可用: " + corePath, "前往「设置 → 内核」重新下载并安装 Xray-core"
	}

	configuration, err := proxy.BuildConfig(node, c.settings(), c.listRoutingRules()...)
	if err != nil {
		return StatusError, "生成 Xray 配置失败: " + err.Error(), "检查当前节点的参数与路由规则后重试，或切换到其他节点"
	}
	configPath, cleanup, err := c.writeTempConfig(configuration)
	if err != nil {
		return StatusError, "写入临时配置失败: " + err.Error(), "检查数据目录权限与磁盘空间"
	}
	defer cleanup()

	commandCtx, cancel := context.WithTimeout(ctx, coreConfigTestTimeout)
	defer cancel()

	command := exec.CommandContext(commandCtx, corePath, "run", "-test", "-config", configPath)
	if directory := c.geoDirectory(); directory != "" {
		command.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+directory)
	}
	platform.ConfigureHiddenProcess(command)

	output, runErr := command.CombinedOutput()
	if runErr == nil {
		return StatusOK, "Xray 配置校验通过（节点：" + nodeName + "）", ""
	}
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return StatusError, "Xray 配置校验超时（超过 10 秒）", "检查系统负载后重试，或切换到其他节点"
	}

	detail := "Xray 配置校验失败"
	if tail := tailOutput(output, configOutputLimit); tail != "" {
		detail += ": " + tail
	} else {
		detail += ": " + runErr.Error()
	}
	return StatusError, detail, "检查当前节点参数（地址、端口、协议、UUID/密码）后重试，或切换到其他节点"
}

// checkLocalPort reports whether the local SOCKS/HTTP inbound port is usable.
func (c *Checker) checkLocalPort(_ context.Context) (string, string, string) {
	settings := c.settings()
	port := settings.XrayLocalPort

	switch c.proxyState().Status {
	case "running":
		return StatusOK, fmt.Sprintf("本地代理正在运行，端口 %d 由 Xray-core 监听", port), ""
	case "starting":
		return StatusOK, "本地代理正在启动，端口即将由 Xray-core 监听", ""
	}

	if !validPort(port) {
		return StatusWarn, fmt.Sprintf("本地端口配置无效: %d", port), "在「设置 → 网络接管」中填写 1-65535 范围内的端口"
	}
	if err := c.probePort(port); err != nil {
		return StatusWarn, fmt.Sprintf("本地端口 %d 不可用: %v", port, err), "结束占用该端口的程序，或在设置中修改本地端口"
	}
	return StatusOK, fmt.Sprintf("本地端口 %d 可用", port), ""
}

// checkAPIPort reports whether the stats/management API port is usable when the
// feature is enabled.
func (c *Checker) checkAPIPort(_ context.Context) (string, string, string) {
	settings := c.settings()
	if !settings.XrayAPIEnabled && !settings.StatsEnabled {
		return StatusOK, "统计 / API 未启用，无需检查 API 端口", ""
	}
	port := settings.XrayAPIPort

	switch c.proxyState().Status {
	case "running":
		return StatusOK, fmt.Sprintf("统计 / API 已启用，端口 %d 由 Xray-core 监听", port), ""
	case "starting":
		return StatusOK, "本地代理正在启动，API 端口即将由 Xray-core 监听", ""
	}

	if !validPort(port) {
		return StatusWarn, fmt.Sprintf("API 端口配置无效: %d", port), "在设置中填写 1-65535 范围内的 API 端口"
	}
	if port == settings.XrayLocalPort {
		return StatusWarn, "API 端口与本地代理端口相同", "在设置中为 API 端口选择一个不同的值，否则代理可能启动失败"
	}
	if err := c.probePort(port); err != nil {
		return StatusWarn, fmt.Sprintf("API 端口 %d 不可用: %v", port, err), "结束占用该端口的程序，或在设置中修改 API 端口"
	}
	return StatusOK, fmt.Sprintf("API 端口 %d 可用", port), ""
}

// checkTunPrivilege reports whether TUN mode can actually run with the current
// process privileges.
func (c *Checker) checkTunPrivilege(_ context.Context) (string, string, string) {
	if !c.settings().TunMode {
		return StatusOK, "TUN 模式未启用", ""
	}
	if c.Elevated == nil {
		return StatusWarn, "TUN 模式已开启，但无法确认当前进程是否具有管理员权限", "如果 TUN 模式无法启动，请以管理员身份重新运行 FastV"
	}
	if c.Elevated() {
		return StatusOK, "已以管理员身份运行，TUN 模式权限正常", ""
	}
	return StatusWarn, "TUN 模式已开启，但当前进程没有管理员权限", "以管理员身份重新启动 FastV（应用会自动请求提权），否则 TUN 模式无法启动"
}

// checkSystemProxy compares the configured system-proxy mode with the actual
// proxy runtime state. The Windows registry is intentionally not read here: the
// system-proxy module owns the registry entry and only the app can know whether
// it enabled the proxy. When the state cannot be confirmed the check degrades to
// a warning instead of a false error.
func (c *Checker) checkSystemProxy(_ context.Context) (string, string, string) {
	settings := c.settings()
	mode := strings.ToLower(strings.TrimSpace(settings.SystemProxyMode))
	if mode == "" {
		mode = "system"
	}

	switch mode {
	case "off", "none":
		return StatusOK, "系统代理模式已关闭，不会修改 Windows 系统代理", ""
	case "system", "pac":
		// Handled below. Both modes make the application own the WinINet
		// proxy configuration: "system" writes a fixed ProxyServer while "pac"
		// writes AutoConfigURL pointing at the generated fastv.pac script.
	default:
		return StatusWarn, fmt.Sprintf("未知的系统代理模式: %q", settings.SystemProxyMode), "在「设置 → 网络接管」中选择系统代理、PAC 或关闭"
	}

	if settings.TunMode {
		return StatusOK, "TUN 模式接管系统流量，无需设置 Windows 系统代理或 PAC", ""
	}

	state := c.proxyState()
	switch state.Status {
	case "running":
		address := strings.TrimSpace(state.LocalAddr)
		if address == "" && validPort(settings.XrayLocalPort) {
			address = net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.XrayLocalPort))
		}
		if address == "" {
			address = "本地代理端口"
		}
		detail := "代理正在运行，Windows 系统代理由系统代理模块指向 " + address + "（此处不直接读取注册表）"
		if mode == "pac" {
			detail = "代理正在运行，PAC 模式由 FastV 生成的 fastv.pac 接管，脚本将代理流量指向 " + address + "（此处不直接读取注册表）"
		}
		return StatusOK, detail, ""
	case "starting":
		return StatusOK, "代理正在启动，系统代理会在代理就绪后自动设置", ""
	default:
		detail := "系统代理模式已开启，但本地代理未运行，系统代理不会生效"
		if message := strings.TrimSpace(state.Error); message != "" {
			detail += "（上次错误：" + message + "）"
		}
		return StatusWarn, detail, "启动本地代理后系统代理会自动设置；如果不需要系统代理，请在设置中关闭"
	}
}

// checkNetworkTestURL probes the configured test URL directly (without the local
// proxy) so a broken upstream network is distinguishable from a broken proxy.
func (c *Checker) checkNetworkTestURL(ctx context.Context) (string, string, string) {
	target := strings.TrimSpace(c.settings().TestURL)
	if target == "" {
		return StatusWarn, "未配置网络测试地址", "在「设置 → 网络」中填写一个可访问的测试 URL（例如 https://www.gstatic.com/generate_204）"
	}

	requestCtx, cancel := context.WithTimeout(ctx, networkTestTimeout)
	defer cancel()

	statusCode, err := c.probeURL(requestCtx, target)
	if err != nil {
		return StatusWarn, fmt.Sprintf("测试地址 %s 不可达: %v", target, err), "检查本机网络连接与系统代理设置，或更换测试地址后重试"
	}
	if statusCode >= http.StatusOK && statusCode < http.StatusBadRequest {
		return StatusOK, fmt.Sprintf("测试地址可达（HTTP %d）: %s", statusCode, target), ""
	}
	return StatusWarn, fmt.Sprintf("测试地址 %s 返回异常状态码 HTTP %d", target, statusCode), "更换为返回 204/2xx/3xx 的测试地址（例如 https://www.gstatic.com/generate_204）"
}

// checkGeoData verifies that the geo assets required by routing rules are
// installed next to the core binary.
func (c *Checker) checkGeoData(_ context.Context) (string, string, string) {
	directory := c.geoDirectory()
	if directory == "" {
		return StatusWarn, "无法定位 Geo 数据目录", "前往「设置 → 内核」更新 Geo 数据后重试"
	}

	missing := make([]string, 0, 2)
	present := make([]string, 0, 2)
	for _, name := range []string{geoIPFileName, geoSiteFileName} {
		info, err := os.Stat(filepath.Join(directory, name))
		if err != nil || info.IsDir() || info.Size() == 0 {
			missing = append(missing, name)
			continue
		}
		present = append(present, fmt.Sprintf("%s（%.1f MB）", name, float64(info.Size())/(1<<20)))
	}
	if len(missing) > 0 {
		return StatusWarn, "缺少 Geo 数据文件: " + strings.Join(missing, "、") + "（目录 " + directory + "）", "前往「设置 → 内核」点击更新 Geo 数据，或重新下载 Xray-core"
	}
	return StatusOK, "Geo 数据完整: " + strings.Join(present, "，"), ""
}

// checkDataWritable verifies that the data directory can be written to, which is
// required for the store, the runtime configuration and logs.
func (c *Checker) checkDataWritable(_ context.Context) (string, string, string) {
	directory := strings.TrimSpace(c.DataDir)
	if directory == "" {
		return StatusWarn, "未配置数据目录，无法检查写入权限", "检查应用数据目录配置，或重新安装 FastV"
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return StatusError, "数据目录不可用: " + err.Error(), "检查数据目录权限或磁盘空间"
	}

	file, err := os.CreateTemp(directory, ".selfcheck-*.tmp")
	if err != nil {
		return StatusError, "数据目录不可写: " + err.Error(), "检查数据目录权限或磁盘空间"
	}
	path := file.Name()
	_, writeErr := file.WriteString("fastv selfcheck " + c.now().Format(time.RFC3339Nano))
	closeErr := file.Close()
	removeErr := os.Remove(path)

	switch {
	case writeErr != nil:
		return StatusError, "数据目录写入失败: " + writeErr.Error(), "检查数据目录权限或磁盘空间"
	case closeErr != nil:
		return StatusError, "数据目录写入失败: " + closeErr.Error(), "检查数据目录权限或磁盘空间"
	case removeErr != nil:
		return StatusWarn, "数据目录可写，但临时文件清理失败: " + removeErr.Error(), "检查数据目录中残留的 .selfcheck-*.tmp 文件"
	}
	return StatusOK, "数据目录可写: " + directory, ""
}

// checkSubscriptions reports enabled subscriptions whose last refresh failed.
func (c *Checker) checkSubscriptions(_ context.Context) (string, string, string) {
	subscriptions := c.listSubscriptions()
	enabled := 0
	failed := make([]model.Subscription, 0)
	for _, subscription := range subscriptions {
		if !subscription.Enabled {
			continue
		}
		enabled++
		if strings.TrimSpace(subscription.LastError) != "" {
			failed = append(failed, subscription)
		}
	}

	if len(failed) == 0 {
		if enabled == 0 {
			return StatusOK, "没有启用的订阅", ""
		}
		return StatusOK, fmt.Sprintf("%d 个启用的订阅状态正常", enabled), ""
	}

	name := strings.TrimSpace(failed[0].Name)
	if name == "" {
		name = failed[0].ID
	}
	return StatusWarn, fmt.Sprintf("%d 个启用的订阅上次更新失败，首个: %s", len(failed), name), "打开「订阅」页重新更新订阅，或检查订阅地址是否仍然有效"
}

// settings returns the current settings or the model default when no provider is
// wired. A missing provider is reported by the individual checks, never by a
// panic.
func (c *Checker) settings() model.Settings {
	if c.Store == nil {
		return model.Settings{}
	}
	return c.Store.Settings()
}

// coreInfo returns the current core info or the zero value.
func (c *Checker) coreInfo() model.CoreInfo {
	if c.Core == nil {
		return model.CoreInfo{}
	}
	return c.Core.Info()
}

// proxyState returns the current local proxy state or the zero value.
func (c *Checker) proxyState() model.ProxyState {
	if c.Proxy == nil {
		return model.ProxyState{}
	}
	return c.Proxy.State()
}

// activeNode returns the persisted active node when a node provider is wired.
func (c *Checker) activeNode() (model.Node, bool) {
	if c.Nodes == nil {
		return model.Node{}, false
	}
	return c.Nodes.ActiveNode()
}

// listSubscriptions returns the persisted subscriptions or nil.
func (c *Checker) listSubscriptions() []model.Subscription {
	if c.Subscriptions == nil {
		return nil
	}
	return c.Subscriptions.ListSubscriptions()
}

// listRoutingRules returns the persisted routing rules or nil.
func (c *Checker) listRoutingRules() []model.RoutingRule {
	if c.Rules == nil {
		return nil
	}
	return c.Rules.ListRoutingRules()
}

// now returns the injected clock or the wall clock.
func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// probePort checks whether a local TCP port can be bound.
func (c *Checker) probePort(port int) error {
	if c.PortAvailable != nil {
		return c.PortAvailable(port)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return listener.Close()
}

// writeTempConfig writes the generated configuration into a temporary file next
// to the application data (falling back to the OS temp directory) and returns
// the path plus a cleanup function.
func (c *Checker) writeTempConfig(payload []byte) (string, func(), error) {
	directory := strings.TrimSpace(c.DataDir)
	if directory == "" {
		directory = os.TempDir()
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", func() {}, err
	}

	file, err := os.CreateTemp(directory, ".selfcheck-xray-*.json")
	if err != nil {
		return "", func() {}, err
	}
	path := file.Name()
	cleanup := func() {
		_ = os.Remove(path)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

// geoDirectory resolves the directory that holds geoip.dat/geosite.dat: the
// explicit override, the directory of the core binary, or the default layout
// under the application data directory.
func (c *Checker) geoDirectory() string {
	if directory := strings.TrimSpace(c.GeoDir); directory != "" {
		return directory
	}
	if path := strings.TrimSpace(c.coreInfo().Path); path != "" {
		return filepath.Dir(path)
	}
	if directory := strings.TrimSpace(c.DataDir); directory != "" {
		return filepath.Join(directory, "cores", "xray")
	}
	return ""
}

// httpClient returns the injected HTTP client or a client bounded by the
// network probe timeout.
func (c *Checker) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: networkTestTimeout}
}

// probeURL requests the test URL, preferring HEAD and falling back to GET for
// servers that do not implement HEAD (or reject it). It returns the final status
// code when at least one request completed.
func (c *Checker) probeURL(ctx context.Context, target string) (int, error) {
	client := c.httpClient()

	headStatus, headErr := requestStatus(ctx, client, http.MethodHead, target)
	if headErr == nil && headStatus < http.StatusBadRequest {
		return headStatus, nil
	}
	getStatus, getErr := requestStatus(ctx, client, http.MethodGet, target)
	if getErr == nil {
		return getStatus, nil
	}
	if headErr != nil {
		return 0, headErr
	}
	return 0, getErr
}

// requestStatus performs one HTTP request and returns the response status code.
// The response body is closed without being read: the check only cares about
// reachability.
func requestStatus(ctx context.Context, client *http.Client, method string, target string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", "FastV/selfcheck")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	_ = response.Body.Close()
	return response.StatusCode, nil
}

// validPort reports whether a TCP port number is usable.
func validPort(port int) bool {
	return port > 0 && port <= 65535
}

// describeNode returns a human readable node label for result details.
func describeNode(node model.Node) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(node.ID); id != "" {
		return id
	}
	return "未命名节点"
}

// tailOutput formats the tail of a command output for a single-line result
// detail. Only the last limit runes are kept so huge core dumps stay readable.
func tailOutput(output []byte, limit int) string {
	text := strings.TrimSpace(strings.ToValidUTF8(string(output), "?"))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if limit > 0 && len(runes) > limit {
		text = "..." + string(runes[len(runes)-limit:])
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	return strings.TrimSpace(strings.Join(lines, " | "))
}
