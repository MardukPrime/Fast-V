# Xray Profile Mapping

This document maps FastV profile fields to Xray outbound configuration. The generated configuration always targets the pinned Xray-core version used by the application.

## 1. Internal Profile Model

A FastV profile should preserve enough information to regenerate a complete share link and an Xray outbound:

```go
type Profile struct {
    ID             string
    SubscriptionID string
    Name           string
    Protocol       ProtocolType
    Address        string
    Port           int
    Enabled        bool
    SortOrder      int

    Meta   json.RawMessage // transport, TLS, REALITY and non-secret fields
    Secret []byte          // encrypted UUID/password/private key payload
}
```

Protocol-specific metadata is stored as JSON so new Xray fields can be added without a database migration. Sensitive values remain encrypted until config generation.

## 2. Common Outbound Structure

Every profile becomes an outbound with a stable tag:

```json
{
  "tag": "proxy",
  "protocol": "vless",
  "settings": {},
  "streamSettings": {},
  "mux": {}
}
```

Rules:

- `tag` is always `proxy` for the selected node.
- `settings.address` and `settings.port` come from the profile.
- `streamSettings` is present only when the profile has transport or security settings.
- `mux` is present only when enabled.
- The profile name is a UI label and is not used as an Xray tag.
- One profile maps to exactly one outbound.

## 3. Protocol Mapping

### 3.1 VLESS

FastV mapping:

| FastV field | Xray field | Required | Notes |
|---|---|---|---|
| address | settings.address | yes | Domain, IPv4 or IPv6 |
| port | settings.port | yes | 1-65535 |
| id | settings.id | yes | UUID or mapped string |
| encryption | settings.encryption | yes | Use `none` when VLESS Encryption is disabled |
| flow | settings.flow | no | `xtls-rprx-vision` only when supported by transport/security |
| level | settings.level | no | Default 0 |

```json
{
  "tag": "proxy",
  "protocol": "vless",
  "settings": {
    "address": "example.com",
    "port": 443,
    "id": "00000000-0000-0000-0000-000000000000",
    "encryption": "none",
    "flow": "xtls-rprx-vision"
  },
  "streamSettings": {
    "method": "raw",
    "security": "reality",
    "realitySettings": {}
  }
}
```

Important upstream rules:

- VLESS must use transport security in public networks unless VLESS Encryption is enabled or the peer is a trusted private address.
- `encryption` must not be empty; use `"none"` explicitly when disabled.
- `xtls-rprx-vision` is intended for RAW with TLS or REALITY.
- `xtls-rprx-vision-udp443` keeps UDP/443 available for applications that force QUIC.
- REALITY and XTLS Vision can improve performance, but only when the server and transport match.

### 3.2 VMess

| FastV field | Xray field | Required | Notes |
|---|---|---|---|
| address | settings.address | yes | Domain, IPv4 or IPv6 |
| port | settings.port | yes | 1-65535 |
| id | settings.id | yes | UUID or mapped string |
| security | settings.security | no | `auto`, `aes-128-gcm` or `chacha20-poly1305` |
| level | settings.level | no | Default 0 |

```json
{
  "tag": "proxy",
  "protocol": "vmess",
  "settings": {
    "address": "example.com",
    "port": 443,
    "id": "00000000-0000-0000-0000-000000000000",
    "security": "auto"
  },
  "streamSettings": {
    "method": "websocket",
    "security": "tls",
    "wsSettings": {
      "path": "/ws",
      "host": "example.com"
    }
  }
}
```

Operational rule: VMess depends on system time. The OS UTC time must be within 120 seconds of the server. FastV should detect obvious clock errors and show a warning.

### 3.3 Trojan

| FastV field | Xray field | Required | Notes |
|---|---|---|---|
| address | settings.address | yes | Domain, IPv4 or IPv6 |
| port | settings.port | yes | 1-65535 |
| password | settings.password | yes | Any string |
| email | settings.email | no | Optional label |
| level | settings.level | no | Default 0 |

