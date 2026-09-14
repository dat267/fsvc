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
#   - "Business days" = Mon-Fri, in the account timezone (derived from the
#     tickets' own dates; falls back to the local machine when no ticket has a
#     date). No holidays.
#
# SCOPE: by default only touches tickets whose planned_end_date is null or in
# the past. When $WithinDays > 0 it also touches tickets due within the next
# $WithinDays days; future dates beyond the window are left alone. New dates
# are rounded up to the next quarter hour so they don't look machine-generated.
# New dates are written in the same UTC offset as the tickets' existing dates,
# so they are consistent with the rest of the account. This is a bulk
# operation - review the preview list before confirming.
#
# Usage:
#   Fill in the CONFIG variables below, then run:
#   powershell -ExecutionPolicy Bypass -File Update-PlannedEndDates.ps1

# ---------------------------------------------------------------------------
# CONFIG - edit these before running
# ---------------------------------------------------------------------------

$Subdomain    = "acme"                   # your Freshservice subdomain
$SessionCookie = "PASTE_YOUR_itildesk_session_VALUE_HERE"
$CsrfToken     = "PASTE_YOUR_X-CSRF-Token_VALUE_HERE"   # required for PUT

$BusinessDays = 3                        # business days from now to set the end date

$WithinDays  = 0                         # also bump tickets due within this many days (0 = off; keep off unless intended)

# query_hash filter (JSON array of conditions). Default: self-assigned
# unresolved tickets, same as the CLI's push-end-dates command.
$Filter = @'
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]
'@

$PerPage  = 100
$Confirm  = $true                        # prompt before applying (set $false to auto-apply)

# ---------------------------------------------------------------------------
# Session / request header helpers (keep the server's rotated cookie in sync)
# ---------------------------------------------------------------------------

$BaseUrl = "https://$Subdomain.freshservice.com"

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

# Returns $true when a ticket's planned_end_date should be bumped: the date is
# null, unparseable, in the past, or within the next $WithinDays days (when
# WithinDays > 0). Future dates beyond the window are left alone. Now is the
# reference time; WithinDays is exposed so tests can drive the logic.
function Should-Bump {
    param(
        [AllowNull()][string]$PlannedEndDate,
        [datetimeoffset]$Now,
        [int]$WithinDays
    )
    $at = ConvertTo-FSDateTimeOffset $PlannedEndDate
    if ($null -ne $at) {
        $cutoff = $Now.AddDays($WithinDays)
        # Has a due date. Leave alone unless it's past or within the window.
        if ($at -gt $Now -and ($WithinDays -le 0 -or $at -gt $cutoff)) {
            return $false   # due beyond the window
        }
    }
    return $true
}

# Allow dot-sourcing: `path . Update-PlannedEndDates.ps1` defines the helper
# functions (Add-BusinessDays, Round-Up-QuarterHour, Should-Bump, ...) without
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

# The target must use the account timezone evidenced by the tickets' own
# dates, so pushed dates carry the same offset as existing ones.
$accountOffset = Get-AccountOffset -Tickets $tickets -Fallback ([datetimeoffset]::Now)
$now = ([datetimeoffset]::Now).ToOffset($accountOffset)
$target = Round-Up-QuarterHour (Add-BusinessDays -Start $now -Days $BusinessDays)
$targetIso = Format-Iso8601 $target
Write-Host ("Target planned_end_date: {0}" -f $targetIso)
Write-Host ("Scanned {0} tickets" -f $tickets.Count)

# ---------------------------------------------------------------------------
# Decide changes
# ---------------------------------------------------------------------------

$changes = @()
foreach ($t in $tickets) {
    if (-not (Should-Bump -PlannedEndDate $t.planned_end_date -Now $now -WithinDays $WithinDays)) {
        continue
    }
    $changes += [pscustomobject]@{
        Id   = $t.id
        From = $t.planned_end_date
        To   = $targetIso
    }
}

if ($changes.Count -eq 0) {
    Write-Host "No changes needed."
    exit 0
}

foreach ($c in $changes) {
    Write-Host ("[planned_end_date] ticket {0}: {1} -> {2}" -f $c.Id, $c.From, $c.To)
}

if ($Confirm) {
    $answer = Read-Host ("Apply {0} changes? [y/N] " -f $changes.Count)
    if ($answer -notmatch "^[yY]") {
        Write-Host "Aborted."
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