# Quarter-Hour Rounding for Planned Dates Design

Date: 2026-08-13

## Summary

`fsvc tickets fill-start-dates` and `fsvc tickets push-end-dates` currently set
`planned_start_date` / `planned_end_date` to exact-second timestamps, which
looks obviously automated. Round both computed dates **up to the next quarter
hour** (`:00`/`:15`/`:30`/`:45`) in the timestamp's own timezone so the written
values look human-set.

## Design

### 1. New helper — `roundUpQuarterHour`

Add to `cmd/businessdays.go`:

```go
func roundUpQuarterHour(t time.Time) time.Time
```

- Operates on the timestamp's wall clock in its own `Location`.
- If the time already falls exactly on a quarter-hour boundary (minute % 15 == 0
  and seconds == 0), returns it unchanged.
- Otherwise rounds up to the next boundary (`12:07:30 → 12:15:00`,
  `12:15:30 → 12:30:00`, `12:59:59 → 13:00:00`).
- `time.Date` normalization handles hour/day rollover (e.g. `23:59:59 → 00:00:00`
  next day).

### 2. fill-start-dates

`cmd/tickets.go:454` — replace `t.CreatedAt.Format(time.RFC3339)` with
`roundUpQuarterHour(t.CreatedAt).Format(time.RFC3339)`.

### 3. push-end-dates

`cmd/tickets.go:481` — replace `AddBusinessDays(base, c.Days).Format(...)` with
`roundUpQuarterHour(AddBusinessDays(base, c.Days)).Format(...)`.

Rounding applies only to the value written; the `WithinHours` / existing-date
comparison logic is unchanged.

## Tests

- `cmd/businessdays_test.go` — new `TestRoundUpQuarterHour`:
  - exact boundary unchanged (`12:15:00` stays)
  - mid-quarter rounds up (`12:07:30 → 12:15:00`)
  - boundary-with-seconds rounds up (`12:15:30 → 12:30:00`)
  - hour rollover (`12:59:59 → 13:00:00`)
  - day rollover (`23:59:59 → 00:00:00` next day)
  - timezone preserved (a `+05:30` timestamp stays `+05:30`)
- `cmd/tickets_test.go` — add non-quarter created_at for fill-start-dates and
  assert the rounded value reaches the PUT body. Existing tests use exact
  quarter/known timestamps and continue to pass.

## Out of scope

- No opt-out flag (always on, per user).
- No change to business-day arithmetic, `WithinHours`, or preview/apply flow.
- No rounding of `tickets update` or other commands.

## Verification

- `go build ./...`, `go test ./...`, `go vet ./...`, golangci-lint.