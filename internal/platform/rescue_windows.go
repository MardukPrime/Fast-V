//go:build windows

package platform

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// SystemProxyState is the current user's WinINet proxy configuration as stored
// in HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings.
type SystemProxyState struct {
	Enabled  bool
	Server   string
	Override string
	// AutoConfigURL is the PAC script URL (file:// or http://) configured for
	// the current user, or an empty string when no PAC is set.
	AutoConfigURL string
}

// ReadSystemProxyState reads the WinINet proxy configuration of the current
// user. Missing registry values are not an error: a missing ProxyEnable means
// "disabled" and missing ProxyServer/ProxyOverride mean "empty".
func ReadSystemProxyState() (SystemProxyState, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return SystemProxyState{}, fmt.Errorf("open internet settings: %w", err)
	}
	defer key.Close()

	state := SystemProxyState{}
	if value, _, readErr := key.GetIntegerValue("ProxyEnable"); readErr == nil {
		state.Enabled = value != 0
	} else if raw, _, stringErr := key.GetStringValue("ProxyEnable"); stringErr == nil {
		// Some third-party tools write the DWORD as a string; tolerate it so a
		// stale proxy is still detected.
		if parsed, convErr := strconv.ParseInt(strings.TrimSpace(raw), 0, 64); convErr == nil {
			state.Enabled = parsed != 0
		}
	}
	if value, _, readErr := key.GetStringValue("ProxyServer"); readErr == nil {
		state.Server = strings.TrimSpace(value)
	}
	if value, _, readErr := key.GetStringValue("ProxyOverride"); readErr == nil {
		state.Override = strings.TrimSpace(value)
	}
	if value, _, readErr := key.GetStringValue("AutoConfigURL"); readErr == nil {
		state.AutoConfigURL = strings.TrimSpace(value)
	}
	return state, nil
}

// winHTTPProxyPattern extracts a host:port pair from `netsh winhttp show proxy`
// output. The command is localized and may emit OEM-encoded text, so detection
// relies on the address shape rather than on English labels.
var winHTTPProxyPattern = regexp.MustCompile(`(?:[A-Za-z0-9._-]+|\d{1,3}(?:\.\d{1,3}){3}):\d{1,5}`)

// ReadWinHTTPProxy returns the machine-wide WinHTTP proxy (host:port) when one
// is configured, or an empty string when WinHTTP is in direct-access mode. The
// query is read-only; callers should treat an error as "unavailable, skip".
func ReadWinHTTPProxy() (string, error) {
	cmd := exec.Command("netsh", "winhttp", "show", "proxy")
	ConfigureHiddenProcess(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("query winhttp proxy: %v: %s", err, commandOutputText(output))
	}
	match := winHTTPProxyPattern.FindString(string(output))
	return match, nil
}

// ResetSystemProxy disables the WinINet system proxy for the current user,
// clears its server/override values, notifies WinINet applications, and then
// resets the machine-wide WinHTTP proxy on a best-effort basis (that part can
// require elevation, so its failure must not fail the whole rescue).
func ResetSystemProxy() error {
	if err := regAdd("ProxyEnable", "REG_DWORD", "0"); err != nil {
		return fmt.Errorf("disable system proxy: %w", err)
	}
	if err := regAdd("ProxyServer", "REG_SZ", ""); err != nil {
		return fmt.Errorf("clear system proxy server: %w", err)
	}
	if err := regAdd("ProxyOverride", "REG_SZ", "<local>"); err != nil {
		return fmt.Errorf("reset system proxy override: %w", err)
	}
	refreshInternetSettings()

	resetCmd := exec.Command("netsh", "winhttp", "reset", "proxy")
	ConfigureHiddenProcess(resetCmd)
	_ = resetCmd.Run()
	return nil
}

// FlushDNS flushes the Windows DNS resolver cache.
func FlushDNS() error {
	cmd := exec.Command("ipconfig", "/flushdns")
	ConfigureHiddenProcess(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("flush dns cache: %v: %s", err, commandOutputText(output))
	}
	return nil
}

// PortInUse reports whether 127.0.0.1:port is already bound by another process.
// An invalid port is an error rather than a "free" result.
func PortInUse(port int) (bool, error) {
	if port < 1 || port > 65535 {
		return false, fmt.Errorf("invalid port: %d", port)
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err == nil {
		_ = listener.Close()
		return false, nil
	}
	if isAddressInUse(err) {
		return true, nil
	}
	return false, fmt.Errorf("check port %d: %w", port, err)
}

// isAddressInUse reports whether a listen error means the address is already
// bound. The Windows Sockets error is matched by errno first and by the
// (localized-independent) English message as a fallback.
func isAddressInUse(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// 10048 = WSAEADDRINUSE, 98 = Linux EADDRINUSE.
		if int(errno) == 10048 || int(errno) == 98 {
			return true
		}
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "address already in use") ||
		strings.Contains(message, "only one usage of each socket address")
}

// commandOutputText converts child-process output into a safe one-line message.
// Windows console tools may emit OEM-encoded bytes, so the text is sanitized
// before it is embedded in an error.
func commandOutputText(output []byte) string {
	text := strings.TrimSpace(strings.ToValidUTF8(string(output), "?"))
	if text == "" {
		return "(no output)"
	}
	return text
}
