# fsvc

`fsvc` is a single-binary CLI for the Freshservice private API (`/api/_/`),
authenticated with a session cookie. It does three things:

- `tickets overview` — your unresolved tickets, split into *waiting on
  customer* and *awaiting agent*
- `tickets fill-start-dates` — autofill `planned_start_date` from `created_at`
- `tickets push-end-dates` — update `planned_end_date` to now + N business days

Built on [Kong](https://github.com/alecthomas/kong). One static binary per
platform, no runtime dependencies.

## Install

Download the asset for your platform from the
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

## Setup

Grab the `_itildesk_session` cookie from your browser: F12 → Application →
Cookies → Freshservice domain → copy the cookie value.

```bash
fsvc config set subdomain acme
fsvc config set itildesk-session "<your _itildesk_session value>"
fsvc session
```

Mutations need a CSRF token: grab `X-CSRF-Token` from any POST in the DevTools
**Network** tab and store it.

```bash
fsvc config set csrf-token "4oEDe-..."
```

Business-day math follows `--time-zone` (an IANA name); without it the account
timezone is inferred from the ticket dates.

## Commands

### `fsvc tickets overview`

Your unresolved tickets in two lists: *waiting on customer* (last message is
yours and the customer has not replied for `--older-than-days` business days,
default 2) and *awaiting agent* (the last message is theirs).

```bash
fsvc tickets overview
fsvc tickets overview --older-than-days 5
fsvc tickets overview --include-unassigned     # + the unassigned backlog
fsvc tickets overview --query-json '{"filter":"123"}'
```

| Flag | Purpose |
| --- | --- |
| `--older-than-days` | Business days waiting on the customer before flagging (default 2) |
| `--include-unassigned` | Also list unassigned tickets (not your queue; costs one extra request) |
| `--query-json` | Raw JSON query params for the tickets endpoint |
| `[filter]` | Optional ticket filter/view ID positional argument |
| `--page`, `--per-page` | Paging for the underlying list calls |

### `fsvc tickets fill-start-dates`

Backfill `planned_start_date` from `created_at` (rounded up to the next quarter
hour) on your unresolved tickets that have none.

```bash
fsvc tickets fill-start-dates         # preview, then confirm
fsvc tickets fill-start-dates -y      # skip the confirmation prompt
```

### `fsvc tickets push-end-dates`

Set `planned_end_date` to now + N business days on your unresolved tickets.

```bash
fsvc tickets push-end-dates 3                     # preview, then confirm
fsvc tickets push-end-dates 3 -y                   # skip the prompt
fsvc tickets push-end-dates 3 --within-hours 24    # also push dates due inside 24h
fsvc tickets push-end-dates 3 --end-hour 17        # land on a preferred hour
```

### Other

| Command | Purpose |
| --- | --- |
| `fsvc session` | Verify the session cookie |
| `fsvc config path\|show\|set\|unset` | Manage the configuration file |
| `fsvc version` | Print the build version |

Every mutation previews the changes first (`[field] ticket <id>: <from> -> <to>`)
and asks for confirmation; `-y`/`--yes` skips it.

## Concurrency

The per-ticket conversation scan behind `tickets overview` and the batch PUTs
behind the two date commands run on a bounded worker pool, 8 requests in
flight by default. `--concurrency N` (`FSVC_CONCURRENCY`) changes it;
`--concurrency 1` forces strictly sequential requests.

Against a mock server holding each request for 10 ms, a 24-ticket scan scales
almost linearly:

| workers | wall time | speedup |
| --- | --- | --- |
| 1 | 268 ms | 1.0x |
| 2 | 132 ms | 2.0x |
| 4 | 66 ms | 4.1x |
| 8 | 34 ms | 7.8x |
| 16 | 25 ms | 10.8x |

Reproduce: `go test -run XXX -bench BenchmarkClassifyConversationScan ./cmd/`.

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

Point at a mock server with `--base-url http://127.0.0.1:PORT` for safe testing.

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
