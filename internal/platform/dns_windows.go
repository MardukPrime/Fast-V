//go:build windows

package platform

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/sys/windows/registry"
)

// tcpipInterfacesKey is the registry key whose per-interface subkeys hold the
// statically configured (NameServer) and DHCP-provided (DhcpNameServer) DNS
// servers used by Windows.
const tcpipInterfacesKey = `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces`

// dnsRegistryValueNames lists the per-interface values that may carry DNS
// servers. NameServer wins over DhcpNameServer on a configured interface, but
// both are collected: stale static values can linger next to DHCP ones, and the
// audit should show everything the resolver may use.
var dnsRegistryValueNames = []string{"NameServer", "DhcpNameServer"}

// SystemDNSServers returns the DNS servers currently configured on the Windows
// system adapters, read from
// HKLM\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\*.
//
// Values are comma or whitespace separated lists; entries are trimmed,
// de-duplicated (case-insensitively) and returned in enumeration order. A
// successful read with no configured servers returns an empty slice and a nil
// error, which lets callers distinguish "no DNS configured" from "cannot read
// the configuration".
func SystemDNSServers() ([]string, error) {
	interfaces, err := registry.OpenKey(registry.LOCAL_MACHINE, tcpipInterfacesKey, registry.READ)
	if err != nil {
		return nil, fmt.Errorf("打开网络接口 DNS 配置失败: %w", err)
	}
	defer interfaces.Close()

	names, err := interfaces.ReadSubKeyNames(-1)
	if err != nil {
		return nil, fmt.Errorf("枚举网络接口失败: %w", err)
	}

	servers := make([]string, 0, 4)
	seen := make(map[string]struct{})
	add := func(value string) {
		for _, server := range splitDNSServerList(value) {
			key := strings.ToLower(server)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			servers = append(servers, server)
		}
	}

	for _, name := range names {
		interfaceKey, openErr := registry.OpenKey(interfaces, name, registry.QUERY_VALUE)
		if openErr != nil {
			// A single unreadable adapter (permissions, transient state) must not
			// hide the DNS servers of the other adapters.
			continue
		}
		for _, valueName := range dnsRegistryValueNames {
			value, _, readErr := interfaceKey.GetStringValue(valueName)
			if readErr != nil {
				continue
			}
			add(value)
		}
		_ = interfaceKey.Close()
	}

	return servers, nil
}

// splitDNSServerList splits a registry DNS value ("8.8.8.8, 1.1.1.1" or
// "8.8.8.8 1.1.1.1") into trimmed, non-empty entries. Commas and all Unicode
// whitespace are treated as separators.
func splitDNSServerList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	servers := make([]string, 0, len(fields))
	for _, field := range fields {
		server := strings.TrimSpace(field)
		if server != "" {
			servers = append(servers, server)
		}
	}
	return servers
}
