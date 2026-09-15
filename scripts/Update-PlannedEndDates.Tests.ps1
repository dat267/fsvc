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

Write-Host "== End-date decision (tickets with an existing planned_end_date) ==" -ForegroundColor Cyan
# Every scanned ticket is recomputed to last-comment + N business days; only
# an identical instant is skipped. Future dates that are too far or too short
# are corrected rather than left alone.
$target = [datetimeoffset]::Parse("2026-08-07T17:00:00+04:00")
Assert-Equal (Test-EndDateNeedsUpdate -PlannedEndDate $null -Target $target) $true "null planned end needs update"
Assert-Equal (Test-EndDateNeedsUpdate -PlannedEndDate "2026-08-07T17:00:00+04:00" -Target $target) $false "identical instant (same offset) skipped"
Assert-Equal (Test-EndDateNeedsUpdate -PlannedEndDate "2026-08-07T13:00:00Z" -Target $target) $false "identical instant (different offset) skipped"
Assert-Equal (Test-EndDateNeedsUpdate -PlannedEndDate "2099-01-01T00:00:00Z" -Target $target) $true "far-future planned end corrected"
Assert-Equal (Test-EndDateNeedsUpdate -PlannedEndDate "2020-01-01T00:00:00Z" -Target $target) $true "past planned end corrected"
Assert-Equal (Test-EndDateNeedsUpdate -PlannedEndDate "garbage" -Target $target) $true "unparseable planned end corrected"

Write-Host "== Clamp: planned end is always in the future ==" -ForegroundColor Cyan
$nowTueNoon = [datetimeoffset]::Parse("2026-08-04T12:00:00+00:00")
$oldBase = [datetimeoffset]::Parse("2026-07-01T09:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $oldBase -Days 3 -Hour 17 -Zone $null -Offset $offZero -Now $nowTueNoon)) "2026-08-04T17:00:00Z" "stale comment: target clamped to today at the target hour"

$nowTueEvening = [datetimeoffset]::Parse("2026-08-04T18:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $oldBase -Days 3 -Hour 17 -Zone $null -Offset $offZero -Now $nowTueEvening)) "2026-08-05T17:00:00Z" "target hour already passed: clamp to next business day"

$nowFriEvening = [datetimeoffset]::Parse("2026-08-07T18:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $oldBase -Days 3 -Hour 17 -Zone $null -Offset $offZero -Now $nowFriEvening)) "2026-08-10T17:00:00Z" "Friday evening clamps to Monday"

# A recent comment keeps the natural last-comment + 3 business days target.
$recentBase = [datetimeoffset]::Parse("2026-08-03T09:00:00+00:00")   # Monday
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $recentBase -Days 3 -Hour 17 -Zone $null -Offset $offZero -Now $nowTueNoon)) "2026-08-06T17:00:00Z" "future target left as computed"

# Clamp uses the configured offset for 'today', not the machine timezone.
$nowDubai = [datetimeoffset]::Parse("2026-08-04T12:00:00+04:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $oldBase -Days 3 -Hour 17 -Zone $null -Offset $offDubai -Now $nowDubai)) "2026-08-04T17:00:00+04:00" "clamp rendered in the configured offset"

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

Write-Host "== Target hour / timezone ==" -ForegroundColor Cyan
$offZero = [timespan]::Zero
$offDubai = [timespan]::FromHours(4)
$utcZone = Resolve-TimeZone -Id "UTC"
Assert-Equal (Resolve-TimeZone -Id "") $null "empty TimeZoneId yields null"
Assert-True ($null -ne $utcZone) "UTC resolves to a timezone"
Assert-Equal (ConvertTo-UtcOffset -Value "+04:00").ToString() "04:00:00" "offset string parsed"
Assert-Equal (ConvertTo-UtcOffset -Value "") $null "empty offset yields null"

$tue = [datetimeoffset]::Parse("2026-08-04T12:07:30+00:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $tue -Days 3 -Hour 17 -Zone $null -Offset $offZero)) "2026-08-07T17:00:00Z" "3 business days from base at 17:00 UTC"
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $tue -Days 3 -Hour 17 -Zone $utcZone -Offset $null)) "2026-08-07T17:00:00Z" "timezone id equivalent to UTC offset"
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $tue -Days 0 -Hour 9 -Zone $null -Offset $offZero)) "2026-08-04T09:00:00Z" "zero days keeps the day, sets the hour"

