# fsvc

PowerShell toolkit for the Freshservice **private API** (`/api/_/`), authenticated
with your browser session cookie. Each script is standalone: copy one file, edit
the CONFIG block (or set shared `FSVC_*` environment variables), and run. No
modules to install, no external dependencies.

> The original Go implementation is preserved on the **`archive/go-cli`** branch.

## Requirements

- Windows PowerShell 5.1+ or PowerShell 7+ (`pwsh`)
- A `_itildesk_session` cookie value from your browser (DevTools → Application →
  Cookies → your Freshservice domain)
- An `X-CSRF-Token` from DevTools → Network → any write request, for scripts that
  update tickets

The private API is undocumented and reverse-engineered; it can change without
warning. See [`docs/private-api-notes.md`](docs/private-api-notes.md) for the
accumulated knowledge.

## Install

The installer copies every script to a stable folder (default `~/fsvc`),
optionally adds it to `PATH`, and can store shared configuration in your
PowerShell profile so all scripts pick it up:

```powershell
pwsh scripts/Install-FSvc.ps1 -AddToPath `
     -Subdomain acme -Session "<cookie>" -CsrfToken "<token>" -LogPath "C:\logs\fsvc.log"

# later: remove the folder and the profile block
pwsh scripts/Install-FSvc.ps1 -Uninstall
```

- `-Destination` chooses the folder (default `~/fsvc`); `-Force` overwrites.
- `-ProfilePath` targets a specific profile (useful for testing).
- The profile block is delimited by markers and replaced on every run, so it
  never duplicates and your other profile content is untouched.

## Quick start

Configure once per session with environment variables, or edit the CONFIG block
at the top of each script.

```powershell
$env:FSVC_SUBDOMAIN        = "acme"
$env:FSVC_ITILDESK_SESSION = "<your _itildesk_session value>"
$env:FSVC_CSRF_TOKEN       = "<your X-CSRF-Token value>"   # write scripts only

# Triage overview: unassigned + waiting on customer + awaiting agent
pwsh scripts/Get-TicketOverview.ps1

# One ticket with its conversation trace (default prints; -AsObject pipes)
pwsh scripts/Get-TicketContent.ps1 -Id 10100
./scripts/Get-TicketContent.ps1 -Id 10100 -AsObject | ConvertTo-Json -Depth 10

# List tickets by saved filter id or raw query_hash
pwsh scripts/Get-TicketList.ps1

# Bulk date hygiene
pwsh scripts/Fill-PlannedStartDates.ps1
pwsh scripts/Update-PlannedEndDates.ps1
```

Everything is a preview first: write scripts print the planned changes and ask
before applying (unless auto-confirmed).

## Scripts

| Script | Purpose | Key knobs |
| --- | --- | --- |
| `Install-FSvc.ps1` | Install/copy the scripts, optionally add to `PATH` and persist shared config; `-Uninstall` removes both | `-Destination`, `-AddToPath`, `-Force`, `-Uninstall`, `-ProfilePath` |
| `Get-TicketList.ps1` | List tickets by saved-filter ID or raw `query_hash`, rendered as a table | `$FilterId` / `$QueryHash` (exactly one), `$Properties`, `$PerPage` |
| `Get-TicketContent.ps1` | Show one ticket and its full conversation trace; `-AsObject` emits `{ Ticket, Conversations }` for piping | `-Id`, `-AsObject` |
| `Get-TicketOverview.ps1` | Three-list triage: unassigned (customizable conditions), waiting on customer > N business days, awaiting agent | `$UnassignedQueryHash`, `$AssignedQueryHash`, `$OlderThanDays` |
| `Fill-PlannedStartDates.ps1` | Fill a null `planned_start_date` from `created_at`, rounded up to the quarter hour | `$Filter`, `$Confirm`, `$NonInteractive`, `$LogPath` |
| `Update-PlannedEndDates.ps1` | Set `planned_end_date` to the last comment + N business days, at a chosen hour and timezone; always future | `$BusinessDays`, `$TargetHour`, `$TimeZoneId`/`$UtcOffset`, `$NonInteractive`, `$LogPath` |

Each write script defaults to the self-assigned unresolved tickets
(`status` unresolved + `responder_id = 0`); replace `$Filter` with your
instance's query hash if your conventions differ.

## Shared environment variables (optional)

A non-empty environment variable overrides the value embedded in any script, so
one configuration drives all of them. The names match the Go CLI's where they
existed.

| Variable | Overrides | Notes |
| --- | --- | --- |
| `FSVC_SUBDOMAIN` | `$Subdomain` | e.g. `acme` |
| `FSVC_ITILDESK_SESSION` | `$SessionCookie` | expires; refresh when calls start failing |
| `FSVC_CSRF_TOKEN` | `$CsrfToken` | write scripts only |
| `FSVC_BASE_URL` | `$BaseUrl` | default `https://<subdomain>.freshservice.com`; useful for a mock |
| `FSVC_LOG_PATH` | `$LogPath` | appended transcript for unattended runs |
| `FSVC_TZ` | `$TimeZoneId` | Windows or IANA id, e.g. `Arabian Standard Time` / `Asia/Dubai` |
| `FSVC_UTC_OFFSET` | `$UtcOffset` | e.g. `+04:00`; `""` keeps each ticket's own offset |

## Scheduled tasks

Both write scripts are schedule-ready. Set these in the script, then register:

```powershell
$NonInteractive = $true
$LogPath        = "C:\logs\fsvc.log"
```

```
powershell.exe -NonInteractive -ExecutionPolicy Bypass -File C:\path\Fill-PlannedStartDates.ps1
powershell.exe -NonInteractive -ExecutionPolicy Bypass -File C:\path\Update-PlannedEndDates.ps1
```

- No prompt is shown; changes apply automatically.
- The task exits **non-zero** when any update fails, so Task Scheduler / monitoring
  sees the failure instead of a silent success.
- Runs are serialised with a lock file in the temp directory; a lock older than
  4 hours (crashed run) is taken over.
- `$LogPath` records everything; the session cookie and CSRF token still expire
  and must be refreshed when the task reports failures.

## Dates and timezones

- Freshservice returns timestamps in the **account's UTC offset**, and planned
  dates are interpreted in the account timezone.
- `Fill-PlannedStartDates.ps1` writes dates in the offset of each ticket's
  `created_at`, i.e. the account offset.
- `Update-PlannedEndDates.ps1` writes in `$TimeZoneId` if set, else
  `$UtcOffset`; set both to `""` to follow each comment's own offset.
- Business-day maths (Mon–Fri, no holidays) is computed between absolute
  instants, so the host machine's timezone does not affect the result.
- Planned end dates are recomputed on every run and clamped to the nearest
  future business slot, so a scheduled run can never leave a past date behind.

## Development

Zero-dependency tests (no Pester required); each suite dot-sources its script
and exercises the pure helpers plus the scheduled-run hardening.

```powershell
Get-ChildItem scripts/*.Tests.ps1 | ForEach-Object { pwsh -NonInteractive -File $_.FullName }
```

CI runs these on `ubuntu-latest` and `windows-latest`.

Standalone is a design constraint: scripts intentionally duplicate their helper
functions so any single file can be copied and run on its own. Keep the copies
consistent when changing shared helpers.

## License

MIT — see [LICENSE](LICENSE)
