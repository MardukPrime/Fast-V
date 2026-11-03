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
- Added a TUN mode switch under client network takeover, using the Xray TUN inbound with Wintun, administrator preflight and automatic Windows route setup/cleanup.
- Corrected the previous Xray TUN limitation and added Xray TUN configuration validation.
- Theme selection now previews immediately and follows system theme changes when set to system mode.
- Settings major sections are now collapsible and remember their open/closed state.
- Added node config extraction/editing, Xray outbound JSON preview, advanced JSON override, quick node switching and save-and-apply.
- Added ordered routing rule editing mapped to Xray routing rules, with automatic proxy restart.
- Added Xray runtime log capture, live streaming, filtering, pause, auto-scroll, copy and clear.
- Hardened IP detection with ipwho.is self fallback, alternate IP endpoints, request retries and a clearer unreachable-interface error.
- Upgraded the Go module/toolchain to Go 1.27.3.
- Added direct and proxied network diagnostics with exit IP, geolocation, ASN, Cloudflare edge, latency, jitter and optional download speed.
- Removed external detection-site links; only directly integrated network diagnostics remain.
- Enabled WebView2 GPU acceleration by default and raised small typography/contrast for clearer rendering.
- Reduced glass blur and edge blending to improve panel and text edge crispness.