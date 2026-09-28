# fsvc

`fsvc` is a single-binary CLI for the Freshservice private API (`/api/_/`),
authenticated with a session cookie. Built on
[Kong](https://github.com/alecthomas/kong) following the scaffold pattern from
[min](https://github.com/dat267/min).

One static binary per platform, no runtime dependencies.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/dat267/fsvc/main/install.sh | sh
```

Installs the latest release into `~/.local/bin`. Override the target with
`FSVC_INSTALL_DIR`, pin a release with `FSVC_VERSION=v1.0.0`.

### Manual download

Pick the asset for your platform from the
[releases page](https://github.com/dat267/fsvc/releases/latest) and put it on
`PATH`:

| Platform | Asset |
| --- | --- |
| Linux x86-64 | `fsvc_linux_amd64` |
| Linux arm64 | `fsvc_linux_arm64` |
| Linux i386 | `fsvc_linux_386` |
| macOS Intel | `fsvc_darwin_amd64` |
| macOS Apple silicon | `fsvc_darwin_arm64` |
| Windows x86-64 | `fsvc_windows_amd64.exe` |
| Windows arm64 | `fsvc_windows_arm64.exe` |

```sh
# Linux / macOS
curl -fsSL -o fsvc https://github.com/dat267/fsvc/releases/latest/download/fsvc_linux_amd64
chmod +x fsvc && mv fsvc ~/.local/bin/     # any directory on PATH

# Windows (curl.exe ships with Windows 10+)
curl.exe -fsSL -o fsvc.exe https://github.com/dat267/fsvc/releases/latest/download/fsvc_windows_amd64.exe
```

### From source

```sh
go install github.com/dat267/fsvc@latest
```

Check it works:

```sh
fsvc version
fsvc session    # OK: authenticated (visible tickets: 7)
```

## Quick start

Grab the `_itildesk_session` cookie from your browser: F12 → Application →
Cookies → Freshservice domain → copy the cookie value.

```bash
fsvc config set subdomain acme
fsvc config set itildesk-session "<your _itildesk_session value>"

fsvc session                                # verify
fsvc tickets classify                       # your unresolved tickets, 2 lists
fsvc tickets classify --include-unassigned  # + the unassigned backlog
fsvc tickets list --format json             # raw ticket list
fsvc tickets conversations 10100            # messages on a ticket
fsvc ticket-filters show 1100               # show a saved ticket filter
fsvc users show 2100                        # show a user
```

### Write commands

Mutations need a CSRF token: grab `X-CSRF-Token` from any POST in the DevTools
**Network** tab, then store it.

```bash
fsvc config set csrf-token "4oEDe-..."

fsvc tickets update 10100 status=4                  # resolve a ticket
fsvc tickets fill-start-dates -y                    # backfill planned_start_date
fsvc tickets push-end-dates 3 -y                    # bump due dates by 3 business days
fsvc tickets push-end-dates 3 --within-hours 24 -y  # also push dates due inside 24h
fsvc tickets sync-priority -y                       # sync priority from urgency+impact
fsvc tickets sync-urgency-impact -y                 # minimal urgency+impact per priority
```

Every mutation shows a preview first; `-y`/`--yes` skips the confirmation.

## Config

JSON, resolved as: `$FSVC_CONFIG_FILE` → `./fsvc.json` →
`~/.config/fsvc/fsvc.json` (Windows: `%AppData%\fsvc\fsvc.json`). Flags and
environment variables override the file.

| Key | Flag | Env | Purpose |
| --- | --- | --- | --- |
| `subdomain` | `--subdomain` | `FSVC_SUBDOMAIN` | e.g. `acme` |
| `itildesk-session` | `--itildesk-session` | `FSVC_ITILDESK_SESSION` | `_itildesk_session` cookie value |
| `csrf-token` | `--csrf-token` | `FSVC_CSRF_TOKEN` | CSRF token for write operations |
| `base-url` | `--base-url` | `FSVC_BASE_URL` | override base URL (default `https://<subdomain>.freshservice.com`) |
| `time-zone` | `--time-zone` | `FSVC_TZ` | timezone for business-day math (e.g. `Europe/London`) |
| `concurrency` | `--concurrency` | `FSVC_CONCURRENCY` | max in-flight requests (default 8) |

`fsvc config init|path|show|set|unset|edit` manage the file. Point at a mock
server with `--base-url http://127.0.0.1:PORT` for safe testing.

## Concurrency

Fanned-out work runs on a bounded worker pool:

- the per-ticket conversation scan in `tickets classify`
- the batch PUTs behind `fill-start-dates`, `push-end-dates`, `sync-priority`
  and `sync-urgency-impact`
- image downloads during `tickets export` and `tickets show`

The default is 8 in-flight requests. `--concurrency N` (`FSVC_CONCURRENCY`)
changes it; `--concurrency 1` forces strictly sequential requests. Downloads
keep request order, so concurrent exports are still deterministic.

Against a mock server holding each request for 10 ms, the 24-ticket scan
scales almost linearly:

| workers | wall time | speedup |
| --- | --- | --- |
| 1 | 268 ms | 1.0x |
| 2 | 132 ms | 2.0x |
| 4 | 66 ms | 4.1x |
| 8 | 34 ms | 7.8x |
| 16 | 25 ms | 10.8x |

Reproduce: `go test -run XXX -bench BenchmarkClassifyConversationScan ./cmd/`.

## Commands

### `fsvc session`

Verify the session cookie: `GET /api/_/tickets?per_page=1`.

### `fsvc tickets`

| Command | Purpose |
| --- | --- |
| `tickets list` | List tickets. `--filter <id>`, `--include`, `--order-by`, `--order-type`, `--page`, `--per-page`, `--format table\|json\|csv` |
| `tickets conversations <id>` | Conversations for a ticket. `--per-page`, `--include`, `--format` |
| `tickets classify` | Your unresolved tickets in two lists: stale agent response, customer responded. `--include-unassigned` adds the unassigned backlog (one extra request); `--older-than-days` (business days, default 2), `--query-json`, optional filter ID |
| `tickets show <id>` | Ticket and conversation trace as Markdown |
| `tickets export <id>` | Export to DOCX, Markdown, or HTML |
| `tickets fill-start-dates` | Backfill `planned_start_date` from `created_at` on your unresolved tickets. `-y` |
| `tickets push-end-dates` | Push `planned_end_date` to now + N business days. `[days]` (default 3), `--within-hours`, `-y` |
| `tickets sync-priority` | Sync priority from urgency+impact via the standard matrix. `-y` |
| `tickets sync-urgency-impact` | Set urgency+impact to the minimum pair satisfying the current priority. `-y` |
| `tickets update <id> key=value...` | Update a ticket. Dotted keys for nested fields, or `--body` for raw JSON |

### Other

| Command | Purpose |
| --- | --- |
| `ticket-filters show <id>` | Show a saved ticket filter |
| `users show <id>` | Show a user |
| `version` | Print the build version |

## Build

```bash
go build -ldflags="-X main.version=$(git describe --tags --always)" -o fsvc .
```

Releases are built by `.github/workflows/release.yml` on `v*` tags and attached
to the GitHub release.

## Dev

```bash
go run .
go test -race -count=1 ./...
go vet ./...
```

## API notes

This CLI targets the Freshservice **private API** (`/api/_/`), authenticated
with session cookies (not the public v2 API key). Endpoints and field shapes
were reverse-engineered; `docs/private-api-notes.md` holds the accumulated
knowledge.

## License

MIT — see [LICENSE](LICENSE)
