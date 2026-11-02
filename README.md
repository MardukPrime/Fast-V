# FastV

FastV is a desktop proxy client built with Go, Wails v3 and SolidJS. It is designed around the core workflows of v2rayN: profile management, share-link import, subscriptions, proxy lifecycle control, system proxy integration, routing, latency testing and log inspection.

The project does not implement proxy protocols itself. Xray-core is used as the primary proxy core, with sing-box planned for additional protocols and TUN support.

## Status

Milestone M0: project scaffold.

- Wails v3 desktop shell is wired.
- Go backend uses a thin root entrypoint and an `internal/bootstrap` composition root.
- SolidJS frontend is integrated with generated Wails bindings.
- Application metadata service is available to the UI.
- Build assets and platform packaging files are kept local and excluded from version control.
- Product and architecture documents live under `docs/`.

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
go test ./internal/...
go vet ./internal/...
```

## Project Structure

```text
fastV/
├── main.go                         # Wails entrypoint and embedded frontend assets
├── internal/
│   ├── bootstrap/                  # Application assembly and composition root
│   ├── buildinfo/                  # Product/version metadata
│   └── transport/
│       └── wails/
│           ├── dto/                # Frontend-facing data transfer objects
│           └── services/           # Wails v3 services
├── frontend/
│   ├── src/                        # SolidJS application
│   ├── bindings/                   # Generated TypeScript bindings
│   └── package.json
├── docs/                           # Design, architecture, ADR and operations docs
├── go.mod
└── README.md
```

The root `main.go` is intentionally minimal. Wails embeds `frontend/dist` from the module root, so the entrypoint stays at the root while application wiring lives in `internal/bootstrap`.

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
| Database | SQLite |
| Proxy core | Xray-core (primary), sing-box (planned) |
| Packaging | Wails build + Inno Setup/NSIS |

## Architecture

The project uses a modular monolith with clear dependency boundaries:

```text
main.go
  -> internal/bootstrap
  -> internal/transport/wails
  -> internal/application
  -> internal/domain

internal/infrastructure -> implements application interfaces
internal/platform       -> operating-system integrations
```

Key rules:

- `main.go` contains only asset embedding and startup.
- `internal/bootstrap` is the single composition root.
- `internal/domain` contains pure business models and rules.
- `internal/application` contains use cases and consumer-owned interfaces.
- `internal/infrastructure` contains SQLite, proxy-core, HTTP and crypto adapters.
- `internal/transport/wails` contains Wails services, DTOs and event mapping.
- `internal/platform` isolates Windows-specific integrations.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full dependency rules.

## Documentation

- [Documentation index](docs/README.md)
- [Product and system design](docs/DESIGN.md)
- [Architecture and directory rules](docs/ARCHITECTURE.md)
- [UI design rules](docs/ui/DESIGN_RULES.md)
- [Proxy core documentation](docs/cores/README.md)
- [Xray-core client integration](docs/cores/xray/README.md)
- [Architecture decision records](docs/adr/README.md)

## License

GPL-3.0. See [LICENSE](LICENSE).