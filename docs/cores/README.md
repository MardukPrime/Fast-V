# Proxy Cores

FastV delegates all proxy protocol work to external proxy cores. This directory contains the client-side integration documentation for each supported core.

## Core List

| Core | Status | Role |
|---|---|---|
| [Xray-core](xray/README.md) | First core, primary | VMess, VLESS, Trojan, Shadowsocks, transports and routing |
| sing-box | Planned | Hysteria2, TUIC, WireGuard, TUN and additional protocols |

## Documentation Rules

- This directory describes how FastV integrates each core.
- The upstream project documentation is the source of truth for protocol and configuration semantics.
- FastV-specific rules, config generation, process management and UI behavior belong here.
- Upstream docs should be linked, not copied wholesale.
- Every core document must record the tested core version and the documentation snapshot date.