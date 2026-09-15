#!/usr/bin/env pwsh
# Fill-PlannedStartDates.ps1
#
# Standalone script (not part of the fsvc CLI) that fills a null
# planned_start_date on a targeted set of Freshservice tickets with the
# ticket's created_at (rounded up to the next quarter hour). Mirrors the CLI's
# `fsvc tickets fill-start-dates`.
#
# WHAT THIS IS: a convenience helper, NOT an official Freshservice API client.
# It talks to the undocumented private API (/api/_/) using your browser session
# cookie. That API is reverse-engineered and can change at any time; no
# guarantee it keeps working after a Freshservice platform update.
#
# WHAT IT ASSUMES:
#   - Auth is a session cookie (_itildesk_session) taken from your own browser
#     DevTools. It expires; re-copy it when it stops working. The same token is
#     used for every request in the run.
#   - Writes require an X-CSRF-Token (also from DevTools). Without it the PUTs
#     fail with 401.
#   - The default $Filter below was user-confirmed on one specific instance.
#     Status numbering and "assigned to me" conventions vary between
#     Freshservice accounts. If the default finds no/too many tickets, replace
#     $Filter with your instance's saved-filter query hash (copy it from the
#     Network tab of any tickets list request in DevTools).
#
# SCOPE: only touches tickets whose planned_start_date is null AND whose
# created_at is present. New dates are created_at rounded up to the next
# quarter hour so they don't look machine-generated. This is a bulk operation -
# review the preview list before confirming.
#
# Usage:
#   Fill in the CONFIG variables below, then run:
#   powershell -ExecutionPolicy Bypass -File Fill-PlannedStartDates.ps1
#
# Scheduled task (unattended):
#   Set $NonInteractive = $true and point $LogPath at a writable file, then
#   register:
#     powershell.exe -NonInteractive -ExecutionPolicy Bypass -File Fill-PlannedStartDates.ps1
#   - No prompt is shown; changes are applied automatically.
#   - The task exits non-zero when any update fails, so it is visible in
#     Task Scheduler / monitoring instead of failing silently.
#   - Runs are serialised by a lock file in the temp directory; a lock older
#     than 4 hours (crashed run) is taken over.
#   - The session cookie and CSRF token still expire manually; refresh them
#     when the task starts reporting failures.

# ---------------------------------------------------------------------------
# CONFIG - edit these before running
# ---------------------------------------------------------------------------

# Resolves a config value: a non-empty environment variable wins over the
# value embedded in the script, so a shared set of credentials can drive every
# standalone script without editing each file. Recognised variables:
#   FSVC_SUBDOMAIN, FSVC_ITILDESK_SESSION, FSVC_CSRF_TOKEN, FSVC_BASE_URL,
#   FSVC_LOG_PATH, FSVC_TZ, FSVC_UTC_OFFSET
function Resolve-FSConfigValue {
    param([AllowNull()][string]$Environment, [AllowNull()][string]$Default)
    if ($Environment) { return $Environment }
    return $Default
}

$Subdomain    = Resolve-FSConfigValue -Environment $env:FSVC_SUBDOMAIN -Default "acme"
$SessionCookie = Resolve-FSConfigValue -Environment $env:FSVC_ITILDESK_SESSION -Default "PASTE_YOUR_itildesk_session_VALUE_HERE"
$CsrfToken     = Resolve-FSConfigValue -Environment $env:FSVC_CSRF_TOKEN -Default "PASTE_YOUR_X-CSRF-Token_VALUE_HERE"

# query_hash filter (JSON array of conditions). Default: self-assigned
# unresolved tickets, same as the CLI's fill-start-dates command.
$Filter = @'
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]
'@

$PerPage  = 100
$Confirm  = $true                        # prompt before applying (interactive runs)
$NonInteractive = $false                 # $true for scheduled tasks: never prompt, always apply
$LogPath  = Resolve-FSConfigValue -Environment $env:FSVC_LOG_PATH -Default ""

