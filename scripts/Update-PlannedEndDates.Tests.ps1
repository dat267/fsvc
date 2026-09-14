#!/usr/bin/env pwsh
# Update-PlannedEndDates.Tests.ps1
#
# Regression tests for the selection/date logic in Update-PlannedEndDates.ps1.
# Zero dependencies (no Pester required); run directly:
#
#   powershell -ExecutionPolicy Bypass -File scripts\Update-PlannedEndDates.Tests.ps1
#
# Dot-sourcing the script only defines its helper functions, so the tests can
# exercise them without touching the network or the placeholder config.

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "Update-PlannedEndDates.ps1")

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

Write-Host "== Add-BusinessDays ==" -ForegroundColor Cyan
$fri = [datetimeoffset]::Parse("2026-08-07T12:00:00+00:00")   # Friday
Assert-Equal (Format-Iso8601 (Add-BusinessDays -Start $fri -Days 1)) "2026-08-10T12:00:00Z" "Fri +1 business day = Mon"
Assert-Equal (Format-Iso8601 (Add-BusinessDays -Start $fri -Days 3)) "2026-08-12T12:00:00Z" "Fri +3 business days = Wed"
Assert-Equal (Format-Iso8601 (Add-BusinessDays -Start $fri -Days 0)) "2026-08-07T12:00:00Z" "Fri +0 = unchanged"
$friDubai = [datetimeoffset]::Parse("2026-08-07T12:00:00+04:00")
Assert-Equal (Format-Iso8601 (Add-BusinessDays -Start $friDubai -Days 1)) "2026-08-10T12:00:00+04:00" "account offset survives business-day math"

Write-Host "== Round-Up-QuarterHour ==" -ForegroundColor Cyan
Assert-Equal (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-04T12:07:30+00:00"))).ToString("HH:mm:ss") "12:15:00" "mid-quarter rounds up"
Assert-Equal (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-04T12:15:00+00:00"))).ToString("HH:mm:ss") "12:15:00" "exact boundary unchanged"
Assert-Equal (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-04T12:15:30+00:00"))).ToString("HH:mm:ss") "12:30:00" "boundary+seconds rounds up"
Assert-Equal (Format-Iso8601 (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-04T23:59:59+00:00")))) "2026-08-05T00:00:00Z" "day rollover"

Write-Host "== Offset preservation (account timezone) ==" -ForegroundColor Cyan
# Regression: timestamps used to be parsed into local time and formatted with
# a literal Z, so on a non-UTC machine 12:07:30Z became 19:15:00Z.
Assert-Equal (Format-Iso8601 (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-04T12:07:30+04:00")))) "2026-08-04T12:15:00+04:00" "rounds account wall clock, keeps +04:00"
Assert-Equal (Format-Iso8601 ([datetimeoffset]::Parse("2026-08-04T12:15:00+00:00"))) "2026-08-04T12:15:00Z" "zero offset renders as Z"

Write-Host "== Get-AccountOffset ==" -ForegroundColor Cyan
$fallback = [datetimeoffset]::Parse("2026-08-04T00:00:00+00:00")
$tickets = @(
    [pscustomobject]@{ planned_end_date = $null; created_at = "2026-08-01T10:00:00+04:00" },
    [pscustomobject]@{ planned_end_date = "2026-08-05T09:00:00+04:00"; created_at = "2026-08-01T10:00:00+04:00" }
)
Assert-Equal (Get-AccountOffset -Tickets $tickets -Fallback $fallback).ToString() "04:00:00" "offset derived from ticket dates"
$ticketsCreatedOnly = @([pscustomobject]@{ planned_end_date = $null; created_at = "2026-08-01T10:00:00+05:30" })
Assert-Equal (Get-AccountOffset -Tickets $ticketsCreatedOnly -Fallback $fallback).ToString() "05:30:00" "created_at fallback"
Assert-Equal (Get-AccountOffset -Tickets @() -Fallback $fallback).ToString() "00:00:00" "fallback offset when no dates"

Write-Host "== Should-Bump (WithinDays=7, now=2026-08-04T12:00Z) ==" -ForegroundColor Cyan
$now = [datetimeoffset]::Parse("2026-08-04T12:00:00+00:00")
Assert-Equal (Should-Bump -PlannedEndDate $null -Now $now -WithinDays 7) $true "null date bumps"
Assert-Equal (Should-Bump -PlannedEndDate "2026-08-01T10:00:00Z" -Now $now -WithinDays 7) $true "past date bumps"
Assert-Equal (Should-Bump -PlannedEndDate "2026-08-08T10:00:00Z" -Now $now -WithinDays 7) $true "within 7d bumps"
Assert-Equal (Should-Bump -PlannedEndDate "2026-08-20T10:00:00Z" -Now $now -WithinDays 7) $false "beyond window left alone"
Assert-Equal (Should-Bump -PlannedEndDate "garbage" -Now $now -WithinDays 7) $true "unparseable bumps"

Write-Host "== Should-Bump (WithinDays=0, window off) ==" -ForegroundColor Cyan
Assert-Equal (Should-Bump -PlannedEndDate "2026-08-08T10:00:00Z" -Now $now -WithinDays 0) $false "future left alone when window off"
Assert-Equal (Should-Bump -PlannedEndDate "2026-08-01T10:00:00Z" -Now $now -WithinDays 0) $true "past bumps when window off"

function Assert-True {
    param([bool]$Actual, [string]$Label)
    if (-not $Actual) {
        Write-Host ("FAIL: {0}" -f $Label) -ForegroundColor Red
        $script:failures++
    } else {
        Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray
    }
}

Write-Host "== Append-Query / Build-QueryString (URL construction) ==" -ForegroundColor Cyan
Assert-Equal (Append-Query -Path "tickets" -QueryString "per_page=100") "tickets?per_page=100" "path + query keeps both"
Assert-Equal (Append-Query -Path "tickets" -QueryString "") "tickets" "empty query leaves path intact"
$qs2 = Build-QueryString -Query @{ a = "1"; b = "x y" }
Assert-True (($qs2 -match "a=1") -and ($qs2 -match "b=x%20y")) "values are escaped (x%20y)"

$qs = Build-QueryString -Query @{ per_page = 100; query_hash = '{"k":"v"}' }
Assert-True ($qs -match "^per_page=100&query_hash=" -or $qs -match "^query_hash=.*&per_page=100$") "Build-QueryString joins and escapes params"
# The critical regression: "tickets" must survive the ?-append (the 404 bug).
Assert-True ((Append-Query -Path "tickets" -QueryString $qs) -match "^tickets\?") "tickets? kept in final URL"

Write-Host ""
if ($failures -gt 0) {
    Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed." -ForegroundColor Green
exit 0