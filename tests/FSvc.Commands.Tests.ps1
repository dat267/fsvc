#!/usr/bin/env pwsh
# FSvc.Commands.Tests.ps1 - command-level tests using an injected transport.
# Dot-sources Private + Public so commands run without a real server.

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Get-ChildItem -LiteralPath (Join-Path $repoRoot 'Private') -Filter '*.ps1' -File | Sort-Object Name | ForEach-Object { . $_.FullName }
Get-ChildItem -LiteralPath (Join-Path $repoRoot 'Public') -Filter '*.ps1' -File | Sort-Object Name | ForEach-Object { . $_.FullName }

$failures = 0
function Assert-Equal {
    param($Actual, $Expected, [string]$Label)
    if ($Actual -ne $Expected) {
        Write-Host ("FAIL: {0}`n  expected: {1}`n  actual:   {2}" -f $Label, $Expected, $Actual) -ForegroundColor Red
        $script:failures++
    } else { Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray }
}
function Assert-True {
    param([bool]$Actual, [string]$Label)
    if (-not $Actual) { Write-Host ("FAIL: {0}" -f $Label) -ForegroundColor Red; $script:failures++ }
    else { Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray }
}

# Records requests and returns a scripted response.
function New-StubTransport {
    param([scriptblock]$Handler)
    $script:StubCalls = @()
    $script:StubHandler = $Handler
    $script:FSvcTransport = {
        param($Request)
        $script:StubCalls += $Request
        & $script:StubHandler $Request
    }
}

Write-Host "== Transport seam ==" -ForegroundColor Cyan
New-StubTransport -Handler { param($Request) '{"ok":true}' }
$body = Invoke-FSvcGet -Path "tickets" -Query @{ per_page = 1 } -Config @{ BaseUrl = 'http://stub'; ItildeskSession = 'x' }
Assert-Equal $body '{"ok":true}' "get returns the transport response"
Assert-Equal $script:StubCalls.Count 1 "transport called once"
Assert-Equal $script:StubCalls[0].Method "GET" "method recorded"
Assert-Equal $script:StubCalls[0].Path "tickets" "path recorded"
Assert-Equal $script:StubCalls[0].Query.per_page 1 "query recorded"

New-StubTransport -Handler { param($Request) '{"ticket":{}}' }
$null = Invoke-FSvcPut -Path "tickets/10" -Body @{ priority = 1 } -Config @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
Assert-Equal $script:StubCalls[0].Method "PUT" "put method recorded"
Assert-Equal $script:StubCalls[0].Body.priority 1 "put body recorded"
$script:FSvcTransport = $null

Write-Host "== Invoke-FSvcChangeSet ==" -ForegroundColor Cyan
$script:applied = 0
$never = Invoke-FSvcChangeSet -Change @([pscustomobject]@{ Id = 1; Field = 'planned_start_date'; From = $null; To = '2026-01-01T00:00:00Z' }) `
    -Should { param($Target, $Action) $false } `
    -Apply { param($c) $script:applied++ } `
    -LockName ("fsvc-test-" + [guid]::NewGuid().ToString()) -Config @{}
Assert-Equal $never.Count 1 "one change emitted"
Assert-Equal $never[0].Applied $false "declined change marked not applied"
Assert-Equal $script:applied 0 "apply not called when declined"

$script:applied = 0
$lockName = "fsvc-test-" + [guid]::NewGuid().ToString()
$done = Invoke-FSvcChangeSet -Change @([pscustomobject]@{ Id = 2; Field = 'planned_start_date'; From = $null; To = '2026-01-01T00:00:00Z' }) `
    -Should { param($Target, $Action) $true } `
    -Apply { param($c) $script:applied++ } `
    -LockName $lockName -Config @{}
Assert-Equal $script:applied 1 "apply called when approved"
Assert-Equal $done[0].Applied $true "approved change marked applied"
Assert-True (-not (Test-Path -LiteralPath (Join-Path ([System.IO.Path]::GetTempPath()) ($lockName + '.lock')))) "lock released after run"

Write-Host "== Invoke-FSvcPagedQuery ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
New-StubTransport -Handler {
    param($Request)
    if ($Request.Query.page -eq 1) { '{"tickets":[{"id":1}],"meta":{"has_next":true}}' }
    else { '{"tickets":[{"id":2}],"meta":{"has_next":false}}' }
}
$all = @(Invoke-FSvcPagedQuery -Path 'tickets' -ArrayKey 'tickets' -BaseQuery @{ per_page = 1 } -Config $script:FSvcConfig)
Assert-Equal $all.Count 2 "walks both pages"
Assert-Equal $all[1].id 2 "second page item returned"
Assert-Equal @($script:StubCalls).Count 2 "two requests"
Assert-Equal $script:StubCalls[0].Query.page 1 "first request is page 1"
Assert-Equal $script:StubCalls[1].Query.page 2 "second request is page 2"

