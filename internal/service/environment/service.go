// Package environment reports and removes the operating-system leftovers of a
// previous FastV installation: the fastv:// URL protocol registration, the
// Windows autostart entry and the generated PAC script.
//
// The service never touches user data (subscriptions, nodes, settings); a
// cleanup only removes system integration state and tells the user where the
// data directory remains. Every platform and filesystem effect goes through an
// injected interface so the detection and cleanup logic is fully testable
// without touching the real registry or the real data directory.
package environment

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MardukPrime/Fast-V/internal/platform"
	"github.com/MardukPrime/Fast-V/internal/store"
)

const (
	// serviceName identifies the service in the Wails bindings.
	serviceName = "EnvironmentService"

	// protocolScheme is the deep-link scheme registered by FastV.
	protocolScheme = "fastv"

	// pacFileName is the generated PAC script name. It mirrors the unexported
	// constant in the proxy package: the script always lives under
	// <data dir>/pac.
	pacFileName = "fastv.pac"

	// Cleanup item names are stable Chinese labels reported to the UI.
	cleanupProtocol  = "移除 fastv 协议注册"
	cleanupAutostart = "关闭开机自启"
	cleanupPACFile   = "删除 PAC 文件"

	// protocolRegistryPath and autostartRegistryPath are the current-user
	// registry locations the residual explanations refer to.
	protocolRegistryPath  = `HKCU\Software\Classes\fastv`
	autostartRegistryPath = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
)

// platformOps is the slice of internal/platform the environment service needs.
// Keeping it small lets tests inject a fake and keeps the service independent of
// the real registry.
type platformOps interface {
	URLSchemeRegistered(scheme string) (bool, error)
	RemoveURLScheme(scheme string) error
	AutostartEnabled() (bool, error)
	SetAutostart(enabled bool) error
}

// fsOps is the slice of the filesystem the environment service needs. Stat
// returns nil when path exists and an error (typically fs.ErrNotExist) when it
// does not; the small surface lets tests run without touching the disk.
type fsOps interface {
	Stat(path string) error
	Remove(path string) error
}

// EnvironmentReport is the outcome of one Inspect run. Residuals lists every
// detected leftover and every check that could not be performed, so the UI can
// explain both what was found and why a check was skipped.
type EnvironmentReport struct {
	ProtocolRegistered bool     `json:"protocolRegistered"`
	AutostartEnabled   bool     `json:"autostartEnabled"`
	PacFilePresent     bool     `json:"pacFilePresent"`
	DataDir            string   `json:"dataDir"`
	Residuals          []string `json:"residuals"`
	CheckedAt          string   `json:"checkedAt"`
}

// CleanupResult is the outcome of one Cleanup run. Removed and Failed contain
// the stable Chinese operation labels ("移除 fastv 协议注册", "关闭开机自启",
// "删除 PAC 文件"). Report describes the state after the cleanup.
type CleanupResult struct {
	Removed []string          `json:"removed"`
	Failed  []string          `json:"failed"`
	Message string            `json:"message"`
	Report  EnvironmentReport `json:"report"`
}

// Service is the Wails adapter for environment cleanup.
type Service struct {
	store *store.Store
	ops   platformOps
	fs    fsOps
	now   func() time.Time
}

// New creates the environment service backed by the real platform primitives
// and the real filesystem. The optional ops argument exists so tests can inject
// a fake platform while keeping the real store; production code calls New(st).
func New(st *store.Store, ops ...platformOps) *Service {
	service := &Service{store: st, now: time.Now}
	if len(ops) > 0 {
		service.ops = ops[0]
	}
	return service
}

// NewWithOps is New with an injected platform and filesystem implementation. It
// exists so tests can replace every OS effect; production code uses New.
func NewWithOps(st *store.Store, ops platformOps, filesystem fsOps) *Service {
	return &Service{store: st, ops: ops, fs: filesystem, now: time.Now}
}

// ServiceName identifies the service in the Wails bindings.
func (s *Service) ServiceName() string {
	return serviceName
}

