#!/usr/bin/env pwsh
# Get-TicketContent.Tests.ps1
#
# Regression tests for Get-TicketContent.ps1 (zero dependencies, no Pester).
# Dot-sources the script so its helper functions can be exercised without
# touching the network.
#
#   pwsh scripts/Get-TicketContent.Tests.ps1

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "Get-TicketContent.ps1")

$failures = 0

function Assert-Equal {
    param($Actual, $Expected, [string]$Label)
    if ($Actual -ne $Expected) {
        Write-Host ("FAIL: {0}`n  expected: {1}`n  actual:   {2}" -f $Label, $Expected, $Actual) -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray
    }
}

function Assert-Match {
    param([string]$Actual, [string]$Pattern, [string]$Label)
    if ($Actual -notmatch $Pattern) {
        Write-Host ("FAIL: {0}`n  pattern: {1}`n  actual:  {2}" -f $Label, $Pattern, $Actual) -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray
    }
}

Write-Host "== ConvertTo-PlainText ==" -ForegroundColor Cyan

Assert-Equal (ConvertTo-PlainText "<p>Hello <b>world</b></p>") "Hello world" "strips tags"
Assert-Equal (ConvertTo-PlainText "A &amp; B &lt;x&gt;") "A & B <x>" "decodes entities"
Assert-Equal (ConvertTo-PlainText "  <p>x</p>  ") "x" "trims whitespace"
Assert-Equal (ConvertTo-PlainText "") "" "empty input yields empty"
Assert-Equal (ConvertTo-PlainText $null) "" "null input yields empty"
Assert-Equal (ConvertTo-PlainText "<p>one</p><p>two</p>") "one`ntwo" "paragraphs become newlines"

Write-Host "== Get-ConversationAuthor ==" -ForegroundColor Cyan

$named = [pscustomobject]@{ user_id = 3100; user = [pscustomobject]@{ name = "Nadia Rahman" } }
Assert-Equal (Get-ConversationAuthor -Conversation $named) "Nadia Rahman" "prefers nested user.name"
$numeric = [pscustomobject]@{ user_id = 2100 }
Assert-Equal (Get-ConversationAuthor -Conversation $numeric) "2100" "falls back to user_id"
Assert-Equal (Get-ConversationAuthor -Conversation $null) "" "null conversation yields empty"

Write-Host "== Get-ConversationDirection ==" -ForegroundColor Cyan

Assert-Equal (Get-ConversationDirection -Conversation ([pscustomobject]@{ incoming = $true })) "incoming" "incoming true"
Assert-Equal (Get-ConversationDirection -Conversation ([pscustomobject]@{ incoming = $false })) "outgoing" "incoming false"
Assert-Equal (Get-ConversationDirection -Conversation ($null)) "outgoing" "null defaults to outgoing"

Write-Host "== Format-FieldValue ==" -ForegroundColor Cyan

$ticket = [pscustomobject]@{
    status      = 2
    status_name = "Open"
    priority    = 3
    group_id    = 4001
    subject     = "Printer"
}
Assert-Equal (Format-FieldValue -Ticket $ticket -Key "status") "Open" "prefers <key>_name"
Assert-Equal (Format-FieldValue -Ticket $ticket -Key "priority") "3" "falls back to raw value"
Assert-Equal (Format-FieldValue -Ticket $ticket -Key "group") "4001" "resolves group_id fallback key"
Assert-Equal (Format-FieldValue -Ticket $ticket -Key "subject") "Printer" "plain key"
Assert-Equal (Format-FieldValue -Ticket $ticket -Key "missing") "" "missing key yields empty"

Write-Host "== Format-TicketContents ==" -ForegroundColor Cyan

