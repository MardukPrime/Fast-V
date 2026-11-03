# Changelog

All notable changes to FastV are documented here.

## [0.1.0] - 2026-10-31

### Added

- Initial Wails v3 + Go project scaffold.
- SolidJS frontend with generated Wails bindings.
- Application metadata service.
- Source-only repository policy; build assets and platform packaging are local-only.
- Architecture, design, ADR, API, operations and testing documentation.
- Go and frontend verification commands documented in the README.
- Xray-core client integration documentation, including validated example configs.
- UI design rules for layout, components, pages, routing editor and accessibility.
- Dashboard, subscription management and basic settings are implemented and running.
- Liquid-glass UI refresh with restrained translucency, tighter typography, navigation icons, dashboard metrics and theme previews.
- Xray-core version detection, GitHub release check, SHA-256 verified download, local install and optional automatic updates.
- Frameless window mode with a custom title bar and window controls.
- Liquid-glass surface refinement with first-class light and dark themes and improved dark-mode text contrast.
- Node management with search, filters, active node selection, enable/disable, deletion and direct TCP latency testing.
- Subscription updates preserve node identity and enabled state through stable source keys.
- Settings split into client settings and core settings, with Xray-specific parameters shown conditionally.
- Backend refactored into a root entrypoint, composition root, feature packages and Wails service adapters.
- Added Xray config generation for VMess, VLESS, Trojan and Shadowsocks nodes.
- Added Xray start/stop, local SOCKS/HTTP inbound, Windows Job Object cleanup and proxy testing through the local inbound.
- Wired proxy mode, local port, log level, Mux and sniffing settings into the generated Xray config.
- Added subscription auto-update scheduler driven by settings.
- Added running-node details to the overview, including resolved IP, protocol, subscription, inbound and PID.
- Added a TUN mode switch under client network takeover, gated for the current Xray-core implementation.
- Upgraded the Go module/toolchain to Go 1.27.3.