# ---------------------------------------------------------------------------
# Session / request header helpers (keep the server's rotated cookie in sync)
# ---------------------------------------------------------------------------

$BaseUrl = Resolve-FSConfigValue -Environment $env:FSVC_BASE_URL -Default "https://$Subdomain.freshservice.com"

# Builds a query string (without leading ?) from a hashtable of params.
function Build-QueryString {
    param([hashtable]$Query)
    if (-not $Query -or $Query.Count -eq 0) {
        return ""
    }
    return ($Query.GetEnumerator() | ForEach-Object { "{0}={1}" -f $_.Key, [uri]::EscapeDataString([string]$_.Value) }) -join "&"
}

# Appends a query string to a path as ?key=value&... . Uses -f rather than
# "$Path?$qs" because `?` is a legal character in PowerShell variable names
# and "$Path?$qs" would swallow the ? (and the path) into an undefined
# variable - producing a broken URL like /api/_/per_page=100&...
function Append-Query {
    param([string]$Path, [string]$QueryString)
    if ($QueryString) {
        return "{0}?{1}" -f $Path, $QueryString
    }
    return $Path
}

function Invoke-FSGet {
    param([string]$Path, [hashtable]$Query)
    $Path = Append-Query -Path $Path -QueryString (Build-QueryString -Query $Query)
    $getHeaders = @{
        "Accept" = "application/json"
        "Cookie" = "_itildesk_session=$SessionCookie"
    }
    $resp = Invoke-WebRequest -Uri "$BaseUrl/api/_/$Path" -Headers $getHeaders -UseBasicParsing
    return $resp.Content
}

function Invoke-FSPut {
    param([string]$Path, [hashtable]$Body)
    $putHeaders = @{
        "Accept"        = "application/json"
        "Cookie"        = "_itildesk_session=$SessionCookie"
        "Content-Type"  = "application/json; charset=utf-8"
        "X-CSRF-Token"  = $CsrfToken
    }
    $json = $Body | ConvertTo-Json -Compress
    $resp = Invoke-WebRequest -Uri "$BaseUrl/api/_/$Path" -Method Put -Headers $putHeaders -Body $json -UseBasicParsing
    return $resp.Content
}

# Parses API JSON. PowerShell 7.5+ keeps ISO timestamp strings verbatim via
# -DateKind String; older versions parse them into DateTime (the date helpers
# below convert those back to DateTimeOffset). Without this, timestamps are
# converted to local time and the account offset is lost.
function ConvertFrom-FSJson {
    param([Parameter(Mandatory, ValueFromPipeline)][string]$Json)
    if ((Get-Command ConvertFrom-Json).Parameters.ContainsKey("DateKind")) {
        return $Json | ConvertFrom-Json -DateKind String
    }
    return $Json | ConvertFrom-Json
}

# Converts an API timestamp (string, DateTime or DateTimeOffset) to a
# DateTimeOffset that preserves the account's UTC offset. $null when absent or
# unparseable.
function ConvertTo-FSDateTimeOffset {
    param([AllowNull()]$Value)
    if ($null -eq $Value) { return $null }
    if ($Value -is [datetimeoffset]) { return $Value }
    if ($Value -is [datetime]) { return [datetimeoffset]$Value }
    $parsed = [datetimeoffset]::MinValue
    if ([datetimeoffset]::TryParse([string]$Value, [ref]$parsed)) { return $parsed }
    return $null
}

# Renders a DateTimeOffset as RFC 3339, using Z for a zero offset to match the
# Go CLI.
function Format-Iso8601 {
    param([datetimeoffset]$Value)
    if ($Value.Offset -eq [timespan]::Zero) {
        return $Value.ToString("yyyy-MM-ddTHH:mm:ss") + "Z"
    }
    return $Value.ToString("yyyy-MM-ddTHH:mm:sszzz")
}

function Add-BusinessDays {
    param([datetimeoffset]$Start, [int]$Days)
    $t = $Start
    $added = 0
    while ($added -lt $Days) {
        $t = $t.AddDays(1)
        if ($t.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $t.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
            $added++
        }
    }
    return $t
}

