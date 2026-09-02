# fsvc

`fsvc` is a CLI for the Freshservice private API (`/api/_/`), authenticated
via session cookies. Built on [Kong](https://github.com/alecthomas/kong)
following the scaffold pattern from [min](https://github.com/dat267/min).

## Quick start

```bash
go install github.com/dat267/fsvc@latest

# Grab your _itildesk_session cookie from browser DevTools
#   F12 → Application → Cookies → Freshservice domain
#   Copy the value of the _itildesk_session cookie

# Configure
fsvc config set subdomain acme
fsvc config set itildesk-session "<your _itildesk_session value>"

# Verify
fsvc session
# OK: authenticated (visible tickets: 7)

# Use
fsvc tickets classify                     # your unresolved tickets in 3 lists
fsvc tickets list --format json             # raw ticket list
fsvc tickets conversations 10100            # messages on a ticket
fsvc ticket-filters show 1100               # show a saved ticket filter
fsvc users show 2100                        # show a user
```

### Write commands

Mutation commands need a CSRF token. Grab it from any POST request in the
DevTools **Network** tab (`X-CSRF-Token` header) and set it in config:

```bash
fsvc config set csrf-token "4oEDe-..."

# Then:
fsvc tickets update 10100 status=4                     # resolve a ticket
fsvc tickets fill-start-dates -y                       # backfill planned_start_date
fsvc tickets push-end-dates 3 -y                        # bump due dates by 3 business days
fsvc tickets push-end-dates 3 --within-hours 24 -y      # also push dates due inside 24h
fsvc tickets sync-priority -y                          # sync priority from urgency+impact
fsvc tickets sync-urgency-impact -y                    # set minimal urgency+impact for each priority
```

- Config lives at `~/.config/fsvc/fsvc.json` (or set `--subdomain`/`--itildesk-session` flags per invocation)
- Use `--time-zone Europe/London` for accurate business-day math on `classify` and `push-end-dates`
- Point at a mock server with `--base-url http://127.0.0.1:PORT` for safe testing

## Config

JSON file, same resolution as min: `$FSVC_CONFIG_FILE` env → `./fsvc.json` →
`~/.config/fsvc/fsvc.json`. Or set flags / env vars directly:

```bash
fsvc --subdomain acme --itildesk-session "..." session
```

| Key | Flag | Env | Purpose |
| --- | --- | --- | --- |
| `subdomain` | `--subdomain` | `FSVC_SUBDOMAIN` | e.g. `acme` |
| `itildesk-session` | `--itildesk-session` | `FSVC_ITILDESK_SESSION` | `_itildesk_session` cookie value |
| `csrf-token` | `--csrf-token` | `FSVC_CSRF_TOKEN` | CSRF token for write operations |
| `base-url` | `--base-url` | `FSVC_BASE_URL` | override base URL (default `https://<subdomain>.freshservice.com`) |
| `time-zone` | `--time-zone` | `FSVC_TZ` | timezone for business-day calculations (e.g. `Europe/London`) |

Config commands (init/path/show/set/unset/edit) are inherited from min.

## Commands

### `fsvc session`
Verify the session cookie works. Hits `GET /api/_/tickets?per_page=1`.

### `fsvc tickets`

| Command | Purpose |
| --- | --- |
| `tickets list` | List tickets. `--filter <id>`, `--include`, `--order-by`, `--order-type`, `--page`, `--per-page`, `--format table\|json\|csv` |
| `tickets conversations <id>` | List conversations for a ticket. `--per-page`, `--include`, `--format` |
| `tickets classify` | Categorize your unresolved tickets into 3 lists: unassigned, stale agent response, customer responded. `--older-than-days` (business days, default 2), `--query-json`, optional filter ID arg |
| `tickets fill-start-dates` | Backfill `planned_start_date` from `first_responded_at` on your unresolved tickets. Preview + confirm, `-y` to skip prompt |
| `tickets push-end-dates` | Push `planned_end_date` to now + N business days. `[days]` (default 3), `--within-hours` (push dates due inside window), `-y` |
| `tickets sync-priority` | Sync priority from urgency+impact using the standard priority matrix. `-y` |
| `tickets sync-urgency-impact` | Set urgency+impact to the minimum pair that satisfies the current priority (prefers raising urgency over impact). `-y` |
| `tickets update <id> key=value...` | Update a ticket. `key=value` pairs (dotted keys for nested), `--body` for raw JSON |

All mutation commands (`fill-start-dates`, `push-end-dates`, `sync-priority`, `sync-urgency-impact`, `update`) require a CSRF token for write operations. Preview is always shown; `-y`/`--yes` skips the confirmation prompt.

### `fsvc ticket-filters show <id>`

### `fsvc users show <id>`

### `fsvc version`

## Build

```bash
go build -ldflags="-X main.version=$(git describe --tags --always)" -o fsvc .
```

## Dev

```bash
go run .
go install .
go test -race -count=1 ./...
golangci-lint run ./...
```

## API notes

This CLI targets the Freshservice **private API** (`/api/_/`), authenticated
via session cookies (not the public v2 API key). Endpoints and field shapes
were reverse-engineered. See `docs/private-api-notes.md` for the accumulated
knowledge.

## License

MIT — see [LICENSE](LICENSE)
