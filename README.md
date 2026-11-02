# FastV

FastV is a desktop proxy client built with Go, Wails v3 and SolidJS. It follows the core workflows of v2rayN: profile management, share-link import, subscriptions, proxy lifecycle control, system proxy integration, routing, latency testing and log inspection.

The project does not implement proxy protocols itself. Xray-core is used as the primary proxy core, with sing-box planned for additional protocols and TUN support.

## Status

Milestone M1: dashboard, subscriptions and basic settings are functional.

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

## Quick Start

### Prerequisites

- Go 1.26+
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

The project follows the Wails v3 service-based layout: a root `main.go`, root service files, and a Vite frontend.

```text
fastV/
├── main.go                     # Wails entrypoint and embedded frontend assets
├── appservice.go               # Application metadata service
├── dashboardservice.go         # Dashboard aggregate service
├── subscriptionservice.go      # Subscription fetching and node management
├── nodeservice.go               # Node listing, selection, enablement and TCP checks
├── settingsservice.go          # Basic settings service
├── coreservice.go              # Xray-core detection, download and install
├── models.go                   # Frontend-facing models
├── store.go                    # Local JSON persistence
├── parser.go                   # Subscription and share-link parser
├── parser_test.go
├── subscription_service_test.go
├── settings_service_test.go
├── core_service_test.go
├── node_service_test.go
├── core_download_test.go
├── frontend/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── pages/
│   │   │   ├── DashboardPage.tsx
│   │   │   ├── SubscriptionsPage.tsx
│   │   │   ├── SettingsPage.tsx
│   │   │   └── PlaceholderPage.tsx
│   │   └── styles.css
│   ├── bindings/               # Generated TypeScript bindings
│   └── package.json
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

The application uses Wails v3 services as the boundary between Go and TypeScript:

```text
SolidJS pages
  -> generated Wails bindings
  -> root Go services
  -> Store / Parser / HTTP client / Xray updater
```

Services:

- `AppService`: application metadata.
- `DashboardService`: aggregate counts and recent subscriptions.
- SubscriptionService: add, update, delete, list and inspect nodes.
- NodeService: search/filter nodes, select the active node, toggle enabled state, delete nodes and run direct TCP latency checks.
- SettingsService: read, save and reset basic settings.
- CoreService: detect the installed Xray-core version, check GitHub releases, verify SHA-256 and install updates.

The current store is a small local JSON file. It is intentionally isolated behind `Store` so it can be replaced with SQLite without changing the frontend contract.

## Documentation

- [Documentation index](docs/README.md)
- [Product and system design](docs/DESIGN.md)
- [Architecture and directory rules](docs/ARCHITECTURE.md)
- [UI design rules](docs/ui/DESIGN_RULES.md)
- [Proxy core documentation](docs/cores/README.md)
- [Xray-core client integration](docs/cores/xray/README.md)
- [Architecture decision records](docs/adr/README.md)
- [Subscription and node testing](docs/testing/subscription-test.md)

## License

GPL-3.0. See [LICENSE](LICENSE).