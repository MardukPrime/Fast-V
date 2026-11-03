# FastV

FastV is a desktop proxy client built with Go, Wails v3 and SolidJS. It follows the core workflows of v2rayN: profile management, share-link import, subscriptions, proxy lifecycle control, system proxy integration, routing, latency testing and log inspection.

The project does not implement proxy protocols itself. Xray-core is used as the primary proxy core, including Xray TUN mode on Windows. sing-box is planned for additional protocols and cross-platform TUN support.

## Status

Milestone M3: dashboard, subscriptions, node management, Xray-core updates, local proxy start/stop and proxy testing are functional.

Implemented:

- Wails v3 application shell with a source-only repository policy.
- Dashboard summary with subscription and node counts.
- Subscription add, update, update-all, delete and node listing.
- Base64 subscription fetching and common share-link parsing.
- Basic settings for theme, language, proxy mode and subscription update interval.
- Local JSON persistence under the user data directory with a workspace fallback.
- Unit tests for the parser, subscription service, settings service and core updater.
- Frameless Wails window with a custom draggable title bar and window controls.
- Client and core settings are separated; Xray-specific parameters only appear when Xray-core is selected.
- Xray-core version detection, latest release check, SHA-256 verified download and local installation.
- Automatic core update setting with startup check and download.
- Xray config generation from the active node, proxy mode and Xray runtime settings.
- Xray process start/stop with Windows Job Object cleanup.
- Local SOCKS/HTTP inbound and proxy test through the local inbound.
- Settings now drive the generated config: local port, log level, Mux, sniffing and routing mode.
- Overview shows the running node protocol, server address, resolved IP, subscription, local inbound, proxy mode and PID after startup.
- TUN mode uses the Xray TUN inbound on Windows, with Wintun, administrator preflight and automatic system route setup/cleanup.
- Network diagnostics for direct and proxied exit IP, geolocation, ASN, Cloudflare edge, latency and optional download speed.
- Browser-only WebRTC/DNS leak, IP purity and anonymity scoring are intentionally not linked or scraped.

## Quick Start

### Prerequisites

- Go 1.27+
- Node.js 20+
- pnpm 10+
- Wails v3 CLI (`wails3`)
- Microsoft Edge WebView2 Runtime
- Windows 10/11 for the current primary target

### Install frontend dependencies

```bash
pnpm --dir frontend install
```

### Generate Wails bindings

```bash
wails3 generate bindings -clean=true -ts -i
```

### Development

```bash
wails3 dev
```

### Production build

Build assets are intentionally not tracked in this repository. A local Wails v3 build setup is required before running:

```bash
wails3 build
```

### Frontend checks

```bash
pnpm --dir frontend run typecheck
pnpm --dir frontend run build
```

### Go checks

```bash
go build .
go test .
go vet .
```

## Project Structure

The project uses a root Wails entrypoint, a composition root, feature-oriented internal packages, and a Vite frontend.

```text
fastV/
├── main.go                         # Wails entrypoint and embedded frontend assets
├── internal/
│   ├── bootstrap/
│   │   └── app.go                  # Composition root: Wails app, services, window
│   ├── buildinfo/
│   │   └── buildinfo.go            # Product/version metadata
│   ├── model/
│   │   └── models.go               # Shared domain models and frontend DTOs
│   ├── store/
│   │   └── store.go                # Local JSON persistence
│   ├── subscription/
│   │   ├── fetcher.go              # Subscription HTTP fetch
│   │   ├── parser.go               # Share-link and subscription parser
│   │   └── nodes.go                # Parsed node builder
│   ├── core/
│   │   ├── manager.go              # Xray version detection and update lifecycle
│   │   ├── release.go              # GitHub release and asset selection
│   │   └── installer.go            # Download, SHA-256, unzip and install
│   ├── proxy/
│   │   ├── builder.go              # Active node/share-link to Xray config
│   │   ├── manager.go              # Process lifecycle, local inbound and proxy test
│   │   ├── process_windows.go      # Windows Job Object integration
│   │   ├── tun_windows.go          # Xray TUN and Windows route setup
│   │   ├── tun_other.go            # Non-Windows TUN placeholder
│   │   └── process_other.go        # Non-Windows process stubs
│   ├── diagnostics/
│   │   └── checker.go              # Direct/proxied IP and network quality probes
│   └── service/
│       ├── app/service.go          # AppService
│       ├── core/service.go         # CoreService Wails adapter
│       ├── dashboard/service.go    # DashboardService
│       ├── diagnostics/service.go  # DiagnosticsService
│       ├── node/service.go         # NodeService
│       ├── proxy/service.go        # ProxyService
│       ├── settings/service.go     # SettingsService
│       └── subscription/service.go # SubscriptionService
├── frontend/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── components/
│   │   ├── pages/
│   │   └── styles.css
│   ├── bindings/                   # Wails generated TypeScript bindings
│   └── package.json
├── test/fixtures/                  # Local subscription test fixtures
├── docs/
├── go.mod
├── go.sum
└── README.md
```

