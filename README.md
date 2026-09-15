# fsvc

PowerShell module for the Freshservice **private API** (`/api/_/`), authenticated
with your browser session cookie. It provides ticket triage, ticket content, and
planned-date hygiene as native commands.

```powershell
Install-Module fsvc -Scope CurrentUser
Import-Module fsvc

Set-FSvcConfig -Subdomain acme -ItildeskSession '<cookie>' -CsrfToken '<token>'

Get-FSvcTicketOverview                              # Category / Subject / Elapsed / Since / Link
Get-FSvcTicketContent -Id 10100 | Format-FSvcTicketContent
Update-FSvcPlannedEndDates -WhatIf
```

> The previous implementations are archived: the Go CLI on `archive/go-cli`, the
> standalone scripts and installer on `archive/standalone-scripts`.

## Install

From the PowerShell Gallery:

```powershell
Install-Module fsvc -Scope CurrentUser      # PowerShellGet
Install-PSResource fsvc -Scope CurrentUser  # PSResourceGet
```

Without the Gallery, the same module installs from GitHub — the bootstrap
copies it into your user module folder, after which PowerShell auto-loads it:

```powershell
# remote one-liner (installs from main)
irm https://raw.githubusercontent.com/dat267/fsvc/main/Install.ps1 | iex

# a specific release tag, or refresh an existing install
pwsh Install.ps1 -Version v1.0.0 -Force
pwsh Install.ps1 -Ref main -Force

# remove it
pwsh Install.ps1 -Uninstall
```

Or just clone and import, with no install step:

```powershell
Import-Module ./fsvc.psd1
```

After any install, `Import-Module fsvc` is optional: because the module lands on
`PSModulePath`, PowerShell auto-loads it the first time you call one of its
commands. After an upgrade, an already-open session keeps the old code until
`Import-Module fsvc -Force` or a new session; after `Install.ps1 -Uninstall`, run
`Remove-Module fsvc` or open a new session.

Publishing happens on `v*` tags (`.github/workflows/release.yml`): it runs the
tests, publishes to the Gallery using the `PSGALLERY_API_KEY` repository secret,
and attaches a packaged zip to the GitHub release. `Install.ps1 -Version <tag>`
consumes the tag's source archive.

## Configure

`Set-FSvcConfig` stores settings for the session and **persists them to a small
JSON file** for future sessions: `%LOCALAPPDATA%\fsvc\config.json` on Windows,
`~/.config/fsvc/config.json` elsewhere. The effective value is resolved as
per-call parameter, then session value, then the `FSVC_*` environment variable,
then the config file.

Within a session, `Set-FSvcConfig` values take precedence; the `FSVC_*`
environment variables fill anything not set. Passing an empty value clears a
setting from both the session and the persisted store.

```powershell
Set-FSvcConfig -Subdomain acme -ItildeskSession '<cookie>' -CsrfToken '<token>'
Get-FSvcConfig   # shows the effective values (secrets masked)
Test-FSvcSession # verifies the cookie works
Set-FSvcConfig -ItildeskSession ''   # clear a persisted setting
```

`ItildeskSession` and `CsrfToken` are persisted in plaintext, readable by any
process running as you; clear them when they expire.

| Setting | Environment variable | Purpose |
| --- | --- | --- |
| `Subdomain` | `FSVC_SUBDOMAIN` | e.g. `acme` |
| `ItildeskSession` | `FSVC_ITILDESK_SESSION` | `_itildesk_session` value |
| `CsrfToken` | `FSVC_CSRF_TOKEN` | required for writes |
| `BaseUrl` | `FSVC_BASE_URL` | override the API base URL |
| `UtcOffset` | `FSVC_UTC_OFFSET` | e.g. `+04:00`; `""` keeps the ticket's offset |
| `LogPath` | `FSVC_LOG_PATH` | transcript file for write commands |

The private API is undocumented and reverse-engineered; see
[`docs/private-api-notes.md`](docs/private-api-notes.md).

## Commands

All commands output objects, so use the normal PowerShell pipeline
(`Format-Table`, `Where-Object`, `ConvertTo-Json`, `Export-Csv`, ...).