# Round up to the next quarter hour (:00/:15/:30/:45), keeping the timestamp's
# own UTC offset (the account timezone).
function Round-Up-QuarterHour {
    param([datetimeoffset]$t)
    $total = $t.Hour * 60 + $t.Minute
    if ($t.Second -ne 0 -or $t.Millisecond -ne 0 -or ($total % 15) -ne 0) {
        $total = [math]::Floor($total / 15) * 15 + 15
    }
    $base = [datetimeoffset]::new($t.Year, $t.Month, $t.Day, 0, 0, 0, $t.Offset)
    return $base.AddMinutes($total)
}

# Returns $true when a ticket's planned_start_date should be filled: the date
# is null/unset AND a created_at value is present. Tickets that already have a
# planned_start_date (or no created_at) are left alone.
function Should-FillStart {
    param(
        [AllowNull()][string]$PlannedStartDate,
        [AllowNull()][string]$CreatedAt
    )
    if ($PlannedStartDate) {
        return $false   # already filled
    }
    if (-not $CreatedAt) {
        return $false   # nothing to derive the date from
    }
    return $true
}

# --- scheduled-run hardening -------------------------------------------------

# Decides whether to apply the changes. Non-interactive runs always apply;
# interactive runs honour $Confirm and the typed answer.
function Get-ApplyDecision {
    param([bool]$Confirm, [bool]$NonInteractive, [AllowNull()][string]$Answer)
    if ($NonInteractive -or -not $Confirm) { return $true }
    return ($Answer -match "^[yY]")
}

# Exit code for the run: non-zero when not every planned change was applied, so
# Task Scheduler reports a failure instead of a silent success.
function Get-ApplyExitCode {
    param([int]$Applied, [int]$Total)
    if ($Applied -ne $Total) { return 1 }
    return 0
}

