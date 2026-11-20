//go:build windows

package proxy

import (
	"errors"
	"os/exec"

	"github.com/MardukPrime/Fast-V/internal/platform"
)

// preflightSingBoxTun verifies the requirements for the sing-box Windows TUN
// mode. sing-box creates and manages the adapter/routes itself (no wintun.dll
// next to the binary), but an elevated process is still required.
func preflightSingBoxTun() error {
	if !platform.IsElevated() {
		return errors.New("sing-box TUN 模式需要管理员权限，请以管理员身份启动 FastV")
	}
	return nil
}

// cleanupSingBoxTun removes routes and addresses that may survive a forced
// process termination of the sing-box TUN interface. The adapter itself is
// owned by the wintun session and normally disappears with the process.
func cleanupSingBoxTun() error {
	script := `
$ErrorActionPreference = 'SilentlyContinue'
$tun = Get-NetAdapter -Name 'FastV-TUN'
if (-not $tun) { exit 0 }
$idx = [int]$tun.ifIndex
Get-NetRoute -InterfaceIndex $idx | Remove-NetRoute -Confirm:$false
Get-NetIPAddress -InterfaceIndex $idx | Remove-NetIPAddress -Confirm:$false
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	return cmd.Run()
}