| Command | Purpose |
| --- | --- |
| `Set-FSvcConfig` | Store connection settings for the session |
| `Get-FSvcConfig` | Show the effective settings (secrets masked) |
| `Test-FSvcSession` | Verify the session cookie works |
| `Get-FSvcTicketList` | Tickets by saved-filter id or raw `query_hash` |
| `Get-FSvcTicketContent` | One ticket plus its conversation trace |
| `Format-FSvcTicketContent` | Renders that object as readable text |
| `Get-FSvcTicketOverview` | Triage: `unassigned`, `waiting`, `awaiting_agent` |
| `Set-FSvcPlannedStartDates` | Fill null `planned_start_date` from `created_at` |
| `Update-FSvcPlannedEndDates` | Set `planned_end_date` to last comment + N business days |

Examples:

```powershell
Get-FSvcTicketList -FilterId 1100 | Format-Table id, subject, status, priority
Get-FSvcTicketContent -Id 10100 | ConvertTo-Json -Depth 10
Get-FSvcTicketOverview -OlderThanDays 2 | Where-Object Category -eq 'waiting'
```
`Get-FSvcTicketOverview` rows are grouped `unassigned`, `waiting`, `awaiting_agent`
and sorted by `Days` descending within each group. `Days` is a numeric business-day
count (weekends skipped, holidays not modelled) measured from `Since` -- `created_at`
for unassigned rows, the last message otherwise. `Since` keeps the account's own UTC
offset and renders as RFC 3339 (`2026-08-27T11:44:02+04:00`), so it is unaffected by the
host machine's timezone; `Elapsed` is the same value
humanized (`13d 14h`, or `1h 30m` below a day). `Unanswered` counts the customer messages an agent has not answered yet (the consecutive incoming run since the last agent message), shown as `-` when there are none or the row is unassigned. `Id` stays a property even though the default view omits it.
```powershell
Set-FSvcPlannedStartDates -WhatIf
Update-FSvcPlannedEndDates -BusinessDays 3 -TargetHour 17 -UtcOffset '+04:00'
```

## Writing dates

The two write commands support `-WhatIf` / `-Confirm` and emit one object per
change (`Id`, `Field`, `From`, `To`, `Applied`). Runs are serialised with a lock
file so overlapping calls cannot double-apply, and `-LogPath` records a
transcript. They default to the `SelfAssigned` view; pass `-View Unassigned` or
a raw `-QueryHash` to target something else. `Get-FSvcTicketOverview` uses the
named `SelfAssigned`/`Unassigned` views unless you override its query hashes.

- `Update-FSvcPlannedEndDates` recomputes every scanned ticket to its **last
  comment + N business days** (private note or public reply, falling back to
  `created_at`) at `-TargetHour` in `-UtcOffset`. A date that would
  be in the past is clamped to the nearest future business slot, so the planned
  end is always in the future; identical dates are skipped.
- Times are handled as absolute instants and rendered in the account/target
  offset, so the host machine's timezone never changes the result.

For unattended use, `-Confirm:$false`:

```powershell
Update-FSvcPlannedEndDates -Confirm:$false -LogPath C:\logs\fsvc.log
```

## Development

```powershell
Import-Module ./fsvc.psd1 -Force
Get-ChildItem tests/*.Tests.ps1 | ForEach-Object { pwsh -NonInteractive -File $_.FullName }
```

Layout:

- `fsvc.psd1` / `fsvc.psm1` — manifest and loader (`Private`, then `Public`).
- `Private/` — helpers: config resolution, HTTP, dates, tickets, run lock/log.
- `Public/` — the exported commands.
- `tests/` — zero-dependency suites (dot-source `Private` for unit tests;
  `FSvc.Module.Tests.ps1` validates the manifest and exports).

CI runs the suites on `ubuntu-latest` and `windows-latest`; `v*` tags run the
release workflow, which publishes to the PowerShell Gallery and attaches the
packaged module to the GitHub release.

Versioning: `ModuleVersion` in `fsvc.psd1` starts at `0.0.1` and must equal the
release tag (the workflow refuses a mismatch). Bump **conservatively** — patch
(`0.0.2`, `0.0.3`, ...) for fixes and small changes, minor for new capabilities,
major for breaking changes; do not bump for docs or test-only commits.

## License

MIT — see [LICENSE](LICENSE)
