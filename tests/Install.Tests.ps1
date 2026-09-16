#!/usr/bin/env pwsh
# Install.Tests.ps1 - tests for the GitHub bootstrap installer.

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
. (Join-Path $repoRoot "Install.ps1")

$failures = 0
function Assert-True {
    param([bool]$Actual, [string]$Label)
    if (-not $Actual) { Write-Host ("FAIL: {0}" -f $Label) -ForegroundColor Red; $script:failures++ }
    else { Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray }
}
function Assert-Equal {
    param($Actual, $Expected, [string]$Label)
    if ($Actual -ne $Expected) {
        Write-Host ("FAIL: {0}`n  expected: {1}`n  actual:   {2}" -f $Label, $Expected, $Actual) -ForegroundColor Red
        $script:failures++
    } else { Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray }
}

Write-Host "== Get-FSvcArchiveUrl ==" -ForegroundColor Cyan
Assert-Equal (Get-FSvcArchiveUrl -Repository "dat267/fsvc" -Ref "main" -Version "") "https://github.com/dat267/fsvc/archive/refs/heads/main.zip" "branch archive url"
Assert-Equal (Get-FSvcArchiveUrl -Repository "dat267/fsvc" -Ref "main" -Version "v1.2.0") "https://github.com/dat267/fsvc/archive/refs/tags/v1.2.0.zip" "tag archive url"
Assert-Equal (Get-FSvcArchiveUrl -Repository "dat267/fsvc/" -Ref "main" -Version "") "https://github.com/dat267/fsvc/archive/refs/heads/main.zip" "trailing slash trimmed"

Write-Host "== Get-FSvcCurrentUserModuleDir ==" -ForegroundColor Cyan
$sep = [System.IO.Path]::PathSeparator
$corePath = "C:\Users\me\Documents\PowerShell\Modules;C:\Program Files\PowerShell\Modules"
$coreExpected = "C:\Users\me\Documents\PowerShell\Modules"
Assert-Equal (Get-FSvcCurrentUserModuleDir -PSModulePath $corePath -Documents "C:\Users\me\Documents" -UserHome "C:\Users\me" -OnWindows $true -DesktopEdition $false -Separator ";" ) $coreExpected "core picks PowerShell\Modules entry"
$desktopPath = "C:\Users\me\Documents\WindowsPowerShell\Modules"
Assert-Equal (Get-FSvcCurrentUserModuleDir -PSModulePath $desktopPath -Documents "C:\Users\me\Documents" -UserHome "C:\Users\me" -OnWindows $true -DesktopEdition $true -Separator ";") $desktopPath "desktop picks WindowsPowerShell\Modules entry"
Assert-Equal (Get-FSvcCurrentUserModuleDir -PSModulePath "" -Documents "C:\Users\me\Documents" -UserHome "C:\Users\me" -OnWindows $true -DesktopEdition $false) ([System.IO.Path]::Combine("C:\Users\me\Documents", "PowerShell", "Modules")) "windows fallback to Documents"
Assert-Equal (Get-FSvcCurrentUserModuleDir -PSModulePath "" -Documents "" -UserHome "/home/me" -OnWindows $false -DesktopEdition $false) ([System.IO.Path]::Combine("/home/me", ".local", "share", "powershell", "Modules")) "unix fallback under home"

Write-Host "== Get-FSvcModuleItems ==" -ForegroundColor Cyan
$items = Get-FSvcModuleItems
Assert-Equal ($items -join ",") "fsvc.psd1,fsvc.psm1,fsvc.format.ps1xml,Private,Public,LICENSE" "module item list"

Write-Host "== Packaging completeness ==" -ForegroundColor Cyan
# Every file the manifest points at must exist and be shipped by BOTH packaging
# paths, otherwise the published module fails to import (FormatsToProcess
# missing) or installs incomplete.
$manifestData = Import-PowerShellDataFile -Path (Join-Path $repoRoot 'fsvc.psd1')
$referenced = @(
    $manifestData.RootModule
    @($manifestData.FormatsToProcess)
    @($manifestData.ScriptsToProcess)
    @($manifestData.NestedModules)
    @($manifestData.RequiredAssemblies)
) | Where-Object { $_ } | Sort-Object -Unique

function Test-FSvcShippedBy {
    param([string]$File, [object[]]$Items)
    foreach ($item in $Items) {
        if ($item -eq $File) { return $true }
        if ($File -like ($item + '/*') -or $File -like ($item + '\*')) { return $true }
    }
    return $false
}

$releaseYml = Get-Content -LiteralPath (Join-Path $repoRoot '.github/workflows/release.yml') -Raw
foreach ($f in $referenced) {
    Assert-True (Test-Path -LiteralPath (Join-Path $repoRoot $f)) "manifest file exists: $f"
    Assert-True (Test-FSvcShippedBy -File $f -Items $items) "installer ships: $f"
    Assert-True ($releaseYml -match [regex]::Escape($f)) "release workflow stages: $f"
}

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
