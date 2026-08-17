# AGENTS.md

Cross-platform daemon exposing multiple PUQU AQ20-compatible BLE label printers as direct-addressed driverless IPP printers. One Go binary owns Bluetooth, IPP, SQLite, per-printer queues, and an embedded local React administration UI.

## Architecture

- `cmd/puqu-ipp/` — cobra CLI, Koanf bootstrap loading, diagnostics, hardware test print, native service management.
- `internal/config/` — immutable process bootstrap config from TOML, environment, and CLI. Listener/data/log settings live here, not SQLite.
- `internal/fleet/` — configured printer runtimes and driver registry. One `printer.Manager` per enabled logical printer.
- `internal/ipp/` — IPP gateway routing stable `/ipp/<slug>` paths to isolated queues. No mDNS/DNS-SD discovery.
- `internal/raster/` — pure PWG Raster and JPEG decoding. Exact 203 dpi profile dimensions; no silent scaling.
- `internal/printer/` — one physical printer connection, reconnect loop, serialized print/cancel flow.
- `internal/ble/` — device-agnostic native BlueZ/CoreBluetooth/WinRT central. Adapter scans serialize; connections coexist.
- `internal/puqu/` — pure reverse-engineered PUQU wire protocol.
- `internal/store/` — SQLite business state through ncruces/go-sqlite3, goose, and sqlc.
- `internal/admin/` — local JSON management interface. No IPP protocol logic.
- `web/` — React 19, TanStack Router/Query, Tailwind CSS v4 via `@tailwindcss/vite`.
- `internal/web/` — build-tag-gated embedded SPA.

Dependencies point inward: delivery modules call `fleet`, `store`, `printer`, and `raster`; `fleet` owns printer managers; `printer` calls `ble` and `puqu`.

## Invariants

- A logical printer has one stable slug/UUID, one driver, zero or one device, and one label profile.
- A physical device belongs to at most one printer. Profiles may be shared.
- Each printer has an isolated ordered queue; different printers may print concurrently.
- Jobs belong to exactly one printer and never move between queues.
- IPP accepts PWG Raster and JPEG only. Clients connect directly to `ipp://HOST:PORT/ipp/<slug>`; CUPS may sit above the bridge when discovery or format conversion is needed.
- Bootstrap config precedence is CLI > environment > TOML > defaults. Default file: OS user config directory `puqu-ipp/config.toml`; admin listen stays loopback-only.
- SQLite stores mutable business state and job history, never process bootstrap settings.
- Documents are bounded at 16 MiB; each printer queue capacity is 32.
- Restart aborts uncertain pending/processing jobs; never replay unknown hardware state.

## Commands

```bash
mise install
mise run setup
mise run dev
mise run build
mise run test
mise run vet
mise run web:typecheck
mise run ci
mise run sqlc
```

Hardware commands: `mise run discover`, `mise run print`, `mise run smoke`.

## Conventions

- Go: gofmt; comments only for protocol and non-obvious lifecycle behavior.
- TypeScript: strict mode, TanStack Query owns server state, TanStack Router owns navigation, Tailwind v4 owns UI styling.
- Tests run without hardware. IPP tests submit encoded messages through `httptest`; fleet boundaries use fakes.
- SQL source lives in `internal/store/queries/`; regenerate `internal/store/sqlitedb/` with query changes.
- Shipped schema changes get a new numbered goose migration. During an unshipped migration branch, keep its up/down path internally consistent.
- Exported symbol changes migrate every caller in one cutover; no compatibility shims.

## Verification

- IPP: targeted Go tests; CUPS `ipptool` for operation/attribute changes.
- Config: test defaults/file/env/CLI precedence, strict unknown-key rejection, and service config-path persistence.
- UI: typecheck/build, then exercise changed routes in a browser at desktop and mobile widths.
- BLE/hardware: use `discover`, `print`, or `smoke`; state untested platforms and hardware explicitly.
