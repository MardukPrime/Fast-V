# Xray-core Upstream References

This page indexes the upstream Xray documentation used to prepare the FastV client integration notes.

- Source: https://xtls.github.io/document/
- Documentation snapshot: 2026-11-02
- Example validation: Xray-core v26.10.31
- Machine-readable index: https://xtls.github.io/llms.txt
- Upstream documentation is authoritative and may change independently of FastV.

## Getting Started

- [Quick start](https://xtls.github.io/document/)
- [Installation](https://xtls.github.io/document/install.html)
- [Configuration and running](https://xtls.github.io/document/config.html)
- [Command arguments](https://xtls.github.io/document/command.html)

## Base Configuration

- [Log](https://xtls.github.io/config/log.html)
- [API](https://xtls.github.io/config/api.html)
- [DNS](https://xtls.github.io/config/dns.html)
- [FakeDNS](https://xtls.github.io/config/fakedns.html)
- [Inbound](https://xtls.github.io/config/inbound.html)
- [Outbound](https://xtls.github.io/config/outbound.html)
- [Transport](https://xtls.github.io/config/transport.html)
- [Policy](https://xtls.github.io/config/policy.html)
- [Routing](https://xtls.github.io/config/routing.html)
- [Statistics](https://xtls.github.io/config/stats.html)
- [Geodata](https://xtls.github.io/config/geodata.html)

## Outbound Protocols

- [VLESS](https://xtls.github.io/config/outbounds/vless.html)
- [VMess](https://xtls.github.io/config/outbounds/vmess.html)
- [Trojan](https://xtls.github.io/config/outbounds/trojan.html)
- [Shadowsocks](https://xtls.github.io/config/outbounds/shadowsocks.html)
- [Socks](https://xtls.github.io/config/outbounds/socks.html)
- [HTTP](https://xtls.github.io/config/outbounds/http.html)
- [Freedom](https://xtls.github.io/config/outbounds/freedom.html)
- [Blackhole](https://xtls.github.io/config/outbounds/blackhole.html)

## Inbound Protocols

- [Socks](https://xtls.github.io/config/inbounds/socks.html)
- [HTTP](https://xtls.github.io/config/inbounds/http.html)
- [VLESS](https://xtls.github.io/config/inbounds/vless.html)
- [VMess](https://xtls.github.io/config/inbounds/vmess.html)
- [Trojan](https://xtls.github.io/config/inbounds/trojan.html)
- [Shadowsocks](https://xtls.github.io/config/inbounds/shadowsocks.html)
- [TUN](https://xtls.github.io/config/inbounds/tun.html)

## Transports

- [RAW](https://xtls.github.io/config/transports/raw.html)
- [WebSocket](https://xtls.github.io/config/transports/websocket.html)
- [gRPC](https://xtls.github.io/config/transports/grpc.html)
- [HTTPUpgrade](https://xtls.github.io/config/transports/httpupgrade.html)
- [XHTTP](https://xtls.github.io/config/transports/xhttp.html)
- [mKCP](https://xtls.github.io/config/transports/mkcp.html)
- [TLS](https://xtls.github.io/config/transports/tls.html)
- [REALITY](https://xtls.github.io/config/transports/reality.html)
- [Sockopt](https://xtls.github.io/config/transports/sockopt.html)

## Upstream Notes That Affect FastV

- Xray documents the SOCKS inbound as compatible with SOCKS4, SOCKS4a, SOCKS5 and HTTP.
- VLESS requires transport security in public networks unless VLESS Encryption is enabled or the peer is a trusted private address.
- VLESS `encryption` must not be empty. Use `"none"` when disabled.
- Trojan should use TLS in public networks.
- VMess depends on UTC time and requires clock accuracy within 120 seconds.
- REALITY supports only raw, xhttp and gRPC.
- Xray's routing rules are evaluated from top to bottom and the first match wins.
- The first outbound is the default route when no routing rule matches.
- The Xray API should bind to localhost and be protected from external access.
- `xray run -test` is the required preflight validation step.

## Documentation Maintenance

When updating FastV's Xray integration:

1. Pin the new Xray-core version.
2. Re-read the upstream docs for changed fields.
3. Update the FastV integration pages.
4. Run all golden config tests with `xray -test`.
5. Update the snapshot date in this document.
6. Record the change in `CHANGELOG.md` or an ADR when the decision is architectural.