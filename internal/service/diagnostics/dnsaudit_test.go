package diagnostics

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// DNS warning literals asserted by the tests.
const (
	warningNoSystemDNS   = "未检测到系统 DNS"
	warningPlaintextDNS  = "Xray DNS 使用明文 DNS（UDP 53），可能被运营商劫持或泄露真实解析"
	warningMismatchDNS   = "系统 DNS 与 Xray DNS 不同，非代理流量可能使用系统 DNS"
	warningSystemProxy   = "系统代理模式下 DNS 可能绕过代理泄露"
	warningSystemDNSRead = "无法读取系统 DNS"
)

// --- DNS wire-format helpers ----------------------------------------------

// testDNSAnswer describes one resource record written by buildTestDNSMessage.
type testDNSAnswer struct {
	pointerOffset int // <0 encodes name on the wire; >=0 writes a compression pointer
	name          string
	recordType    uint16
	class         uint16
	ttl           uint32
	data          []byte
}

func encodeTestDNSName(name string) []byte {
	encoded := make([]byte, 0, len(name)+2)
	for _, label := range strings.Split(name, ".") {
		encoded = append(encoded, byte(len(label)))
		encoded = append(encoded, label...)
	}
	return append(encoded, 0)
}

// buildTestDNSMessage builds a DNS response message with an optional question
// section (encoded at offset 12, where answer compression pointers point) and
// the given answer records.
func buildTestDNSMessage(id uint16, flags uint16, question string, answers []testDNSAnswer) []byte {
	message := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(message[0:2], id)
	binary.BigEndian.PutUint16(message[2:4], flags)
	binary.BigEndian.PutUint16(message[6:8], uint16(len(answers)))
	if question != "" {
		message = append(message, encodeTestDNSName(question)...)
		message = append(message, 0, dnsTypeA, 0, dnsClassIN)
		binary.BigEndian.PutUint16(message[4:6], 1)
	}
	for _, answer := range answers {
		if answer.pointerOffset >= 0 {
			message = append(message, byte(0xC0|(answer.pointerOffset>>8)), byte(answer.pointerOffset))
		} else {
			message = append(message, encodeTestDNSName(answer.name)...)
		}
		record := make([]byte, 10)
		binary.BigEndian.PutUint16(record[0:2], answer.recordType)
		binary.BigEndian.PutUint16(record[2:4], answer.class)
		binary.BigEndian.PutUint32(record[4:8], answer.ttl)
		binary.BigEndian.PutUint16(record[8:10], uint16(len(answer.data)))
		message = append(message, record...)
		message = append(message, answer.data...)
	}
	return message
}

func testARecord(ip string) testDNSAnswer {
	return testDNSAnswer{
		pointerOffset: 12,
		recordType:    dnsTypeA,
		class:         dnsClassIN,
		ttl:           60,
		data:          net.ParseIP(ip).To4(),
	}
}

// --- Query construction ----------------------------------------------------

func TestBuildDNSQueryWireFormat(t *testing.T) {
	want := []byte{
		0x12, 0x34, // transaction ID
		0x01, 0x00, // flags: RD
		0x00, 0x01, // QDCOUNT
		0x00, 0x00, // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,       // root label
		0x00, 0x01, // QTYPE A
		0x00, 0x01, // QCLASS IN
	}
	got := buildDNSQueryWithID("example.com", 0x1234)
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected query bytes:\n got=% x\nwant=% x", got, want)
	}

	for _, domain := range []string{" example.com ", "example.com."} {
		if !bytes.Equal(buildDNSQueryWithID(domain, 0x1234), want) {
			t.Fatalf("domain %q should encode like example.com: % x", domain, buildDNSQueryWithID(domain, 0x1234))
		}
	}
}

func TestBuildDNSQueryRandomID(t *testing.T) {
	query := buildDNSQuery("example.com")
	if query == nil {
		t.Fatal("buildDNSQuery returned nil for a valid domain")
	}
	if len(query) != 29 {
		t.Fatalf("unexpected query length: %d", len(query))
	}
	id := binary.BigEndian.Uint16(query[0:2])
	if !bytes.Equal(query, buildDNSQueryWithID("example.com", id)) {
		t.Fatalf("random-ID query differs from explicit-ID query: % x", query)
	}
	if query[4] != 0 || query[5] != 1 {
		t.Fatalf("expected QDCOUNT=1, got bytes % x", query[4:6])
	}
}