```json
{
  "tag": "proxy",
  "protocol": "trojan",
  "settings": {
    "address": "example.com",
    "port": 443,
    "password": "replace-me"
  },
  "streamSettings": {
    "method": "raw",
    "security": "tls",
    "tlsSettings": {
      "serverName": "example.com"
    }
  }
}
```

Trojan should use TLS in public networks. The core documentation also warns that without Mux, inner TLS traffic can create a detectable TLS-in-TLS pattern.

### 3.4 Shadowsocks

| FastV field | Xray field | Required | Notes |
|---|---|---|---|
| address | settings.address | yes | Domain, IPv4 or IPv6 |
| port | settings.port | yes | 1-65535 |
| method | settings.method | yes | Cipher method |
| password | settings.password | yes | Passphrase or 2022 key |
| email | settings.email | no | Optional label |
| level | settings.level | no | Default 0 |

```json
{
  "tag": "proxy",
  "protocol": "shadowsocks",
  "settings": {
    "address": "example.com",
    "port": 8388,
    "method": "2022-blake3-aes-256-gcm",
    "password": "base64-key"
  }
}
```

Recommended methods from the upstream docs:

```text
2022-blake3-aes-128-gcm
2022-blake3-aes-256-gcm
2022-blake3-chacha20-poly1305
aes-256-gcm
aes-128-gcm
chacha20-poly1305
xchacha20-poly1305
```

For Shadowsocks 2022, the password is a pre-shared key and its length depends on the method. FastV must validate the expected key length and warn before saving an invalid profile.

## 4. Transport Mapping

The FastV `Meta` JSON should use a small normalized model and the builder should translate it to Xray field names:

```go
type TransportMeta struct {
    Method  string // raw, websocket, grpc, httpupgrade, xhttp
    Security string // none, tls, reality

    Path        string
    Host        string
    ServiceName string
    Authority   string
    Headers     map[string]string
    EarlyData   int

    ServerName string
    ALPN       []string
    Fingerprint string
    AllowInsecure bool

    RealityPublicKey string
    RealityShortID   string
    SpiderX          string
}
```

See [transports.md](transports.md) for the complete transport and security rules.

## 5. Mux

Mux is optional and maps to the outbound object:

```json
{
  "mux": {
    "enabled": true,
    "concurrency": 8,
    "xudpConcurrency": 16,
    "xudpProxyUDP443": "reject"
  }
}
```

FastV should default Mux to disabled. Mux reduces handshake latency but often reduces throughput for downloads, video and speed tests. Do not enable it automatically.

## 6. Share Link Mapping

### VMess

`vmess://` usually contains a Base64-encoded JSON object. The parser should map common fields:

| Link field | Profile field |
|---|---|
| `ps` | name |
| `add` | address |
| `port` | port |
| `id` | UUID |
| `aid` | alterId, legacy compatibility only |
| `scy` | security |
| `net` | transport method |
| `type` | header type or transport variant |
| `host` | host or authority |
| `path` | path or service name |
| `tls` | security mode |
| `sni` | server name |
| `alpn` | ALPN list |
| `fp` | fingerprint |

### VLESS and Trojan

These links use URL-style query parameters. Common parameters:

```text
type, security, sni, alpn, fp, flow, path, host, serviceName,
pbk, sid, spx, headerType, encryption
```

### Shadowsocks

Support both SIP002 and legacy Base64 formats. Parse the cipher method, password, host and port, then validate the method against the supported list.

## 7. Validation Rules

Reject or warn before saving:

- address is empty
- port is outside 1-65535
- UUID is empty or malformed for VLESS/VMess
- password is empty for Trojan/Shadowsocks
- Shadowsocks method is unknown or key length is invalid
- REALITY is selected without a public key or server name
- TLS is selected for a transport/security combination that does not support it
- WebSocket path does not start with `/`
- gRPC service name is empty
- VLESS `flow` is used with an incompatible transport or security
- Mux concurrency is outside the supported range
- plaintext public VLESS/Trojan without a trusted private network or VLESS Encryption

## 8. Builder Output

The config builder should emit:

```text
outbounds[0] = proxy
outbounds[1] = direct
outbounds[2] = block
```

Do not emit a proxy outbound if the profile is invalid. A failed validation must prevent the core from starting.