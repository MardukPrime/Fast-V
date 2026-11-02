# Xray Client Examples

The examples below are documentation fixtures. Replace all placeholders before use. They are not production credentials.

Both fixtures were validated with Xray-core v26.10.31 using `xray run -test -config`. The REALITY public key is a generated documentation fixture and must be replaced.

Raw fixtures:

- [vless-reality.json](examples/vless-reality.json)
- [vless-websocket-tls.json](examples/vless-websocket-tls.json)

## 1. VLESS + REALITY + Vision

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
        "realitySettings": {
          "serverName": "example.com",
          "fingerprint": "chrome",
          "password": "DjzVfKBE3XZv1fWeu1jDBacmMPsRJYXMFvMZFmM2Njs",
          "shortId": "0123456789abcdef",
          "spiderX": "/"
        }
      }
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
    "rules": [
      {
        "type": "field",
        "ip": ["geoip:private"],
        "outboundTag": "direct"
      }
    ]
  }
}
```

## 2. VLESS + WebSocket + TLS

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
      "settings": {
        "address": "example.com",
        "port": 443,
        "id": "00000000-0000-0000-0000-000000000000",
        "encryption": "none"
      },
      "streamSettings": {
        "method": "websocket",
        "security": "tls",
        "tlsSettings": {
          "serverName": "example.com",
          "alpn": ["http/1.1"],
          "fingerprint": "chrome"
        },
        "wsSettings": {
          "path": "/ws",
          "host": "example.com"
        }
      }
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

## 3. VLESS + gRPC + TLS

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
      }
    }
  ],
  "outbounds": [
    {
      "tag": "proxy",
      "protocol": "vless",
      "settings": {
        "address": "example.com",
        "port": 443,
        "id": "00000000-0000-0000-0000-000000000000",
        "encryption": "none"
      },
      "streamSettings": {
        "method": "grpc",
        "security": "tls",
        "tlsSettings": {
          "serverName": "example.com",
          "alpn": ["h2", "http/1.1"]
        },
        "grpcSettings": {
          "serviceName": "my-service",
          "multiMode": false
        }
      }
    },
    {
      "tag": "direct",
      "protocol": "freedom"
    },
    {
      "tag": "block",
      "protocol": "blackhole"
    }
  ]
}
```

## 4. Routing Preset

```json
{
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {
        "type": "field",
        "ip": ["geoip:private"],
        "outboundTag": "direct"
      },
      {
        "type": "field",
        "domain": ["geosite:category-ads-all"],
        "outboundTag": "block"
      },
      {
        "type": "field",
        "network": "tcp,udp",
        "outboundTag": "proxy"
      }
    ]
  }
}
```

## 5. DNS Example

```json
{
  "dns": {
    "servers": [
      "1.1.1.1",
      "https://dns.google/dns-query"
    ],
    "queryStrategy": "UseIP",
    "disableCache": false
  }
}
```

## 6. Validation Checklist for Examples

- `xray run -test -config` must pass.
- Tags must be unique.
- The first outbound is the default route.
- The local inbound must bind to `127.0.0.1`.
- REALITY is only used with raw, xhttp or grpc.
- VLESS `encryption` is explicitly set.
- gRPC ALPN includes `h2` when needed.
- No real credentials are committed to the repository.