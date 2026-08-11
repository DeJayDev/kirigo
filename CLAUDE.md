# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

- `make build` / `make` — build all `cmd/*` binaries into `bin/` (build depends on `test`).
- `make deploy` — build and install binaries to `~/.local/bin` (override with `PREFIX=` or `BINDIR=`).
- `make test` — `go test ./...`
- Single test: `go test ./internal/commute -run TestName`
- Run a binary directly: `go run ./cmd/commute --origin "..." --destination "..."`

## Architecture

A monorepo of small Go CLI binaries. Each binary lives under `cmd/<name>` and is a thin `main` that wires together `internal/` packages. `make` auto-discovers binaries via `wildcard cmd/*`, so adding a new tool is just a new `cmd/<name>` directory — no Makefile edit.

`commute` and `gcal` are the fullest examples of the intended layering (`gh-app-token` follows the same shape under `internal/ghapp`):

- `cmd/<name>/main.go` — flag parsing, exit codes, and error rendering only. All machine-readable output (results and errors, including startup errors) goes through `internal/output`. Exit codes: `2` config/validation error, `1` runtime/API error, `0` ok.
- `internal/commute`, `internal/gcal` — pure domain logic behind an injected client interface and clock (`now func() time.Time`), so they test without HTTP. In commute, `Config.Normalized()` is the single validation gate, called in `main` and defensively inside `Lookup`. These packages own the public result shape.
- `internal/googlemaps` — thin transport for the Google Routes REST endpoint (`v2:computeRoutes`, `TRAFFIC_AWARE`): marshals the official `routingpb` types with `protojson`, `RoutesFieldMask` builds the `X-Goog-FieldMask`, `NewClientWithHTTP` injects a test server.
- `internal/googlecal` — gcal's Calendar client, built on the official `google.golang.org/api/calendar/v3` discovery client.
- `internal/configenv` — loads env from the first existing file among `~/.config/kirigo/{.env,env,kirigo.env}`, or `$KIRIGO_ENV_FILE`. Missing files are not errors.
- `internal/output` — the output writer: JSON (default, raw 2-space) or TOON (`-format toon`, `KIRIGO_FORMAT`, or agent-context autodetect, via `toon-go`). Route all machine-readable output through `output`.

### Conventions

- **Official Google types, never hand-rolled.** For any Google API, consume Google's generated types — never hand-write request/response structs. The transport mechanism differs by what Google ships for that API, and that is expected:
  - **Calendar** (gcal) → the discovery client `google.golang.org/api/calendar/v3` (batteries included: transport, pagination, auth).
  - **Routes** (commute) → the proto types `cloud.google.com/go/maps/routing/apiv2/routingpb` marshaled with `protojson` over a thin REST call in `internal/googlemaps` — the official Routes client is gRPC and ~3M heavier per binary, so we take its types but write our own lean transport.
  - The invariant across the repo: **types are always Google's; transport is whatever keeps the binary lean.** Follow the same rule when adding a new Google-API tool.
- Binaries emit JSON (or TOON) only, for agent/script consumption — both results and errors. Route new output through `internal/output`.
- Keep `main` thin: validation and logic belong in an `internal/` package unit-tested with injected dependencies (client interface, clock, HTTP endpoint).
