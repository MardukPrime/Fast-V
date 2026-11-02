# Xray-core Integration

This document defines how FastV starts, validates and observes the Xray-core process. It is a client integration guide, not a replacement for the upstream Xray configuration reference.

## 1. Integration Boundary

```text
SolidJS UI
  -> Wails service
  -> Go application service
  -> Xray config builder
  -> Xray-core child process
  -> local SOCKS/HTTP-compatible inbound
  -> remote proxy server
```

FastV owns:

- profile data and secret storage
- outbound configuration generation
- local inbound and port allocation
- process lifecycle
- system proxy integration
- routing presets and UI
- log and statistics collection

Xray-core owns:

- protocol implementation
- transport and TLS/REALITY handshake
- DNS resolution inside the core
- routing execution
- Mux and XUDP behavior
- traffic counters

## 2. Binary Management

### Runtime location

```text
%APPDATA%\FastV\cores\xray\
├── xray.exe
├── version.json
└── LICENSE
```

The exact installed location is controlled by the application data directory. The binary is not read from the installation directory at runtime, so the application can update the core without requiring administrator rights.

### Version record

`version.json` should contain at least:

```json
{
  "version": "X.Y.Z",
  "sha256": "hex-encoded-sha256",
  "source": "https://github.com/XTLS/Xray-core/releases",
  "installedAt": "2026-11-02T00:00:00Z"
}
```

FastV must:

- pin a tested Xray-core version per application release
- verify the binary SHA-256 before first start
- refuse to launch a binary with an unexpected hash unless the user explicitly imports a custom core
- keep the previous working core version for rollback

### Supported commands

FastV uses these upstream commands:

```bash
xray version
xray run -test -config config.json
xray run -config config.json
xray uuid
xray x25519
xray api statsquery --server=127.0.0.1:10085
```

The generated configuration is validated with `-test` before every production start.

## 3. Configuration Generation

The builder must use structured Go types or maps and `json.MarshalIndent`. Do not concatenate JSON strings.

### Generation pipeline

```text
Profile
  -> validate domain fields
  -> decrypt credentials
  -> map protocol settings
  -> map transport settings
  -> map TLS or REALITY settings
  -> map routing and DNS
  -> produce complete Xray JSON
  -> write temporary file atomically
  -> run xray -test
  -> start process
```

### Stable tags

Use stable tags so routing rules and statistics can refer to them:

```text
local-in       local SOCKS/HTTP-compatible inbound
proxy          selected remote outbound
direct         freedom outbound
block          blackhole outbound
dns-out        DNS outbound
api            API service
```

Every tag must be unique. The first outbound is the default route when no routing rule matches.

### Local inbound

Xray documents the SOCKS inbound as compatible with SOCKS4, SOCKS4a, SOCKS5 and HTTP. FastV uses one local SOCKS inbound as the default local proxy entry:

```json
{
  "tag": "local-in",
  "listen": "127.0.0.1",
  "port": 10808,
  "protocol": "socks",
  "settings": {
    "auth": "noauth",
    "udp": true
  },
  "sniffing": {
    "enabled": true,
    "destOverride": ["http", "tls", "quic"],
    "routeOnly": false
  }
}
```

The system proxy uses the same local endpoint as an HTTP proxy:

```text
http://127.0.0.1:10808
```

Do not listen on `0.0.0.0` by default. LAN sharing is an explicit opt-in feature and must show a security warning.

### Complete skeleton

```json
{
  "log": {
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "tag": "local-in",
      "listen": "127.0.0.1",
      "port": 10808,
      "protocol": "socks",
      "settings": {
        "auth": "noauth",
        "udp": true
      },
      "sniffing": {
        "enabled": true,
        "destOverride": ["http", "tls", "quic"]
      }
    }
  ],
  "outbounds": [
    {
      "tag": "proxy",
      "protocol": "vless",
      "settings": {},
      "streamSettings": {}
    },
    {
      "tag": "direct",
      "protocol": "freedom"
    },
    {
      "tag": "block",
      "protocol": "blackhole"
    }
  ],
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": []
  }
}
```

