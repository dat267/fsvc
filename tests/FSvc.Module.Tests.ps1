#!/usr/bin/env pwsh
# FSvc.Module.Tests.ps1 - manifest and module surface checks.

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

$failures = 0
function Assert-True {
    param([bool]$Actual, [string]$Label)
    if (-not $Actual) { Write-Host ("FAIL: {0}" -f $Label) -ForegroundColor Red; $script:failures++ }
    else { Write-Host ("ok: {0}" -f $Label) -ForegroundColor DarkGray }
}

Write-Host "== Manifest ==" -ForegroundColor Cyan
$manifest = Test-ModuleManifest -Path (Join-Path $repoRoot 'fsvc.psd1')
Assert-True ($null -ne $manifest) "manifest is valid"
Assert-True ($manifest.Name -eq 'fsvc') "module name is fsvc"
Assert-True ($manifest.Version -ge [version]'0.0.1') "version is set"

Write-Host "== Import / exports ==" -ForegroundColor Cyan
Import-Module (Join-Path $repoRoot 'fsvc.psd1') -Force
$expected = @(
    'Get-FSvcConfig', 'Set-FSvcConfig', 'Test-FSvcSession',
    'Get-FSvcTicketList', 'Get-FSvcTicketContent', 'Format-FSvcTicketContent',
    'Get-FSvcTicketOverview', 'Set-FSvcPlannedStartDates', 'Update-FSvcPlannedEndDates'
)
$exported = (Get-Command -Module fsvc).Name
foreach ($fn in $expected) { Assert-True ($exported -contains $fn) "exports $fn" }
Assert-True (-not ($exported -contains 'Invoke-FSvcGet')) "private helpers are not exported"

Write-Host "== Config round-trip ==" -ForegroundColor Cyan
$tempProfile = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-module-" + [guid]::NewGuid().ToString() + ".ps1")
# Keep persistence out of the real profile during tests.
Set-FSvcConfig -Subdomain acme -SessionCookie secret -CsrfToken tok -ProfilePath $tempProfile
$cfg = Get-FSvcConfig
Assert-True ($cfg.Subdomain -eq 'acme') "config stored"
Assert-True ($cfg.SessionCookie -eq '<set>') "session is masked in output"
Assert-True ($cfg.BaseUrl -eq 'https://acme.freshservice.com') "base url derived"
$persisted = & (Get-Module fsvc) { param($p) Read-FSvcProfileSettings -ProfilePath $p } $tempProfile
Assert-True ($persisted['FSVC_SUBDOMAIN'] -eq 'acme') "setting persisted in the profile"
Assert-True ($persisted['FSVC_ITILDESK_SESSION'] -eq 'secret') "session cookie persisted in the profile"
Remove-Item -LiteralPath $tempProfile -Force -ErrorAction SilentlyContinue

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
