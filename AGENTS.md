# AGENTS.md

Cross-platform daemon exposing PUQU AQ20 BLE label printers as driverless IPP printers. One Go binary owns Bluetooth, IPP, DNS-SD, SQLite, queueing, and an embedded local-only React administration UI.

## Architecture

- `cmd/puqu-ipp/` — cobra CLI: default/`serve`, BLE diagnostics, hardware test print, and native service management through `kardianos/service`.
- `internal/ipp/` — IPP delivery module, persistent job state, in-memory document queue, DNS-SD, printer/job attributes, and read-only network printer page.
- `internal/raster/` — pure PWG Raster, Apple Raster, and JPEG decoding to PUQU-ready 1bpp bitmaps. Exact 203 dpi profile dimensions only; never scale silently.
- `internal/printer/` — single active BLE printer, reconnect loop, serialized print/cancel flow.
- `internal/ble/` — device-agnostic tinygo Bluetooth central. Native BlueZ/CoreBluetooth/WinRT; no raw HCI.
- `internal/puqu/` — pure reverse-engineered PUQU wire protocol.
- `internal/store/` — SQLite through `ncruces/go-sqlite3`, goose migrations, sqlc queries. Configuration and job history persist; interrupted active jobs become aborted on startup.
- `internal/admin/` — local JSON administration interface. No IPP logic.
- `web/` — small React 19 configuration UI. No label editor and no browser printing path.
- `internal/web/` — build-tag-gated embedded SPA. `web:build` writes `internal/web/dist`; only `-tags embed` includes it.

Dependencies point inward: delivery modules (`admin`, `ipp`, `cmd`) call `store`, `printer`, and `raster`; `printer` calls `ble` and `puqu`; pure protocol/raster modules know nothing about transports or storage.

## Invariants

- System clients print through IPP; browser never submits labels.
- One active BLE connection and one serialized hardware print flow.
- Active label profile defines exact media dimensions and PUQU settings.
- IPP accepts PWG Raster and JPEG; Apple Raster only when AirPrint is enabled.
- DNS-SD always advertises `_ipp._tcp,_print`; AirPrint additionally advertises `_universal` and URF.
- Admin listener defaults to loopback. IPP listener is unauthenticated; document trusted-LAN exposure clearly.
- Request documents are bounded at 16 MiB and queue capacity at 32.
- Service restart aborts uncertain pending/processing jobs; never replay unknown hardware state.
- Configuration updates validate loopback-only admin addresses before persistence.

## Commands

```bash
mise install
mise run setup            # pnpm install --frozen-lockfile
mise run dev              # Go daemon + Vite
mise run build            # embedded production binary: bin/puqu-ipp
mise run test             # go test ./...
mise run vet              # go vet ./...
mise run web:typecheck
mise run ci
mise run sqlc             # regenerate internal/store/sqlitedb
```

Hardware commands: `mise run discover`, `mise run print`, `mise run smoke`.

## Conventions

- Go: `gofmt`; minimal comments for protocol and non-obvious lifecycle behavior only.
- TypeScript: strict mode, no extra state library, native fetch and semantic HTML.
- Tests live beside code and run without hardware. Printer tests use fake links/printers; IPP tests submit real encoded IPP messages through `httptest`.
- SQL source lives in `internal/store/queries/`; generated `internal/store/sqlitedb/` changes with it.
- New schema changes require a new numbered goose migration; never rewrite a migration already shipped.
- Exported symbol changes require all callers updated in the same cutover; no compatibility shims.
- Do not reintroduce the removed `apps/` browser-editor architecture, gin/SSE/TanStack/Fabric dependencies, Moon/Proto, or browser-localStorage configuration.

## Verification

- IPP behavior: targeted Go tests plus CUPS `ipptool` when changing attributes/operations.
- DNS-SD: browse `_print._sub._ipp._tcp` and, when enabled, `_universal._sub._ipp._tcp`.
- UI: build/typecheck, then exercise the changed flow in a browser.
- BLE/hardware behavior: use `discover`, `print`, or `smoke`; do not claim macOS/Windows/iOS hardware validation without running it there.
