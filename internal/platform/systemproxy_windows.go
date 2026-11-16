//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type proxySnapshot struct {
	enable      uint32
	server      string
	override    string
	hadServer   bool
	hadOverride bool
}

// EnableSystemProxy configures the current user's WinINet proxy and returns a restore function.
func EnableSystemProxy(address string) (func() error, error) {
	if address == "" {
		return nil, errors.New("system proxy address is empty")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("open internet settings: %w", err)
	}
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
	_ = key.Close()

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

	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() {
			restoreErr = restoreSystemProxy(snapshot)
		})
		return restoreErr
	}, nil
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
