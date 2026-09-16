#!/usr/bin/env pwsh
# Install.ps1
#
# Installs the fsvc module from GitHub into the current user's module folder,
# so `Import-Module fsvc` works. Use it instead of a PowerShell Gallery
# repository when you do not want to publish to the Gallery.
#
# In-memory mode (`irm <link> | iex`, or a scriptblock) never calls exit, so it
# will not close your shell.
#
# Usage:
#   # from a clone
#   pwsh Install.ps1
#
#   # remote one-liner (installs from main)
#   irm https://raw.githubusercontent.com/dat267/fsvc/main/Install.ps1 | iex
#
#   # a specific release tag or branch
#   pwsh Install.ps1 -Version v1.0.0
#   pwsh Install.ps1 -Ref main -Force
#
#   # remove it
#   pwsh Install.ps1 -Uninstall
#
# After installing:  Import-Module fsvc  ;  Get-FSvcConfig

[CmdletBinding()]
param(
    [string]$Repository = 'dat267/fsvc',
    [string]$Ref = 'main',
    [string]$Version,
    [string]$ArchiveUrl,
    [string]$Destination,
    [switch]$Force,
    [switch]$Uninstall
)

# --- helpers -----------------------------------------------------------------

# GitHub archive (zip) URL for a branch/tag. $Version wins over $Ref.
function Get-FSvcArchiveUrl {
    param([string]$Repository, [string]$Ref, [string]$Version)
    $repo = $Repository.Trim().TrimEnd('/')
    if ($Version) { return ("https://github.com/{0}/archive/refs/tags/{1}.zip" -f $repo, $Version) }
    return ("https://github.com/{0}/archive/refs/heads/{1}.zip" -f $repo, $Ref)
}

# Current user's module folder: the matching entry from PSModulePath when
# present, else the conventional Documents / home location.
function Get-FSvcCurrentUserModuleDir {
    param(
        [AllowNull()][string]$PSModulePath,
        [AllowNull()][string]$Documents,
        [AllowNull()][string]$UserHome,
        [bool]$OnWindows,
        [bool]$DesktopEdition,
        [string]$Separator = [string][System.IO.Path]::PathSeparator
    )
    $folder = if ($DesktopEdition) { 'WindowsPowerShell' } else { 'PowerShell' }
    if ($PSModulePath) {
        $pattern = '[\\/]' + [regex]::Escape($folder) + '[\\/]Modules$'
        foreach ($entry in ($PSModulePath -split [regex]::Escape($Separator))) {
            if (-not $entry) { continue }
            $normalized = $entry.TrimEnd('\', '/')
            if ($normalized -match $pattern) { return $normalized }
        }
    }
    if ($OnWindows -and $Documents) {
        return [System.IO.Path]::Combine($Documents, $folder, 'Modules')
    }
    if ($UserHome) {
        return [System.IO.Path]::Combine($UserHome, '.local', 'share', 'powershell', 'Modules')
    }
    return $null
}

# Files and folders that make up the module.
function Get-FSvcModuleItems {
    return @('fsvc.psd1', 'fsvc.psm1', 'fsvc.format.ps1xml', 'Private', 'Public', 'LICENSE')
}

# Allow dot-sourcing for tests: define helpers without installing.
if ($MyInvocation.InvocationName -eq '.') { return }

# --- resolve defaults --------------------------------------------------------

$onWindows = if ($null -ne $IsWindows) { [bool]$IsWindows } else { $env:OS -eq 'Windows_NT' }
$desktop = $PSVersionTable.PSEdition -eq 'Desktop'
$documents = if ($onWindows) { [Environment]::GetFolderPath('MyDocuments') } else { $null }

if (-not $Destination) {
    $Destination = Get-FSvcCurrentUserModuleDir -PSModulePath $env:PSModulePath -Documents $documents -UserHome $HOME -OnWindows $onWindows -DesktopEdition $desktop
}
if (-not $Destination) {
    throw "Could not determine a module folder. Pass -Destination."
}
$moduleDir = Join-Path $Destination 'fsvc'

# In-memory (iex) mode must not exit the caller's shell.
$inMemory = [string]::IsNullOrEmpty($PSScriptRoot)
function Complete-FSvcInstall {
    param([int]$Code)
    if ($inMemory) { return } else { exit $Code }
}

# --- uninstall ---------------------------------------------------------------

if ($Uninstall) {
    if (Test-Path -LiteralPath $moduleDir) {
        Remove-Item -LiteralPath $moduleDir -Recurse -Force
        Write-Host ("Removed {0}" -f $moduleDir)
    } else {
        Write-Host ("Nothing to remove at {0}" -f $moduleDir)
    }
    Write-Host "Close and reopen your session to fully unload it if it was imported."
    Complete-FSvcInstall -Code 0
    return
}

# --- install -----------------------------------------------------------------

if ((Test-Path -LiteralPath $moduleDir) -and -not $Force) {
    Write-Host ("{0} already exists. Use -Force to update it." -f $moduleDir) -ForegroundColor Yellow
    Complete-FSvcInstall -Code 1
    return
}

if (-not $ArchiveUrl) {
    $ArchiveUrl = Get-FSvcArchiveUrl -Repository $Repository -Ref $Ref -Version $Version
}

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("fsvc-install-" + [guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $temp -Force | Out-Null
$zip = Join-Path $temp 'fsvc.zip'
$extract = Join-Path $temp 'extract'

try {
    try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 } catch { }
    Write-Host ("Downloading {0}" -f $ArchiveUrl)
    Invoke-WebRequest -Uri $ArchiveUrl -OutFile $zip -UseBasicParsing
    Expand-Archive -LiteralPath $zip -DestinationPath $extract -Force

    $root = Get-ChildItem -LiteralPath $extract -Directory | Select-Object -First 1
    if (-not $root) { throw "Archive did not contain a top-level folder." }

    if (Test-Path -LiteralPath $moduleDir) { Remove-Item -LiteralPath $moduleDir -Recurse -Force }
    New-Item -ItemType Directory -Path $moduleDir -Force | Out-Null
    foreach ($item in (Get-FSvcModuleItems)) {
        $source = Join-Path $root.FullName $item
        if (Test-Path -LiteralPath $source) {
            Copy-Item -LiteralPath $source -Destination $moduleDir -Recurse -Force
        }
    }
    Write-Host ("Installed fsvc to {0}" -f $moduleDir)
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "Done. Use it with:"
Write-Host "  Import-Module fsvc"
Write-Host "  Set-FSvcConfig -Subdomain <domain> -SessionCookie '<cookie>' -CsrfToken '<token>'"
Write-Host "  Get-FSvcTicketOverview"
Complete-FSvcInstall -Code 0
