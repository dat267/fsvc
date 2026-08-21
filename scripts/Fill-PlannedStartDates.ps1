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
#     DevTools. It expires; re-copy it when it stops working. Server-side
#     rotation of that cookie is handled within a single run.
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

# ---------------------------------------------------------------------------
# CONFIG - edit these before running
# ---------------------------------------------------------------------------

$Subdomain    = "acme"                   # your Freshservice subdomain
$SessionCookie = "PASTE_YOUR_itildesk_session_VALUE_HERE"
$CsrfToken     = "PASTE_YOUR_X-CSRF-Token_VALUE_HERE"   # required for PUT

# query_hash filter (JSON array of conditions). Default: self-assigned
# unresolved tickets, same as the CLI's fill-start-dates command.
$Filter = @'
[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]
'@

$PerPage  = 100
$Confirm  = $true                        # prompt before applying (set $false to auto-apply)

# ---------------------------------------------------------------------------
# Session / request header helpers (keep the server's rotated cookie in sync)
# ---------------------------------------------------------------------------

$BaseUrl = "https://$Subdomain.freshservice.com"

$script:rotatedSession = $SessionCookie

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
        "Cookie" = "_itildesk_session=$script:rotatedSession"
    }
    $resp = Invoke-WebRequest -Uri "$BaseUrl/api/_/$Path" -Headers $getHeaders -UseBasicParsing
    Update-SessionCookie $resp
    return $resp.Content
}

function Invoke-FSPut {
    param([string]$Path, [hashtable]$Body)
    $putHeaders = @{
        "Accept"        = "application/json"
        "Cookie"        = "_itildesk_session=$script:rotatedSession"
        "Content-Type"  = "application/json; charset=utf-8"
        "X-CSRF-Token"  = $CsrfToken
    }
    $json = $Body | ConvertTo-Json -Compress
    $resp = Invoke-WebRequest -Uri "$BaseUrl/api/_/$Path" -Method Put -Headers $putHeaders -Body $json -UseBasicParsing
    Update-SessionCookie $resp
    return $resp.Content
}

# The private API rotates _itildesk_session via Set-Cookie; capture it so
# subsequent requests in this run stay authenticated.
function Update-SessionCookie {
    param($Response)
    $setCookie = $Response.Headers["Set-Cookie"]
    if ($setCookie) {
        if ($setCookie -match "_itildesk_session=([^;]+)") {
            $script:rotatedSession = $Matches[1]
        }
    }
}

function Add-BusinessDays {
    param([datetime]$Start, [int]$Days)
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

# Round up to the next quarter hour (:00/:15/:30/:45).
function Get-Minutes {
    param([datetime]$t)
    return $t.Hour * 60 + $t.Minute
}

function Round-Up-QuarterHour {
    param([datetime]$t)
    $total = Get-Minutes $t
    if ($t.Second -ne 0 -or ($total % 15) -ne 0) {
        $total = [math]::Floor($total / 15) * 15 + 15
    }
    $base = [datetime]::new($t.Year, $t.Month, $t.Day, 0, 0, 0, $t.Kind)
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
    $data = $content | ConvertFrom-Json
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
    $at = [datetime]::Parse($t.created_at)
    $to = (Round-Up-QuarterHour $at).ToString("yyyy-MM-ddTHH:mm:ssZ")
    $changes += [pscustomobject]@{
        Id   = $t.id
        From = $t.planned_start_date
        To   = $to
    }
}

if ($changes.Count -eq 0) {
    Write-Host "No changes needed."
    exit 0
}

foreach ($c in $changes) {
    Write-Host ("[planned_start_date] ticket {0}: {1} -> {2}" -f $c.Id, $c.From, $c.To)
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
        Invoke-FSPut -Path ("tickets/{0}" -f $c.Id) -Body @{ planned_start_date = $c.To } | Out-Null
        Write-Host ("OK: ticket {0}" -f $c.Id)
        $applied++
    } catch {
        Write-Host ("FAILED: ticket {0}: {1}" -f $c.Id, $_.Exception.Message) -ForegroundColor Yellow
    }
}

Write-Host ("Done: {0}/{1} applied" -f $applied, $changes.Count)