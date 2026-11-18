package proxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// pacFileName is the generated PAC script name under <data dir>/pac.
const pacFileName = "fastv.pac"

// pacProxyHostPattern constrains the host part of a PAC proxy address to plain
// hostnames, IPv4 literals and Windows machine names. Anything that cannot be
// embedded in the generated script without further quoting is rejected.
var pacProxyHostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// pacScriptTemplate is the PAC script served to WinINet when the system proxy
// mode is "pac". The single placeholder is the host:port of the local inbound.
// The script is intentionally pure ASCII so any PAC parser reads it.
const pacScriptTemplate = `// FastV PAC script - generated automatically, do not edit.
// Private and local hosts connect directly; every other request uses the
// local FastV inbound.
var FASTV_PROXY = "PROXY %s";

function FindProxyForURL(url, host) {
  if (isPrivateHost(host)) {
    return "DIRECT";
  }
  return FASTV_PROXY;
}

function isPrivateHost(host) {
  var h = host ? host.toLowerCase() : "";
  if (h === "" || h === "localhost" || h === "127.0.0.1" || h === "::1" || h === "[::1]") {
    return true;
  }
  if (dnsDomainIs(h, ".local")) {
    return true;
  }
  return shExpMatch(h, "127.*") ||
    shExpMatch(h, "10.*") ||
    shExpMatch(h, "192.168.*") ||
    shExpMatch(h, "172.1[6-9].*") ||
    shExpMatch(h, "172.2[0-9].*") ||
    shExpMatch(h, "172.3[01].*");
}
`

// BuildPAC renders the PAC script that sends private and local hosts directly
// and every other request to proxyAddress (host:port). An empty or malformed
// proxy address yields an empty string: callers must treat that as "PAC mode
// unavailable" instead of pointing the system at a broken script.
func BuildPAC(proxyAddress string) string {
	address := strings.TrimSpace(proxyAddress)
	if err := validatePACProxyAddress(address); err != nil {
		return ""
	}
	return fmt.Sprintf(pacScriptTemplate, address)
}

// validatePACProxyAddress checks that address is a host:port pair suitable for
// a PAC "PROXY" directive. IPv6 literals are rejected: the local FastV inbound
// always listens on an IPv4 loopback address and PAC proxy directives for IPv6
// literals are not portable.
func validatePACProxyAddress(address string) error {
	if address == "" {
		return errors.New("代理地址为空")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("代理地址必须是 host:port 形式: %w", err)
	}
	if host == "" {
		return errors.New("代理地址缺少主机名")
	}
	if strings.Contains(host, ":") {
		return fmt.Errorf("暂不支持 IPv6 代理地址: %q", host)
	}
	if !pacProxyHostPattern.MatchString(host) {
		return fmt.Errorf("代理地址包含非法字符: %q", host)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("代理端口无效: %q", port)
	}
	return nil
}

// pacFilePath returns the PAC script location for an application data
// directory. The file lives under its own "pac" directory so a stop can remove
// it without touching anything else.
func pacFilePath(dataDir string) string {
	return filepath.Join(dataDir, "pac", pacFileName)
}

// writePACFile renders the PAC script for localAddr and writes it to
// <dataDir>/pac/fastv.pac with owner-only permissions. The returned path is
// absolute and can be passed to platform.EnableSystemProxyPAC.
func writePACFile(dataDir, localAddr string) (string, error) {
	address := strings.TrimSpace(localAddr)
	if err := validatePACProxyAddress(address); err != nil {
		return "", fmt.Errorf("本地代理地址无法生成 PAC 脚本: %w", err)
	}
	pac := BuildPAC(address)
	if pac == "" {
		return "", fmt.Errorf("本地代理地址无法生成 PAC 脚本: %q", localAddr)
	}
	path := pacFilePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("创建 PAC 目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(pac), 0o600); err != nil {
		return "", fmt.Errorf("写入 PAC 文件失败: %w", err)
	}
	return path, nil
}

// removePACFile deletes a generated PAC script on a best-effort basis. A missing
// file is not an error: the stop paths must never fail because the file was
// already cleaned up.
func removePACFile(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}
