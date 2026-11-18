package diagnostics

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
)

// DNS audit wire-format constants.
const (
	// dnsHeaderSize is the fixed size of a DNS message header.
	dnsHeaderSize = 12
	// dnsTypeA / dnsClassIN request IPv4 address records over the Internet class.
	dnsTypeA   = 1
	dnsClassIN = 1
	// dnsFlagResponse marks a DNS message as a response.
	dnsFlagResponse = 0x8000
	// dnsFlagRecursionDesired asks the server to resolve recursively.
	dnsFlagRecursionDesired = 0x0100
)

// dnsAuditFallbackServer is the public resolver always probed, so the audit can
// tell "the system/Xray resolver is broken" apart from "the network is down".
const dnsAuditFallbackServer = "1.1.1.1"

// dnsAuditDomains are the names queried towards every probe target: one
// international and one domestic domain, both of which almost always resolve.
var dnsAuditDomains = []string{"example.com", "www.baidu.com"}

// Indirections keep DNSAudit unit-testable: tests replace them so no real
// registry access or network I/O happens.
var (
	systemDNSFunc   = platform.SystemDNSServers
	dnsProbeFunc    = probeDNSServer
	dnsProbeTimeout = 3 * time.Second
)

// DNSProbeResult is the outcome of one A-record lookup sent to one DNS server.
type DNSProbeResult struct {
	Server    string   `json:"server"`
	Query     string   `json:"query"`
	IPs       []string `json:"ips"`
	LatencyMS int      `json:"latencyMs"`
	Error     string   `json:"error"`
}

// DNSAuditResult compares the system resolver, the Xray resolver and a public
// fallback resolver so DNS leaks and plaintext-DNS risks can be surfaced.
type DNSAuditResult struct {
	SystemServers []string         `json:"systemServers"`
	XrayServers   []string         `json:"xrayServers"`
	Probes        []DNSProbeResult `json:"probes"`
	Warnings      []string         `json:"warnings"`
	CheckedAt     string           `json:"checkedAt"`
}

// dnsAuditWarningInput carries everything the warning rules look at. It is a
// plain value so the rules can be tested without touching the network.
type dnsAuditWarningInput struct {
	SystemServers []string
	SystemErr     error
	XrayServers   []string
	Probes        []DNSProbeResult
	Settings      model.Settings
	HasSettings   bool
}

// DNSAudit reads the system DNS servers, the configured Xray DNS servers and
// probes each of them (plus a public fallback resolver) with real UDP DNS
// queries, then derives user-facing warnings.
//
// Failures are reported as probe entries and warnings instead of a returned
// error: an unreadable system DNS configuration (for example on non-Windows
// platforms) must not prevent the remaining checks from running.
func (s *DiagnosticsService) DNSAudit() (DNSAuditResult, error) {
	result := DNSAuditResult{
		SystemServers: []string{},
		XrayServers:   []string{},
		Probes:        make([]DNSProbeResult, 0, 6),
		Warnings:      []string{},
		CheckedAt:     time.Now().Format(time.RFC3339),
	}

	systemServers, systemErr := systemDNSFunc()
	if len(systemServers) > 0 {
		result.SystemServers = append(result.SystemServers, systemServers...)
	}

	settings, hasSettings := s.dnsAuditSettings()
	result.XrayServers = splitServers(settings.XrayDNSServers)

	for _, server := range dnsAuditProbeTargets(result.SystemServers, result.XrayServers) {
		for _, domain := range dnsAuditDomains {
			result.Probes = append(result.Probes, dnsProbeFunc(server, domain))
		}
	}

	result.Warnings = buildDNSAuditWarnings(dnsAuditWarningInput{
		SystemServers: result.SystemServers,
		SystemErr:     systemErr,
		XrayServers:   result.XrayServers,
		Probes:        result.Probes,
		Settings:      settings,
		HasSettings:   hasSettings,
	})
	return result, nil
}

// dnsAuditSettings returns the current application settings when the report
// store was wired into the service. The second result is false when no store is
// available, in which case settings-dependent warnings are skipped.
func (s *DiagnosticsService) dnsAuditSettings() (model.Settings, bool) {
	if s == nil {
		return model.Settings{}, false
	}
	provider := s.report.Store
	if provider == nil {
		return model.Settings{}, false
	}
	return provider.Settings(), true
}

