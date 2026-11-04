# Xray Transports and Security

This document defines the transport layer used by FastV when generating Xray outbounds. Xray separates transport method, transport security and additional socket options under `streamSettings`.

## 1. Structure

```json
{
  "streamSettings": {
    "method": "raw",
    "rawSettings": {},
    "wsSettings": {},
    "grpcSettings": {},
    "httpupgradeSettings": {},
    "xhttpSettings": {},
    "security": "tls",
    "tlsSettings": {},
    "realitySettings": {},
    "sockopt": {}
  }
}
```

- `method` controls how bytes are carried.
- `security` controls TLS or REALITY.
- `sockopt` controls lower-level network behavior.
- Only the settings object matching `method` and `security` should be emitted.

## 2. Compatibility Matrix

### Method + security

| Method | none | tls | reality |
|---|---|---|---|
| raw | supported | supported | supported |
| xhttp | supported | supported | supported |
| grpc | supported | supported | supported |
| websocket | supported | supported | not supported |
| httpupgrade | supported | supported | not supported |
| mkcp | supported | supported | not supported |
| hysteria | not supported | required | not supported |
| masque | not supported | required | not supported |

FastV MVP uses:

```text
raw + tls
raw + reality
websocket + tls
grpc + tls
```

HTTPUpgrade and XHTTP are planned after the MVP.

## 3. RAW

RAW is the default transport. It is used for direct TCP-style proxy connections and is required for the common VLESS Vision and REALITY combinations.

```json
{
  "method": "raw",
  "security": "tls",
  "tlsSettings": {
    "serverName": "example.com"
  }
}
```

FastV should emit `rawSettings` only when the profile explicitly needs an HTTP header disguise. Most share links use no extra raw settings.

## 4. WebSocket

```json
{
  "method": "websocket",
  "security": "tls",
  "wsSettings": {
    "path": "/ws",
    "host": "example.com",
    "headers": {},
    "heartbeatPeriod": 0
  }
}
```

Fields:

| FastV field | Xray field | Notes |
|---|---|---|
| path | wsSettings.path | Must start with `/`; may contain `?ed=2560` for Early Data |
| host | wsSettings.host | Host header priority: host > headers > address |
| headers | wsSettings.headers | Client-only custom headers |
| heartbeat | wsSettings.heartbeatPeriod | Seconds; 0 disables ping |

WebSocket is supported with TLS but not with REALITY. The upstream docs recommend XHTTP for new deployments because WebSocket has recognizable ALPN and traffic characteristics.

## 5. gRPC

```json
{
  "method": "grpc",
  "security": "tls",
  "grpcSettings": {
    "authority": "grpc.example.com",
    "serviceName": "my-service",
    "multiMode": false,
    "user_agent": ""
  }
}
```

Rules:

- `serviceName` is required.
- gRPC uses HTTP/2 and has built-in multiplexing; do not combine it with Mux without a specific reason.
- gRPC does not support Host override through the address. Use a correct domain or TLS `serverName`.
- When behind Nginx/Caddy, HTTP/2 must be enabled.
- `multiMode` is experimental and should not be enabled by default.
- gRPC is supported with TLS and REALITY, but not with WebSocket-style fallback paths.

## 6. HTTPUpgrade

HTTPUpgrade performs an HTTP/1.1 upgrade similar to WebSocket but with less protocol overhead.

```json
{
  "method": "httpupgrade",
  "security": "tls",
  "httpupgradeSettings": {
    "path": "/upgrade",
    "host": "example.com",
    "headers": {}
  }
}
```

It is supported with TLS but not REALITY. FastV can add it after the MVP. The `path` supports `?ed=<threshold>` Early Data.

## 7. XHTTP

XHTTP is the newer Xray transport documented upstream as "XHTTP: Beyond REALITY". The upstream docs currently point to the Xray-core discussion rather than a full local reference.

FastV policy:

- Do not implement XHTTP in the MVP.
- Add it behind a version capability check.
- Keep the config builder isolated so XHTTP settings can be added without changing the domain model.
- Validate the selected core version before offering XHTTP in the UI.

## 8. TLS

```json
{
  "security": "tls",
  "tlsSettings": {
    "serverName": "example.com",
    "alpn": ["h2", "http/1.1"],
    "fingerprint": "chrome",
    "useSystemCA": false,
    "pinnedPeerCertSha256": ""
  }
}
```

Important client fields:

| Field | Use |
|---|---|
| serverName | SNI and certificate validation name; defaults to address when omitted |
| alpn | TLS ALPN list; gRPC usually needs h2 first |
| fingerprint | uTLS ClientHello fingerprint; default upstream behavior is `chrome` |
| useSystemCA | Use OS CA store instead of bundled CA list |
| pinnedPeerCertSha256 | Pin a leaf or CA certificate SHA-256 |
| allowInsecure | Deprecated; do not expose as a normal UI option |

