#!/usr/bin/env pwsh
# Get-TicketOverview.ps1
#
# Standalone script (not part of the fsvc CLI) that prints a three-list triage
# overview of Freshservice tickets from the private API (/api/_/):
#
#   1. Unassigned  - tickets matching the customizable $UnassignedQueryHash
#   2. Waiting on customer > N business days - responder replied (or never did)
#      and the customer has been silent, so the ticket is a follow-up/resolve
#      candidate
#   3. Awaiting agent - the last message is from someone other than the
#      responder, so an agent reply is due
#
# Lists 2 and 3 mirror `fsvc tickets classify`.
#
# WHAT THIS IS: a convenience helper, NOT an official Freshservice API client.
# It talks to the undocumented private API using your browser session cookie.
# That API is reverse-engineered and can change at any time.
#
# WHAT IT ASSUMES:
#   - Auth is a session cookie (_itildesk_session) from your own browser
#     DevTools. It expires; re-copy it when it stops working. The same token is
#     used for every request in the run.
#   - "Business days" = Mon-Fri; holidays are not considered. Partial days
#     count fractionally.
#
# Usage: edit the CONFIG values below, then run:
#   powershell -ExecutionPolicy Bypass -File Get-TicketOverview.ps1

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

$Subdomain     = Resolve-FSConfigValue -Environment $env:FSVC_SUBDOMAIN -Default "acme"
$SessionCookie = Resolve-FSConfigValue -Environment $env:FSVC_ITILDESK_SESSION -Default "PASTE_YOUR_itildesk_session_VALUE_HERE"

# List 1: fully customizable query_hash conditions (JSON array). Default:
# unassigned unresolved tickets.
$UnassignedQueryHash = @'
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["-1"],"type":"default"}]
'@

# Lists 2 and 3: the set that gets scanned per-ticket for its latest message.
# Default: self-assigned unresolved tickets.
$AssignedQueryHash = @'
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]
'@

$OlderThanDays = 2                        # list 2 threshold, in business days
$PerPage       = 100
$MaxPages      = 1000                     # safety cap on pagination

# ---------------------------------------------------------------------------
# Request helpers
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

# Parses API JSON. PowerShell 7.5+ keeps ISO timestamp strings verbatim via
# -DateKind String; older versions parse them into DateTime (the date helpers
# below convert those back to DateTimeOffset).
function ConvertFrom-FSJson {
    param([Parameter(Mandatory, ValueFromPipeline)][string]$Json)
    if ((Get-Command ConvertFrom-Json).Parameters.ContainsKey("DateKind")) {
        return $Json | ConvertFrom-Json -DateKind String
    }
    return $Json | ConvertFrom-Json
}

function ConvertTo-FSDateTimeOffset {
    param([AllowNull()]$Value)
    if ($null -eq $Value) { return $null }
    if ($Value -is [datetimeoffset]) { return $Value }
    if ($Value -is [datetime]) { return [datetimeoffset]$Value }
    $parsed = [datetimeoffset]::MinValue
    if ([datetimeoffset]::TryParse([string]$Value, [ref]$parsed)) { return $parsed }
    return $null
}

# ---------------------------------------------------------------------------
# Classification helpers
# ---------------------------------------------------------------------------

# Number of business days between two instants, weekends skipped, partial
# start/end days counting fractionally. Both instants are normalised to the
# same UTC offset first so the result does not depend on the machine timezone.
function Get-BusinessDaysBetween {
    param([datetimeoffset]$From, [datetimeoffset]$To)

    $from = $From.ToOffset($From.Offset)
    $to = $To.ToOffset($From.Offset)
    if ($to -lt $from) {
        $swap = $from; $from = $to; $to = $swap
    }

    $start = [datetimeoffset]::new($from.Year, $from.Month, $from.Day, 0, 0, 0, $from.Offset)
    $end = [datetimeoffset]::new($to.Year, $to.Month, $to.Day, 0, 0, 0, $to.Offset)

    $full = 0.0
    for ($d = $start; $d -lt $end; $d = $d.AddDays(1)) {
        if ($d.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $d.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
            $full++
        }
    }

    $fracFrom = 0.0
    if ($from.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $from.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
        $fracFrom = ($from - $start).TotalMinutes / 1440
    }
    $fracTo = 0.0
    if ($to.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $to.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
        $fracTo = ($to - $end).TotalMinutes / 1440
    }
    return $full - $fracFrom + $fracTo
}

# Decides which list a self-assigned ticket belongs to, mirroring the CLI's
# Classify: unassigned / awaiting_agent / waiting / none.
function Get-TicketCategory {
    param(
        [AllowNull()]$ResponderID,
        [AllowNull()]$LastMessage,
        [int64]$LastUserID,
        [datetimeoffset]$CreatedAt,
        [double]$OlderThanDays,
        [datetimeoffset]$Now
    )

    if ($null -eq $ResponderID -or [int64]$ResponderID -lt 0) {
        return "unassigned"
    }

    $last = ConvertTo-FSDateTimeOffset $LastMessage
    if ($null -ne $last -and $LastUserID -ne [int64]$ResponderID) {
        return "awaiting_agent"
    }

    $ref = $CreatedAt
    if ($null -ne $last) { $ref = $last }
    if ((Get-BusinessDaysBetween -From $ref -To $Now) -gt $OlderThanDays) {
        return "waiting"
    }
    return "none"
}

