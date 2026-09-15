# Update-PlannedEndDates.ps1
#
# Standalone script (not part of the fsvc CLI) that bumps planned_end_date to
# now + N business days on a targeted set of Freshservice tickets.
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
#     fail with 401. There is no way around this.
#   - The default $Filter below was user-confirmed on one specific instance.
#     Status numbering and "assigned to me" conventions vary between
#     Freshservice accounts. If the default finds no/too many tickets, replace
#     $Filter with your instance's saved-filter query hash (copy it from the
#     Network tab of any tickets list request in DevTools).
#   - "Business days" = Mon-Fri; holidays are not considered.
#   - The new planned_end_date is derived from the ticket's LATEST conversation
#     comment (private note or public reply - any kind), falling back to
#     created_at when the ticket has no comments yet. So each ticket keeps its
#     own cadence instead of sharing one global "now + N days" date.
#
# SCOPE: every scanned ticket is recomputed so its planned_end_date sits N
# business days after its latest comment, at $TargetHour in the configured
# timezone. If that would land in the past, it is clamped to the nearest future
# business slot (still at $TargetHour) so the planned end is always in the
# future. Tickets whose current date already equals the computed instant are
# skipped, so re-running is a no-op. This is a bulk operation - review the
# preview list before confirming.
#
# Usage:
#   Fill in the CONFIG variables below, then run:
#   powershell -ExecutionPolicy Bypass -File Update-PlannedEndDates.ps1
#
# Scheduled task (unattended):
#   Set $NonInteractive = $true and point $LogPath at a writable file, then
#   register:
#     powershell.exe -NonInteractive -ExecutionPolicy Bypass -File Update-PlannedEndDates.ps1
#   - No prompt is shown; changes are applied automatically.
#   - The task exits non-zero when any update fails, so it is visible in
#     Task Scheduler / monitoring instead of failing silently.
#   - Runs are serialised by a lock file in the temp directory; a lock older
#     than 4 hours (crashed run) is taken over.
#   - The session cookie and CSRF token still expire manually; refresh them
#     when the task starts reporting failures.

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

# ---------------------------------------------------------------------------
# CONFIG - edit these before running
# ---------------------------------------------------------------------------

$Subdomain    = Resolve-FSConfigValue -Environment $env:FSVC_SUBDOMAIN -Default "acme"
$SessionCookie = Resolve-FSConfigValue -Environment $env:FSVC_ITILDESK_SESSION -Default "PASTE_YOUR_itildesk_session_VALUE_HERE"
$CsrfToken     = Resolve-FSConfigValue -Environment $env:FSVC_CSRF_TOKEN -Default "PASTE_YOUR_X-CSRF-Token_VALUE_HERE"

$BusinessDays = 3                        # business days from the last comment to set the end date
$TargetHour   = 17                       # 0-23: hour of day for the new planned_end_date
$TimeZoneId   = Resolve-FSConfigValue -Environment $env:FSVC_TZ -Default ""
$UtcOffset    = Resolve-FSConfigValue -Environment $env:FSVC_UTC_OFFSET -Default "+04:00"

# query_hash filter (JSON array of conditions). Default: self-assigned
# unresolved tickets, same as the CLI's push-end-dates command.
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

# Returns the UTC offset evidenced by the tickets' own dates (planned_end_date
# preferred, created_at fallback). Falls back to the reference when no ticket
# carries a parseable date.
function Get-AccountOffset {
    param($Tickets, [datetimeoffset]$Fallback)
    foreach ($t in @($Tickets)) {
        $d = ConvertTo-FSDateTimeOffset $t.planned_end_date
        if ($null -ne $d) { return $d.Offset }
    }
    foreach ($t in @($Tickets)) {
        $d = ConvertTo-FSDateTimeOffset $t.created_at
        if ($null -ne $d) { return $d.Offset }
    }
    return $Fallback.Offset
}

# Resolves a Windows or IANA timezone id to a TimeZoneInfo. $null when the id
# is empty (callers then use $UtcOffset or the comment's own offset).
function Resolve-TimeZone {
    param([string]$Id)
    if (-not $Id) { return $null }
    try {
        return [System.TimeZoneInfo]::FindSystemTimeZoneById($Id)
    } catch {
        throw ("Unknown TimeZoneId '{0}'. Use a Windows id (e.g. 'Arabian Standard Time') or an IANA id (e.g. 'Asia/Dubai')." -f $Id)
    }
}

# Parses a "+04:00" style UTC offset into a TimeSpan. $null when empty.
function ConvertTo-UtcOffset {
    param([string]$Value)
    if (-not $Value) { return $null }
    try {
        return [datetimeoffset]::Parse("2000-01-01T00:00:00" + $Value).Offset
    } catch {
        throw ("Invalid UtcOffset '{0}'. Use a value like '+04:00'." -f $Value)
    }
}

# Converts an instant to the target timezone (by id) or UTC offset.
function ConvertTo-TargetZone {
    param([datetimeoffset]$Value, [AllowNull()]$Zone, [AllowNull()]$Offset)
    if ($null -ne $Zone) {
        return [System.TimeZoneInfo]::ConvertTime($Value, $Zone)
    }
    if ($null -ne $Offset) {
        return $Value.ToOffset([timespan]$Offset)
    }
    return $Value
}

