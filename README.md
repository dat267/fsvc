# fsvc

PowerShell module for the Freshservice **private API** (`/api/_/`), authenticated
with your browser session cookie. It provides ticket triage, ticket content, and
planned-date hygiene as native commands.

```powershell
Install-Module fsvc -Scope CurrentUser
Import-Module fsvc

Set-FSvcConfig -Subdomain acme -SessionCookie '<cookie>' -CsrfToken '<token>'

Get-FSvcTicketOverview | Format-Table Category, Id, Subject, Days
Get-FSvcTicketContent -Id 10100 | Format-FSvcTicketContent
Update-FSvcPlannedEndDates -WhatIf
```

> The previous implementations are archived: the Go CLI on `archive/go-cli`, the
> standalone scripts and installer on `archive/standalone-scripts`.

## Install

This module is **not published to the PowerShell Gallery**. Install it from
GitHub instead — the bootstrap copies it into your user module folder, after
which `Import-Module fsvc` works.

```powershell
# remote one-liner (installs from main)
irm https://raw.githubusercontent.com/dat267/fsvc/main/Install.ps1 | iex

# a specific release tag, or refresh an existing install
pwsh Install.ps1 -Version v1.0.0 -Force
pwsh Install.ps1 -Ref main -Force

# remove it
pwsh Install.ps1 -Uninstall
```

After installing, `Import-Module fsvc` is optional: the installer targets a
folder on `PSModulePath`, so PowerShell auto-loads the module the first time you
call one of its commands. Importing explicitly is still fine.

```powershell
Set-FSvcConfig -Subdomain acme -SessionCookie '<cookie>' -CsrfToken '<token>'
Get-FSvcTicketOverview | Format-Table Category, Id, Subject, Days
```

Auto-load notes: after an upgrade (`Install.ps1 -Force`), an already-open session
keeps the old code until `Import-Module fsvc -Force` or a new session; a custom
`-Destination` outside `PSModulePath` needs an explicit import; and after
`-Uninstall`, run `Remove-Module fsvc` or open a new session.

Other ways to get it running:

```powershell
# clone and import, no install step
Import-Module ./fsvc.psd1

# if you clone directly into a PSModulePath folder
Import-Module fsvc
```

`Install-Module` / `Install-PSResource` require a package repository (the Gallery
or a private NuGet feed); a GitHub repo URL is not one. If you operate a feed,
`Publish-PSResource -Path . -Repository <feed>` from a clone is the standard
route. Tagging `v*` builds and attaches the packaged module as a GitHub release
asset (`.github/workflows/release.yml`), which `Install.ps1 -Version <tag>`
consumes.

## Configure

Settings live for the session; a non-empty `FSVC_*` environment variable
overrides them, so a shared environment configuration works without re-running
`Set-FSvcConfig`.

```powershell
Set-FSvcConfig -Subdomain acme -SessionCookie '<cookie>' -CsrfToken '<token>'
Get-FSvcConfig   # shows the effective values (secrets masked)
Test-FSvcSession # verifies the cookie works
```

| Setting | Environment variable | Purpose |
| --- | --- | --- |
| `Subdomain` | `FSVC_SUBDOMAIN` | e.g. `acme` |
| `SessionCookie` | `FSVC_ITILDESK_SESSION` | `_itildesk_session` value |
| `CsrfToken` | `FSVC_CSRF_TOKEN` | required for writes |
| `BaseUrl` | `FSVC_BASE_URL` | override the API base URL |
| `TimeZoneId` | `FSVC_TZ` | Windows/IANA id for planned end dates |
| `UtcOffset` | `FSVC_UTC_OFFSET` | e.g. `+04:00`; `""` keeps the ticket's offset |
| `LogPath` | `FSVC_LOG_PATH` | transcript file for write commands |

The private API is undocumented and reverse-engineered; see
[`docs/private-api-notes.md`](docs/private-api-notes.md).

## Commands

All commands output objects, so use the normal PowerShell pipeline
(`Format-Table`, `Where-Object`, `ConvertTo-Json`, `Export-Csv`, ...).

| Command | Purpose |
| --- | --- |
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
  `created_at`) at `-TargetHour` in `-TimeZoneId`/`-UtcOffset`. A date that would
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

CI runs the suites on `ubuntu-latest` and `windows-latest`; `v*` tags build the
module package and attach it to a GitHub release (no Gallery publishing).

## License

MIT — see [LICENSE](LICENSE)