// dnsAuditProbeTargets picks the servers probed by DNSAudit: the first system
// server, the first plaintext Xray server and the public fallback. Duplicates
// are collapsed so the same resolver is not queried twice, and encrypted Xray
// resolvers (DoH/DoT/DoQ) are skipped because they cannot be reached over
// plain UDP/53.
func dnsAuditProbeTargets(systemServers, xrayServers []string) []string {
	targets := make([]string, 0, 3)
	seen := make(map[string]struct{}, 3)
	add := func(server string) {
		display := strings.TrimSpace(server)
		if display == "" {
			return
		}
		key := normalizeDNSServer(display)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, display)
	}
	if len(systemServers) > 0 {
		add(systemServers[0])
	}
	if len(xrayServers) > 0 && isPlainDNSServer(xrayServers[0]) {
		add(xrayServers[0])
	}
	add(dnsAuditFallbackServer)
	return targets
}

// buildDNSAuditWarnings applies the DNS risk rules in a stable order.
func buildDNSAuditWarnings(in dnsAuditWarningInput) []string {
	warnings := make([]string, 0, 4)

	if in.SystemErr != nil {
		warnings = append(warnings, "无法读取系统 DNS："+in.SystemErr.Error())
	}
	if len(in.SystemServers) == 0 {
		warnings = append(warnings, "未检测到系统 DNS")
	}

	for _, probe := range in.Probes {
		if strings.TrimSpace(probe.Error) == "" {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("DNS 服务器 %s 解析 %s 失败：%s", probe.Server, probe.Query, probe.Error))
	}

	if hasPlaintextDNSServer(in.XrayServers) {
		warnings = append(warnings, "Xray DNS 使用明文 DNS（UDP 53），可能被运营商劫持或泄露真实解析")
	}

	if len(in.SystemServers) > 0 && len(in.XrayServers) > 0 && !sameDNSServerSet(in.SystemServers, in.XrayServers) {
		warnings = append(warnings, "系统 DNS 与 Xray DNS 不同，非代理流量可能使用系统 DNS")
	}

	if in.HasSettings && !in.Settings.TunMode && systemProxyModeEnabled(in.Settings.SystemProxyMode) {
		warnings = append(warnings, "系统代理模式下 DNS 可能绕过代理泄露")
	}

	return warnings
}

// splitServers splits a comma/whitespace separated DNS server list, trimming
// entries and removing duplicates (interpreting addresses case-insensitively).
// The returned slice is never nil.
func splitServers(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	servers := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		server := strings.TrimSpace(field)
		if server == "" {
			continue
		}
		key := normalizeDNSServer(server)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		servers = append(servers, server)
	}
	return servers
}

// encryptedDNSSchemes are the Xray DNS URL schemes that protect the query in
// transit. Anything else (bare IP, udp://, tcp://, http://) is plaintext DNS.
var encryptedDNSSchemes = []string{"https://", "tls://", "quic://", "h3://", "doq://", "dot://"}

// isPlainDNSServer reports whether the configured Xray DNS entry is sent over
// plaintext DNS (at least without transport encryption).
func isPlainDNSServer(server string) bool {
	value := strings.ToLower(strings.TrimSpace(server))
	if value == "" {
		return false
	}
	for _, scheme := range encryptedDNSSchemes {
		if strings.HasPrefix(value, scheme) {
			return false
		}
	}
	return true
}

// hasPlaintextDNSServer reports whether at least one Xray DNS server entry is a
// plaintext resolver.
func hasPlaintextDNSServer(servers []string) bool {
	for _, server := range servers {
		if isPlainDNSServer(server) {
			return true
		}
	}
	return false
}

// systemProxyModeEnabled reports whether the configured system-proxy mode asks
// the app to set the Windows system proxy. An empty mode uses the default
// ("system"), matching store defaults and the self-check module.
func systemProxyModeEnabled(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "system":
		return true
	default:
		return false
	}
}

// normalizeDNSServer canonicalizes a server entry for comparison: lower case,
// no trailing DNS root dot, IPv6 brackets removed and the default port 53
// dropped.
func normalizeDNSServer(server string) string {
	value := strings.ToLower(strings.TrimSpace(server))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		return value
	}
	value = strings.TrimSuffix(value, ".")
	if host, port, err := net.SplitHostPort(value); err == nil {
		host = strings.Trim(host, "[]")
		if host == "" {
			return ""
		}
		if port == "53" {
			return host
		}
		return net.JoinHostPort(host, port)
	}
	return strings.Trim(value, "[]")
}