func TestBuildDNSQueryRejectsInvalidNames(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		".",
		"a..b",
		strings.Repeat("a", 64) + ".com",
		strings.Repeat("a.", 128) + "com", // longer than 255 bytes on the wire
	}
	for _, domain := range invalid {
		if query := buildDNSQuery(domain); query != nil {
			t.Fatalf("buildDNSQuery(%q) = % x, want nil", domain, query)
		}
	}
}

// --- Response parsing ------------------------------------------------------

func TestParseDNSResponseCollectsARecords(t *testing.T) {
	const id = 0x4d2
	response := buildTestDNSMessage(id, 0x8180, "example.com", []testDNSAnswer{
		testARecord("93.184.216.34"),
		testARecord("93.184.216.35"),
	})
	ips, err := parseDNSResponse(response, id)
	if err != nil {
		t.Fatalf("parseDNSResponse: %v", err)
	}
	want := []string{"93.184.216.34", "93.184.216.35"}
	if !slices.Equal(ips, want) {
		t.Fatalf("ips = %v, want %v", ips, want)
	}
}

func TestParseDNSResponseUncompressedName(t *testing.T) {
	const id = 7
	response := buildTestDNSMessage(id, 0x8180, "", []testDNSAnswer{{
		pointerOffset: -1,
		name:          "example.com",
		recordType:    dnsTypeA,
		class:         dnsClassIN,
		ttl:           30,
		data:          net.ParseIP("1.2.3.4").To4(),
	}})
	ips, err := parseDNSResponse(response, id)
	if err != nil {
		t.Fatalf("parseDNSResponse: %v", err)
	}
	if !slices.Equal(ips, []string{"1.2.3.4"}) {
		t.Fatalf("ips = %v", ips)
	}
}

func TestParseDNSResponseSkipsNonARecords(t *testing.T) {
	const id = 9
	aliasName := encodeTestDNSName("alias.example.com")
	response := buildTestDNSMessage(id, 0x8180, "example.com", []testDNSAnswer{
		{pointerOffset: 12, recordType: 5, class: dnsClassIN, ttl: 60, data: aliasName}, // CNAME
		testARecord("5.6.7.8"),
		{pointerOffset: 12, recordType: 28, class: dnsClassIN, ttl: 60, data: []byte{0, 0, 0, 0}}, // AAAA
		{pointerOffset: 12, recordType: dnsTypeA, class: 3, ttl: 60, data: []byte{9, 9, 9, 9}},    // class CH
	})
	ips, err := parseDNSResponse(response, id)
	if err != nil {
		t.Fatalf("parseDNSResponse: %v", err)
	}
	if !slices.Equal(ips, []string{"5.6.7.8"}) {
		t.Fatalf("ips = %v, want only the A/IN record", ips)
	}
}

func TestParseDNSResponseNoARecords(t *testing.T) {
	const id = 11
	response := buildTestDNSMessage(id, 0x8180, "example.com", nil)
	ips, err := parseDNSResponse(response, id)
	if err != nil {
		t.Fatalf("parseDNSResponse: %v", err)
	}
	if len(ips) != 0 {
		t.Fatalf("ips = %v, want empty", ips)
	}
}

