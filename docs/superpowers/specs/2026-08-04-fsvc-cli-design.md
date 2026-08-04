# fsvc — Freshservice private-API CLI

## Overview

`fsvc` is a Go CLI for the **private** (session-cookie-authenticated) Freshservice
API. It targets endpoints under `/api/_/` on the customer's Freshservice
instance. The command structure and config machinery follow the scaffolding
tool `github.com/dat267/min` (Kong-based CLI, JSON config file, `Run() error`
command structs). Endpoint shapes are derived from HAR captures supplied by the
user in `har/`.

Authenticated commands must never be pointed at the real instance during
development or automated testing. All verification uses a local mock HTTP
server. The `PUT` (ticket update) flow in particular is only ever exercised
against mocks.

## Context and source material

Six HAR captures in `har/` document the following requests against
`acme.freshservice.com`:

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/api/_/ticket_filters/1100` | returns `{ "ticket_filter": {...} }` |
| GET | `/api/_/tickets` | query: `filter`, `include`, `order_by`, `order_type`, `page`, `per_page`, `query_hash`, `advanced_query_hash`; returns `{ "tickets": [...], "meta": {...} }` |
| GET | `/api/_/tickets/10100/conversations` | query: `include`, `per_page`; returns `{ "conversations": [...], "meta": {...} }` |
| GET | `/api/_/tickets/10100/requested_items/7101` | query: `view`; returns `{ "business_rules": ..., "requested_item": {...} }` |
| GET | `/api/_/users/2105` | returns `{ "user": {...} }` |
| PUT | `/api/_/tickets/10100` | body: `{ "priority", "group_id", "responder_id", "department_id", "br_validation_excludes" }`; requires `X-CSRF-Token` header; returns `{ "meta": {...}, "ticket": {...} }` |

Key observed facts:

- Private API path prefix is `/api/_/`.
- Response header `x-freshservice-api-version: latest=v2; requested=private`.
- Auth is a `Cookie` header carrying session cookies
  (`_itildesk_session`, `helpdesk_node_session`, and others). The server
  rotates `_itildesk_session` via `Set-Cookie` on responses.
- Writes also require an `X-CSRF-Token` header.

## Goals

- A Kong-based Go CLI mirroring the `min` project structure.
- Generic HTTP client for the private API: configurable base URL, cookie
  passthrough with cookie-jar rotation handling, optional CSRF header, clear
  error mapping.
- One command per captured endpoint.
- Human-friendly output: markdown table by default, with `json` and `csv`
  formats available via a flag.
- A flexible field-update mechanism so any known JSON key can be updated
  without adding per-field CLI flags.
- All tests run against local `httptest` mock servers. No real Freshservice
  traffic during development or testing.

## Non-goals (scaffold phase)

- The public `/api/v2` API.
- Asset/device/service-catalog features beyond the captured endpoints.
- `br_validation_excludes` generation (JWT); it is opaque and passed through
  only if the user supplies it via `--body`.

## Project layout

```
/workspace/                     (git repo, Go module "fsvc")
  go.mod / go.sum
  main.go                       package main; calls cmd.Execute(context.Background())
  version.go                    var version = "dev" (ldflags -X main.version=...)
  cmd/
    root.go                     CLI struct, SetAppName("fsvc"), VersionCmd
    runtime.go                  from min: App, Execute, config-path resolution, JSONResolver
    config.go                   from min: config command group (init/path/show/set/unset/edit)
    client.go                   resolves config -> builds *fsapi.Client, binds into Kong
    session.go                  SessionCmd
    tickets.go                  TicketsCmdGroup: list, conversations, requested-items, update
    ticketfilters.go            TicketFiltersCmd: show
    users.go                    UsersCmd: show
    output.go                   markdown table / json / csv renderers
  internal/fsapi/
    client.go                   Client: base URL, cookie jar, CSRF, Do/GetJSON, CheckStatus
    values.go                   generic key=value -> JSON body builder (dotted keys, typed values)
    client_test.go
    values_test.go
  testdata/                     fixture JSON bodies derived from HARs
  .golangci.yml                 copied from min
  .gitignore                    copied from min
```

## Configuration

JSON config file; path resolution identical to min:

1. `FSVC_CONFIG_FILE` env var
2. `./fsvc.json`
3. `~/.config/fsvc/fsvc.json`

Keys (also exposed as root flags and env vars):

| Key | Flag | Env | Purpose |
| --- | --- | --- | --- |
| `subdomain` | `--subdomain` | `FSVC_SUBDOMAIN` | e.g. `acme`; used to build the default base URL |
| `cookie` | `--cookie` | `FSVC_COOKIE` | raw session cookie string, sent verbatim as the `Cookie` header |
| `csrf-token` | `--csrf-token` | `FSVC_CSRF_TOKEN` | optional; sent as `X-CSRF-Token` on writes |
| `base-url` | `--base-url` | `FSVC_BASE_URL` | optional override; default `https://<subdomain>.freshservice.com` |

Config takes precedence ordering: command-line flag > env > config file >
default. `config` command group is copied from min unchanged.

The cookie value is a secret. It is stored in the config file as the user
requested, but the scaffold notes in help text that env-var / flag overrides
exist.

## API client (`internal/fsapi`)

`New(cfg ClientConfig) *Client`:

- `ClientConfig{ Subdomain, Cookie, CSRF, BaseURL string }`
- base URL resolved from `BaseURL` if set, else `https://<subdomain>.freshservice.com`
- `http.Client` with a `cookiejar.Jar` so `Set-Cookie` rotations
  (`_itildesk_session`) are captured and replayed on subsequent calls
- 30-second per-request timeout

