#!/usr/bin/env pwsh
# Get-TicketList.Tests.ps1
#
# Regression tests for Get-TicketList.ps1 (zero dependencies, no Pester).
# Dot-sources the script so its helper functions can be exercised without
# touching the network.
#
#   powershell -ExecutionPolicy Bypass -File scripts\Get-TicketList.Tests.ps1

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "Get-TicketList.ps1")

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

function Assert-True {
    param([bool]$Actual, [string]$Label)
    if (-not $Actual) {
        Write-Host ("FAIL: {0}" -f $Label) -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray
    }
}

Write-Host "== Query construction ==" -ForegroundColor Cyan

# filter-ID mode
$q1 = @{ "filter" = 21000018917; "per_page" = 100 }
$url1 = Append-Query -Path "tickets" -QueryString (Build-QueryString -Query $q1)
Assert-True ($url1 -match "filter=21000018917") "filter mode: filter=id present"
Assert-True ($url1 -match "per_page=100") "filter mode: per_page present"

# query_hash mode (URL-escaped, and the '?' must survive - the 404 regression)
$qh = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]'
$q2 = @{ "query_hash" = $qh }
$url2 = Append-Query -Path "tickets" -QueryString (Build-QueryString -Query $q2)
Assert-True ($url2 -match "^tickets\?query_hash=") "query_hash mode: tickets? preserved (404 regression)"
Assert-True ($url2 -match "%5B%7B%22condition%22") "query_hash mode: JSON is URL-escaped"

# empty query leaves path intact
Assert-Equal (Append-Query -Path "tickets" -QueryString "") "tickets" "empty query leaves path intact"

Write-Host ""
if ($failures -gt 0) {
    Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed." -ForegroundColor Green
exit 0