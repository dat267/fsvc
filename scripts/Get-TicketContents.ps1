#!/usr/bin/env pwsh
# Get-TicketContents.ps1
#
# Standalone script (not part of the fsvc CLI) that shows one Freshservice
# ticket and its conversation trace from the private API (/api/_/). Mirrors
# the CLI's `fsvc tickets show`.
#
# WHAT THIS IS: a convenience helper, NOT an official Freshservice API client.
# It talks to the undocumented private API using your browser session cookie.
# That API is reverse-engineered and can change at any time.
#
# WHAT IT ASSUMES:
#   - Auth is a session cookie (_itildesk_session) from your own browser
#     DevTools. It expires; re-copy it when it stops working. The same token is
#     used for every request in the run.
#   - The private API is undocumented; field names and conversation shapes can
#     vary between Freshservice accounts. Unknown fields are omitted.
#
# Usage:
#   Fill in the CONFIG variables below, then run:
#
#   # print a readable summary to the terminal (default)
#   pwsh scripts/Get-TicketContents.ps1 -Id 10100
#
#   # emit raw objects instead, for piping. Run this form inside a PowerShell
#   # session: a child `pwsh script.ps1` process serialises its output to text,
#   # so only same-session invocation lets objects reach the next command.
#   ./scripts/Get-TicketContents.ps1 -Id 10100 -AsObject | ConvertTo-Json -Depth 10
#   ./scripts/Get-TicketContents.ps1 -Id 10100 -AsObject | Select-Object -ExpandProperty Conversations

param(
    [Parameter(Position = 0)]
    [int]$Id = 0,          # ticket ID to fetch
    [switch]$AsObject      # emit { Ticket, Conversations } to the pipeline instead of printing
)

# ---------------------------------------------------------------------------
# CONFIG - edit these before running
# ---------------------------------------------------------------------------

$Subdomain     = "acme"                   # your Freshservice subdomain
$SessionCookie = "PASTE_YOUR_itildesk_session_VALUE_HERE"

$PerPage       = 100                      # conversations per page
$MaxPages      = 1000                     # safety cap on pagination

# ---------------------------------------------------------------------------
# Request helpers
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

# Parses API JSON. PowerShell 7.5+ can keep ISO timestamp strings verbatim via
# -DateKind String; older versions parse them into DateTime (the renderer then
# formats those back to ISO 8601). Without this, timestamps print in local
# culture format and the account's offset is lost.
function ConvertFrom-FSJson {
    param([Parameter(Mandatory, ValueFromPipeline)][string]$Json)
    if ((Get-Command ConvertFrom-Json).Parameters.ContainsKey("DateKind")) {
        return $Json | ConvertFrom-Json -DateKind String
    }
    return $Json | ConvertFrom-Json
}

# Renders a timestamp as ISO 8601 (with offset when available).

# ---------------------------------------------------------------------------
# Rendering helpers
# ---------------------------------------------------------------------------

# Strips HTML tags and decodes entities so conversation bodies print readably.
# Block-level closing tags become newlines; each line is trimmed.
function ConvertTo-PlainText {
    param([AllowNull()][string]$Html)
    if ([string]::IsNullOrEmpty($Html)) {
        return ""
    }
    $text = $Html -replace '(?i)<(br|/p|/div|/li|/tr)/?>', "`n"
    $text = $text -replace '(?s)<[^>]*>', ''
    $text = [System.Net.WebUtility]::HtmlDecode($text)
    $text = ($text -split "`n" | ForEach-Object { $_.Trim() }) -join "`n"
    return $text.Trim()
}

# Author display: nested user.name when present, else the numeric user_id.
function Get-ConversationAuthor {
    param($Conversation)
    if ($null -eq $Conversation) {
        return ""
    }
    if ($Conversation.user -and $Conversation.user.name) {
        return [string]$Conversation.user.name
    }
    return [string]$Conversation.user_id
}

function Get-ConversationDirection {
    param($Conversation)
    if ($null -ne $Conversation -and $Conversation.incoming) {
        return "incoming"
    }
    return "outgoing"
}

# Renders a value as an ISO 8601 timestamp when it is a date type, otherwise
# returns it unchanged. Fallback for PowerShell versions that parse dates.
function Format-Timestamp {
    param($Value)
    if ($Value -is [datetimeoffset] -or $Value -is [datetime]) {
        return $Value.ToString("yyyy-MM-ddTHH:mm:sszzz")
    }
    return "$Value"
}

