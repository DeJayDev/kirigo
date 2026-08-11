# commute

Fetches real-time driving commute data from the Google Maps Routes API using `TRAFFIC_AWARE` routing. JSON in, JSON out.

## Usage

```bash
mkdir -p ~/.config/kirigo
printf 'GOOGLE_MAPS_API_KEY=...\n' > ~/.config/kirigo/.env

go run ./cmd/commute \
  --origin "123 Main St, SF" \
  --destination "1 Ferry Building, SF"
```

## Flags

- `--origin` required origin address/place/lat,lng.
- `--destination` required destination address/place/lat,lng.
- `--mode` travel mode: `driving` (default), `transit`, `walking`, `bicycling`.
- `--alternatives` include alternative routes.
- `--arrival` arrive-by time (`HH:MM`, `3:04pm`, or RFC3339) — computes when to leave (`depart_at`).
- `--departure` depart-at time (default now).
- `--waypoint` intermediate stop, ordered, repeatable.
- `--tolls` include toll cost when the route has tolls (driving).
- `--segments` include per-leg times and traffic-speed spans — see where it's slow in one call (driving).
- `--format` output format: `json` (default) or `toon`.

## Examples

```bash
commute --origin "123 Main St, SF" --destination "1 Ferry Building, SF"
commute --origin "1 Ferry Building, SF" --destination "123 Main St, SF" --arrival 17:30    # when do I leave?
commute --origin "123 Main St, SF" --destination "1 Ferry Building, SF" --mode transit
commute --origin "123 Main St, SF" --destination "PNC Burlingame" --waypoint "Copper Chimney" --alternatives
commute --origin "123 Main St, SF" --destination "1 Ferry Building, SF" --segments          # which stretch is jammed
```

`--arrival` is native for transit; for driving it iterates `departureTime` against traffic until `depart_at + duration` lands on your arrival (a few API calls). All-scalar knobs are additive — the plain two-arg call still returns the original shape plus `mode`.

Environment is loaded from the first existing file at `~/.config/kirigo/.env`, `~/.config/kirigo/env`, or `~/.config/kirigo/kirigo.env`. Set `KIRIGO_ENV_FILE` to point at a different file.

Output is JSON by default; `--format toon` (or `KIRIGO_FORMAT=toon`) emits the same data as [TOON](https://github.com/toon-format/toon-go) — ~26% fewer tokens on a commute result. Inside a known agent harness (`CLAUDECODE`, `CODEX_THREAD_ID`, `OPENCODE`, `KIRI`) output defaults to TOON; `KIRIGO_FORMAT=json` forces JSON. This is shared with every kirigo binary via `internal/output`.
