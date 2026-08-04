# fsvc

`fsvc` is a CLI for the Freshservice private API (`/api/_/`), authenticated
via session cookies. Built on [Kong](https://github.com/alecthomas/kong)
following the scaffold pattern from [min](https://github.com/dat267/min).

## Quick start

```bash
go install github.com/dat267/fsvc@latest

# Configure
fsvc config set subdomain acme
fsvc config set cookie "helpdesk_node_session=..."

# Verify
fsvc session
# OK: authenticated (visible tickets: 7)
```

## Config

JSON file, same resolution as min: `$FSVC_CONFIG_FILE` env → `./fsvc.json` →
`~/.config/fsvc/fsvc.json`. Or set flags / env vars directly:

```bash
fsvc --subdomain acme --cookie "..." session
```

| Key | Flag | Env | Purpose |
| --- | --- | --- | --- |
| `subdomain` | `--subdomain` | `FSVC_SUBDOMAIN` | e.g. `acme` |
| `cookie` | `--cookie` | `FSVC_COOKIE` | raw session cookie string |
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
| `tickets categorize` | Categorize your unresolved tickets into 3 lists: unassigned, stale agent response, customer responded. `--older-than-days` (business days, default 2), `--query-json`, optional filter ID arg |
| `tickets fill-start-dates` | Backfill `planned_start_date` from `first_responded_at` on your unresolved tickets. Preview + confirm, `-y` to skip prompt |
| `tickets fill-end-dates` | Bulk-set `planned_end_date` to now + N business days. Bumps past-due dates too. `--days` (default 3), `-y` |
| `tickets sync-priority` | Sync priority from urgency+impact using the standard priority matrix. `-y` |
| `tickets sync-urgency-impact` | Set urgency+impact to the minimum pair that satisfies the current priority (prefers raising urgency over impact). `-y` |
| `tickets update <id> key=value...` | Update a ticket. `key=value` pairs (dotted keys for nested), `--body` for raw JSON |

All mutation commands (`fill-start-dates`, `fill-end-dates`, `sync-priority`, `sync-urgency-impact`, `update`) require a CSRF token for write operations. Preview is always shown; `-y`/`--yes` skips the confirmation prompt.

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