New-StubTransport -Handler { param($Request) '{"tickets":[{"id":9}],"meta":{"has_next":true}}' }
$capped = @(Invoke-FSvcPagedQuery -Path 'tickets' -ArrayKey 'tickets' -BaseQuery @{} -MaxPages 2 -Config $script:FSvcConfig)
Assert-Equal @($script:StubCalls).Count 2 "stops at MaxPages"

Write-Host "== Set-FSvcPlannedStartDates honours ShouldProcess ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
$ticketJson = '{"tickets":[{"id":10,"subject":"T","planned_start_date":null,"created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}'
New-StubTransport -Handler { param($Request) if ($Request.Method -eq 'PUT') { '{"ticket":{}}' } else { $ticketJson } }
$preview = Set-FSvcPlannedStartDates -WhatIf
Assert-Equal @($preview).Count 1 "preview emits a change"
Assert-Equal @($preview)[0].Applied $false "-WhatIf marks not applied"
Assert-True (-not (@($script:StubCalls | Where-Object { $_.Method -eq 'PUT' }).Count)) "-WhatIf issues no PUT"

New-StubTransport -Handler { param($Request) if ($Request.Method -eq 'PUT') { '{"ticket":{}}' } else { $ticketJson } }
$applied = Set-FSvcPlannedStartDates -Confirm:$false
Assert-Equal @($applied)[0].Applied $true "-Confirm:$false applies"
Assert-Equal @($script:StubCalls | Where-Object { $_.Method -eq 'PUT' }).Count 1 "one PUT issued"
Assert-Equal $script:StubCalls[1].Body.planned_start_date '2026-09-01T10:00:00+04:00' "PUT carries the rounded date"
$script:FSvcTransport = $null

Write-Host "== Get-FSvcTicketList paging ==" -ForegroundColor Cyan
New-StubTransport -Handler {
    param($Request)
    if ($Request.Query.page -eq 1) { '{"tickets":[{"id":1}],"meta":{"has_next":true}}' }
    else { '{"tickets":[{"id":2}],"meta":{"has_next":false}}' }
}
$list = @(Get-FSvcTicketList -QueryHash 'x' -PerPage 1)
Assert-Equal $list.Count 2 "command walks pages"
Assert-Equal $list[1].id 2 "second page returned"
$script:FSvcTransport = $null

Write-Host "== Update-FSvcPlannedEndDates policy via command ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't'; UtcOffset = '+04:00' }
New-StubTransport -Handler {
    param($Request)
    if ($Request.Method -eq 'PUT') { return '{"ticket":{}}' }
    if ($Request.Path -like '*/conversations') { return '{"conversations":[{"id":1,"user_id":2,"created_at":"2026-09-11T09:00:00+04:00","body_text":"x"}],"meta":{"has_next":false}}' }
    return '{"tickets":[{"id":10,"subject":"T","planned_end_date":null,"created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}'
}
$out = @(Update-FSvcPlannedEndDates -WhatIf)
Assert-Equal $out.Count 1 "one change proposed"
Assert-True ($out[0].To -match 'T17:00:00\+04:00$') "target is at the configured hour and offset"
Assert-Equal $out[0].Applied $false "-WhatIf marks not applied"
Assert-True (-not (@($script:StubCalls | Where-Object { $_.Method -eq 'PUT' }).Count)) "-WhatIf issues no PUT"
$script:FSvcTransport = $null

Write-Host "== Format-FSvcTicketContent ==" -ForegroundColor Cyan
$contentObj = [pscustomobject]@{
    Ticket        = [pscustomobject]@{ id = 10; display_id = 10; subject = "T"; status_name = "Open"; created_at = "2026-09-01T10:00:00+04:00" }
    Conversations = @(
        [pscustomobject]@{ id = 1; user_id = 2100; user = [pscustomobject]@{ name = "Nadia" }; incoming = $true; created_at = "2026-09-01T10:30:00+04:00"; body_text = "please fix" },
        [pscustomobject]@{ id = 2; user_id = 3100; incoming = $false; created_at = "2026-09-01T11:00:00+04:00"; body = "<p>will do</p>" }
    )
}
$text = $contentObj | Format-FSvcTicketContent
Assert-True ($text -match "Ticket #10 - T") "title"
Assert-True ($text -match "Nadia \(incoming, 2026-09-01T10:30:00\+04:00\)") "author, direction and timestamp line"
Assert-True ($text -match "please fix") "body_text rendered"
Assert-True ($text -match "3100 \(outgoing") "numeric author, outgoing"
Assert-True ($text -match "will do") "body fallback rendered"