# Acquires an exclusive lock file so overlapping runs cannot double-apply. A
# lock older than $StaleMinutes (from a crashed run) is taken over. Returns the
# open file handle, or $null when another run holds the lock.
function Enter-RunLock {
    param([string]$Path, [int]$StaleMinutes = 240)
    for ($attempt = 0; $attempt -lt 2; $attempt++) {
        try {
            return [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        } catch {
            try {
                $age = (Get-Date) - (Get-Item -LiteralPath $Path -ErrorAction Stop).LastWriteTime
                if ($age.TotalMinutes -ge $StaleMinutes) {
                    Remove-Item -LiteralPath $Path -Force -ErrorAction Stop
                    continue
                }
            } catch { }
            return $null
        }
    }
    return $null
}

function Exit-RunLock {
    param([AllowNull()]$Handle, [string]$Path)
    if ($null -ne $Handle) { try { $Handle.Close() } catch { } }
    try { if (Test-Path -LiteralPath $Path) { Remove-Item -LiteralPath $Path -Force } } catch { }
}

# Mirrors all host output to $LogPath (appended) when set.
$script:TranscriptActive = $false
function Start-Logging {
    if (-not $LogPath) { return }
    try {
        Start-Transcript -Path $LogPath -Append | Out-Null
        $script:TranscriptActive = $true
    } catch {
        Write-Warning ("Could not start transcript at {0}: {1}" -f $LogPath, $_.Exception.Message)
    }
}

function Stop-Logging {
    if ($script:TranscriptActive) {
        try { Stop-Transcript | Out-Null } catch { }
        $script:TranscriptActive = $false
    }
}

# Allow dot-sourcing: `path . Fill-PlannedStartDates.ps1` defines the helper
# functions (Round-Up-QuarterHour, Should-FillStart, ...) without running the
# script body. Run it directly to actually fill dates.
if ($MyInvocation.InvocationName -eq '.') { return }

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

# Realistic expectations: these checks only catch blank placeholders. They do
# NOT verify the session is valid, the CSRF token is accepted, or the filter
# matches anything - failures surface later as HTTP errors or "0 changes".

if ($CsrfToken -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$CsrfToken (and \$SessionCookie) at the top of the script." -ForegroundColor Red
    exit 1
}
if ($SessionCookie -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$SessionCookie at the top of the script." -ForegroundColor Red
    exit 1
}

# One run at a time, and capture output for unattended runs.
$lockPath = Join-Path ([System.IO.Path]::GetTempPath()) "fsvc-fill-planned-start-dates.lock"
$runLock = Enter-RunLock -Path $lockPath
if ($null -eq $runLock) {
    Write-Host ("ERROR: another run is in progress (lock: {0}). If that is stale, delete it and retry." -f $lockPath) -ForegroundColor Red
    exit 1
}
Start-Logging

# ---------------------------------------------------------------------------
# Collect tickets (paginate until meta.has_next is false)
# ---------------------------------------------------------------------------

$tickets = @()
$page = 1
$query = @{
    "order_by"   = "created_at"
    "order_type" = "asc"
    "per_page"   = $PerPage
    "query_hash" = $Filter
}

do {
    $query["page"] = $page
    $content = Invoke-FSGet -Path "tickets" -Query $query
    $data = $content | ConvertFrom-FSJson
    $tickets += @($data.tickets)
    $hasNext = $data.meta.has_next
    $page++
} while ($hasNext -and $page -lt 1000)

Write-Host ("Scanned {0} tickets" -f $tickets.Count)

# ---------------------------------------------------------------------------
# Decide changes
# ---------------------------------------------------------------------------

$changes = @()
foreach ($t in $tickets) {
    if (-not (Should-FillStart -PlannedStartDate $t.planned_start_date -CreatedAt $t.created_at)) {
        continue
    }
    $at = ConvertTo-FSDateTimeOffset $t.created_at
    if ($null -eq $at) {
        continue   # created_at unparseable; leave it alone
    }
    $to = Format-Iso8601 (Round-Up-QuarterHour $at)
    $changes += [pscustomobject]@{
        Id   = $t.id
        From = $t.planned_start_date
        To   = $to
    }
}

if ($changes.Count -eq 0) {
    Write-Host "No changes needed."
    Stop-Logging
    Exit-RunLock -Handle $runLock -Path $lockPath
    exit 0
}

foreach ($c in $changes) {
    Write-Host ("[planned_start_date] ticket {0}: {1} -> {2}" -f $c.Id, $c.From, $c.To)
}

if ($Confirm -and -not $NonInteractive) {
    try {
        $answer = Read-Host ("Apply {0} changes? [y/N] " -f $changes.Count)
    } catch {
        Write-Host "ERROR: no console available for confirmation. Set \$NonInteractive = \$true for scheduled runs." -ForegroundColor Red
        Stop-Logging
        Exit-RunLock -Handle $runLock -Path $lockPath
        exit 1
    }
    if (-not (Get-ApplyDecision -Confirm $Confirm -NonInteractive $NonInteractive -Answer $answer)) {
        Write-Host "Aborted."
        Stop-Logging
        Exit-RunLock -Handle $runLock -Path $lockPath
        exit 0
    }
}

# ---------------------------------------------------------------------------
# Apply
# ---------------------------------------------------------------------------

$applied = 0
foreach ($c in $changes) {
    try {
        Invoke-FSPut -Path ("tickets/{0}" -f $c.Id) -Body @{ planned_start_date = $c.To } | Out-Null
        Write-Host ("OK: ticket {0}" -f $c.Id)
        $applied++
    } catch {
        Write-Host ("FAILED: ticket {0}: {1}" -f $c.Id, $_.Exception.Message) -ForegroundColor Yellow
    }
}

Write-Host ("Done: {0}/{1} applied" -f $applied, $changes.Count)
Stop-Logging
Exit-RunLock -Handle $runLock -Path $lockPath
exit (Get-ApplyExitCode -Applied $applied -Total $changes.Count)