#!/usr/bin/env pwsh
# FSvc.Core.Tests.ps1 - zero-dependency tests for the private helpers.
# Dot-sources Private/*.ps1 so no module import is needed.

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Get-ChildItem -LiteralPath (Join-Path $repoRoot 'Private') -Filter '*.ps1' -File | Sort-Object Name | ForEach-Object { . $_.FullName }

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

Write-Host "== Query building ==" -ForegroundColor Cyan
$url = Add-FSvcQuery -Path "tickets" -QueryString (Build-FSvcQueryString -Query @{ query_hash = '{"k":"v"}'; per_page = 100 })
Assert-True ($url -match "^tickets\?") "path and ? preserved"
Assert-True ($url -match "per_page=100") "per_page present"
Assert-True ($url -match "%7B%22k%22") "JSON escaped"
Assert-Equal (Add-FSvcQuery -Path "tickets" -QueryString "") "tickets" "empty query leaves path intact"

Write-Host "== JSON date preservation ==" -ForegroundColor Cyan
$parsed = '{"t":"2026-08-01T10:00:00+04:00"}' | ConvertFrom-FSvcJson
Assert-Equal $parsed.t "2026-08-01T10:00:00+04:00" "ISO string kept verbatim"

Write-Host "== Dates ==" -ForegroundColor Cyan
$fri = [datetimeoffset]::Parse("2026-08-07T12:00:00+00:00")
Assert-Equal (Format-Iso8601 (Add-FSvcBusinessDays -Start $fri -Days 1)) "2026-08-10T12:00:00Z" "Fri +1 = Mon"
Assert-Equal (Format-Iso8601 (Add-FSvcBusinessDays -Start $fri -Days 3)) "2026-08-12T12:00:00Z" "Fri +3 = Wed"
Assert-Equal (Format-Iso8601 (Round-FSvcQuarterHour ([datetimeoffset]::Parse("2026-08-04T12:07:30+00:00")))) "2026-08-04T12:15:00Z" "quarter round up"
Assert-Equal (Format-Iso8601 (Round-FSvcQuarterHour ([datetimeoffset]::Parse("2026-08-04T12:07:30+04:00")))) "2026-08-04T12:15:00+04:00" "quarter round keeps offset"

$mon = [datetimeoffset]::Parse("2026-08-03T12:00:00+00:00")
$nextMon = [datetimeoffset]::Parse("2026-08-10T12:00:00+00:00")
Assert-Equal ([math]::Round((Get-FSvcBusinessDaysBetween -From $mon -To $nextMon), 4)) 5 "business days Mon->Mon = 5"
Assert-Equal ([math]::Round((Get-FSvcBusinessDaysBetween -From $mon -To $mon), 4)) 0 "same instant = 0"

