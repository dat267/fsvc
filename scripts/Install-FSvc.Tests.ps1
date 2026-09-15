#!/usr/bin/env pwsh
# Install-FSvc.Tests.ps1
#
# Regression tests for Install-FSvc.ps1 (zero dependencies, no Pester).
# Dot-sources the script so its helper functions can be exercised without
# touching the real install location or profile.
#
#   pwsh scripts/Install-FSvc.Tests.ps1

$ErrorActionPreference = "Stop"

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $here "Install-FSvc.ps1")

$failures = 0
$sep = [System.IO.Path]::PathSeparator

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

Write-Host "== Add-PathEntry ==" -ForegroundColor Cyan
Assert-Equal (Add-PathEntry -PathValue "" -Entry "/opt/fsvc") "/opt/fsvc" "empty path gains the entry"
Assert-Equal (Add-PathEntry -PathValue "/usr/bin" -Entry "/opt/fsvc") "/usr/bin$sep/opt/fsvc" "entry appended"
Assert-Equal (Add-PathEntry -PathValue "/opt/fsvc" -Entry "/opt/fsvc") "/opt/fsvc" "already present is unchanged"
Assert-Equal (Add-PathEntry -PathValue "/opt/fsvc$sep/usr/bin" -Entry "/opt/fsvc") "/opt/fsvc$sep/usr/bin" "present in a list is unchanged"
Assert-Equal (Add-PathEntry -PathValue "/OPT/FSVC" -Entry "/opt/fsvc") "/OPT/FSVC" "match is case-insensitive (Windows-safe)"

Write-Host "== Remove-PathEntry ==" -ForegroundColor Cyan
Assert-Equal (Remove-PathEntry -PathValue "/opt/fsvc$sep/usr/bin" -Entry "/opt/fsvc") "/usr/bin" "removed from the front"
Assert-Equal (Remove-PathEntry -PathValue "/usr/bin$sep/opt/fsvc" -Entry "/opt/fsvc") "/usr/bin" "removed from the end"
Assert-Equal (Remove-PathEntry -PathValue "/usr/bin" -Entry "/opt/fsvc") "/usr/bin" "missing entry leaves path intact"
Assert-Equal (Remove-PathEntry -PathValue "/opt/fsvc$sep/opt/fsvc" -Entry "/opt/fsvc") "" "duplicates all removed"

Write-Host "== Get-FSvcEnvAssignments ==" -ForegroundColor Cyan
$map = Get-FSvcEnvAssignments -Subdomain "acme" -Session "s" -CsrfToken "c" -LogPath "l" -TimeZoneId "tz" -UtcOffset "+04:00"
Assert-Equal $map["FSVC_SUBDOMAIN"] "acme" "subdomain mapped"
Assert-Equal $map["FSVC_ITILDESK_SESSION"] "s" "session mapped"
Assert-Equal $map["FSVC_CSRF_TOKEN"] "c" "csrf mapped"
Assert-Equal $map["FSVC_LOG_PATH"] "l" "log path mapped"
Assert-Equal $map["FSVC_TZ"] "tz" "timezone mapped"
Assert-Equal $map["FSVC_UTC_OFFSET"] "+04:00" "offset mapped"
$emptyMap = Get-FSvcEnvAssignments
Assert-Equal $emptyMap.Count 0 "empty parameters produce no assignments"

Write-Host "== New-FSvcProfileBlock ==" -ForegroundColor Cyan
$block = New-FSvcProfileBlock -PathEntry "/opt/fsvc" -EnvMap ([ordered]@{ "FSVC_SUBDOMAIN" = "acme" })
Assert-True ($block -match [regex]::Escape($script:FSvcBlockStart)) "block start marker present"
Assert-True ($block -match [regex]::Escape($script:FSvcBlockEnd)) "block end marker present"
Assert-True ($block -match "opt.{0,2}fsvc") "path entry present"
Assert-True ($block -match "FSVC_SUBDOMAIN") "env assignment present"
$noPath = New-FSvcProfileBlock -PathEntry "" -EnvMap ([ordered]@{ "FSVC_SUBDOMAIN" = "acme" })
Assert-True (-not ($noPath -match "PATH")) "no PATH line without a path entry"
Assert-Equal (New-FSvcProfileBlock -PathEntry "" -EnvMap ([ordered]@{})).Trim() "" "nothing to write yields an empty block"

Write-Host "== Update-FSvcProfile (insert / replace / remove) ==" -ForegroundColor Cyan
$profilePath = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-profile-" + [guid]::NewGuid().ToString() + ".ps1")
$original = "# my profile`nSet-Alias ll Get-ChildItem`n"
[System.IO.File]::WriteAllText($profilePath, $original)

Update-FSvcProfile -ProfilePath $profilePath -Block (New-FSvcProfileBlock -PathEntry "/opt/fsvc" -EnvMap ([ordered]@{ "FSVC_SUBDOMAIN" = "one" }))
$afterFirst = Get-Content -LiteralPath $profilePath -Raw
Assert-True ($afterFirst -match "Set-Alias ll") "existing profile content preserved"
Assert-True ($afterFirst -match "FSVC_SUBDOMAIN") "block inserted"
Assert-Equal ([regex]::Matches($afterFirst, [regex]::Escape($script:FSvcBlockStart)).Count) 1 "one block after first update"

Update-FSvcProfile -ProfilePath $profilePath -Block (New-FSvcProfileBlock -PathEntry "" -EnvMap ([ordered]@{ "FSVC_SUBDOMAIN" = "two" }))
$afterSecond = Get-Content -LiteralPath $profilePath -Raw
Assert-Equal ([regex]::Matches($afterSecond, [regex]::Escape($script:FSvcBlockStart)).Count) 1 "still one block after re-update"
Assert-True ($afterSecond -match "two") "block replaced, not appended"
Assert-True (-not ($afterSecond -match '"one"')) "old block content gone"
Assert-True ($afterSecond -match "Set-Alias ll") "profile content still preserved"

Update-FSvcProfile -ProfilePath $profilePath -Block ""
$afterRemove = Get-Content -LiteralPath $profilePath -Raw
Assert-True (-not ($afterRemove -match [regex]::Escape($script:FSvcBlockStart))) "block removed"
Assert-True ($afterRemove -match "Set-Alias ll") "profile content survives removal"
Remove-Item -LiteralPath $profilePath -Force

Write-Host "== Get-FSvcInstallFiles ==" -ForegroundColor Cyan
$srcDir = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-src-" + [guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $srcDir | Out-Null
"x" | Set-Content -LiteralPath (Join-Path $srcDir "A.ps1")
"x" | Set-Content -LiteralPath (Join-Path $srcDir "B.ps1")
"x" | Set-Content -LiteralPath (Join-Path $srcDir "notes.txt")
$files = Get-FSvcInstallFiles -SourceDir $srcDir
Assert-Equal $files.Count 2 "only .ps1 files are installed"
Assert-True (($files | Where-Object { $_ -like "*A.ps1" }).Count -eq 1) "A.ps1 included"
Remove-Item -LiteralPath $srcDir -Recurse -Force

Write-Host ""
if ($failures -gt 0) {
    Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red
    exit 1
}
Write-Host "All tests passed." -ForegroundColor Green
exit 0