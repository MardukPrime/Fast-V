# Node Config Editor

FastV extracts editable node settings from subscription share links and maps them to Xray outbound configuration.

## Editing Modes

### Form Mode

Form mode exposes the most common fields:

- Protocol, name, address and port
- VMess/VLESS UUID
- VLESS flow
- Trojan/Shadowsocks password
- Shadowsocks method
- Network: RAW/TCP, WebSocket, gRPC, HTTPUpgrade, XHTTP
- TLS, REALITY
- SNI, fingerprint, ALPN
- Host, path, gRPC service name
- REALITY public key, short ID and SpiderX
- Allow insecure TLS

Saving form mode regenerates the share link and clears any advanced outbound JSON override.

### Xray JSON Mode

JSON mode edits the Xray outbound object directly. This is useful for protocol or transport fields not yet exposed by the form.

```json
{
  "protocol": "vless",
  "settings": {
    "address": "example.com",
    "port": 443,
    "id": "00000000-0000-0000-0000-000000000002",
    "encryption": "none"
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

Behavior:

- The JSON object must contain a `protocol` field.
- The advanced JSON is stored as the node's `outboundJson`.
- When `outboundJson` is non-empty, proxy config generation uses it instead of parsing the share link.
- Clearing the JSON and saving in JSON mode restores form-generated configuration.
- The preview panel shows the effective outbound JSON.

## Round Trip

```text
share link
  -> nodeconfig.Extract
  -> editable NodeConfig
  -> nodeconfig.BuildRaw
  -> regenerated share link
```

```text
NodeConfig
  -> nodeconfig.BuildOutbound
  -> Xray outbound JSON
  -> proxy.BuildConfig
  -> full Xray config
```

## Quick Switching

Node management provides:

- Current node radio selection
- Quick switch dropdown
- Save and apply
- Automatic proxy restart when switching while the proxy is running

## Validation

- Form fields are validated before the share link is regenerated.
- Advanced JSON must be a non-empty object with a protocol.
- Generated node configurations are passed through the Xray config builder.
- Invalid advanced JSON is rejected before it is persisted.

## Related Xray Documentation

- Outbound configuration: https://xtls.github.io/config/outbounds/
- Transport configuration: https://xtls.github.io/config/transports/
- VLESS: https://xtls.github.io/config/outbounds/vless.html
- VMess: https://xtls.github.io/config/outbounds/vmess.html
- Trojan: https://xtls.github.io/config/outbounds/trojan.html
- Shadowsocks: https://xtls.github.io/config/outbounds/shadowsocks.html