# Resolves a metadata value, preferring the human-readable <key>_name field,
# then a <key>_id field, then the raw <key>. Missing values yield "".
function Format-FieldValue {
    param($Ticket, [string]$Key)
    if ($null -eq $Ticket) {
        return ""
    }
    $props = @($Ticket.PSObject.Properties.Name)

    $nameKey = "${Key}_name"
    if ($props -contains $nameKey -and "$($Ticket.$nameKey)" -ne "") {
        return "$($Ticket.$nameKey)"
    }

    $idKey = "${Key}_id"
    $keys = @($Key)
    if ($props -contains $idKey) {
        $keys = @($idKey, $Key)
    }
    foreach ($k in $keys) {
        if (($props -contains $k) -and "$($Ticket.$k)" -ne "") {
            return Format-Timestamp $Ticket.$k
        }
    }
    return ""
}

# Renders a ticket and its conversations as a readable plain-text document.
function Format-TicketContents {
    param($Ticket, $Conversations)

    $lines = [System.Collections.Generic.List[string]]::new()

    $display = Format-FieldValue -Ticket $Ticket -Key "display_id"
    if (-not $display) { $display = Format-FieldValue -Ticket $Ticket -Key "id" }
    $lines.Add(("Ticket #{0} — {1}" -f $display, $Ticket.subject))
    $lines.Add("")

    $fields = @(
        @{ Label = "Status";     Key = "status" },
        @{ Label = "Priority";   Key = "priority" },
        @{ Label = "Urgency";    Key = "urgency" },
        @{ Label = "Impact";     Key = "impact" },
        @{ Label = "Group";      Key = "group" },
        @{ Label = "Requester";  Key = "requester" },
        @{ Label = "Responder";  Key = "responder" },
        @{ Label = "Department"; Key = "department" },
        @{ Label = "Created";    Key = "created_at" },
        @{ Label = "Updated";    Key = "updated_at" }
    )
    $width = ($fields | ForEach-Object { $_.Label.Length } | Measure-Object -Maximum).Maximum
    foreach ($f in $fields) {
        $value = Format-FieldValue -Ticket $Ticket -Key $f.Key
        $lines.Add(("{0} : {1}" -f $f.Label.PadRight($width), $value))
    }
    $lines.Add("")

    $desc = $Ticket.description_text
    if (-not $desc -and $Ticket.description) {
        $desc = ConvertTo-PlainText -Html $Ticket.description
    }
    if ($desc) {
        $lines.Add("$desc")
        $lines.Add("")
    }

    $convs = @($Conversations)
    $lines.Add(("Conversations ({0})" -f $convs.Count))
    $lines.Add("")
    $n = 0
    foreach ($c in $convs) {
        $n++
        $author = Get-ConversationAuthor -Conversation $c
        $dir = Get-ConversationDirection -Conversation $c
        $lines.Add(("--- [{0}] {1} ({2}, {3})" -f $n, $author, $dir, (Format-Timestamp $c.created_at)))
        $body = $c.body_text
        if (-not $body -and $c.body) {
            $body = ConvertTo-PlainText -Html $c.body
        }
        if ($body) {
            $lines.Add("$body")
        } else {
            $lines.Add("(no body)")
        }
        $lines.Add("")
    }

    return ($lines -join "`n")
}

# Allow dot-sourcing: `path . Get-TicketContents.ps1` defines the helper
# functions without running the script body. Run it directly to fetch a ticket.
if ($MyInvocation.InvocationName -eq '.') { return }

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

if ($SessionCookie -match "PASTE_YOUR") {
    Write-Host "ERROR: fill in \$SessionCookie at the top of the script." -ForegroundColor Red
    exit 1
}
if ($Id -le 0) {
    Write-Host "ERROR: pass a ticket ID, e.g. -Id 10100." -ForegroundColor Red
    exit 1
}

# ---------------------------------------------------------------------------
# Fetch
# ---------------------------------------------------------------------------

$ticket = (Invoke-FSGet -Path ("tickets/{0}" -f $Id) | ConvertFrom-FSJson).ticket
if (-not $ticket) {
    Write-Host ("ERROR: ticket {0} not found." -f $Id) -ForegroundColor Red
    exit 1
}

$conversations = @()
$page = 1
do {
    $query = @{
        "order_by"   = "created_at"
        "order_type" = "asc"
        "per_page"   = $PerPage
        "page"       = $page
    }
    $data = (Invoke-FSGet -Path ("tickets/{0}/conversations" -f $Id) -Query $query) | ConvertFrom-FSJson
    $conversations += @($data.conversations)
    $hasNext = $data.meta.has_next
    $page++
} while ($hasNext -and $page -lt $MaxPages)

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

if ($AsObject) {
    [pscustomobject]@{
        Ticket        = $ticket
        Conversations = $conversations
    }
    exit 0
}

Write-Host (Format-TicketContents -Ticket $ticket -Conversations $conversations)