func TestParseDNSResponseErrors(t *testing.T) {
	const id = 0x2222

	t.Run("short header", func(t *testing.T) {
		if _, err := parseDNSResponse([]byte{0x22}, id); err == nil {
			t.Fatal("expected error for short message")
		}
	})

	t.Run("id mismatch", func(t *testing.T) {
		response := buildTestDNSMessage(id+1, 0x8180, "example.com", nil)
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "ID 不匹配") {
			t.Fatalf("err = %v, want ID mismatch", err)
		}
	})

	t.Run("not a response", func(t *testing.T) {
		response := buildTestDNSMessage(id, dnsFlagRecursionDesired, "example.com", nil)
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "不是响应报文") {
			t.Fatalf("err = %v, want not-a-response error", err)
		}
	})

	t.Run("nxdomain", func(t *testing.T) {
		response := buildTestDNSMessage(id, 0x8183, "missing.example.com", nil)
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "NXDOMAIN") {
			t.Fatalf("err = %v, want NXDOMAIN", err)
		}
	})

	t.Run("servfail", func(t *testing.T) {
		response := buildTestDNSMessage(id, 0x8182, "example.com", nil)
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "SERVFAIL") {
			t.Fatalf("err = %v, want SERVFAIL", err)
		}
	})

	t.Run("truncated answer data", func(t *testing.T) {
		response := buildTestDNSMessage(id, 0x8180, "example.com", []testDNSAnswer{testARecord("1.2.3.4")})
		response = response[:len(response)-2]
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "截断") {
			t.Fatalf("err = %v, want truncation error", err)
		}
	})

	t.Run("truncated answer name", func(t *testing.T) {
		response := make([]byte, 12)
		binary.BigEndian.PutUint16(response[0:2], id)
		binary.BigEndian.PutUint16(response[2:4], 0x8180)
		binary.BigEndian.PutUint16(response[6:8], 1)
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "域名被截断") {
			t.Fatalf("err = %v, want truncated name error", err)
		}
	})

	t.Run("invalid label length", func(t *testing.T) {
		response := make([]byte, 13)
		binary.BigEndian.PutUint16(response[0:2], id)
		binary.BigEndian.PutUint16(response[2:4], 0x8180)
		binary.BigEndian.PutUint16(response[6:8], 1)
		response[12] = 0x40
		_, err := parseDNSResponse(response, id)
		if err == nil || !strings.Contains(err.Error(), "标签长度非法") {
			t.Fatalf("err = %v, want invalid label error", err)
		}
	})
}

// --- Server list handling --------------------------------------------------

