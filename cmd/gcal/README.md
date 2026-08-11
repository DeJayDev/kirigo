# gcal

Full Google Calendar event CRUD for agents: JSON in, JSON out, and every mutation snapshotted so it can be restored.

## Setup

Create a **Desktop** OAuth client in Google Cloud Console (any project with the Calendar API enabled), then put its credentials in kirigo's env:

```bash
printf 'GOOGLE_OAUTH_CLIENT_ID=...\nGOOGLE_OAUTH_CLIENT_SECRET=...\n' >> ~/.config/kirigo/.env
gcal setup --account work    # browser consent (scopes: calendar.events + calendar.readonly)
```

`setup` runs the installed-app OAuth flow: it serves a localhost callback and prints the consent URL, or use `--paste` to paste the redirected URL back (remote-safe, same as `gh-app-token setup`). The refresh token is stored at `~/.config/kirigo/gcal/<account>/token.json` (`0600`).

Multiple accounts live side by side; select one with `--account <name>` or `KIRIGO_GCAL_ACCOUNT`. With a single account it is used automatically; otherwise an account named `default` is used.

## Output format

Commands emit JSON by default. Pass `--format toon` (before or after the subcommand), or set `KIRIGO_FORMAT=toon`, to get [TOON](https://github.com/toon-format/toon-go) — a token-compact encoding of the same structure, aimed at LLM/agent contexts:

```bash
gcal --format toon calendars
KIRIGO_FORMAT=toon gcal list --from today --to 'in 7 days'
```

When run inside a known agent harness (any of `CLAUDECODE=1`, `CODEX_THREAD_ID`, `OPENCODE=1`, `KIRI=1`), output **defaults to TOON**. Precedence: `--format` flag > `KIRIGO_FORMAT` > agent autodetect > json. Set `KIRIGO_FORMAT=json` to force JSON everywhere — e.g. when piping to `jq`.

TOON mirrors the JSON shape exactly (same keys). For arrays of uniform objects it collapses to a header plus CSV rows — `calendars[10]{access_role,id,primary,summary,timezone}:` — filling any gaps with explicit `null` so rows stay uniform, and pipe-joining scalar arrays. Measured against Anthropic's token counter: **~56% fewer tokens** for `calendars`, **~44%** for `log`, **~37%** for a mixed `list`.

## Reading

```bash
gcal calendars                                            # ids, timezones, access roles
gcal list --from today --to 'in 7 days'                   # your week as JSON
gcal list --calendar primary --calendar team@x.com --from now --to tomorrow
gcal get <event-id>                                       # trimmed shape (gcal get <event-id> --raw for the full resource)
gcal freebusy --from 'tomorrow at 9am' --to 'tomorrow at 6pm'   # busy blocks + free windows
```

Times are natural language via [go-naturaldate](https://github.com/tj/go-naturaldate) — `now`, `today`, `tomorrow`, `next monday`, `in 2 hours`, `in 7 days`, `tomorrow at 3pm`, `10am` — plus ISO forms `2026-08-10` (a bare date makes an **all-day** event), `2026-08-10 15:30`, and full RFC3339. Bare/relative times resolve in the target calendar's timezone. For all-day events the end date is **exclusive** (as Google stores it): omit `-end` to get a single day, or set it to the day after the last day you want covered.

## Writing

```bash
gcal create --title "Focus" --start 'tomorrow at 3pm' --end 'tomorrow at 4pm'
gcal create --title "Trip" --start 2026-08-20 --end 2026-08-25    # all-day; end is exclusive -> covers Aug 20-24
gcal update --start 'in 1 hour' --end 'in 2 hours' <event-id>
gcal delete --confirm <event-id>                        # delete requires --confirm
gcal quickadd "Lunch with Sam tomorrow 12pm"
```

Flags and the positional `<event-id>`/`<op-id>` may appear in any order — `gcal get <event-id> --raw` and `gcal get --raw <event-id>` both work.

Any mutation takes `--dry-run` to preview the change without touching the calendar. This CLI never sets attendees, and never notifies them (`sendUpdates=none`); editing an event that already has attendees leaves them untouched. For a recurring event, writes default to the single instance; `--scope all` targets the whole series (`--scope following` is not supported in v1). Advanced fields go through `--json -` (a full Google event resource on stdin); flags override matching fields.

## Restore

```bash
gcal log                     # past mutations, newest first, each with an op_id
gcal restore <op-id>         # re-apply the pre-change state (or --last)
gcal prune --all             # or --before <time>; history is otherwise kept forever
```

Snapshots live under `~/.config/kirigo/gcal/<account>/backups/`. Restoring a **deleted** event re-creates it with a **new id** — Google tombstones a deleted event's id and rejects reuse, so the original id cannot be brought back.