// sameDNSServerSet compares two server lists as unordered sets of canonical
// server identifiers.
func sameDNSServerSet(left, right []string) bool {
	leftSet := dnsServerSet(left)
	rightSet := dnsServerSet(right)
	if len(leftSet) != len(rightSet) {
		return false
	}
	for server := range leftSet {
		if _, exists := rightSet[server]; !exists {
			return false
		}
	}
	return true
}

func dnsServerSet(servers []string) map[string]struct{} {
	set := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		if key := normalizeDNSServer(server); key != "" {
			set[key] = struct{}{}
		}
	}
	return set
}

// buildDNSQuery builds a recursive A-record query for domain with a random
// transaction ID. It returns nil when domain cannot be encoded as a DNS name.
func buildDNSQuery(domain string) []byte {
	return buildDNSQueryWithID(domain, randomDNSID())
}

// buildDNSQueryWithID builds a recursive A-record query with an explicit
// transaction ID; the test suite uses it for byte-exact expectations.
func buildDNSQueryWithID(domain string, id uint16) []byte {
	labels, ok := splitDNSLabels(domain)
	if !ok {
		return nil
	}

	size := dnsHeaderSize + 1 + 4
	for _, label := range labels {
		size += 1 + len(label)
	}
	query := make([]byte, dnsHeaderSize, size)
	binary.BigEndian.PutUint16(query[0:2], id)
	binary.BigEndian.PutUint16(query[2:4], dnsFlagRecursionDesired)
	binary.BigEndian.PutUint16(query[4:6], 1) // QDCOUNT
	for _, label := range labels {
		query = append(query, byte(len(label)))
		query = append(query, label...)
	}
	query = append(query, 0) // root label terminates QNAME
	query = append(query, 0, dnsTypeA, 0, dnsClassIN)
	return query
}

// splitDNSLabels splits a domain name into DNS wire labels. It rejects names
// that cannot be encoded (empty labels, labels longer than 63 bytes or a name
// longer than 255 bytes).
func splitDNSLabels(domain string) ([]string, bool) {
	name := strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if name == "" {
		return nil, false
	}
	labels := strings.Split(name, ".")
	wireLength := 1 // terminating root label
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return nil, false
		}
		wireLength += 1 + len(label)
		if wireLength > 255 {
			return nil, false
		}
	}
	return labels, true
}

// randomDNSID returns a random DNS transaction ID.
func randomDNSID() uint16 {
	var id [2]byte
	if _, err := rand.Read(id[:]); err == nil {
		return binary.BigEndian.Uint16(id[:])
	}
	return uint16(time.Now().UnixNano())
}

// parseDNSResponse extracts the A-record IPv4 addresses from a DNS response,
// validating the transaction ID and the header flags. A valid response without
// A records returns an empty slice and a nil error.
func parseDNSResponse(response []byte, id uint16) ([]string, error) {
	if len(response) < dnsHeaderSize {
		return nil, errors.New("DNS 响应不完整：缺少报文头部")
	}
	if responseID := binary.BigEndian.Uint16(response[0:2]); responseID != id {
		return nil, fmt.Errorf("DNS 响应 ID 不匹配：期望 %d，实际 %d", id, responseID)
	}
	flags := binary.BigEndian.Uint16(response[2:4])
	if flags&dnsFlagResponse == 0 {
		return nil, errors.New("DNS 响应无效：不是响应报文")
	}
	if rcode := flags & 0x000F; rcode != 0 {
		return nil, dnsRCodeError(rcode)
	}

	questions := int(binary.BigEndian.Uint16(response[4:6]))
	answers := int(binary.BigEndian.Uint16(response[6:8]))

	offset := dnsHeaderSize
	for i := 0; i < questions; i++ {
		next, err := skipDNSName(response, offset)
		if err != nil {
			return nil, err
		}
		if next+4 > len(response) {
			return nil, errors.New("DNS 响应不完整：问题段被截断")
		}
		offset = next + 4 // QTYPE + QCLASS
	}

	ips := make([]string, 0, answers)
	for i := 0; i < answers; i++ {
		next, err := skipDNSName(response, offset)
		if err != nil {
			return nil, err
		}
		if next+10 > len(response) {
			return nil, errors.New("DNS 响应不完整：资源记录被截断")
		}
		recordType := binary.BigEndian.Uint16(response[next : next+2])
		recordClass := binary.BigEndian.Uint16(response[next+2 : next+4])
		rdLength := int(binary.BigEndian.Uint16(response[next+8 : next+10]))
		dataStart := next + 10
		if dataStart+rdLength > len(response) {
			return nil, errors.New("DNS 响应不完整：记录数据被截断")
		}
		if recordType == dnsTypeA && recordClass == dnsClassIN && rdLength == 4 {
			ips = append(ips, net.IP(response[dataStart:dataStart+4]).String())
		}
		offset = dataStart + rdLength
	}
	return ips, nil
}