Assert-Equal (Format-Iso8601 (Get-FSvcTargetEndDate -Base ([datetimeoffset]::Parse("2026-08-04T12:07:30+00:00")) -Days 3 -Hour 17 -Zone $null -Offset ([timespan]::Zero))) "2026-08-07T17:00:00Z" "target = base +3bd at hour"
$now = [datetimeoffset]::Parse("2026-08-04T12:00:00+00:00")
$old = [datetimeoffset]::Parse("2026-07-01T09:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-FSvcTargetEndDate -Base $old -Days 3 -Hour 17 -Zone $null -Offset ([timespan]::Zero) -Now $now)) "2026-08-04T17:00:00Z" "stale target clamped to today"
$evening = [datetimeoffset]::Parse("2026-08-04T18:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-FSvcTargetEndDate -Base $old -Days 3 -Hour 17 -Zone $null -Offset ([timespan]::Zero) -Now $evening)) "2026-08-05T17:00:00Z" "past hour clamps to next business day"

$target = [datetimeoffset]::Parse("2026-08-07T17:00:00+04:00")
Assert-Equal (Test-FSvcEndDateNeedsUpdate -PlannedEndDate $null -Target $target) $true "null needs update"
Assert-Equal (Test-FSvcEndDateNeedsUpdate -PlannedEndDate "2026-08-07T13:00:00Z" -Target $target) $false "equal instant different offset skipped"
Assert-Equal (Test-FSvcEndDateNeedsUpdate -PlannedEndDate "2099-01-01T00:00:00Z" -Target $target) $true "far future corrected"
Assert-Equal (Test-FSvcFillStart -PlannedStartDate $null -CreatedAt "2026-08-01T00:00:00Z") $true "null start fills"
Assert-Equal (Test-FSvcFillStart -PlannedStartDate "2026-08-01T00:00:00Z" -CreatedAt "2026-08-01T00:00:00Z") $false "set start skipped"

Write-Host "== Timezone config ==" -ForegroundColor Cyan
Assert-Equal (ConvertTo-FSvcUtcOffset -Value "+04:00").ToString() "04:00:00" "offset parsed"
Assert-Equal (ConvertTo-FSvcUtcOffset -Value "") $null "empty offset is null"
Assert-True ($null -ne (Resolve-FSvcTimeZone -Id "UTC")) "UTC resolves"

Write-Host "== Category ==" -ForegroundColor Cyan
$cnow = [datetimeoffset]::Parse("2026-08-04T12:00:00+00:00")
Assert-Equal (Get-FSvcTicketCategory -ResponderID (-1) -LastMessage $null -LastUserID 0 -CreatedAt $cnow.AddDays(-3) -OlderThanDays 1 -Now $cnow) "unassigned" "responder -1 unassigned"
Assert-Equal (Get-FSvcTicketCategory -ResponderID 5 -LastMessage $cnow -LastUserID 9 -CreatedAt $cnow.AddDays(-3) -OlderThanDays 1 -Now $cnow) "awaiting_agent" "other replied awaiting agent"
Assert-Equal (Get-FSvcTicketCategory -ResponderID 5 -LastMessage $cnow.AddDays(-3) -LastUserID 5 -CreatedAt $cnow.AddDays(-3) -OlderThanDays 1 -Now $cnow) "waiting" "stale agent waiting"
Assert-Equal (Get-FSvcTicketCategory -ResponderID 5 -LastMessage $cnow -LastUserID 5 -CreatedAt $cnow.AddDays(-3) -OlderThanDays 1 -Now $cnow) "none" "recent agent none"

Write-Host "== Config precedence ==" -ForegroundColor Cyan
$old = $env:FSVC_SUBDOMAIN
try {
    $env:FSVC_SUBDOMAIN = "env-sub"
    $cfg = Get-FSvcEffectiveConfig
    Assert-Equal $cfg.Subdomain "env-sub" "env var used"
    Assert-Equal $cfg.BaseUrl "https://env-sub.freshservice.com" "base url derived"
    $cfg2 = Get-FSvcEffectiveConfig -Overrides @{ Subdomain = "override-sub" }
    Assert-Equal $cfg2.Subdomain "override-sub" "override wins over env"
} finally { $env:FSVC_SUBDOMAIN = $old }

Write-Host "== Run lock ==" -ForegroundColor Cyan
$lockPath = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-mod-lock-" + [guid]::NewGuid().ToString())
$h1 = Enter-FSvcRunLock -Path $lockPath
Assert-True ($null -ne $h1) "first acquire succeeds"
Assert-True ($null -eq (Enter-FSvcRunLock -Path $lockPath)) "second acquire blocked"
Exit-FSvcRunLock -Handle $h1 -Path $lockPath
Assert-True ($null -ne (Enter-FSvcRunLock -Path $lockPath)) "acquire after release succeeds" | Out-Null
$h2 = Enter-FSvcRunLock -Path $lockPath; Exit-FSvcRunLock -Handle $h2 -Path $lockPath

Write-Host "== Get-FSvcPlannedEndDate (policy) ==" -ForegroundColor Cyan
$zero = [timespan]::Zero
$before = [datetimeoffset]::Parse("2026-09-01T00:00:00+00:00")
$mon = [pscustomobject]@{ created_at = "2026-09-07T10:00:00+00:00"; planned_end_date = $null }   # Monday
# base = created_at; Mon +3bd = Thu 17:00
Assert-Equal (Format-Iso8601 (Get-FSvcPlannedEndDate -Ticket $mon -LatestConversationAt $null -Now $before -BusinessDays 3 -TargetHour 17 -Zone $null -Offset $zero)) "2026-09-10T17:00:00Z" "no comment uses created_at"
# base = last comment; Tue 2026-09-08 +3bd = Fri 17:00
$comment = [datetimeoffset]::Parse("2026-09-08T09:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-FSvcPlannedEndDate -Ticket $mon -LatestConversationAt $comment -Now $before -BusinessDays 3 -TargetHour 17 -Zone $null -Offset $zero)) "2026-09-11T17:00:00Z" "comment drives the base"
# already exactly the target instant -> no change
$same = [pscustomobject]@{ created_at = "2026-09-01T10:00:00+00:00"; planned_end_date = "2026-09-11T17:00:00+00:00" }
Assert-Equal (Get-FSvcPlannedEndDate -Ticket $same -LatestConversationAt $comment -Now $before -BusinessDays 3 -TargetHour 17 -Zone $null -Offset $zero) $null "identical date is skipped"
# a far-future date differs -> corrected
$far = [pscustomobject]@{ created_at = "2026-09-01T10:00:00+00:00"; planned_end_date = "2099-01-01T00:00:00Z" }
Assert-Equal (Format-Iso8601 (Get-FSvcPlannedEndDate -Ticket $far -LatestConversationAt $comment -Now $before -BusinessDays 3 -TargetHour 17 -Zone $null -Offset $zero)) "2026-09-11T17:00:00Z" "far-future date is corrected"
# stale base clamps into the future relative to now
$stale = [pscustomobject]@{ created_at = "2026-08-01T10:00:00+00:00"; planned_end_date = $null }
$nowTue = [datetimeoffset]::Parse("2026-09-08T12:00:00+00:00")
Assert-Equal (Format-Iso8601 (Get-FSvcPlannedEndDate -Ticket $stale -LatestConversationAt $null -Now $nowTue -BusinessDays 3 -TargetHour 17 -Zone $null -Offset $zero)) "2026-09-08T17:00:00Z" "stale target clamps to today at hour"
# no dates at all -> no target
$empty = [pscustomobject]@{ created_at = $null; planned_end_date = $null }
Assert-Equal (Get-FSvcPlannedEndDate -Ticket $empty -LatestConversationAt $null -Now $before -BusinessDays 3 -TargetHour 17 -Zone $null -Offset $zero) $null "no base date yields no target"

Write-Host "== ConvertTo-FSvcConversationView ==" -ForegroundColor Cyan
$raw1 = [pscustomobject]@{ id = 1; user_id = 2100; user = [pscustomobject]@{ name = "Nadia" }; incoming = $true; created_at = "2026-08-01T10:30:00+04:00"; body_text = "hello"; body = "<p>hello</p>" }
$v1 = ConvertTo-FSvcConversationView $raw1
Assert-Equal $v1.Author "Nadia" "nested user name preferred"
Assert-Equal $v1.UserId 2100 "user id preserved"
Assert-Equal $v1.Direction "incoming" "incoming direction"
Assert-Equal (Format-Iso8601 $v1.At) "2026-08-01T10:30:00+04:00" "timestamp parsed with offset"
Assert-Equal $v1.Body "hello" "body_text preferred over body"
$raw2 = [pscustomobject]@{ id = 2; user_id = 99; incoming = $false; created_at = "2026-08-01T11:00:00Z"; body = "<p>hi</p>" }
$v2 = ConvertTo-FSvcConversationView $raw2
Assert-Equal $v2.Author "99" "numeric author fallback"
Assert-Equal $v2.Direction "outgoing" "outgoing direction"
Assert-Equal $v2.Body "<p>hi</p>" "body fallback when no body_text"

Write-Host "== Named ticket views ==" -ForegroundColor Cyan
$self = Get-FSvcViewQueryHash -View SelfAssigned
$unassigned = Get-FSvcViewQueryHash -View Unassigned
Assert-True ($self -match '"responder_id".*"0"') "self-assigned view targets responder 0"
Assert-True ($unassigned -match '"responder_id".*"-1"') "unassigned view targets responder -1"
Assert-True ($self -match '"status"') "views filter on status"
$parsedSelf = $self | ConvertFrom-Json
$parsedUnassigned = $unassigned | ConvertFrom-Json
Assert-Equal $parsedSelf.Count 2 "self-assigned view has two conditions"
Assert-Equal $parsedUnassigned.Count 2 "unassigned view has two conditions"

Write-Host "== Persistent settings ==" -ForegroundColor Cyan
$envMap = ConvertTo-FSvcEnvironmentMap -Settings @{ Subdomain = 'acme'; SessionCookie = ''; CsrfToken = 'tok'; Unknown = 'x' }
Assert-Equal $envMap['FSVC_SUBDOMAIN'] 'acme' "subdomain mapped"
Assert-Equal $envMap['FSVC_CSRF_TOKEN'] 'tok' "csrf mapped"
Assert-True ($envMap.ContainsKey('FSVC_ITILDESK_SESSION')) "empty value included so it can be cleared"
Assert-True (-not $envMap.ContainsKey('Unknown')) "unknown keys are ignored"

$block = New-FSvcProfileBlock -Map @{ 'FSVC_SUBDOMAIN' = 'acme'; 'FSVC_CSRF_TOKEN' = "a b" }
Assert-True ($block -match [regex]::Escape($script:FSvcBlockStart)) "block start marker"
Assert-True ($block -match "FSVC_SUBDOMAIN = 'acme'") "assignment rendered"
Assert-True ($block -match "FSVC_CSRF_TOKEN = 'a b'") "value rendered as-is"
Assert-Equal (New-FSvcProfileBlock -Map @{}).Trim() "" "empty map yields no block"

$pf = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-persist-" + [guid]::NewGuid().ToString() + ".ps1")
[System.IO.File]::WriteAllText($pf, "# mine`n")
Update-FSvcProfile -ProfilePath $pf -Block (New-FSvcProfileBlock -Map @{ 'FSVC_SUBDOMAIN' = 'one' })
Assert-Equal (Read-FSvcProfileSettings -ProfilePath $pf)['FSVC_SUBDOMAIN'] 'one' "profile block readable"
Update-FSvcProfile -ProfilePath $pf -Block (New-FSvcProfileBlock -Map @{ 'FSVC_SUBDOMAIN' = 'two'; 'FSVC_CSRF_TOKEN' = 't' })
$readBack = Read-FSvcProfileSettings -ProfilePath $pf
Assert-Equal $readBack['FSVC_SUBDOMAIN'] 'two' "block replaced"
Assert-Equal $readBack['FSVC_CSRF_TOKEN'] 't' "second key present"
Assert-True ((Get-Content -LiteralPath $pf -Raw) -match '# mine') "other profile content preserved"
Update-FSvcProfile -ProfilePath $pf -Block ""
Assert-True (-not ((Get-Content -LiteralPath $pf -Raw) -match [regex]::Escape($script:FSvcBlockStart))) "block removed"
Remove-Item -LiteralPath $pf -Force

Write-Host "== Set-FSvcPersistentSettings ==" -ForegroundColor Cyan
$pf2 = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-persist2-" + [guid]::NewGuid().ToString() + ".ps1")
$script:persistCalls = @{}
Set-FSvcPersistentSettings -Settings @{ Subdomain = 'acme'; CsrfToken = '' } -OnWindows $true -ProfilePath $pf2 -SetUserEnvironment { param($n, $v) $script:persistCalls[$n] = $v }
Assert-Equal $script:persistCalls['FSVC_SUBDOMAIN'] 'acme' "windows path sets the user env var"
Assert-True ($script:persistCalls.ContainsKey('FSVC_CSRF_TOKEN')) "windows path clears the empty key"
Assert-True (-not (Test-Path -LiteralPath $pf2)) "windows path leaves the profile untouched"

Set-FSvcPersistentSettings -Settings @{ Subdomain = 'acme' } -OnWindows $false -ProfilePath $pf2 -SetUserEnvironment { param($n, $v) }
Set-FSvcPersistentSettings -Settings @{ CsrfToken = 'tok' } -OnWindows $false -ProfilePath $pf2 -SetUserEnvironment { param($n, $v) }
$merged = Read-FSvcProfileSettings -ProfilePath $pf2
Assert-Equal $merged['FSVC_SUBDOMAIN'] 'acme' "non-windows merges first key"
Assert-Equal $merged['FSVC_CSRF_TOKEN'] 'tok' "non-windows merges second key"
Set-FSvcPersistentSettings -Settings @{ Subdomain = '' } -OnWindows $false -ProfilePath $pf2 -SetUserEnvironment { param($n, $v) }
Assert-True (-not (Read-FSvcProfileSettings -ProfilePath $pf2).ContainsKey('FSVC_SUBDOMAIN')) "empty clears a persisted key"
Remove-Item -LiteralPath $pf2 -Force -ErrorAction SilentlyContinue

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