$t = [pscustomobject]@{
    id               = 10100
    display_id       = 10100
    subject          = "Printer not working"
    status           = 2
    status_name      = "Open"
    priority         = 2
    priority_name    = "Medium"
    urgency          = 2
    impact           = 2
    group_id         = 4001
    group_name       = "Support"
    requester_name   = "Omar Saleh"
    responder_name   = "Nadia Rahman"
    department_name  = "IT"
    created_at       = "2026-08-01T10:00:00+04:00"
    updated_at       = "2026-08-02T09:00:00+04:00"
    description_text = "Printer jammed"
}
$convs = @(
    [pscustomobject]@{ id = 1; user_id = 2100; incoming = $true; created_at = "2026-08-01T10:30:00+04:00"; body_text = "Please fix the printer" },
    [pscustomobject]@{ id = 2; user = [pscustomobject]@{ name = "Nadia Rahman" }; incoming = $false; created_at = "2026-08-01T11:00:00+04:00"; body = "<p>Will do</p>" }
)

$out = Format-TicketContents -Ticket $t -Conversations $convs
Assert-Match $out "Ticket #10100 — Printer not working" "title with display id and subject"
Assert-Match $out "Status\s+:\s+Open" "aligned status name"
Assert-Match $out "Priority\s+:\s+Medium" "priority name"
Assert-Match $out "Requester\s+:\s+Omar Saleh" "requester name"
Assert-Match $out "Printer jammed" "description text"
Assert-Match $out "Conversations \(2\)" "conversation count"
Assert-Match $out "2100 \(incoming, 2026-08-01T10:30:00\+04:00\)" "numeric author, incoming"
Assert-Match $out "Please fix the printer" "conversation body_text"
Assert-Match $out "Nadia Rahman \(outgoing" "named author preferred, outgoing"
Assert-Match $out "Will do" "HTML body rendered as plain text"

# Fallbacks: no display_id, no description_text, conversation without body.
$t2 = [pscustomobject]@{ id = 7; subject = "No display id"; description = "<p>From HTML</p>"; status = 5 }
$out2 = Format-TicketContents -Ticket $t2 -Conversations @()
Assert-Match $out2 "Ticket #7 " "id fallback when display_id missing"
Assert-Match $out2 "From HTML" "description falls back to stripped HTML"
Assert-Match $out2 "Conversations \(0\)" "zero conversations"

# Conversation with no body text.
$t3 = [pscustomobject]@{ id = 8; subject = "Empty body" }
$c3 = @([pscustomobject]@{ id = 1; user_id = 5; incoming = $true; created_at = "2026-08-01T10:30:00Z" })
$out3 = Format-TicketContents -Ticket $t3 -Conversations $c3
Assert-Match $out3 "\(no body\)" "missing conversation body prints placeholder"

Write-Host "== JSON date preservation ==" -ForegroundColor Cyan

# Regression: ConvertFrom-Json turns ISO timestamps into DateTime and renders
# them in local culture format (e.g. 08/01/2026 13:00:00), losing the API's
# offset. The script's JSON helper must keep timestamps verbatim.
$parsed = ConvertFrom-FSJson -Json '{"ticket":{"id":1,"subject":"T","created_at":"2026-08-01T10:00:00+04:00"},"conversations":[{"id":1,"user_id":2,"incoming":true,"created_at":"2026-08-01T10:30:00+04:00","body_text":"hi"}]}'
$parsedOut = Format-TicketContents -Ticket $parsed.ticket -Conversations $parsed.conversations
Assert-Match $parsedOut "2026-08-01T10:00:00\+04:00" "ticket timestamp keeps API offset"
Assert-Match $parsedOut "2026-08-01T10:30:00\+04:00" "conversation timestamp keeps API offset"
Assert-Equal (Format-Timestamp ([datetimeoffset]::Parse("2026-08-01T10:00:00+04:00"))) "2026-08-01T10:00:00+04:00" "Format-Timestamp renders ISO offset"
Assert-Equal (('{"t":"piped"}' | ConvertFrom-FSJson).t) "piped" "accepts JSON from the pipeline"

Write-Host "== Resolve-FSConfigValue (shared env overrides embedded config) ==" -ForegroundColor Cyan
Assert-Equal (Resolve-FSConfigValue -Environment "from-env" -Default "embedded") "from-env" "env value wins"
Assert-Equal (Resolve-FSConfigValue -Environment "" -Default "embedded") "embedded" "empty env falls back"
Assert-Equal (Resolve-FSConfigValue -Environment $null -Default "embedded") "embedded" "null env falls back"

Write-Host ""
if ($failures -gt 0) {
    Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed." -ForegroundColor Green
exit 0