# Computes the new planned_end_date from a base instant: convert to the target
# timezone/offset, add business days, set the hour, round up to the quarter
# hour, and - when -Now is supplied - clamp so the result is always in the
# future (today at the target hour, else the next business day at that hour).
function Get-TargetEndDate {
    param(
        [datetimeoffset]$Base,
        [int]$Days,
        [int]$Hour,
        [AllowNull()]$Zone,
        [AllowNull()]$Offset,
        [AllowNull()]$Now
    )
    $b = ConvertTo-TargetZone -Value $Base -Zone $Zone -Offset $Offset
    $t = Add-BusinessDays -Start $b -Days $Days
    $t = [datetimeoffset]::new($t.Year, $t.Month, $t.Day, $Hour, 0, 0, $t.Offset)
    $t = Round-Up-QuarterHour $t

    if ($null -ne $Now) {
        $n = ConvertTo-TargetZone -Value $Now -Zone $Zone -Offset $Offset
        if ($t -le $n) {
            # Desired date is stale: use the nearest future business slot so the
            # planned end is never in the past, staying as close as possible.
            $slot = [datetimeoffset]::new($n.Year, $n.Month, $n.Day, $Hour, 0, 0, $n.Offset)
            if ($slot -le $n) {
                $slot = Add-BusinessDays -Start $slot -Days 1
            }
            $t = $slot
        }
    }
    return $t
}

# True when a ticket's planned_end_date differs from the computed target.
# Comparisons are on the instant, so a different UTC offset for the same
# moment does not count as a change. Missing/unparseable dates need an update.
function Test-EndDateNeedsUpdate {
    param([AllowNull()]$PlannedEndDate, [datetimeoffset]$Target)
    $cur = ConvertTo-FSDateTimeOffset $PlannedEndDate
    if ($null -eq $cur) { return $true }
    return ($cur.UtcDateTime -ne $Target.UtcDateTime)
}

# Most recent conversation for a ticket, of any kind (private note or public
# reply), or $null when it has none.
function Get-LatestConversation {
    param([int64]$TicketId)
    $query = @{
        "order_by"   = "created_at"
        "order_type" = "desc"
        "per_page"   = 1
        "page"       = 1
    }
    $data = (Invoke-FSGet -Path ("tickets/{0}/conversations" -f $TicketId) -Query $query) | ConvertFrom-FSJson
    $c = @($data.conversations) | Select-Object -First 1
    if ($null -eq $c) { return $null }
    return [pscustomobject]@{
        CreatedAt = ConvertTo-FSDateTimeOffset $c.created_at
        UserID    = [int64]$c.user_id
    }
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

# Allow dot-sourcing: `path . Update-PlannedEndDates.ps1` defines the helper
# functions (Add-BusinessDays, Round-Up-QuarterHour, Get-TargetEndDate, ...) without
# running the script body. Run it directly to actually bump dates.
if ($MyInvocation.InvocationName -eq '.') { return }

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

# Realistic expectations: these checks only catch blank placeholders. They do
# NOT verify the session is valid, the CSRF token is accepted, or the filter
# matches anything - failures surface later as HTTP errors or "0 changes".
# If you see "Scanned N tickets" = 0, the cookie or $Filter is wrong, not the
# script.

if ($CsrfToken -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$CsrfToken (and \$SessionCookie) at the top of the script." -ForegroundColor Red
    exit 1
}
if ($SessionCookie -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$SessionCookie at the top of the script." -ForegroundColor Red
    exit 1
}

# One run at a time, and capture output for unattended runs.
$lockPath = Join-Path ([System.IO.Path]::GetTempPath()) "fsvc-update-planned-end-dates.lock"
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

# The reference "now" uses the account timezone evidenced by the tickets'
# dates, so past/future comparisons are consistent.
$accountOffset = Get-AccountOffset -Tickets $tickets -Fallback ([datetimeoffset]::Now)
$now = ([datetimeoffset]::Now).ToOffset($accountOffset)

$zone = Resolve-TimeZone -Id $TimeZoneId
$offset = ConvertTo-UtcOffset -Value $UtcOffset
$where = if ($TimeZoneId) { $TimeZoneId } elseif ($UtcOffset) { $UtcOffset } else { "each comment's own offset" }
Write-Host ("Target: {0} business days from each ticket's last comment at {1}:00 ({2})" -f $BusinessDays, $TargetHour, $where)
Write-Host ("Scanned {0} tickets" -f $tickets.Count)

# ---------------------------------------------------------------------------
# Decide changes
# ---------------------------------------------------------------------------

$changes = @()
foreach ($t in $tickets) {
    # Base the new date on the latest comment (any kind), else created_at.
    $latest = Get-LatestConversation -TicketId $t.id
    $base = $null
    if ($null -ne $latest) { $base = $latest.CreatedAt }
    if ($null -eq $base) { $base = ConvertTo-FSDateTimeOffset $t.created_at }
    if ($null -eq $base) { continue }   # nothing to derive a date from

    $target = Get-TargetEndDate -Base $base -Days $BusinessDays -Hour $TargetHour -Zone $zone -Offset $offset -Now $now
    if (-not (Test-EndDateNeedsUpdate -PlannedEndDate $t.planned_end_date -Target $target)) {
        continue   # already exactly where it should be
    }
    $changes += [pscustomobject]@{
        Id   = $t.id
        From = $t.planned_end_date
        To   = Format-Iso8601 $target
    }
}

if ($changes.Count -eq 0) {
    Write-Host "No changes needed."
    Stop-Logging
    Exit-RunLock -Handle $runLock -Path $lockPath
    exit 0
}

foreach ($c in $changes) {
    Write-Host ("[planned_end_date] ticket {0}: {1} -> {2}" -f $c.Id, $c.From, $c.To)
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
        Invoke-FSPut -Path ("tickets/{0}" -f $c.Id) -Body @{ planned_end_date = $c.To } | Out-Null
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