# Most recent conversation for a ticket, or $null when it has none. Takes the
# latest message of any kind (private note or public reply).
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

function Format-OverviewLink {
    param([string]$BaseUrl, [int64]$Id)
    return "$BaseUrl/a/tickets/$Id"
}

function Format-Days {
    param([double]$Days)
    $rounded = [math]::Round($Days, 1, [System.MidpointRounding]::AwayFromZero)
    return $rounded.ToString("0.0", [System.Globalization.CultureInfo]::InvariantCulture)
}

# Paginates a tickets query, returning every ticket.
function Get-FSTickets {
    param([string]$QueryHash)
    $tickets = @()
    $page = 1
    do {
        $query = @{
            "order_by"   = "created_at"
            "order_type" = "asc"
            "per_page"   = $PerPage
            "query_hash" = $QueryHash
            "page"       = $page
        }
        $data = (Invoke-FSGet -Path "tickets" -Query $query) | ConvertFrom-FSJson
        $tickets += @($data.tickets)
        $hasNext = $data.meta.has_next
        $page++
    } while ($hasNext -and $page -lt $MaxPages)
    return $tickets
}

function Write-OverviewSection {
    param([string]$Title, $Rows)
    Write-Host ""
    Write-Host ("## {0} ({1})" -f $Title, @($Rows).Count) -ForegroundColor Cyan
    Write-Host ""
    if (@($Rows).Count -eq 0) {
        Write-Host "(none)"
        return
    }
    $Rows | Format-Table -Property Subject, Link, Days -AutoSize | Out-Host
}

# Allow dot-sourcing: `path . Get-TicketOverview.ps1` defines the helper
# functions without running the script body.
if ($MyInvocation.InvocationName -eq '.') { return }

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

if ($SessionCookie -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$SessionCookie at the top of the script." -ForegroundColor Red
    exit 1
}

# ---------------------------------------------------------------------------
# Collect
# ---------------------------------------------------------------------------

$now = [datetimeoffset]::Now

$unassigned = @(Get-FSTickets -QueryHash $UnassignedQueryHash)
$assigned = @(Get-FSTickets -QueryHash $AssignedQueryHash)

Write-Host ("Scanned {0} assigned + {1} unassigned tickets" -f $assigned.Count, $unassigned.Count)

# ---------------------------------------------------------------------------
# Classify
# ---------------------------------------------------------------------------

$unassignedRows = @()
foreach ($t in $unassigned) {
    $created = ConvertTo-FSDateTimeOffset $t.created_at
    $days = 0.0
    if ($null -ne $created) { $days = Get-BusinessDaysBetween -From $created -To $now }
    $unassignedRows += [pscustomobject]@{
        Subject = $t.subject
        Link    = Format-OverviewLink -BaseUrl $BaseUrl -Id $t.id
        Days    = Format-Days $days
    }
}

$waitingRows = @()
$awaitingRows = @()
foreach ($t in $assigned) {
    $latest = Get-LatestConversation -TicketId $t.id
    $lastMessage = $null
    $lastUser = [int64]0
    if ($null -ne $latest) {
        $lastMessage = $latest.CreatedAt
        $lastUser = $latest.UserID
    }
    $category = Get-TicketCategory -ResponderID $t.responder_id -LastMessage $lastMessage -LastUserID $lastUser -CreatedAt (ConvertTo-FSDateTimeOffset $t.created_at) -OlderThanDays $OlderThanDays -Now $now

    $ref = ConvertTo-FSDateTimeOffset $t.created_at
    if ($null -ne $lastMessage) { $ref = $lastMessage }
    $days = 0.0
    if ($null -ne $ref) { $days = Get-BusinessDaysBetween -From $ref -To $now }

    $row = [pscustomobject]@{
        Subject = $t.subject
        Link    = Format-OverviewLink -BaseUrl $BaseUrl -Id $t.id
        Days    = Format-Days $days
    }
    # Days as a number for ordering (the formatted string above is for display).
    $row | Add-Member -NotePropertyName DaysValue -NotePropertyValue $days
    switch ($category) {
        "waiting"        { $waitingRows += $row }
        "awaiting_agent" { $awaitingRows += $row }
    }
}

$waitingRows = @($waitingRows | Sort-Object -Property DaysValue -Descending)
$awaitingRows = @($awaitingRows | Sort-Object -Property DaysValue -Descending)

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------

Write-OverviewSection -Title "Unassigned" -Rows $unassignedRows
Write-OverviewSection -Title ("Waiting on customer > {0} business days - follow up or resolve" -f $OlderThanDays) -Rows $waitingRows
Write-OverviewSection -Title "Last reply from someone else, awaiting agent" -Rows $awaitingRows