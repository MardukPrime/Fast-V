# FastV

FastV is a desktop proxy client built with Go, Wails v3 and SolidJS. It is designed around the core workflows of v2rayN: profile management, share-link import, subscriptions, proxy lifecycle control, system proxy integration, routing, latency testing and log inspection.

The project does not implement proxy protocols itself. Xray-core is used as the primary proxy core, with sing-box planned for additional protocols and TUN support.

## Status

The repository is currently in the design and architecture phase.

- Product and system design: [docs/DESIGN.md](docs/DESIGN.md)
- Architecture and directory rules: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- Documentation index: [docs/README.md](docs/README.md)

## Planned Stack

| Layer | Choice |
|---|---|
| Desktop | Wails v3 |
| Backend | Go |
| Frontend | SolidJS + TypeScript + Vite |
| Database | SQLite |
| Proxy core | Xray-core (primary), sing-box (planned) |
| Packaging | Wails build + Inno Setup/NSIS |

## Architecture

The project uses a modular monolith with clear dependency boundaries:

```text
cmd/fastv
  -> internal/bootstrap
  -> internal/transport/wails
  -> internal/application
  -> internal/domain

internal/infrastructure -> implements application interfaces
internal/platform       -> operating-system integrations
```

Key rules:

- `cmd` contains only startup code.
- `internal/bootstrap` is the single composition root.
- `internal/domain` contains pure business models and rules.
- `internal/application` contains use cases and consumer-owned interfaces.
- `internal/infrastructure` contains SQLite, proxy-core, HTTP and crypto adapters.
- `internal/transport/wails` contains Wails services, DTOs and event mapping.
- `internal/platform` isolates Windows-specific integrations.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the complete tree and dependency rules.

## Documentation

```text
docs/
├── README.md
├── DESIGN.md
├── ARCHITECTURE.md
├── adr/
├── api/
├── operations/
└── testing/
```

## License

GPL-3.0. See [LICENSE](LICENSE).
