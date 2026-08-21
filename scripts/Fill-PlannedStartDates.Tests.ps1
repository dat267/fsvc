#!/usr/bin/env pwsh
# Fill-PlannedStartDates.Tests.ps1
#
# Regression tests for Fill-PlannedStartDates.ps1 (zero dependencies, no
# Pester). Dot-sources the script so its helper functions can be exercised
# without touching the network.
#
#   powershell -ExecutionPolicy Bypass -File scripts\Fill-PlannedStartDates.Tests.ps1

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "Fill-PlannedStartDates.ps1")

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

Write-Host "== Should-FillStart ==" -ForegroundColor Cyan
Assert-Equal (Should-FillStart -PlannedStartDate $null -CreatedAt "2026-08-01T12:07:30Z") $true "null start + created_at fills"
Assert-Equal (Should-FillStart -PlannedStartDate "" -CreatedAt "2026-08-01T12:07:30Z") $true "empty start + created_at fills"
Assert-Equal (Should-FillStart -PlannedStartDate "2026-08-01T00:00:00Z" -CreatedAt "2026-08-01T12:07:30Z") $false "already-set start left alone"
Assert-Equal (Should-FillStart -PlannedStartDate $null -CreatedAt "") $false "null start but no created_at skipped"
Assert-Equal (Should-FillStart -PlannedStartDate $null -CreatedAt $null) $false "null start + null created_at skipped"

Write-Host "== Round-Up-QuarterHour (derived from created_at) ==" -ForegroundColor Cyan
Assert-Equal (Round-Up-QuarterHour ([datetime]"2026-08-01T12:07:30Z")).ToString("yyyy-MM-ddTHH:mm:ssZ") "2026-08-01T12:15:00Z" "created_at 12:07:30 -> 12:15:00"
Assert-Equal (Round-Up-QuarterHour ([datetime]"2026-08-01T23:59:59Z")).ToString("yyyy-MM-ddTHH:mm:ssZ") "2026-08-02T00:00:00Z" "day rollover"

Write-Host "== URL construction ==" -ForegroundColor Cyan
# The 404 regression: "tickets?" must survive the query append.
$q = @{ "query_hash" = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]' }
$url = Append-Query -Path "tickets" -QueryString (Build-QueryString -Query $q)
Assert-True ($url -match "^tickets\?query_hash=") "tickets? preserved (404 regression)"
Assert-True ($url -match "%5B%7B%22condition%22") "query_hash JSON URL-escaped"

Write-Host ""
if ($failures -gt 0) {
    Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed." -ForegroundColor Green
exit 0