$tueDubai = [datetimeoffset]::Parse("2026-08-04T12:07:30+04:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $tueDubai -Days 3 -Hour 17 -Zone $null -Offset $offDubai)) "2026-08-07T17:00:00+04:00" "target rendered in the configured offset"

$fri = [datetimeoffset]::Parse("2026-08-07T12:07:30+00:00")
Assert-Equal (Format-Iso8601 (Get-TargetEndDate -Base $fri -Days 1 -Hour 17 -Zone $null -Offset $offZero)) "2026-08-10T17:00:00Z" "Friday +1 business day lands on Monday"

Write-Host "== Get-ApplyDecision (non-interactive / confirm) ==" -ForegroundColor Cyan
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $true -Answer "") $true "non-interactive applies without a prompt"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $true -Answer "n") $true "non-interactive ignores an answer"
Assert-Equal (Get-ApplyDecision -Confirm $false -NonInteractive $false -Answer "") $true "confirm off applies"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "y") $true "y applies"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "Y") $true "Y applies"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "n") $false "n aborts"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "") $false "empty aborts"

Write-Host "== Get-ApplyExitCode (scheduled-task failure signal) ==" -ForegroundColor Cyan
Assert-Equal (Get-ApplyExitCode -Applied 3 -Total 3) 0 "full success exits 0"
Assert-Equal (Get-ApplyExitCode -Applied 2 -Total 3) 1 "partial failure exits 1"
Assert-Equal (Get-ApplyExitCode -Applied 0 -Total 0) 0 "no changes exits 0"
Assert-Equal (Get-ApplyExitCode -Applied 0 -Total 5) 1 "total failure exits 1"

Write-Host "== Run lock (prevents overlapping scheduled runs) ==" -ForegroundColor Cyan
$lockPath = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-lock-test-" + [guid]::NewGuid().ToString())
$h1 = Enter-RunLock -Path $lockPath
Assert-True ($null -ne $h1) "first acquire succeeds"
$h2 = Enter-RunLock -Path $lockPath
Assert-True ($null -eq $h2) "second acquire blocked while held"
Exit-RunLock -Handle $h1 -Path $lockPath
$h3 = Enter-RunLock -Path $lockPath
Assert-True ($null -ne $h3) "acquire after release succeeds"
Exit-RunLock -Handle $h3 -Path $lockPath
[System.IO.File]::WriteAllText($lockPath, "stale")
(Get-Item -LiteralPath $lockPath).LastWriteTime = (Get-Date).AddHours(-5)
$h4 = Enter-RunLock -Path $lockPath -StaleMinutes 60
Assert-True ($null -ne $h4) "stale lock taken over"
Exit-RunLock -Handle $h4 -Path $lockPath
Assert-True (-not (Test-Path -LiteralPath $lockPath)) "lock file removed on release"

Write-Host "== Logging (transcript for scheduled runs) ==" -ForegroundColor Cyan
$logPath = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-log-test-" + [guid]::NewGuid().ToString() + ".log")
$oldLogPath = $LogPath
$LogPath = $logPath
Start-Logging
Write-Host "log-marker-42"
Stop-Logging
Assert-True (Test-Path -LiteralPath $logPath) "log file created"
Assert-True ((Get-Content -LiteralPath $logPath -Raw) -match "log-marker-42") "host output captured"
Remove-Item -LiteralPath $logPath -Force
$LogPath = $oldLogPath

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