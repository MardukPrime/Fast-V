//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

// IsElevated reports whether the current process is running with an elevated token.
func IsElevated() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

// LaunchElevatedRestart asks UAC to relaunch FastV after the current process exits.
func LaunchElevatedRestart() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString("cmd.exe")
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(fmt.Sprintf(`/c ping 127.0.0.1 -n 3 >nul & start "" "%s" --elevated`, executable))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, params, nil, windows.SW_HIDE)
}

// ConfigureHiddenProcess hides console windows for child processes.
func ConfigureHiddenProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
