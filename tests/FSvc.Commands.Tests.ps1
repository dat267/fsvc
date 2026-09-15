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

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