FastV rules:

- Default `allowInsecure` to false.
- Do not show `allowInsecure` in the simple profile editor.
- If the user imports an insecure profile, mark it visibly and warn.
- For WebSocket and HTTPUpgrade, the ALPN should normally be `http/1.1`.
- For gRPC, ensure `h2` is included first when a custom ALPN is set.

## 9. REALITY

REALITY is a modified TLS handshake that borrows the appearance of a target site. It is supported with RAW, XHTTP and gRPC only.

Client configuration fields:

```json
{
  "security": "reality",
  "realitySettings": {
    "serverName": "example.com",
    "fingerprint": "chrome",
    "password": "public-key",
    "shortId": "0123456789abcdef",
    "spiderX": "/"
  }
}
```

| FastV field | Xray field | Required |
|---|---|---|
| serverName | realitySettings.serverName | yes |
| fingerprint | realitySettings.fingerprint | yes |
| publicKey | realitySettings.password | yes |
| shortId | realitySettings.shortId | yes, may be empty if server allows |
| spiderX | realitySettings.spiderX | recommended |
| mldsa65Verify | realitySettings.mldsa65Verify | optional |

Rules:

- Do not emit server-only fields such as `target`, `privateKey`, `shortIds` or `serverNames` in a client outbound.
- `password` is the public key generated from the server private key. The upstream docs renamed the client field from `publicKey` to `password`.
- `shortId` must have an even number of hex characters and is padded with zeroes by the core.
- REALITY does not support WebSocket or HTTPUpgrade.
- REALITY plus VLESS Vision is a performance-oriented combination, not a universal default.
- The UI must not call the public key a private key.

## 10. Sockopt

FastV should expose only a small safe subset of `sockopt` in the advanced editor:

```json
{
  "sockopt": {
    "domainStrategy": "UseIP",
    "tcpFastOpen": false,
    "mark": 0
  }
}
```

Advanced fields should be hidden behind an expert mode. Do not allow arbitrary JSON to be pasted into the generated core config in the normal UI.

## 11. Validation Matrix

Before saving a profile, FastV should validate:

- `method` is one of the supported methods.
- `security` is one of `none`, `tls`, `reality`.
- `reality` is used only with raw, xhttp or grpc.
- WebSocket and HTTPUpgrade do not use REALITY.
- WebSocket path starts with `/`.
- gRPC serviceName is non-empty.
- TLS serverName is present when required by the profile.
- REALITY password and serverName are present.
- ALPN and fingerprint values are from the allowed sets.
- The selected Xray version supports the chosen transport.
## FinalMask

FinalMask is a transport-layer packet masking / fragmentation layer under `streamSettings.finalmask`.

FastV stores a full JSON object in Settings and writes it into the active outbound:

```json
{
  "streamSettings": {
    "finalmask": {
      "tcp": [
        {
          "type": "fragment",
          "settings": {
            "packets": "tlshello",
            "length": "100-200",
            "delay": "10-20"
          }
        }
      ]
    }
  }
}
```

Rules:

- Empty FinalMask JSON emits no `finalmask` field.
- Non-empty JSON must be an object.
- FastV validates JSON before saving.
- The JSON is copied unchanged into `streamSettings.finalmask`.
- FinalMask is applied to the current outbound stream, not to local inbound sockets.

## Sockopt

Sockopt is written under `streamSettings.sockopt`.

Structured FastV settings:

| FastV setting | Xray field |
|---|---|
| TCP Fast Open | `tcpFastOpen` |
| TCP MPTCP | `tcpMptcp` |
| KeepAlive Idle | `tcpKeepAliveIdle` |
| KeepAlive Interval | `tcpKeepAliveInterval` |
| TCP User Timeout | `tcpUserTimeout` |
| TCP Congestion | `tcpCongestion` |
| Domain Strategy | `domainStrategy` |

Advanced Sockopt JSON is merged over the structured fields. This allows fields such as:

```json
{
  "mark": 0,
  "acceptProxyProtocol": false,
  "customSockopt": []
}
```

The generated outbound looks like:

```json
{
  "streamSettings": {
    "sockopt": {
      "tcpFastOpen": true,
      "tcpMptcp": false,
      "tcpKeepAliveIdle": 30,
      "tcpKeepAliveInterval": 10,
      "tcpUserTimeout": 60000,
      "tcpCongestion": "bbr",
      "domainStrategy": "UseIPv4"
    }
  }
}
```

Both FinalMask and Sockopt settings are global client core settings and are applied to newly generated outbound configurations after saving and restarting the proxy.