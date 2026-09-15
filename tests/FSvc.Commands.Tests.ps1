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
$body = Invoke-FSvcGet -Path "tickets" -Query @{ per_page = 1 } -Config @{ BaseUrl = 'http://stub'; SessionCookie = 'x' }
Assert-Equal $body '{"ok":true}' "get returns the transport response"
Assert-Equal $script:StubCalls.Count 1 "transport called once"
Assert-Equal $script:StubCalls[0].Method "GET" "method recorded"
Assert-Equal $script:StubCalls[0].Path "tickets" "path recorded"
Assert-Equal $script:StubCalls[0].Query.per_page 1 "query recorded"

New-StubTransport -Handler { param($Request) '{"ticket":{}}' }
$null = Invoke-FSvcPut -Path "tickets/10" -Body @{ priority = 1 } -Config @{ BaseUrl = 'http://stub'; SessionCookie = 'x'; CsrfToken = 't' }
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
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; SessionCookie = 'x'; CsrfToken = 't' }
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
$script:FSvcConfig = @{ BaseUrl = 'http://stub'; SessionCookie = 'x'; CsrfToken = 't' }
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

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