The actual protocol and transport settings are documented in [profile-mapping.md](profile-mapping.md) and [transports.md](transports.md).

## 4. Process Lifecycle

### State machine

```text
Stopped
  -> Starting
  -> Running
  -> Stopping
  -> Stopped

Starting -> Failed
Running  -> Failed
```

### Start sequence

1. Acquire the proxy operation mutex.
2. Check that the selected profile exists and is enabled.
3. Verify the Xray binary and hash.
4. Allocate the local inbound port.
5. Decrypt the profile credentials.
6. Generate a complete configuration.
7. Write the config to a temporary file and atomically rename it.
8. Run `xray run -test -config`.
9. Start `xray.exe` with a hidden console window.
10. Bind the child process to a Windows Job Object with kill-on-close.
11. Poll the local inbound port until it accepts connections or the timeout expires.
12. Set the system proxy if automatic proxy mode is enabled.
13. Publish the Running state.

### Stop sequence

1. Stop accepting new UI commands.
2. Cancel pending requests.
3. Restore the system proxy.
4. Terminate the Xray process.
5. Wait for the process to exit.
6. Force kill after the timeout.
7. Publish the Stopped state.

### Windows process rules

- Use `CREATE_NO_WINDOW` to avoid a console window.
- Use `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` so Xray exits if FastV crashes.
- Read stdout and stderr asynchronously to avoid pipe deadlocks.
- Never use shell execution or string concatenation for command arguments.
- Record the process ID and start time for diagnostics.
- Do not start a second core instance while one is running.

## 5. Logs

Xray log levels in the generated config:

```json
{
  "log": {
    "loglevel": "warning",
    "error": ""
  }
}
```

FastV log behavior:

- Xray stdout and stderr go to a ring buffer.
- UI receives batches every 100 to 250ms.
- Sensitive values are redacted before they reach the UI or file log.
- The log viewer is virtualized.
- The default UI buffer holds the most recent 5000 lines.
- A full Xray config is never written to the application log.

## 6. Statistics API

Xray statistics require both `stats` and the API service. The recommended simple API configuration is:

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

FastV can query traffic with:

```bash
xray api statsquery --server=127.0.0.1:10085
```

Relevant counter names include:

```text
inbound>>>local-in>>>traffic>>>uplink
inbound>>>local-in>>>traffic>>>downlink
outbound>>>proxy>>>traffic>>>uplink
outbound>>>proxy>>>traffic>>>downlink
```

Implementation rules:

- The API listens only on `127.0.0.1`.
- Use a random high port when 10085 is occupied.
- Poll at most once per second.
- Push aggregated snapshots to the UI.
- Statistics failure must not stop the proxy.
- Do not expose the API to the LAN.

## 7. Error Mapping

| Xray condition | FastV error code |
|---|---|
| Binary missing or hash mismatch | `CORE_NOT_FOUND` |
| `xray -test` failure | `CORE_TEST_FAILED` |
| Local port unavailable | `PORT_CONFLICT` |
| Process exits before ready | `CORE_START_FAILED` |
| Readiness timeout | `CORE_START_TIMEOUT` |
| Process exits unexpectedly | `CORE_CRASHED` |
| System proxy update failed | `SYSTEM_PROXY_FAILED` |
| Invalid profile fields | `PROFILE_INVALID` |

The UI should show a short localized message and a details action. It must not display raw file paths, UUIDs or credentials.

## 8. Security Rules

- The local inbound binds to `127.0.0.1` by default.
- The API inbound binds to `127.0.0.1` only.
- Decrypted credentials exist only in memory during config generation.
- Generated config files are stored in a per-user runtime directory.
- Temporary config files are removed after the core exits.
- Core downloads must be verified by SHA-256.
- Custom core import requires an explicit user action and a warning.
- Logs must redact passwords, UUIDs, subscription URLs and private keys.