`Do(ctx, method, path, query url.Values, body io.Reader) (*http.Response, error)`:

- URL = base + `/api/_/` + path, with query params appended
- sets `Cookie: <raw cookie string>` (verbatim), `Accept: application/json`,
  `Content-Type: application/json; charset=utf-8` when a body is present,
  `X-CSRF-Token` when configured
- `CheckStatus(resp)` returns nil for 2xx; otherwise a wrapped error including
  status code and a short body snippet. 401/403 map to a hint:
  "session cookie invalid or expired (run `fsvc config set cookie ...`)".

`GetJSON(ctx, path, query, out any) error` — GET + decode into `out`.

The client keeps one public seam for testability: the base URL is
configurable, so commands can be exercised against `httptest` servers and the
mock server is usable by end users too (`--base-url http://localhost:PORT`).

## Flexible field updates (`internal/fsapi/values.go`)

A generic builder converts `key=value` pairs into a JSON object:

- split each pair on the first `=`
- values are parsed as JSON when valid (`1` -> number, `true` -> bool,
  `null`, `[1,2]`, `{"a":1}`); otherwise kept as string
- dotted keys produce nested objects: `custom_fields.x=y` ->
  `{"custom_fields":{"x":"y"}}`
- duplicate keys: later pairs win
- exposed as `BuildBody(pairs []string) ([]byte, error)`; reusable by any
  future write endpoint, not just ticket update

`--body <raw-json>` (on update commands) bypasses the builder and sends the
string verbatim, allowing opaque values like `br_validation_excludes` and full
payload overrides. When `--body` is present, `key=value` pairs are ignored.

## Commands

### `fsvc session`
GET `/api/_/tickets?per_page=1`. Prints whether the session works (HTTP status,
`meta.count` if parseable). Non-2xx is an error. This is the cookie-verification
command.

### `fsvc tickets list`
GET `/api/_/tickets`.

Flags:
- `--filter <id>` (view/filter id)
- `--include <comma-list>` (e.g. `stats,responder,requester,ticket_states,ticket_status,group`)
- `--order-by <field>` (default `created_at`)
- `--order-type <desc|asc>` (default `desc`)
- `--page <n>` (default 1)
- `--per-page <n>` (default 30)

Default output: markdown table with columns
`id | subject | status | priority | requester | group_id | created_at`.

### `fsvc tickets conversations <id>`
GET `/api/_/tickets/<id>/conversations`.

Flags: `--include <comma-list>`, `--per-page <n>` (default 3).

Default output: markdown table with columns
`id | user_id | incoming | private | created_at | body_text`.

### `fsvc tickets requested-items <id> <item-id>`
GET `/api/_/tickets/<id>/requested_items/<item-id>`.

Default output: markdown table of the `requested_item` top-level scalar keys.

### `fsvc tickets update <id> [key=value ...]`
PUT `/api/_/tickets/<id>`.

- positional `key=value` pairs → JSON body via `BuildBody`
- `--body <raw-json>` sends a verbatim payload
- `--csrf-token <token>` overrides the config value for this call
- `--format` applies to the returned ticket
- requires at least one `key=value` pair or `--body`; otherwise a usage error
  ("nothing to update")

Any known ticket JSON key can be updated (e.g. `status`, `priority`,
`group_id`, `responder_id`, `department_id`, nested `custom_fields.*`).
This command is only ever run against a local mock during development/tests.

### `fsvc ticket-filters show <id>`
GET `/api/_/ticket_filters/<id>`.

### `fsvc users show <id>`
GET `/api/_/users/<id>`.

### `fsvc config ...` / `fsvc version`
Copied from min unchanged.

## Output rendering

- Default: markdown table. First row is a header row; second row is a
  `---` separator (GitHub-flavored).
- `--format table|json|csv`:
  - `json`: the raw response body (already compact/pretty JSON)
  - `csv`: header row + comma-separated values, quoted when containing commas
- Each command defines its own column list (see per-command sections above).
  Columns may reference nested JSON fields via dotted paths (e.g.
  `requester.name`).
  Rendering lives in `cmd/output.go` and is shared.

## Error handling

- Missing `subdomain`/`cookie` when a command needs them: clear error hinting
  `fsvc config set subdomain ...` / `fsvc config set cookie ...`.
- `--base-url` without a subdomain: allowed (subdomain unused).
- Network/timeouts: wrapped error.
- Non-2xx: `HTTP <code>: <body snippet>`; 401/403 additionally carry the
  "session cookie invalid or expired" hint.
- Invalid `key=value` pairs (no `=`) or invalid `--body` JSON: usage error.

## Testing

- Every HTTP interaction is tested against `httptest` servers. **No test, and
  no development verification, contacts a real Freshservice instance.**
- The update (PUT) command is tested solely against a mock server that replays
  the captured PUT response shape; the test asserts method, path, headers
  (`X-CSRF-Token`, `Content-Type`), and body contents.
- `testdata/` holds fixture JSON derived from the HAR response bodies.
- Unit tests: `values_test.go` (parsing, nesting, types, precedence),
  `client_test.go` (URL building, cookie header, jar rotation, CSRF header,
  401/403 mapping), command tests against mock servers, config tests.
- Session logic and command `Run` bodies are factored into callable functions
  so they can be unit-tested without running Kong.
- Verification commands: `go vet ./...`, `go test -race -count=1 ./...`,
  `golangci-lint run ./...` (config copied from min).

## Build/version

- `version.go`: `var version = "dev"`, overridable via
  `go build -ldflags="-X main.version=<tag>"`.
- Module path `fsvc` (single segment; fine for local builds, trivial to rename).
