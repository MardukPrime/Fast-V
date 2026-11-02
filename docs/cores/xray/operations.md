# Xray-core Operations

This document covers the commands and operational checks FastV uses with Xray-core.

## 1. Version Check

```bash
xray version
```

FastV should store the exact version string and display it in diagnostics. A core version change requires rerunning the full config golden test suite.

## 2. Config Validation

```bash
xray run -test -config config.json
```

FastV runs this command before every proxy start. A non-zero exit code blocks startup and returns a concise error to the UI.

Useful additional flags:

```text
-test    validate configuration only
-dump    print the merged configuration
```

`-dump` must never be shown to the UI by default because it can contain credentials. It may be used only in a debug build with redaction.

## 3. Run

```bash
xray run -config config.json
```

FastV starts the process directly, without a shell:

```text
xray.exe run -config C:\...\runtime\xray-config.json
```

The process must be bound to a Windows Job Object so it exits when the desktop application exits.

## 4. Generate Credentials

### UUID

```bash
xray uuid
xray uuid -i "custom-string"
```

Use UUID generation for new VLESS and VMess profiles. Do not use predictable or reused UUIDs.

### REALITY key pair

```bash
xray x25519
xray x25519 -i "server-private-key"
```

The server private key stays on the server. The client stores the corresponding public key in the REALITY outbound `password` field.

### Other tools

```bash
xray wg
xray mldsa65
xray mlkem768
xray vlessenc
```

FastV should expose these only when the relevant advanced feature is implemented.

## 5. API and Statistics

Recommended simple API configuration:

```json
{
  "api": {
    "tag": "api",
    "listen": "127.0.0.1:10085",
    "services": ["StatsService"]
  },
  "stats": {}
}
```

Query statistics:

```bash
xray api statsquery --server=127.0.0.1:10085
```

FastV should:

- bind the API to `127.0.0.1` only
- choose a random free port if 10085 is occupied
- poll no more than once per second
- tolerate API errors without stopping the proxy
- never expose the API port to the LAN

## 6. Logs

Xray log level is configured in the generated config:

```json
{
  "log": {
    "loglevel": "warning"
  }
}
```

Recommended levels:

| Mode | Level |
|---|---|
| Normal use | warning |
| Debugging a connection | info |
| Deep diagnostics | debug, temporary only |

The UI should allow changing the level for the next start, not rewrite a running core config.

## 7. Troubleshooting

### VMess connection fails

- Check that the system UTC time is within 120 seconds of the server.
- Check UUID and security method.
- Check that the server port and transport match.

### TLS handshake fails

- Check `serverName` and SNI.
- Check ALPN, especially for gRPC.
- Check certificate pinning.
- Do not recommend `allowInsecure` as a permanent fix.

### REALITY handshake fails

- Check `serverName`, public key (`password`), shortId and fingerprint.
- Confirm that the server's `serverNames` includes the chosen SNI.
- Confirm that REALITY is used with raw, xhttp or grpc.
- Confirm that the client and server core versions support the selected REALITY features.

### WebSocket connection fails

- Check path and Host.
- Check that the path starts with `/`.
- Check the reverse proxy upgrade configuration.
- Check TLS ALPN; WebSocket normally uses HTTP/1.1.

### gRPC connection fails

- Check serviceName.
- Check that HTTP/2 is enabled on the reverse proxy.
- Check that `h2` is first in ALPN when custom ALPN is used.
- Check that the domain resolves to the correct server.

### Local port already in use

- Try another port.
- Do not kill an unknown process automatically.
- Show the conflicting port and the error to the user.

### Core exits immediately

- Run `xray run -test -config`.
- Check the core log buffer.
- Check whether the binary hash matches the expected version.
- Check whether antivirus or endpoint protection blocked the process.

## 8. Version Compatibility Checklist

Before releasing a new pinned Xray-core version:

- [ ] Run every golden config through `xray -test`.
- [ ] Test VMess, VLESS, Trojan and Shadowsocks.
- [ ] Test RAW, WebSocket and gRPC.
- [ ] Test TLS and REALITY.
- [ ] Test Mux on and off.
- [ ] Test routing direct/proxy/block.
- [ ] Test DNS and sniffing.
- [ ] Test statistics API.
- [ ] Test Windows Job Object cleanup.
- [ ] Test system proxy restoration.
- [ ] Verify geodata compatibility.