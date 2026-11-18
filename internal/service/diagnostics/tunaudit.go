package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
)

// TUN audit check statuses. They are deliberately more explicit than the
// self-check statuses: "skip" means the check does not apply (for example on a
// non-Windows host or when the required provider was not wired in), while
// "warning" means the TUN mode is simply not in use right now.
const (
	tunStatusOK      = "ok"
	tunStatusWarning = "warning"
	tunStatusError   = "error"
	tunStatusSkip    = "skip"
)

// Stable check identifiers consumed by the frontend.
const (
	tunCheckPlatform  = "platform"
	tunCheckSetting   = "tun-setting"
	tunCheckElevation = "elevation"
	tunCheckCore      = "core"
	tunCheckWintun    = "wintun"
	tunCheckProxy     = "proxy-state"
)

// tunWintunDLLName is the user-space WireGuard driver that Windows TUN routing
// requires next to the core binary; internal/proxy checks the same file in
// preflightTun.
const tunWintunDLLName = "wintun.dll"

// tunFallbackHint is shown when TUN mode is enabled but a fatal prerequisite is
// missing: switching to the system proxy always keeps the user online.
const tunFallbackHint = "TUN 模式所需的运行条件未满足。建议关闭 TUN 模式，改用系统代理或 PAC 模式，然后重启代理即可继续使用。"

// tunProxyRunningStatus is the proxy state reported by ProxyService while the
// core process is running; Check() compares against the same literal.
const tunProxyRunningStatus = "running"

// Indirections keep TunAudit unit-testable: tests override them so no real
// registry/token query and no real file system access happens.
var (
	// tunGOOSFunc reports the host platform. It is a variable so the
	// non-Windows branch can be exercised on every CI host.
	tunGOOSFunc = func() string { return runtime.GOOS }
	// tunIsElevatedFunc reports whether the process has administrator rights,
	// which TUN needs to create the virtual adapter and install routes.
	tunIsElevatedFunc = platform.IsElevated
	// tunStatFunc checks for the presence of wintun.dll.
	tunStatFunc = os.Stat
)

// TunCheckItem is one step of the TUN self-check. Remedy is a user-facing
// instruction shown when the step failed or was skipped for a fixable reason.
type TunCheckItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy"`
}

// TunAuditResult is the outcome of the TUN self-check. CanFallback tells the
// frontend whether it should offer the one-click system-proxy fallback, and
// FallbackHint carries the explanation to show next to that action.
type TunAuditResult struct {
	Checks       []TunCheckItem `json:"checks"`
	CanFallback  bool           `json:"canFallback"`
	FallbackHint string         `json:"fallbackHint"`
	CheckedAt    string         `json:"checkedAt"`
}

