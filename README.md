# kirigo

Small, single-purpose Go binaries for agent and script workflows. Each tool lives under `cmd/<name>`. The `main` stays thin and keeps its logic in testable `internal/` packages.

## Goals

- **Machine-readable output.** Results and errors are structured for agents and scripts to parse, not prose to scrape. Output is JSON by default, or the token-compact TOON format on request (`-format toon` / `KIRIGO_FORMAT=toon`) and by default inside a known agent harness. `gh-app-token` is the exception: as a git credential helper it prints a bare token to stdout.
- **Thin `main`, testable core.** Flag parsing and exit codes live in `main`. Validation and logic live in `internal/` packages, behind injected dependencies (client interfaces, clocks, HTTP endpoints).
- **One place for config.** Credentials and environment variables load from `~/.config/kirigo/`. To load from a different env file, set `KIRIGO_ENV_FILE`. Any secrets a binary persists are written with mode `0600`, inside a `0700` directory.
- **Exit codes are a contract.** `2` is a config or validation error. `1` is a runtime or API error. `0` is ok.

## Binaries

- [commute](cmd/commute/README.md) — real-time driving times from the Google Maps Routes API (`TRAFFIC_AWARE`).
- [gh-app-token](cmd/gh-app-token/README.md) — scoped, short-lived GitHub App installation tokens. It also works as a git credential helper.
- [gcal](cmd/gcal/README.md) — full Google Calendar event CRUD, with restorable, snapshotted mutations.

## Build

```bash
make            # test, then build every cmd/* into bin/
make test       # go test ./...
make deploy     # build and install to ~/.local/bin
```

Override the install location with `PREFIX=/some/path make deploy` or `BINDIR=/some/path/bin make deploy`. To add a new tool, create a new `cmd/<name>` directory. `make` discovers it automatically.
