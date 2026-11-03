//go:build windows

package proxy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func preflightTun(corePath string) error {
	dllPath := filepath.Join(filepath.Dir(corePath), "wintun.dll")
	if _, err := os.Stat(dllPath); err != nil {
		return errors.New("wintun.dll 未找到，请更新或重新安装 Xray-core")
	}
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return errors.New("无法检查进程权限")
	}
	defer token.Close()
	if !token.IsElevated() {
		return errors.New("TUN 模式需要管理员权限，请以管理员身份启动 FastV")
	}
	return nil
}

func setupTunRoutes(tunName string, proxyIP string) (func() error, error) {
	script := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$tunName = '%s'
$proxyIP = '%s'
$tunAdapter = $null
for ($i = 0; $i -lt 50; $i++) {
    $tunAdapter = Get-NetAdapter -Name $tunName -ErrorAction SilentlyContinue
    if ($tunAdapter) { break }
    Start-Sleep -Milliseconds 200
}
if (-not $tunAdapter) { throw 'TUN adapter was not created' }
$tunIndex = [int]$tunAdapter.ifIndex
$defaultRoute = Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue |
    Where-Object { $_.InterfaceIndex -ne $tunIndex -and $_.NextHop -ne '0.0.0.0' } |
    Sort-Object RouteMetric | Select-Object -First 1
if (-not $defaultRoute) { throw 'physical default route not found' }
$gateway = $defaultRoute.NextHop
$physicalIndex = [int]$defaultRoute.InterfaceIndex
Get-NetIPAddress -InterfaceIndex $tunIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
    Remove-NetIPAddress -Confirm:$false -ErrorAction SilentlyContinue
New-NetIPAddress -InterfaceIndex $tunIndex -IPAddress 198.18.0.1 -PrefixLength 15 -ErrorAction Stop | Out-Null
Set-NetIPInterface -InterfaceIndex $tunIndex -InterfaceMetric 1 -ErrorAction SilentlyContinue | Out-Null
Set-DnsClientServerAddress -InterfaceIndex $tunIndex -ServerAddresses ('1.1.1.1','8.8.8.8') -ErrorAction SilentlyContinue | Out-Null
Remove-NetRoute -DestinationPrefix "$proxyIP/32" -InterfaceIndex $physicalIndex -PolicyStore ActiveStore -Confirm:$false -ErrorAction SilentlyContinue
Remove-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIndex -PolicyStore ActiveStore -Confirm:$false -ErrorAction SilentlyContinue
New-NetRoute -DestinationPrefix "$proxyIP/32" -InterfaceIndex $physicalIndex -NextHop $gateway -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null
New-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIndex -NextHop '0.0.0.0' -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null
Write-Output "$tunIndex|$physicalIndex"
`, tunName, proxyIP)

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) != 2 {
		_ = cleanupTunRoutes(tunName, proxyIP, 0, 0)
		return nil, fmt.Errorf("unexpected TUN route setup output: %s", strings.TrimSpace(string(output)))
	}
	tunIndex, err1 := strconv.Atoi(parts[0])
	physicalIndex, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		_ = cleanupTunRoutes(tunName, proxyIP, 0, 0)
		return nil, errors.New("failed to parse TUN interface indexes")
	}
	return func() error {
		return cleanupTunRoutes(tunName, proxyIP, tunIndex, physicalIndex)
	}, nil
}

func cleanupTunRoutes(tunName string, proxyIP string, tunIndex int, physicalIndex int) error {
	if tunIndex == 0 {
		if adapter, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", fmt.Sprintf("(Get-NetAdapter -Name '%s' -ErrorAction SilentlyContinue).ifIndex", tunName)).Output(); err == nil {
			if parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(adapter))); parseErr == nil {
				tunIndex = parsed
			}
		}
	}
	script := fmt.Sprintf(`
$ErrorActionPreference = 'SilentlyContinue'
if (%d -gt 0) { Remove-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex %d -PolicyStore ActiveStore -Confirm:$false -ErrorAction SilentlyContinue }
if (%d -gt 0) { Remove-NetRoute -DestinationPrefix '%s/32' -InterfaceIndex %d -PolicyStore ActiveStore -Confirm:$false -ErrorAction SilentlyContinue }
`, tunIndex, tunIndex, physicalIndex, proxyIP, physicalIndex)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	hideWindow(cmd)
	return cmd.Run()
}