// TunAudit checks every local prerequisite of Windows TUN mode: the platform,
// the persisted TUN switch, administrator rights, the installed core, the
// wintun.dll beside it and the running proxy. Checks that are not applicable
// are reported as "skip" instead of failing the whole audit, so the result is
// always a complete checklist for the wizard UI.
//
// The method itself never returns an error today: every failure is represented
// as a check item, which keeps a broken settings store or an unwired core
// provider from hiding the remaining checks. The error result stays in the
// signature for symmetry with the other audit entry points and future use.
func (s *DiagnosticsService) TunAudit() (TunAuditResult, error) {
	result := TunAuditResult{
		Checks:    make([]TunCheckItem, 0, 6),
		CheckedAt: time.Now().Format(time.RFC3339),
	}

	settings, hasSettings := s.tunAuditSettings()
	tunEnabled := hasSettings && settings.TunMode
	isWindows := strings.EqualFold(tunGOOSFunc(), "windows")

	// a) Platform: TUN is only implemented for Windows.
	if isWindows {
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckPlatform,
			Status: tunStatusOK,
			Title:  "运行平台",
			Detail: "Windows 平台支持 TUN 模式。",
		})
	} else {
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckPlatform,
			Status: tunStatusSkip,
			Title:  "运行平台",
			Detail: "当前平台不支持 TUN",
			Remedy: "TUN 模式仅支持 Windows。",
		})
	}

	// b) Persisted TUN switch.
	switch {
	case !hasSettings:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckSetting,
			Status: tunStatusSkip,
			Title:  "TUN 模式",
			Detail: "无法读取应用设置，跳过 TUN 设置检查。",
		})
	case settings.TunMode:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckSetting,
			Status: tunStatusOK,
			Title:  "TUN 模式",
			Detail: "TUN 模式已开启。",
		})
	default:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckSetting,
			Status: tunStatusWarning,
			Title:  "TUN 模式",
			Detail: "TUN 模式未开启，当前使用系统代理或 PAC 模式。",
			Remedy: "在设置中开启 TUN 模式。",
		})
	}

	// c) Administrator rights: creating the TUN adapter and its routes needs
	// an elevated process on Windows.
	if !isWindows {
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckElevation,
			Status: tunStatusSkip,
			Title:  "管理员权限",
			Detail: "当前平台不支持 TUN，跳过管理员权限检查。",
		})
	} else if tunIsElevatedFunc() {
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckElevation,
			Status: tunStatusOK,
			Title:  "管理员权限",
			Detail: "已以管理员身份运行。",
		})
	} else {
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckElevation,
			Status: tunStatusError,
			Title:  "管理员权限",
			Detail: "当前进程没有管理员权限，无法创建 TUN 虚拟网卡。",
			Remedy: "以管理员身份启动 FastV",
		})
	}

	// d) Core installation.
	coreInfo, hasCoreInfo := s.tunAuditCoreInfo()
	coreReady := hasCoreInfo && coreInfo.Installed && strings.TrimSpace(coreInfo.Path) != ""
	switch {
	case !isWindows:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckCore,
			Status: tunStatusSkip,
			Title:  "Xray 内核",
			Detail: "当前平台不支持 TUN，跳过内核检查。",
		})
	case !hasCoreInfo:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckCore,
			Status: tunStatusSkip,
			Title:  "Xray 内核",
			Detail: "无法读取内核信息，跳过内核检查。",
		})
	case !coreReady:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckCore,
			Status: tunStatusError,
			Title:  "Xray 内核",
			Detail: "未检测到已安装的 Xray 内核。",
			Remedy: "先在设置中下载 Xray 内核",
		})
	default:
		detail := "Xray 内核已安装。"
		if version := strings.TrimSpace(coreInfo.Version); version != "" {
			detail = fmt.Sprintf("Xray 内核已安装（版本 %s）。", version)
		}
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckCore,
			Status: tunStatusOK,
			Title:  "Xray 内核",
			Detail: detail,
		})
	}

	// e) wintun.dll next to the core binary, mirroring preflightTun.
	switch {
	case !isWindows:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckWintun,
			Status: tunStatusSkip,
			Title:  "wintun.dll",
			Detail: "当前平台不支持 TUN，跳过 wintun.dll 检查。",
		})
	case !hasCoreInfo:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckWintun,
			Status: tunStatusSkip,
			Title:  "wintun.dll",
			Detail: "无法读取内核信息，跳过 wintun.dll 检查。",
		})
	case !coreReady:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckWintun,
			Status: tunStatusSkip,
			Title:  "wintun.dll",
			Detail: "Xray 内核未安装，无法检查 wintun.dll。",
		})
	default:
		dllPath := filepath.Join(filepath.Dir(coreInfo.Path), tunWintunDLLName)
		if tunWintunPresent(dllPath) {
			result.Checks = append(result.Checks, TunCheckItem{
				ID:     tunCheckWintun,
				Status: tunStatusOK,
				Title:  "wintun.dll",
				Detail: "wintun.dll 已就绪。",
			})
		} else {
			result.Checks = append(result.Checks, TunCheckItem{
				ID:     tunCheckWintun,
				Status: tunStatusError,
				Title:  "wintun.dll",
				Detail: fmt.Sprintf("未在内核目录中找到 %s（%s）。", tunWintunDLLName, filepath.Dir(coreInfo.Path)),
				Remedy: "重新安装/更新 Xray-core 以获取 wintun.dll",
			})
		}
	}

	// f) Running proxy: with TUN enabled and the proxy up, the mode is active.
	switch {
	case !isWindows:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckProxy,
			Status: tunStatusSkip,
			Title:  "代理状态",
			Detail: "当前平台不支持 TUN，跳过代理状态检查。",
		})
	case !hasSettings:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckProxy,
			Status: tunStatusSkip,
			Title:  "代理状态",
			Detail: "无法读取应用设置，跳过代理状态检查。",
		})
	case !settings.TunMode:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckProxy,
			Status: tunStatusSkip,
			Title:  "代理状态",
			Detail: "TUN 模式未开启，跳过代理状态检查。",
		})
	case s.tunAuditProxyRunning():
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckProxy,
			Status: tunStatusOK,
			Title:  "代理状态",
			Detail: "代理正在运行，TUN 模式已生效。",
		})
	default:
		result.Checks = append(result.Checks, TunCheckItem{
			ID:     tunCheckProxy,
			Status: tunStatusWarning,
			Title:  "代理状态",
			Detail: "代理未运行，TUN 模式尚未生效。",
			Remedy: "启动代理以启用 TUN 模式。",
		})
	}

	// g) One-click fallback is only meaningful while TUN is actually enabled
	// and a fatal prerequisite blocks it.
	if tunEnabled {
		for _, check := range result.Checks {
			if check.Status == tunStatusError {
				result.CanFallback = true
				break
			}
		}
	}
	if result.CanFallback {
		result.FallbackHint = tunFallbackHint
	}
	return result, nil
}

// tunWintunPresent reports whether path exists and is a regular file. A
// directory named wintun.dll cannot be loaded as a driver, so it is treated as
// missing.
func tunWintunPresent(path string) bool {
	info, err := tunStatFunc(path)
	if err != nil || info == nil {
		return false
	}
	return !info.IsDir()
}

// tunAuditSettings returns the current application settings when the report
// store was wired into the service. The second result is false when no store is
// available, in which case settings-dependent checks are skipped.
func (s *DiagnosticsService) tunAuditSettings() (model.Settings, bool) {
	if s == nil {
		return model.Settings{}, false
	}
	provider := s.report.Store
	if provider == nil {
		return model.Settings{}, false
	}
	return provider.Settings(), true
}

// tunAuditCoreInfo asks the injected core provider for the installed core
// description. The second result is false when no provider was wired in.
func (s *DiagnosticsService) tunAuditCoreInfo() (model.CoreInfo, bool) {
	if s == nil || s.report.CoreInfo == nil {
		return model.CoreInfo{}, false
	}
	return s.report.CoreInfo(), true
}

// tunAuditProxyRunning reports whether the local proxy is currently running.
func (s *DiagnosticsService) tunAuditProxyRunning() bool {
	if s == nil || s.proxy == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(s.proxy.State().Status), tunProxyRunningStatus)
}