Write-Host "== Get-FSvcLatestConversation view ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
New-StubTransport -Handler { param($Request) '{"conversations":[{"id":1,"user_id":2100,"user":{"name":"Nadia"},"incoming":true,"created_at":"2026-09-01T10:30:00+04:00","body_text":"x"}],"meta":{"has_next":false}}' }
$lc = Get-FSvcLatestConversation -TicketId 5 -Config $script:FSvcConfig
Assert-Equal $lc.Author "Nadia" "latest conversation is a view with author"
Assert-Equal $lc.Direction "incoming" "latest conversation direction"
Assert-Equal (Format-Iso8601 $lc.At) "2026-09-01T10:30:00+04:00" "latest conversation timestamp"
$script:FSvcTransport = $null

Write-Host "== Get-FSvcViewTickets ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
New-StubTransport -Handler { param($Request) '{"tickets":[{"id":1}],"meta":{"has_next":false}}' }
$null = Get-FSvcViewTickets -View SelfAssigned -Config $script:FSvcConfig
Assert-True ($script:StubCalls[0].Query.query_hash -match '"responder_id".*"0"') "self-assigned query hash sent"
$null = Get-FSvcViewTickets -View Unassigned -Config $script:FSvcConfig
Assert-True ($script:StubCalls[1].Query.query_hash -match '"-1"') "unassigned query hash sent"

Write-Host "== Write commands default to the self-assigned view ==" -ForegroundColor Cyan
New-StubTransport -Handler { param($Request) '{"tickets":[{"id":10,"subject":"T","planned_start_date":null,"created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}' }
$null = Set-FSvcPlannedStartDates -WhatIf
Assert-True ($script:StubCalls[0].Query.query_hash -match '"responder_id".*"0"') "default view is self-assigned"
$null = Set-FSvcPlannedStartDates -View Unassigned -WhatIf
Assert-True ($script:StubCalls[1].Query.query_hash -match '"-1"') "-View Unassigned overrides the default"
$script:FSvcTransport = $null

Write-Host "== Test-FSvcSession surfaces failures ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
$script:FSvcTransport = { param($Request) throw 'boom' }
$threw = $false
try { $null = Test-FSvcSession } catch { $threw = $true }
Assert-True $threw "a failing request throws instead of reporting Ok"
$script:FSvcTransport = $null

Write-Host "== Overview default view ==" -ForegroundColor Cyan
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; ItildeskSession = 'x'; CsrfToken = 't' }
New-StubTransport -Handler {
    param($Request)
    if ($Request.Path -like '*/conversations') { '{"conversations":[],"meta":{"has_next":false}}' }
    else { '{"tickets":[{"id":10,"subject":"T","responder_id":3100,"created_at":"2026-09-01T10:00:00+04:00"}],"meta":{"has_next":false}}' }
}
$ov = @(Get-FSvcTicketOverview -OlderThanDays 2)
Assert-Equal $ov[0].PSObject.TypeNames[0] 'FSvc.TicketOverviewRow' "overview rows carry a type name"
Update-FormatData -PrependPath (Join-Path $repoRoot 'fsvc.format.ps1xml')
$rendered = $ov[0] | Out-String
Assert-True ($rendered -match '(?m)^Link\s*:') "default view shows Link"
Assert-True ($rendered -notmatch '(?m)^Id\s*:') "default view hides Id"
Assert-True ($null -ne $ov[0].Id) "Id is still on the object for scripting"
Assert-True ($rendered -match '(?m)^Elapsed\s*:') "default view shows Elapsed"
Assert-True ($rendered -match '(?m)^Since\s*:') "default view shows the anchor timestamp"
Assert-True ($rendered -notmatch '(?m)^Days\s*:') "default view hides the raw decimal Days"
Assert-True ($ov[0].Elapsed -match '^(\d+d( \d+h)?|\d+h( \d+m)?|\d+m)$') "Elapsed is a humanized business duration"
Assert-True ($null -ne $ov[0].Since) "Since is a timestamp on the object"
Assert-True ($null -ne $ov[0].Days) "numeric Days is still on the object"
$script:FSvcTransport = $null

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
