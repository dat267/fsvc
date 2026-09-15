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
$tempConfig = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-module-" + [guid]::NewGuid().ToString() + ".json")
# Keep persistence out of the real config file during tests.
Set-FSvcConfig -Subdomain acme -ItildeskSession secret -CsrfToken tok -ConfigPath $tempConfig
$cfg = Get-FSvcConfig
Assert-True ($cfg.Subdomain -eq 'acme') "config stored"
Assert-True ($cfg.ItildeskSession -eq '<set>') "session is masked in output"
Assert-True ($cfg.BaseUrl -eq 'https://acme.freshservice.com') "base url derived"
$persisted = Get-Content -LiteralPath $tempConfig -Raw | ConvertFrom-Json
Assert-True ($persisted.Subdomain -eq 'acme') "setting persisted to the config file"
Assert-True ($persisted.ItildeskSession -eq 'secret') "session cookie persisted to the config file"
Remove-Item -LiteralPath $tempConfig -Force -ErrorAction SilentlyContinue

Write-Host "== No-op guard ==" -ForegroundColor Cyan
$tempConfig = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-noop-" + [guid]::NewGuid().ToString() + ".json")
$warnings = @()
Set-FSvcConfig -ConfigPath $tempConfig -WarningVariable warnings -WarningAction SilentlyContinue | Out-Null
Assert-True (@($warnings).Count -ge 1) "warns when no settings are supplied"
Assert-True (-not (Test-Path -LiteralPath $tempConfig)) "writes no config file when nothing is supplied"
Remove-Item -LiteralPath $tempConfig -Force -ErrorAction SilentlyContinue

Write-Host "== Format data ==" -ForegroundColor Cyan
$fmt = Get-FormatData -TypeName 'FSvc.TicketOverviewRow'
Assert-True ($null -ne $fmt) "module registers the overview format view"

Write-Host "== PowerShell 5.1 / 7 parity ==" -ForegroundColor Cyan
$httpSource = Get-Content -LiteralPath (Join-Path $repoRoot 'Private/FSvc.Http.ps1') -Raw
Assert-True ($httpSource -notmatch 'PSEdition') "no PowerShell-edition branching in the HTTP path"
Assert-True ($httpSource -notmatch '"Cookie"\s*=') "auth cookie travels in the WebSession, not a Cookie header"
Assert-True ($httpSource -match 'WebSession') "requests pass the shared WebSession"

Write-Host ""
if ($failures -gt 0) { Write-Host ("{0} test(s) failed" -f $failures) -ForegroundColor Red; exit 1 }
Write-Host "All tests passed." -ForegroundColor Green
exit 0