func TestSplitServers(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"comma and space", "1.1.1.1, 8.8.8.8 9.9.9.9", []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}},
		{"duplicates", "1.1.1.1,1.1.1.1 , 8.8.8.8", []string{"1.1.1.1", "8.8.8.8"}},
		{"default port duplicate", "1.1.1.1:53,1.1.1.1", []string{"1.1.1.1:53"}},
		{"empty", "", []string{}},
		{"only separators", " , ,  ", []string{}},
		{"ipv6", "2001:4860:4860::8888, 8.8.8.8", []string{"2001:4860:4860::8888", "8.8.8.8"}},
		{"encrypted url", "https://dns.google/dns-query tls://1.1.1.1", []string{"https://dns.google/dns-query", "tls://1.1.1.1"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := splitServers(testCase.input)
			if got == nil {
				t.Fatal("splitServers returned nil")
			}
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("splitServers(%q) = %v, want %v", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestNormalizeDNSServer(t *testing.T) {
	cases := map[string]string{
		"1.1.1.1":            "1.1.1.1",
		" 1.1.1.1 ":          "1.1.1.1",
		"1.1.1.1:53":         "1.1.1.1",
		"1.1.1.1:5353":       "1.1.1.1:5353",
		"dns.google.":        "dns.google",
		"[::1]:53":           "::1",
		"::1":                "::1",
		"https://dns.google": "https://dns.google",
		"":                   "",
	}
	for input, want := range cases {
		if got := normalizeDNSServer(input); got != want {
			t.Fatalf("normalizeDNSServer(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSameDNSServerSet(t *testing.T) {
	cases := []struct {
		name        string
		left, right []string
		want        bool
	}{
		{"same order", []string{"1.1.1.1", "8.8.8.8"}, []string{"1.1.1.1", "8.8.8.8"}, true},
		{"different order", []string{"1.1.1.1", "8.8.8.8"}, []string{"8.8.8.8", "1.1.1.1"}, true},
		{"default port", []string{"1.1.1.1:53", "8.8.8.8"}, []string{"8.8.8.8", "1.1.1.1"}, true},
		{"different", []string{"192.168.1.1"}, []string{"8.8.8.8"}, false},
		{"different size", []string{"1.1.1.1"}, []string{"1.1.1.1", "8.8.8.8"}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := sameDNSServerSet(testCase.left, testCase.right); got != testCase.want {
				t.Fatalf("sameDNSServerSet(%v, %v) = %v, want %v", testCase.left, testCase.right, got, testCase.want)
			}
		})
	}
}

func TestIsPlainDNSServer(t *testing.T) {
	cases := map[string]bool{
		"1.1.1.1":                      true,
		"1.1.1.1:53":                   true,
		"udp://1.1.1.1":                true,
		"tcp://1.1.1.1":                true,
		"http://dns.local/resolve":     true,
		"https://dns.google/dns-query": false,
		"tls://1.1.1.1":                false,
		"quic://dns.adguard.com":       false,
		"h3://dns.google/dns-query":    false,
		"":                             false,
	}
	for server, want := range cases {
		if got := isPlainDNSServer(server); got != want {
			t.Fatalf("isPlainDNSServer(%q) = %v, want %v", server, got, want)
		}
	}
}

func TestDNSServerAddress(t *testing.T) {
	cases := map[string]string{
		"1.1.1.1":                      "1.1.1.1:53",
		" 8.8.8.8 ":                    "8.8.8.8:53",
		"1.1.1.1:5353":                 "1.1.1.1:5353",
		"::1":                          "[::1]:53",
		"[::1]:53":                     "[::1]:53",
		"dns.google":                   "dns.google:53",
		"https://dns.google/dns-query": "",
		"":                             "",
	}
	for server, want := range cases {
		if got := dnsServerAddress(server); got != want {
			t.Fatalf("dnsServerAddress(%q) = %q, want %q", server, got, want)
		}
	}
}

func TestDNSAuditProbeTargets(t *testing.T) {
	cases := []struct {
		name         string
		system, xray []string
		want         []string
	}{
		{
			name:   "system xray and fallback",
			system: []string{"192.168.1.1", "8.8.4.4"},
			xray:   []string{"8.8.8.8", "9.9.9.9"},
			want:   []string{"192.168.1.1", "8.8.8.8", "1.1.1.1"},
		},
		{
			name:   "fallback already used by xray",
			system: []string{"192.168.1.1"},
			xray:   []string{"1.1.1.1", "8.8.8.8"},
			want:   []string{"192.168.1.1", "1.1.1.1"},
		},
		{
			name:   "same server everywhere",
			system: []string{"1.1.1.1:53"},
			xray:   []string{"1.1.1.1"},
			want:   []string{"1.1.1.1:53"},
		},
		{
			name: "encrypted xray skipped",
			xray: []string{"https://dns.google/dns-query"},
			want: []string{"1.1.1.1"},
		},
		{
			name: "nothing configured",
			want: []string{"1.1.1.1"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := dnsAuditProbeTargets(testCase.system, testCase.xray)
			if !slices.Equal(got, testCase.want) {
				t.Fatalf("targets = %v, want %v", got, testCase.want)
			}
		})
	}
}

// --- Warning rules ---------------------------------------------------------

func warningPresent(warnings []string, want string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return true
		}
	}
	return false
}

func requireWarning(t *testing.T, warnings []string, want string) {
	t.Helper()
	if !warningPresent(warnings, want) {
		t.Fatalf("warnings %v should contain %q", warnings, want)
	}
}

func requireNoWarning(t *testing.T, warnings []string, unwanted string) {
	t.Helper()
	if warningPresent(warnings, unwanted) {
		t.Fatalf("warnings %v should not contain %q", warnings, unwanted)
	}
}

func TestBuildDNSAuditWarnings(t *testing.T) {
	offSettings := model.Settings{TunMode: false, SystemProxyMode: "off"}

	t.Run("no system servers", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			Settings:    offSettings,
			HasSettings: true,
		})
		if !slices.Equal(warnings, []string{warningNoSystemDNS}) {
			t.Fatalf("warnings = %v", warnings)
		}
	})

	t.Run("platform unsupported", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemErr:   errors.New("仅支持 Windows"),
			Settings:    offSettings,
			HasSettings: true,
		})
		requireWarning(t, warnings, warningSystemDNSRead+"：仅支持 Windows")
		requireWarning(t, warnings, warningNoSystemDNS)
	})

	t.Run("probe failure", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"1.1.1.1"},
			XrayServers:   []string{"1.1.1.1"},
			Probes: []DNSProbeResult{
				{Server: "1.1.1.1", Query: "example.com", Error: "查询超时（3s）"},
			},
			Settings:    offSettings,
			HasSettings: true,
		})
		requireWarning(t, warnings, "1.1.1.1")
		requireWarning(t, warnings, "example.com")
		requireWarning(t, warnings, "查询超时")
		requireWarning(t, warnings, warningPlaintextDNS)
		requireNoWarning(t, warnings, warningMismatchDNS)
	})

	t.Run("plaintext xray dns", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			Settings:      offSettings,
			HasSettings:   true,
		})
		requireWarning(t, warnings, warningPlaintextDNS)
		requireWarning(t, warnings, warningMismatchDNS)
	})

	t.Run("encrypted xray dns", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"https://dns.google/dns-query"},
			Settings:      offSettings,
			HasSettings:   true,
		})
		requireNoWarning(t, warnings, warningPlaintextDNS)
		requireWarning(t, warnings, warningMismatchDNS)
	})

	t.Run("same servers different order", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"1.1.1.1:53", "8.8.8.8"},
			XrayServers:   []string{"8.8.8.8", "1.1.1.1"},
			Settings:      offSettings,
			HasSettings:   true,
		})
		requireNoWarning(t, warnings, warningMismatchDNS)
	})

	t.Run("mismatched servers", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			Settings:      offSettings,
			HasSettings:   true,
		})
		requireWarning(t, warnings, warningMismatchDNS)
	})

	t.Run("no mismatch when one side is empty", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			Settings:      offSettings,
			HasSettings:   true,
		})
		requireNoWarning(t, warnings, warningMismatchDNS)
	})

	t.Run("tun off with system proxy", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			Settings:      model.Settings{TunMode: false, SystemProxyMode: "system"},
			HasSettings:   true,
		})
		requireWarning(t, warnings, warningSystemProxy)
	})

	t.Run("tun on with system proxy", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			Settings:      model.Settings{TunMode: true, SystemProxyMode: "system"},
			HasSettings:   true,
		})
		requireNoWarning(t, warnings, warningSystemProxy)
	})

	t.Run("system proxy off", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			Settings:      model.Settings{TunMode: false, SystemProxyMode: "off"},
			HasSettings:   true,
		})
		requireNoWarning(t, warnings, warningSystemProxy)
	})

	t.Run("settings unavailable", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			HasSettings:   false,
		})
		requireNoWarning(t, warnings, warningSystemProxy)
	})

	t.Run("empty system proxy mode defaults to system", func(t *testing.T) {
		warnings := buildDNSAuditWarnings(dnsAuditWarningInput{
			SystemServers: []string{"192.168.1.1"},
			XrayServers:   []string{"8.8.8.8"},
			Settings:      model.Settings{TunMode: false, SystemProxyMode: ""},
			HasSettings:   true,
		})
		requireWarning(t, warnings, warningSystemProxy)
	})
}