// Inspect reports the leftover system integration state. It is best-effort: an
// unavailable platform capability (for example the registry checks on a
// non-Windows system) becomes a Residuals explanation instead of failing the
// whole inspection.
func (s *Service) Inspect() (EnvironmentReport, error) {
	report := EnvironmentReport{
		DataDir:   s.dataDir(),
		Residuals: []string{},
		CheckedAt: s.clock().Format(time.RFC3339),
	}
	ops := s.platform()

	if registered, err := ops.URLSchemeRegistered(protocolScheme); err != nil {
		report.Residuals = append(report.Residuals,
			fmt.Sprintf("无法检测 fastv 协议注册（%v），可能仍存在残留：%s", err, protocolRegistryPath))
	} else {
		report.ProtocolRegistered = registered
		if registered {
			report.Residuals = append(report.Residuals,
				fmt.Sprintf("fastv 协议仍注册在 %s", protocolRegistryPath))
		}
	}

	if enabled, err := ops.AutostartEnabled(); err != nil {
		report.Residuals = append(report.Residuals,
			fmt.Sprintf("无法检测开机自启（%v），可能仍存在残留：%s", err, autostartRegistryPath))
	} else {
		report.AutostartEnabled = enabled
		if enabled {
			report.Residuals = append(report.Residuals,
				fmt.Sprintf("开机自启项仍存在：%s", autostartRegistryPath))
		}
	}

	if pacPath := s.pacFilePath(); pacPath == "" {
		report.Residuals = append(report.Residuals, "无法确定数据目录，未检测 PAC 文件残留")
	} else {
		present, err := s.pacFilePresent()
		report.PacFilePresent = present
		if err != nil {
			report.Residuals = append(report.Residuals,
				fmt.Sprintf("无法检测 PAC 文件（%v），可能仍存在：%s", err, pacPath))
		} else if present {
			report.Residuals = append(report.Residuals, fmt.Sprintf("PAC 文件仍存在：%s", pacPath))
		}
	}
	return report, nil
}

// Cleanup removes the detected system integration leftovers. It is best-effort:
// a failing item is recorded in Failed and the remaining items are still
// processed. User data is never touched; Message always tells the user where the
// data directory remains. The returned Report describes the state after the
// cleanup.
func (s *Service) Cleanup() (CleanupResult, error) {
	removed := []string{}
	failed := []string{}

	if handled, err := s.cleanupProtocol(); err != nil {
		failed = append(failed, cleanupProtocol)
	} else if handled {
		removed = append(removed, cleanupProtocol)
	}
	if handled, err := s.cleanupAutostart(); err != nil {
		failed = append(failed, cleanupAutostart)
	} else if handled {
		removed = append(removed, cleanupAutostart)
	}
	if handled, err := s.cleanupPACFile(); err != nil {
		failed = append(failed, cleanupPACFile)
	} else if handled {
		removed = append(removed, cleanupPACFile)
	}

	report, _ := s.Inspect()
	return CleanupResult{
		Removed: removed,
		Failed:  failed,
		Message: cleanupMessage(removed, failed, report.DataDir),
		Report:  report,
	}, nil
}

// cleanupProtocol removes the URL protocol registration. It reports whether a
// removal was performed and returns the error of the failed operation.
func (s *Service) cleanupProtocol() (bool, error) {
	ops := s.platform()
	registered, err := ops.URLSchemeRegistered(protocolScheme)
	if err == nil && !registered {
		return false, nil
	}
	// When the inspection failed, the state is unknown; still attempt the
	// removal so a leftover is not silently kept, and report any error.
	if removeErr := ops.RemoveURLScheme(protocolScheme); removeErr != nil {
		return false, removeErr
	}
	return true, nil
}

// cleanupAutostart disables the autostart entry when it is present. It reports
// whether a change was performed and returns the error of the failed operation.
func (s *Service) cleanupAutostart() (bool, error) {
	ops := s.platform()
	enabled, err := ops.AutostartEnabled()
	if err == nil && !enabled {
		return false, nil
	}
	if setErr := ops.SetAutostart(false); setErr != nil {
		return false, setErr
	}
	return true, nil
}

