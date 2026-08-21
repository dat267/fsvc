#!/usr/bin/env pwsh
# Get-TicketList.ps1
#
# Standalone script (not part of the fsvc CLI) that lists tickets from the
# Freshservice private API (/api/_/) by a saved-filter ID OR a raw query_hash.
#
# WHAT THIS IS: a convenience helper, NOT an official Freshservice API client.
# It talks to the undocumented private API using your browser session cookie.
# That API is reverse-engineered and can change at any time.
#
# WHAT IT ASSUMES:
#   - Auth is a session cookie (_itildesk_session) from your own browser
#     DevTools. It expires; re-copy it when it stops working. Server-side
#     rotation of that cookie is handled within a single run.
#   - Exactly one of $FilterId / $QueryHash must be set. $FilterId passes
#     filter=<id> directly to the tickets endpoint; $QueryHash passes
#     query_hash=<json>.
#   - The private API is undocumented; status numbering, query_hash conditions,
#     and per_page limits can vary between Freshservice accounts.
#
# Usage:
#   Fill in the CONFIG variables below, then run:
#   powershell -ExecutionPolicy Bypass -File Get-TicketList.ps1
#
# Output is the ticket array piped to PowerShell's Format-Table using the
# columns in $Properties.

# ---------------------------------------------------------------------------
# CONFIG - edit these before running
# ---------------------------------------------------------------------------

$Subdomain    = "acme"                   # your Freshservice subdomain
$SessionCookie = "PASTE_YOUR_itildesk_session_VALUE_HERE"

# Exactly ONE of these must be set:
$FilterId     = 0                        # saved ticket filter ID (0 = off)
$QueryHash    = ""                       # raw query_hash JSON (empty = off)
# e.g. $QueryHash = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]'

$PerPage      = 100
$OrderBy      = "created_at"
$OrderType    = "asc"
$MaxPages     = 1000                     # safety cap on pagination

# Columns shown by Format-Table (ticket JSON field names).
$Properties   = @("id", "subject", "status", "priority", "requester_name", "created_at")

# ---------------------------------------------------------------------------
# Session / request helpers (keep the server's rotated cookie in sync)
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

# Allow dot-sourcing: `path . Get-TicketList.ps1` defines the helper functions
# without running the script body. Run it directly to list tickets.
if ($MyInvocation.InvocationName -eq '.') { return }

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

if ($SessionCookie -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$SessionCookie at the top of the script." -ForegroundColor Red
    exit 1
}
$sel = @($FilterId -ne 0 -and $FilterId -ne $null) + @($QueryHash -ne "")
if (($sel | Where-Object { $_ }).Count -ne 1) {
    Write-Host "ERROR: set exactly ONE of \$FilterId (nonzero) or \$QueryHash (non-empty)." -ForegroundColor Red
    exit 1
}

# ---------------------------------------------------------------------------
# Collect tickets (paginate until meta.has_next is false)
# ---------------------------------------------------------------------------

$query = @{
    "order_by"   = $OrderBy
    "order_type" = $OrderType
    "per_page"   = $PerPage
}
if ($FilterId -ne 0) {
    $query["filter"] = $FilterId
}
if ($QueryHash -ne "") {
    $query["query_hash"] = $QueryHash
}

$tickets = @()
$page = 1
do {
    $query["page"] = $page
    $content = Invoke-FSGet -Path "tickets" -Query $query
    $data = $content | ConvertFrom-Json
    $tickets += @($data.tickets)
    $hasNext = $data.meta.has_next
    $page++
} while ($hasNext -and $page -lt $MaxPages)

Write-Host ("Scanned {0} tickets" -f $tickets.Count)
$tickets | Format-Table -Property $Properties -AutoSize