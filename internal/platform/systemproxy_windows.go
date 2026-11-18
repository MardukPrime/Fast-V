//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// proxySnapshot records the WinINet proxy values of the current user before the
// application changes them. Missing values are tracked separately so restoring
// deletes them instead of writing an empty value.
type proxySnapshot struct {
	enable        uint32
	server        string
	override      string
	autoConfigURL string
	hadServer     bool
	hadOverride   bool
	hadAutoConfig bool
}

// readProxySnapshot reads the WinINet proxy configuration that EnableSystemProxy
// and EnableSystemProxyPAC overwrite. It is shared by both entry points so the
// restore function always covers every value either of them may change.
func readProxySnapshot() (proxySnapshot, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return proxySnapshot{}, fmt.Errorf("open internet settings: %w", err)
	}
	defer key.Close()

	snapshot := proxySnapshot{}
	if value, _, readErr := key.GetIntegerValue("ProxyEnable"); readErr == nil {
		snapshot.enable = uint32(value)
	}
	if value, _, readErr := key.GetStringValue("ProxyServer"); readErr == nil {
		snapshot.server = value
		snapshot.hadServer = true
	}
	if value, _, readErr := key.GetStringValue("ProxyOverride"); readErr == nil {
		snapshot.override = value
		snapshot.hadOverride = true
	}
	if value, _, readErr := key.GetStringValue("AutoConfigURL"); readErr == nil {
		snapshot.autoConfigURL = value
		snapshot.hadAutoConfig = true
	}
	return snapshot, nil
}

// EnableSystemProxy configures the current user's WinINet proxy and returns a restore function.
func EnableSystemProxy(address string) (func() error, error) {
	if address == "" {
		return nil, errors.New("system proxy address is empty")
	}
	snapshot, err := readProxySnapshot()
	if err != nil {
		return nil, err
	}

	if err := regAdd("ProxyEnable", "REG_DWORD", "1"); err != nil {
		return nil, fmt.Errorf("enable proxy: %w", err)
	}
	if err := regAdd("ProxyServer", "REG_SZ", address); err != nil {
		_ = restoreSystemProxy(snapshot)
		return nil, fmt.Errorf("set proxy server: %w", err)
	}
	if err := regAdd("ProxyOverride", "REG_SZ", "<local>;127.*;10.*;172.16.*;192.168.*"); err != nil {
		_ = restoreSystemProxy(snapshot)
		return nil, fmt.Errorf("set proxy override: %w", err)
	}
	refreshInternetSettings()

	return onceRestore(snapshot), nil
}

// EnableSystemProxyPAC points the current user's WinINet automatic proxy
// configuration (AutoConfigURL) at a PAC script and disables the fixed proxy so
// a stale ProxyServer entry cannot conflict with the PAC result. It returns a
// restore function that reverts AutoConfigURL, ProxyEnable, ProxyServer and
// ProxyOverride to the values captured before the change.
func EnableSystemProxyPAC(pacFilePath string) (func() error, error) {
	pacURL, err := pacFileURL(pacFilePath)
	if err != nil {
		return nil, err
	}
	snapshot, err := readProxySnapshot()
	if err != nil {
		return nil, err
	}

	if err := regAdd("ProxyEnable", "REG_DWORD", "0"); err != nil {
		return nil, fmt.Errorf("disable fixed proxy: %w", err)
	}
	if err := regAdd("AutoConfigURL", "REG_SZ", pacURL); err != nil {
		_ = restoreSystemProxy(snapshot)
		return nil, fmt.Errorf("set auto config url: %w", err)
	}
	refreshInternetSettings()

	return onceRestore(snapshot), nil
}

// onceRestore builds a restore function that only runs one time, so concurrent
// stop paths (manual stop, watchdog cleanup, failed start) cannot interleave
// registry writes.
func onceRestore(snapshot proxySnapshot) func() error {
	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() {
			restoreErr = restoreSystemProxy(snapshot)
		})
		return restoreErr
	}
}

// pacFileURL validates a PAC script path and converts it into the file:// URL
// the WinINet AutoConfigURL value expects. Windows file URLs look like
// file:///C:/Users/me/AppData/pac/fastv.pac; spaces and characters that would
// otherwise be interpreted as URL syntax are percent-encoded.
func pacFileURL(pacFilePath string) (string, error) {
	path := strings.TrimSpace(pacFilePath)
	if path == "" {
		return "", errors.New("PAC file path is empty")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve PAC file path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("PAC file is not readable: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("PAC file path is a directory: %s", absPath)
	}
	slashed := filepath.ToSlash(absPath)
	escaped := escapeFileURLPath(slashed)
	// UNC paths (\\server\share) become //server/share; Windows expects
	// file://server/share for them.
	if strings.HasPrefix(slashed, "//") {
		return "file:" + escaped, nil
	}
	return "file:///" + escaped, nil
}

// escapeFileURLPath percent-encodes a slash-separated Windows path for use in a
// file:// URL. ASCII letters, digits and the characters that are meaningful in
// a drive path stay readable; everything else (spaces, '#', '%' and non-ASCII
// UTF-8 bytes) is escaped so the WinINet PAC loader sees an unambiguous URL.
func escapeFileURLPath(path string) string {
	var builder strings.Builder
	builder.Grow(len(path) + 8)
	for i := 0; i < len(path); i++ {
		character := path[i]
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '/', character == ':', character == '-',
			character == '.', character == '_', character == '~':
			builder.WriteByte(character)
		default:
			fmt.Fprintf(&builder, "%%%02X", character)
		}
	}
	return builder.String()
}

func restoreSystemProxy(snapshot proxySnapshot) error {
	if err := regAdd("ProxyEnable", "REG_DWORD", strconv.FormatUint(uint64(snapshot.enable), 10)); err != nil {
		return err
	}
	if snapshot.hadServer {
		if err := regAdd("ProxyServer", "REG_SZ", snapshot.server); err != nil {
			return err
		}
	} else {
		regDelete("ProxyServer")
	}
	if snapshot.hadOverride {
		if err := regAdd("ProxyOverride", "REG_SZ", snapshot.override); err != nil {
			return err
		}
	} else {
		regDelete("ProxyOverride")
	}
	if snapshot.hadAutoConfig {
		if err := regAdd("AutoConfigURL", "REG_SZ", snapshot.autoConfigURL); err != nil {
			return err
		}
	} else {
		regDelete("AutoConfigURL")
	}
	refreshInternetSettings()
	return nil
}

func regAdd(name string, valueType string, value string) error {
	cmd := exec.Command("reg.exe", "add", `HKCU\`+internetSettingsKey, "/v", name, "/t", valueType, "/d", value, "/f")
	ConfigureHiddenProcess(cmd)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

func regDelete(name string) {
	cmd := exec.Command("reg.exe", "delete", `HKCU\`+internetSettingsKey, "/v", name, "/f")
	ConfigureHiddenProcess(cmd)
	_ = cmd.Run()
}

func refreshInternetSettings() {
	wininet := windows.NewLazySystemDLL("wininet.dll")
	internetSetOption := wininet.NewProc("InternetSetOptionW")
	_, _, _ = internetSetOption.Call(0, 39, 0, 0)
	_, _, _ = internetSetOption.Call(0, 37, 0, 0)
}

// SetSystemProxyState is kept for future diagnostics.
func SetSystemProxyState(enabled bool, address string) error {
	if !enabled {
		return nil
	}
	_, err := EnableSystemProxy(address)
	return err
}
