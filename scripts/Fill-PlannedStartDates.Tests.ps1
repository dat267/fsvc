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
Assert-Equal (Format-Iso8601 (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-01T12:07:30+00:00")))) "2026-08-01T12:15:00Z" "created_at 12:07:30 -> 12:15:00"
Assert-Equal (Format-Iso8601 (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-01T23:59:59+00:00")))) "2026-08-02T00:00:00Z" "day rollover"
# Regression: parsing to local time and labelling it Z corrupted the instant
# on any non-UTC machine; the account offset must survive.
Assert-Equal (Format-Iso8601 (Round-Up-QuarterHour ([datetimeoffset]::Parse("2026-08-01T12:07:30+04:00")))) "2026-08-01T12:15:00+04:00" "keeps the account offset"

Write-Host "== URL construction ==" -ForegroundColor Cyan
# The 404 regression: "tickets?" must survive the query append.
$q = @{ "query_hash" = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]' }
$url = Append-Query -Path "tickets" -QueryString (Build-QueryString -Query $q)
Assert-True ($url -match "^tickets\?query_hash=") "tickets? preserved (404 regression)"
Assert-True ($url -match "%5B%7B%22condition%22") "query_hash JSON URL-escaped"

Write-Host "== Get-ApplyDecision (non-interactive / confirm) ==" -ForegroundColor Cyan
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $true -Answer "") $true "non-interactive applies without a prompt"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $true -Answer "n") $true "non-interactive ignores an answer"
Assert-Equal (Get-ApplyDecision -Confirm $false -NonInteractive $false -Answer "") $true "confirm off applies"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "y") $true "y applies"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "n") $false "n aborts"
Assert-Equal (Get-ApplyDecision -Confirm $true -NonInteractive $false -Answer "") $false "empty aborts"

Write-Host "== Get-ApplyExitCode (scheduled-task failure signal) ==" -ForegroundColor Cyan
Assert-Equal (Get-ApplyExitCode -Applied 3 -Total 3) 0 "full success exits 0"
Assert-Equal (Get-ApplyExitCode -Applied 2 -Total 3) 1 "partial failure exits 1"
Assert-Equal (Get-ApplyExitCode -Applied 0 -Total 0) 0 "no changes exits 0"
Assert-Equal (Get-ApplyExitCode -Applied 0 -Total 5) 1 "total failure exits 1"

Write-Host "== Run lock (prevents overlapping scheduled runs) ==" -ForegroundColor Cyan
$lockPath = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-fill-lock-test-" + [guid]::NewGuid().ToString())
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
$logPath = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-fill-log-test-" + [guid]::NewGuid().ToString() + ".log")
$oldLogPath = $LogPath
$LogPath = $logPath
Start-Logging
Write-Host "fill-log-marker-42"
Stop-Logging
Assert-True (Test-Path -LiteralPath $logPath) "log file created"
Assert-True ((Get-Content -LiteralPath $logPath -Raw) -match "fill-log-marker-42") "host output captured"
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