// --- DNSAudit integration with injected dependencies -----------------------

// stubDNSAudit replaces the platform/network dependencies of DNSAudit for the
// duration of one test.
func stubDNSAudit(t *testing.T, system func() ([]string, error), probe func(server, domain string) DNSProbeResult) {
	t.Helper()
	previousSystem, previousProbe := systemDNSFunc, dnsProbeFunc
	systemDNSFunc, dnsProbeFunc = system, probe
	t.Cleanup(func() {
		systemDNSFunc, dnsProbeFunc = previousSystem, previousProbe
	})
}

// newDNSAuditTestStore creates an isolated store whose settings can be tweaked
// through mutate.
func newDNSAuditTestStore(t *testing.T, mutate func(*model.Settings)) *store.Store {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := st.Settings()
	if mutate != nil {
		mutate(&settings)
	}
	if err := st.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestDNSAuditComparesServersAndProbes(t *testing.T) {
	st := newDNSAuditTestStore(t, func(settings *model.Settings) {
		settings.XrayDNSServers = "8.8.8.8, 9.9.9.9"
		settings.TunMode = false
		settings.SystemProxyMode = "system"
	})

	var probed []string
	stubDNSAudit(t,
		func() ([]string, error) { return []string{"192.168.1.1", "8.8.4.4"}, nil },
		func(server, domain string) DNSProbeResult {
			probed = append(probed, server+"|"+domain)
			if server == "192.168.1.1" {
				return DNSProbeResult{Server: server, Query: domain, Error: "查询超时（3s）"}
			}
			return DNSProbeResult{Server: server, Query: domain, IPs: []string{"93.184.216.34"}, LatencyMS: 7}
		},
	)

	service := New(fakeProxyState{}, ReportSources{Store: st})
	result, err := service.DNSAudit()
	if err != nil {
		t.Fatalf("DNSAudit: %v", err)
	}

	if !slices.Equal(result.SystemServers, []string{"192.168.1.1", "8.8.4.4"}) {
		t.Fatalf("systems servers = %v", result.SystemServers)
	}
	if !slices.Equal(result.XrayServers, []string{"8.8.8.8", "9.9.9.9"}) {
		t.Fatalf("xray servers = %v", result.XrayServers)
	}

	wantProbes := []string{
		"192.168.1.1|example.com",
		"192.168.1.1|www.baidu.com",
		"8.8.8.8|example.com",
		"8.8.8.8|www.baidu.com",
		"1.1.1.1|example.com",
		"1.1.1.1|www.baidu.com",
	}
	if !slices.Equal(probed, wantProbes) {
		t.Fatalf("probes = %v, want %v", probed, wantProbes)
	}
	if len(result.Probes) != len(wantProbes) {
		t.Fatalf("len(probes) = %d, want %d", len(result.Probes), len(wantProbes))
	}
	successful := result.Probes[2]
	if successful.Server != "8.8.8.8" || successful.Query != "example.com" {
		t.Fatalf("unexpected probe entry: %+v", successful)
	}
	if !slices.Equal(successful.IPs, []string{"93.184.216.34"}) || successful.LatencyMS != 7 {
		t.Fatalf("unexpected probe payload: %+v", successful)
	}

	if _, parseErr := time.Parse(time.RFC3339, result.CheckedAt); parseErr != nil {
		t.Fatalf("CheckedAt is not RFC3339: %q: %v", result.CheckedAt, parseErr)
	}

	requireWarning(t, result.Warnings, warningMismatchDNS)
	requireWarning(t, result.Warnings, warningPlaintextDNS)
	requireWarning(t, result.Warnings, warningSystemProxy)
	for _, failedServer := range []string{"192.168.1.1"} {
		requireWarning(t, result.Warnings, failedServer)
	}
}

func TestDNSAuditContinuesWhenSystemDNSUnavailable(t *testing.T) {
	var probed []string
	stubDNSAudit(t,
		func() ([]string, error) { return nil, errors.New("仅支持 Windows") },
		func(server, domain string) DNSProbeResult {
			probed = append(probed, server+"|"+domain)
			return DNSProbeResult{Server: server, Query: domain, IPs: []string{"1.2.3.4"}}
		},
	)

	// No ReportSources: xray settings are unavailable and settings-dependent
	// warnings must be skipped.
	service := New(fakeProxyState{})
	result, err := service.DNSAudit()
	if err != nil {
		t.Fatalf("DNSAudit: %v", err)
	}

	if len(result.SystemServers) != 0 {
		t.Fatalf("system servers = %v, want empty", result.SystemServers)
	}
	if len(result.XrayServers) != 0 {
		t.Fatalf("xray servers = %v, want empty", result.XrayServers)
	}
	wantProbes := []string{"1.1.1.1|example.com", "1.1.1.1|www.baidu.com"}
	if !slices.Equal(probed, wantProbes) {
		t.Fatalf("probes = %v, want %v", probed, wantProbes)
	}
	requireWarning(t, result.Warnings, warningSystemDNSRead)
	requireWarning(t, result.Warnings, warningNoSystemDNS)
	requireNoWarning(t, result.Warnings, warningSystemProxy)
	requireNoWarning(t, result.Warnings, warningMismatchDNS)
}

func TestDNSAuditSkipsEncryptedXrayTarget(t *testing.T) {
	st := newDNSAuditTestStore(t, func(settings *model.Settings) {
		settings.XrayDNSServers = "https://dns.google/dns-query"
		settings.SystemProxyMode = "off"
	})

	var probed []string
	stubDNSAudit(t,
		func() ([]string, error) { return []string{"192.168.1.1"}, nil },
		func(server, domain string) DNSProbeResult {
			probed = append(probed, server+"|"+domain)
			return DNSProbeResult{Server: server, Query: domain, IPs: []string{"93.184.216.34"}}
		},
	)

	service := New(fakeProxyState{}, ReportSources{Store: st})
	result, err := service.DNSAudit()
	if err != nil {
		t.Fatalf("DNSAudit: %v", err)
	}

	wantProbes := []string{
		"192.168.1.1|example.com",
		"192.168.1.1|www.baidu.com",
		"1.1.1.1|example.com",
		"1.1.1.1|www.baidu.com",
	}
	if !slices.Equal(probed, wantProbes) {
		t.Fatalf("probes = %v, want %v", probed, wantProbes)
	}
	if !slices.Equal(result.XrayServers, []string{"https://dns.google/dns-query"}) {
		t.Fatalf("xray servers = %v", result.XrayServers)
	}
	requireNoWarning(t, result.Warnings, warningPlaintextDNS)
	requireWarning(t, result.Warnings, warningMismatchDNS)
}

// --- Real UDP probe tests against a loopback DNS server --------------------

// startTestDNSServer starts a loopback UDP server answering every query with
// respond(query) when non-nil. The listener is closed at test cleanup.
func startTestDNSServer(t *testing.T, respond func(query []byte) []byte) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	go func() {
		for {
			buffer := make([]byte, 1024)
			read, address, readErr := conn.ReadFrom(buffer)
			if readErr != nil {
				return
			}
			if response := respond(buffer[:read]); response != nil {
				_, _ = conn.WriteTo(response, address)
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestProbeDNSServerAgainstLoopbackServer(t *testing.T) {
	server := startTestDNSServer(t, func(query []byte) []byte {
		id := binary.BigEndian.Uint16(query[0:2])
		return buildTestDNSMessage(id, 0x8180, "example.com", []testDNSAnswer{testARecord("93.184.216.34")})
	})

	result := probeDNSServer(server.LocalAddr().String(), "example.com")
	if result.Error != "" {
		t.Fatalf("probe failed: %+v", result)
	}
	if result.Server != server.LocalAddr().String() || result.Query != "example.com" {
		t.Fatalf("unexpected probe metadata: %+v", result)
	}
	if !slices.Equal(result.IPs, []string{"93.184.216.34"}) {
		t.Fatalf("ips = %v", result.IPs)
	}
	if result.LatencyMS < 0 {
		t.Fatalf("latency = %d", result.LatencyMS)
	}
}

func TestProbeDNSServerNXDOMAIN(t *testing.T) {
	server := startTestDNSServer(t, func(query []byte) []byte {
		id := binary.BigEndian.Uint16(query[0:2])
		return buildTestDNSMessage(id, 0x8183, "example.com", nil)
	})

	result := probeDNSServer(server.LocalAddr().String(), "example.com")
	if !strings.Contains(result.Error, "NXDOMAIN") {
		t.Fatalf("error = %q, want NXDOMAIN", result.Error)
	}
}

func TestProbeDNSServerTimeout(t *testing.T) {
	// A bound socket that never answers makes the client hit its read deadline.
	server := startTestDNSServer(t, func([]byte) []byte { return nil })

	previousTimeout := dnsProbeTimeout
	dnsProbeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { dnsProbeTimeout = previousTimeout })

	result := probeDNSServer(server.LocalAddr().String(), "example.com")
	if !strings.Contains(result.Error, "查询超时") {
		t.Fatalf("error = %q, want timeout", result.Error)
	}
}

func TestProbeDNSServerRejectsUnsupportedInput(t *testing.T) {
	t.Run("encrypted server", func(t *testing.T) {
		result := probeDNSServer("https://dns.google/dns-query", "example.com")
		if !strings.Contains(result.Error, "无效的 DNS 服务器地址") {
			t.Fatalf("error = %q, want invalid server", result.Error)
		}
	})

	t.Run("invalid domain", func(t *testing.T) {
		result := probeDNSServer("127.0.0.1", "bad..name")
		if !strings.Contains(result.Error, "无效的查询域名") {
			t.Fatalf("error = %q, want invalid domain", result.Error)
		}
	})
}