// skipDNSName validates a DNS name at offset and returns the offset just past
// it. Compressed names (pointer labels) consume two bytes.
func skipDNSName(message []byte, offset int) (int, error) {
	for {
		if offset >= len(message) {
			return 0, errors.New("DNS 响应不完整：域名被截断")
		}
		length := int(message[offset])
		switch {
		case length == 0:
			return offset + 1, nil
		case length&0xC0 == 0xC0:
			if offset+1 >= len(message) {
				return 0, errors.New("DNS 响应不完整：压缩指针被截断")
			}
			return offset + 2, nil
		case length&0xC0 != 0:
			return 0, errors.New("DNS 响应格式错误：域名标签长度非法")
		default:
			if offset+1+length > len(message) {
				return 0, errors.New("DNS 响应不完整：域名标签被截断")
			}
			offset += 1 + length
		}
	}
}

// dnsRCodeError translates a DNS response code into a user-facing error.
func dnsRCodeError(rcode uint16) error {
	switch rcode {
	case 1:
		return errors.New("DNS 服务器返回格式错误（FORMERR）")
	case 2:
		return errors.New("DNS 服务器故障（SERVFAIL）")
	case 3:
		return errors.New("域名不存在（NXDOMAIN）")
	case 4:
		return errors.New("DNS 服务器不支持该查询（NOTIMP）")
	case 5:
		return errors.New("DNS 服务器拒绝解析（REFUSED）")
	default:
		return fmt.Errorf("DNS 服务器返回错误码 %d", rcode)
	}
}

// probeDNSServer sends one recursive A query over UDP/53 to server and parses
// the response. Every failure mode is reported inside the result; the function
// does not panic or block longer than dnsProbeTimeout.
func probeDNSServer(server, domain string) DNSProbeResult {
	result := DNSProbeResult{Server: server, Query: domain, IPs: []string{}}

	query := buildDNSQuery(domain)
	if query == nil {
		result.Error = "无效的查询域名: " + domain
		return result
	}
	id := binary.BigEndian.Uint16(query[0:2])
	address := dnsServerAddress(server)
	if address == "" {
		result.Error = "无效的 DNS 服务器地址: " + server
		return result
	}

	started := time.Now()
	conn, err := net.DialTimeout("udp", address, dnsProbeTimeout)
	if err != nil {
		result.LatencyMS = int(time.Since(started).Milliseconds())
		result.Error = "连接 DNS 服务器失败: " + err.Error()
		return result
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(dnsProbeTimeout)); err != nil {
		result.LatencyMS = int(time.Since(started).Milliseconds())
		result.Error = "设置 DNS 查询超时失败: " + err.Error()
		return result
	}
	if _, err := conn.Write(query); err != nil {
		result.LatencyMS = int(time.Since(started).Milliseconds())
		result.Error = "发送 DNS 查询失败: " + err.Error()
		return result
	}

	buffer := make([]byte, 4096)
	read, err := conn.Read(buffer)
	result.LatencyMS = int(time.Since(started).Milliseconds())
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			result.Error = fmt.Sprintf("查询超时（%s）", dnsProbeTimeout)
		} else {
			result.Error = "读取 DNS 响应失败: " + err.Error()
		}
		return result
	}

	ips, err := parseDNSResponse(buffer[:read], id)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(ips) == 0 {
		result.Error = "DNS 服务器未返回 A 记录"
		return result
	}
	result.IPs = ips
	return result
}

// dnsServerAddress converts a configured DNS server entry into a dialable
// UDP address, adding the standard DNS port 53 and IPv6 brackets as needed.
func dnsServerAddress(server string) string {
	value := strings.TrimSpace(server)
	if value == "" || strings.Contains(value, "://") {
		return ""
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		host = strings.Trim(host, "[]")
		if host == "" || port == "" {
			return ""
		}
		return net.JoinHostPort(host, port)
	}
	host := strings.Trim(value, "[]")
	if host == "" {
		return ""
	}
	return net.JoinHostPort(host, "53")
}
