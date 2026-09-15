#!/usr/bin/env pwsh
# Get-TicketOverview.Tests.ps1
#
# Regression tests for Get-TicketOverview.ps1 (zero dependencies, no Pester).
# Dot-sources the script so its helper functions can be exercised without
# touching the network.
#
#   pwsh scripts/Get-TicketOverview.Tests.ps1

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "Get-TicketOverview.ps1")

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

Write-Host "== Get-BusinessDaysBetween ==" -ForegroundColor Cyan
$mon = [datetimeoffset]::Parse("2026-08-03T12:00:00+00:00")     # Monday noon
$fri = [datetimeoffset]::Parse("2026-08-07T12:00:00+00:00")     # Friday noon
$nextMon = [datetimeoffset]::Parse("2026-08-10T12:00:00+00:00") # next Monday noon

Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $mon -To $mon), 4)) 0 "same instant = 0"
Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $mon -To $mon.AddDays(1)), 4)) 1 "Mon to Tue = 1"
Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $mon -To $fri), 4)) 4 "Mon to Fri = 4"
Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $fri -To $nextMon), 4)) 1 "Fri to Mon = 1 (weekend skipped)"
Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $mon -To $nextMon), 4)) 5 "Mon to next Mon = 5"
Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $mon -To $mon.AddHours(6)), 4)) 0.25 "six hours counts fractionally"
# The threshold must not change with the reader's machine timezone.
$monDubai = [datetimeoffset]::Parse("2026-08-03T12:00:00+04:00")
$friDubai = [datetimeoffset]::Parse("2026-08-07T12:00:00+04:00")
Assert-Equal ([math]::Round((Get-BusinessDaysBetween -From $monDubai -To $friDubai), 4)) 4 "offset-independent"

Write-Host "== Get-TicketCategory ==" -ForegroundColor Cyan
$now = [datetimeoffset]::Parse("2026-08-04T12:00:00+00:00") # Tuesday
$created = $now.AddDays(-3)
$other = [int64]9
$assigned = [int64]5
$unassigned = [int64](-1)

Assert-Equal (Get-TicketCategory -ResponderID $unassigned -LastMessage $null -LastUserID 0 -CreatedAt $created -OlderThanDays 1 -Now $now) "unassigned" "responder -1 = unassigned"
Assert-Equal (Get-TicketCategory -ResponderID $null -LastMessage $null -LastUserID 0 -CreatedAt $created -OlderThanDays 1 -Now $now) "unassigned" "null responder = unassigned"
Assert-Equal (Get-TicketCategory -ResponderID $assigned -LastMessage $null -LastUserID 0 -CreatedAt $created -OlderThanDays 1 -Now $now) "waiting" "no messages, old created_at = waiting on customer"
Assert-Equal (Get-TicketCategory -ResponderID $assigned -LastMessage $null -LastUserID 0 -CreatedAt $now -OlderThanDays 1 -Now $now) "none" "no messages, recent created_at = none"
Assert-Equal (Get-TicketCategory -ResponderID $assigned -LastMessage $now -LastUserID $other -CreatedAt $created -OlderThanDays 1 -Now $now) "awaiting_agent" "someone else replied = awaiting agent"
Assert-Equal (Get-TicketCategory -ResponderID $assigned -LastMessage $created -LastUserID $assigned -CreatedAt $created -OlderThanDays 1 -Now $now) "waiting" "stale responder reply = waiting on customer"
Assert-Equal (Get-TicketCategory -ResponderID $assigned -LastMessage $now -LastUserID $assigned -CreatedAt $created -OlderThanDays 1 -Now $now) "none" "recent responder reply = none"
Assert-Equal (Get-TicketCategory -ResponderID $assigned -LastMessage $now.AddMinutes(-30) -LastUserID $assigned -CreatedAt $created -OlderThanDays 1 -Now $now) "none" "recent within threshold = none"

Write-Host "== Format-OverviewLink / Format-Days ==" -ForegroundColor Cyan
Assert-Equal (Format-OverviewLink -BaseUrl "https://acme.freshservice.com" -Id 10100) "https://acme.freshservice.com/a/tickets/10100" "link uses the agent URL"
Assert-Equal (Format-Days -Days 2.0) "2.0" "days rendered with one decimal"
Assert-Equal (Format-Days -Days 2.25) "2.3" "days rounded to one decimal"

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