// cleanupPACFile removes the generated PAC script when it exists. It reports
// whether a file was deleted and returns the error of the failed operation.
func (s *Service) cleanupPACFile() (bool, error) {
	pacPath := s.pacFilePath()
	if pacPath == "" {
		return false, nil
	}
	filesystem := s.filesystem()
	present, statErr := s.pacFilePresent()
	if statErr != nil {
		// The state is unknown; try to remove the file anyway and treat an
		// already missing file as nothing left to clean.
		removeErr := filesystem.Remove(pacPath)
		switch {
		case removeErr == nil:
			return true, nil
		case errors.Is(removeErr, fs.ErrNotExist):
			return false, nil
		default:
			return false, fmt.Errorf("删除 PAC 文件失败: %w", removeErr)
		}
	}
	if !present {
		return false, nil
	}
	if err := filesystem.Remove(pacPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("删除 PAC 文件失败: %w", err)
	}
	return true, nil
}

// pacFilePresent reports whether the generated PAC script exists. A missing file
// is (false, nil); other filesystem errors are returned so Inspect can explain
// that the check was skipped.
func (s *Service) pacFilePresent() (bool, error) {
	pacPath := s.pacFilePath()
	if pacPath == "" {
		return false, nil
	}
	err := s.filesystem().Stat(pacPath)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("检测 PAC 文件失败: %w", err)
}

// pacFilePath returns <data dir>/pac/fastv.pac, or an empty string when the data
// directory is unknown.
func (s *Service) pacFilePath() string {
	dataDir := s.dataDir()
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "pac", pacFileName)
}

func (s *Service) dataDir() string {
	if s.store == nil {
		return ""
	}
	return s.store.DataDir()
}

func (s *Service) platform() platformOps {
	if s.ops != nil {
		return s.ops
	}
	return defaultPlatform{}
}

func (s *Service) filesystem() fsOps {
	if s.fs != nil {
		return s.fs
	}
	return osFS{}
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// defaultPlatform calls the real platform package. The functions exist on every
// platform: outside Windows they return a "仅支持 Windows" error, which Inspect
// records as a Residuals explanation.
type defaultPlatform struct{}

func (defaultPlatform) URLSchemeRegistered(scheme string) (bool, error) {
	return platform.URLSchemeRegistered(scheme)
}

func (defaultPlatform) RemoveURLScheme(scheme string) error {
	return platform.RemoveURLScheme(scheme)
}

func (defaultPlatform) AutostartEnabled() (bool, error) {
	return platform.AutostartEnabled()
}

func (defaultPlatform) SetAutostart(enabled bool) error {
	return platform.SetAutostart(enabled)
}

// osFS implements fsOps with the standard library.
type osFS struct{}

func (osFS) Stat(path string) error {
	_, err := os.Stat(path)
	return err
}

func (osFS) Remove(path string) error {
	return os.Remove(path)
}

// cleanupMessage builds the user-facing summary. It always states that user data
// stays in the data directory, because a cleanup is not an uninstall.
func cleanupMessage(removed, failed []string, dataDir string) string {
	var summary string
	switch {
	case len(removed) == 0 && len(failed) == 0:
		summary = "未发现可清理的环境残留。"
	case len(failed) == 0:
		summary = fmt.Sprintf("环境清理完成，已处理 %d 项。", len(removed))
	case len(removed) == 0:
		summary = fmt.Sprintf("环境清理未完成，%d 项失败。", len(failed))
	default:
		summary = fmt.Sprintf("环境清理完成，已处理 %d 项，%d 项失败。", len(removed), len(failed))
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return summary + "用户数据未做修改。"
	}
	return summary + fmt.Sprintf("用户数据仍保留在 %s，如需彻底卸载请手动删除该目录。", dataDir)
}
