# Xray Routing and DNS

FastV exposes routing and DNS as structured settings. The Go backend converts them into Xray `routing` and `dns` objects. The frontend never writes raw Xray JSON.

## 1. Routing Object

```json
{
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": []
  }
}
```

The first outbound in the `outbounds` array is the default route when no rule matches.

## 2. Domain Strategy

| Value | Behavior | FastV use |
|---|---|---|
| AsIs | Do not resolve domains for routing | Default for most client setups |
| IPIfNonMatch | Resolve only after the first rule pass misses | Practical default when IP rules are used |
| IPOnDemand | Resolve when an IP rule is encountered | Higher DNS dependency, use deliberately |

FastV should default to `IPIfNonMatch` for normal client use because it allows both domain and IP rules without resolving every domain upfront.

## 3. Rule Object

A routing rule can combine multiple conditions. All conditions in one rule must match for the rule to apply.

Common fields:

| Field | Meaning |
|---|---|
| `domain` | Domain suffix, keyword, regex, full match, geosite or external file |
| `ip` | IP, CIDR, geoip or external file |
| `port` | Destination port or range |
| `sourcePort` | Source port or range |
| `network` | tcp, udp or tcp,udp |
| `inboundTag` | Match the local inbound tag |
| `protocol` | Sniffed protocol: http, tls, quic, bittorrent |
| `process` | Local process name on Windows/Linux |
| `outboundTag` | Destination outbound |
| `balancerTag` | Destination balancer |

Example:

```json
{
  "type": "field",
  "domain": ["domain:example.com", "geosite:cn"],
  "ip": ["geoip:private"],
  "network": "tcp,udp",
  "outboundTag": "direct"
}
```

A rule should target either `outboundTag` or `balancerTag`, not both.

## 4. Rule Order

Rules are evaluated from top to bottom. The first matching rule wins.

Recommended FastV order:

1. Local/private network direct rule.
2. Explicit block rules.
3. User-defined direct rules.
4. User-defined proxy rules.
5. Catch-all proxy rule, if the user selected a global proxy mode.

The UI should use drag-and-drop ordering and preview the resolved order before saving.

## 5. Domain Matching

Supported formats are documented upstream:

```text
example.com                 substring
keyword:example             substring
domain:example.com          domain and subdomains
full:example.com            exact domain
regexp:\.example\.com$      regular expression
geosite:cn                  bundled domain category
ext:custom.dat:tag          external data file
```

FastV should present friendly rule types in the UI and generate the exact upstream syntax in Go.

## 6. IP Matching

Supported forms:

```text
127.0.0.1
10.0.0.0/8
geoip:cn
geoip:private
!geoip:cn
ext:custom.dat:tag
```

The `!` prefix is an exclusion form. FastV must validate exclusion combinations before generating a rule.

## 7. Preset Rules

Suggested default presets:

### Direct private networks

```json
{
  "type": "field",
  "ip": ["geoip:private"],
  "outboundTag": "direct"
}
```

### Block local ad domains

```json
{
  "type": "field",
  "domain": ["geosite:category-ads-all"],
  "outboundTag": "block"
}
```

### Proxy everything else

```json
{
  "type": "field",
  "network": "tcp,udp",
  "outboundTag": "proxy"
}
```

Presets must be editable. Users should be able to see and disable every generated rule.

## 8. DNS Object

```json
{
  "dns": {
    "hosts": {},
    "servers": [
      "1.1.1.1",
      "https://dns.google/dns-query"
    ],
    "queryStrategy": "UseIP",
    "disableCache": false
  }
}
```

Common fields:

| Field | Meaning |
|---|---|
| `hosts` | Static host mappings |
| `servers` | DNS server list |
| `queryStrategy` | UseIP, UseIPv4, UseIPv6 |
| `disableCache` | Disable DNS cache |
| `tag` | DNS inbound/outbound tag for TUN scenarios |
| `useSystemHosts` | Include system hosts file |

Supported server address forms include:

```text
8.8.8.8
8.8.8.8:53
tcp://8.8.8.8:53
https://dns.google/dns-query
https+local://dns.google/dns-query
localhost
fakedns
```

## 9. DNS and Routing Interaction

FastV should keep DNS policy separate from route rules in the UI:

- DNS settings decide how names are resolved.
- Route rules decide which outbound receives the connection.
- Sniffing can recover a domain from a connection that started with an IP.
- `routeOnly` can use the sniffed domain for routing without replacing the original destination.
- FakeDNS is a TUN-oriented feature. TUN support exists on Windows, but FakeDNS remains optional and is not enabled by default.

## 10. Sniffing

The local inbound should enable sniffing for normal browsing:

```json
{
  "sniffing": {
    "enabled": true,
    "destOverride": ["http", "tls", "quic"],
    "routeOnly": false
  }
}
```

Rules:

- `protocol` routing conditions require sniffing.
- `routeOnly` can avoid changing the destination for applications that break under destination rewriting.
- The UI should expose a compatibility mode that enables `routeOnly`.
- ECH can hide the inner domain from sniffing; this must be documented in troubleshooting.

## 11. Geodata

Xray uses `geosite.dat` and `geoip.dat` for `geosite:` and `geoip:` rules. FastV should:

- bundle a known-good geodata version
- verify its hash
- record the geodata version in diagnostics
- allow user replacement only with a warning
- avoid downloading rule files from untrusted sources

Geodata updates are independent from the core binary version.

## 12. Validation Rules

- Rule must have at least one condition and one destination.
- `outboundTag` and `balancerTag` are mutually exclusive.
- Ports must be in the range 1-65535.
- Domain and IP lists must be non-empty when present.
- `geosite:` and `geoip:` categories must exist in the installed data files.
- `process` rules must be marked as platform-dependent.
- Catch-all rules must be clearly marked in the UI.
- A routing preview should show the final generated rules before saving.