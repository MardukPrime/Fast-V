//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// autostartRunKey is the per-user Run key that Windows evaluates at logon.
	autostartRunKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	// autostartValueName is the registry value controlled by FastV.
	autostartValueName = "FastV"
)

// SetAutostart registers FastV in the current user's Run key when enabled is
// true and removes the entry otherwise. Only the current user hive is touched,
// so no administrator rights are required.
func SetAutostart(enabled bool) error {
	if !enabled {
		return removeAutostart()
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %w", err)
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("解析程序路径失败: %w", err)
	}
	if strings.Contains(absolute, `"`) {
		return errors.New("程序路径包含无效字符")
	}

	// The command is quoted so paths containing spaces start correctly.
	return runAutostartReg("add", autostartRunKey, "/v", autostartValueName, "/t", "REG_SZ", "/d", `"`+absolute+`"`, "/f")
}

// AutostartEnabled reports whether the FastV value exists in the current user's
// Run key. The check is read-only.
func AutostartEnabled() (bool, error) {
	cmd := exec.Command("reg.exe", "query", autostartRunKey, "/v", autostartValueName)
	ConfigureHiddenProcess(cmd)

	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// reg.exe exits with a non-zero status when the value does not exist.
		return false, nil
	}
	return false, fmt.Errorf("查询开机自启状态失败: %w", err)
}

// IsEnabled reports whether FastV starts with Windows. It is an alias of
// AutostartEnabled kept for callers that prefer the shorter name.
func IsEnabled() (bool, error) {
	return AutostartEnabled()
}

func removeAutostart() error {
	enabled, err := AutostartEnabled()
	if err != nil {
		return err
	}
	if !enabled {
		return nil
	}

	if err := runAutostartReg("delete", autostartRunKey, "/v", autostartValueName, "/f"); err != nil {
		// If the value disappeared between the query and the delete, the desired
		// end state is already reached.
		if stillEnabled, queryErr := AutostartEnabled(); queryErr == nil && !stillEnabled {
			return nil
		}
		return err
	}
	return nil
}

func runAutostartReg(args ...string) error {
	cmd := exec.Command("reg.exe", args...)
	ConfigureHiddenProcess(cmd)

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("更新开机自启项失败: %w", err)
		}
		return fmt.Errorf("更新开机自启项失败: %v: %s", err, message)
	}
	return nil
}
