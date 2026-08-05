# Single-Package Refactor for fsvc

Date: 2026-08-05
Status: Approved

## Goal

Simplify the fsvc architecture for maintainability by collapsing the three
packages (`cmd`, `internal/biz`, `internal/fsapi`) into one `cmd` package.

## Motivation

- Too many layers / import indirection for a single-user personal CLI.
- Testing complexity: same-package tests currently can't reach fsapi/biz
  unexported state, forcing exported-only seams (e.g. `SetCSRF`).
- Dead code and duplication between raw (`Get`/`Put`) and typed
  (`ListTickets`/`LatestConversation`) API surfaces.

## Target layout (all `package cmd`)

| File | Source | Contents |
|---|---|---|
| `client.go` | `fsapi/client.go` + `cmd/client.go` | `ClientConfig`, `Client`, `New`, `Update`, `Do`/`Get`/`Put`, `CheckStatus`, `BaseURL`, `itildeskSessionCookie` |
| `models.go` | `fsapi/models.go` | `Ticket`, `Conversation`, `ParseTicket`, decode helpers |
| `api.go` | `fsapi/tickets.go` | `ListTickets`, `LatestConversation` (`UpdateTicket` dropped) |
| `values.go` | `fsapi/values.go` | `BuildBody`, `parseValue`, `setNested` |
| `businessdays.go` | `biz/businessdays.go` | business-day math |
| `matrix.go` | `biz/matrix.go` | priority matrices |
| `classify.go` | `biz/classify.go` | `Category`, `Classify` |
| *(unchanged)* | `cmd/*` | `tickets.go`, `config.go`, `output.go`, `runtime.go`, `root.go`, `session.go`, `users.go`, `ticketfilters.go` |

`main.go` stays a thin launcher calling `cmd.Execute`.

## Mechanics

- Delete `internal/` tree; move its files into `cmd/`.
- Remove `fsvc/internal/biz` and `fsvc/internal/fsapi` imports everywhere.
- All test files fold into `package cmd` (they are already same-package
  internal tests, so no rename).
- Dead code removed: `UpdateTicket`, `SetCSRF`. The one test using `SetCSRF`
  sets `client.csrf` directly (same-package access).
- Verified: no identifier collisions between the three packages.

## Result

One package, no import indirection, single `go build ./...` / `go test ./...`.
Files stay small and topically named.