Wails embeds `frontend/dist` from the module root, so `main.go` stays at the root.
## Repository Policy

This repository tracks source code and documentation only. Build configuration, platform assets, packaged binaries, generated distributions and local tooling are excluded.

Not tracked:

```text
build/
Taskfile.yml
.github/workflows/
bin/
frontend/dist/
frontend/node_modules/
.fastv-data/
.tools/
*.exe
*.syso
```

## Planned Stack

| Layer | Choice |
|---|---|
| Desktop | Wails v3.1 |
| Backend | Go |
| Frontend | SolidJS + TypeScript + Vite |
| Persistence | Local JSON now, SQLite planned |
| Proxy core | Xray-core (primary), sing-box (planned) |
| Packaging | Wails build + Inno Setup/NSIS |

## Architecture

The application uses a modular monolith with a Wails transport adapter:

```text
SolidJS pages
  -> generated Wails bindings
  -> internal/service/*
  -> internal/model / internal/store / internal/subscription / internal/core
```

### Service adapters

- `AppService`: application metadata.
- `DashboardService`: aggregate counts, active node and recent subscriptions.
- `SubscriptionService`: add, update, delete, list and inspect nodes.
- `NodeService`: search/filter nodes, select the active node, edit node configs, preview Xray outbound JSON, toggle enabled state, delete nodes and run direct TCP latency checks.
- `ProxyService.SwitchNode`: switch the active node and automatically restart the running proxy.
- `SettingsService`: read, save and reset client and core settings.
- `ProxyService`: start/stop Xray, expose proxy state and test the local inbound.
- `CoreService`: detect the installed Xray-core version, check GitHub releases, verify SHA-256 and install updates.

### Internal modules

- `internal/bootstrap`: the only composition root.
- `internal/model`: shared domain models and frontend DTOs.
- `internal/store`: persistence boundary; currently JSON, replaceable with SQLite.
- `internal/subscription`: parser, fetcher and node builder.
- `internal/core`: Xray version detection, release lookup and installer.
- `internal/proxy`: share-link to Xray config builder, process lifecycle and local proxy testing.
- `internal/service`: thin Wails adapters, one package per feature.

The service adapter layer does not contain protocol parsing or file-format details. `internal/core` does not import Wails.
## Documentation

- [Documentation index](docs/README.md)
- [Product and system design](docs/DESIGN.md)
- [Architecture and directory rules](docs/ARCHITECTURE.md)
- [UI design rules](docs/ui/DESIGN_RULES.md)
- [Proxy core documentation](docs/cores/README.md)
- [Xray-core client integration](docs/cores/xray/README.md)
- [Architecture decision records](docs/adr/README.md)
- [Subscription and node testing](docs/testing/subscription-test.md)
- [Network diagnostics and IP tool assessment](docs/network-diagnostics.md)
- [Node config editor](docs/cores/xray/node-editor.md)

## License

GPL-3.0. See [LICENSE](LICENSE).
