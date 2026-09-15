#!/usr/bin/env pwsh
# Install.ps1
#
# Installs the standalone fsvc scripts into a stable folder, optionally adds
# that folder to PATH, and optionally persists shared FSVC_* configuration in
# your PowerShell profile so every script picks it up without editing.
#
# WHAT THIS IS: a small installer for this repo's scripts. It does not contact
# Freshservice or need any cookie; it only copies files and writes a managed
# block into your profile.
#
# It works from a clone (copies the local scripts/) or remotely: when run
# without a local scripts folder (e.g. piped from the web), it downloads the
# scripts instead. In remote/iex mode it never calls exit, so it will not close
# your shell.
#
# Usage:
#   # install to the default folder (%LOCALAPPDATA%\fsvc on Windows, ~/.fsvc
#   # elsewhere) and record shared config for future shells
#   pwsh scripts/Install.ps1 -AddToPath `
#        -Subdomain acme -Session "<cookie>" -CsrfToken "<token>" -LogPath "C:\logs\fsvc.log"
#
#   # install somewhere else / refresh an existing install
#   pwsh scripts/Install.ps1 -Destination C:\tools\fsvc -Force
#
#   # remove the installed folder and the profile block
#   pwsh scripts/Install.ps1 -Uninstall
#
#   # remote one-liner (no clone needed); use FSVC_* env vars for config
#   irm https://raw.githubusercontent.com/dat267/fsvc/main/scripts/Install.ps1 | iex
#
#   # remote with parameters
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/dat267/fsvc/main/scripts/Install.ps1))) -AddToPath -Subdomain acme
#
# The managed block in your profile is delimited by markers and is replaced on
# every install, so it never duplicates. Use -ProfilePath to target a different
# profile (handy for testing).

param(
    [string]$Destination = "",
    [string]$ProfilePath = $PROFILE,
    [switch]$AddToPath,
    [switch]$Uninstall,
    [switch]$Force,
    [switch]$Remote,
    [string]$RemoteBaseUrl = "https://raw.githubusercontent.com/dat267/fsvc/main/scripts",
    # Optional shared configuration written to the profile and current session:
    [string]$Subdomain,
    [string]$Session,
    [string]$CsrfToken,
    [string]$LogPath,
    [string]$TimeZoneId,
    [string]$UtcOffset
)

# --- helpers -----------------------------------------------------------------

$script:FSvcBlockStart = "# >>> fsvc install (managed block) >>>"
$script:FSvcBlockEnd = "# <<< fsvc install <<<"

# Appends $Entry to a PATH string unless it is already present (case-insensitive
# so Windows paths don't duplicate).
function Add-PathEntry {
    param([AllowNull()][string]$PathValue, [string]$Entry)
    if (-not $PathValue) { return $Entry }
    $sep = [System.IO.Path]::PathSeparator
    foreach ($p in ($PathValue -split [regex]::Escape($sep))) {
        if ($p -and $p.Trim() -ieq $Entry.Trim()) { return $PathValue }
    }
    return $PathValue.TrimEnd($sep) + $sep + $Entry
}

# Removes every occurrence of $Entry from a PATH string.
function Remove-PathEntry {
    param([AllowNull()][string]$PathValue, [string]$Entry)
    if (-not $PathValue) { return "" }
    $sep = [System.IO.Path]::PathSeparator
    $kept = @()
    foreach ($p in ($PathValue -split [regex]::Escape($sep))) {
        if ($p -and $p.Trim() -ine $Entry.Trim()) { $kept += $p }
    }
    return ($kept -join $sep)
}

# Maps the optional parameters to their FSVC_* environment variable names,
# skipping empty values.
function Get-FSvcEnvAssignments {
    param(
        [string]$Subdomain,
        [string]$Session,
        [string]$CsrfToken,
        [string]$LogPath,
        [string]$TimeZoneId,
        [string]$UtcOffset
    )
    $map = [ordered]@{}
    if ($Subdomain)  { $map["FSVC_SUBDOMAIN"] = $Subdomain }
    if ($Session)    { $map["FSVC_ITILDESK_SESSION"] = $Session }
    if ($CsrfToken)  { $map["FSVC_CSRF_TOKEN"] = $CsrfToken }
    if ($LogPath)    { $map["FSVC_LOG_PATH"] = $LogPath }
    if ($TimeZoneId) { $map["FSVC_TZ"] = $TimeZoneId }
    if ($UtcOffset)  { $map["FSVC_UTC_OFFSET"] = $UtcOffset }
    return $map
}

# Builds the profile block: a PATH prepend (optional) plus FSVC_* assignments.
# Returns "" when there is nothing to write.
function New-FSvcProfileBlock {
    param([AllowNull()][string]$PathEntry, $EnvMap)
    $lines = @()
    if ($PathEntry) {
        $sep = [System.IO.Path]::PathSeparator
        $lines += ('$env:PATH = "{0}{1}$env:PATH"' -f $PathEntry, $sep)
    }
    if ($EnvMap) {
        foreach ($k in $EnvMap.Keys) {
            $v = ([string]$EnvMap[$k]).Replace("'", "''")
            $lines += ("`$env:{0} = '{1}'" -f $k, $v)
        }
    }
    if ($lines.Count -eq 0) { return "" }
    return ($script:FSvcBlockStart + "`n" + ($lines -join "`n") + "`n" + $script:FSvcBlockEnd + "`n")
}

# Replaces the managed block in the profile, preserving everything else. An
# empty $Block removes the block. Creates the profile directory when needed.
function Update-FSvcProfile {
    param([string]$ProfilePath, [AllowNull()][string]$Block)
    $existing = ""
    if (Test-Path -LiteralPath $ProfilePath) {
        $existing = Get-Content -LiteralPath $ProfilePath -Raw
    }
    $pattern = "(?ms)" + [regex]::Escape($script:FSvcBlockStart) + ".*?" + [regex]::Escape($script:FSvcBlockEnd) + "\r?\n?"
    $cleaned = [regex]::Replace($existing, $pattern, "").TrimEnd()
    if ($Block) {
        $cleaned = $cleaned + "`n`n" + $Block
    } elseif ($cleaned) {
        $cleaned = $cleaned + "`n"
    }
    $dir = Split-Path -Parent $ProfilePath
    if ($dir -and -not (Test-Path -LiteralPath $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    Set-Content -LiteralPath $ProfilePath -Value $cleaned -NoNewline
}

# True on Windows, across Windows PowerShell 5.1 (which has no $IsWindows)
# and PowerShell 7+.
function Test-IsWindowsHost {
    if ($null -ne $IsWindows) { return [bool]$OnWindows }
    return ($env:OS -eq "Windows_NT")
}

# Default install folder: %LOCALAPPDATA%\fsvc on Windows so the home folder
# stays clean, else the hidden ~/.fsvc. Pass -Destination to override.
function Get-FSvcDefaultInstallDir {
    param([AllowNull()][string]$LocalAppData, [AllowNull()][string]$UserHome, [bool]$OnWindows)
    if ($OnWindows -and $LocalAppData) {
        return [System.IO.Path]::Combine($LocalAppData, "fsvc")
    }
    if ($UserHome) {
        return [System.IO.Path]::Combine($UserHome, ".fsvc")
    }
    return [System.IO.Path]::Combine(".", "fsvc")
}

# The .ps1 files to install from a source directory.
function Get-FSvcInstallFiles {
    param([string]$SourceDir)
    if (-not (Test-Path -LiteralPath $SourceDir)) { return @() }
    return @(Get-ChildItem -LiteralPath $SourceDir -Filter "*.ps1" -File | ForEach-Object { $_.FullName })
}

# The install set for remote mode, where there is no directory to enumerate.
# Kept in sync with scripts/ by Install.Tests.ps1.
$script:FSvcScriptNames = @(
    "Fill-PlannedStartDates.ps1",
    "Fill-PlannedStartDates.Tests.ps1",
    "Get-TicketContent.ps1",
    "Get-TicketContent.Tests.ps1",
    "Get-TicketList.ps1",
    "Get-TicketList.Tests.ps1",
    "Get-TicketOverview.ps1",
    "Get-TicketOverview.Tests.ps1",
    "Install.ps1",
    "Install.Tests.ps1",
    "Update-PlannedEndDates.ps1",
    "Update-PlannedEndDates.Tests.ps1"
)

# Builds name -> URL pairs for a remote install.
function Get-FSvcDownloadPlan {
    param([string]$RemoteBaseUrl, [string[]]$Names)
    $plan = @()
    $base = $RemoteBaseUrl.TrimEnd('/')
    foreach ($n in $Names) {
        $plan += [pscustomobject]@{ Name = $n; Url = ($base + "/" + $n) }
    }
    return $plan
}

# Downloads the install set into $Destination. PS 5.1 needs TLS 1.2 and
# -UseBasicParsing for raw GitHub content.
function Install-FSvcFromRemote {
    param([string]$RemoteBaseUrl, [string[]]$Names, [string]$Destination, [switch]$Force)
    try { [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12 } catch { }
    $plan = Get-FSvcDownloadPlan -RemoteBaseUrl $RemoteBaseUrl -Names $Names
    $copied = 0
    $skipped = 0
    foreach ($item in $plan) {
        $target = Join-Path $Destination $item.Name
        if ((Test-Path -LiteralPath $target) -and -not $Force) {
            $skipped++
            continue
        }
        Invoke-WebRequest -Uri $item.Url -OutFile $target -UseBasicParsing
        $copied++
    }
    return [pscustomobject]@{ Copied = $copied; Skipped = $skipped }
}

# Allow dot-sourcing: `path . Install.ps1` defines the helper functions
# without performing an install.
if ($MyInvocation.InvocationName -eq '.') { return }

# When piped through iex / a scriptblock there is no script path; calling exit
# would close the caller's shell, so terminate with return instead.
$inMemory = [string]::IsNullOrEmpty($PSScriptRoot)

if (-not $Destination) {
    $Destination = Get-FSvcDefaultInstallDir -LocalAppData $env:LOCALAPPDATA -UserHome $HOME -OnWindows (Test-IsWindowsHost)
}

# --- uninstall ---------------------------------------------------------------

if ($Uninstall) {
    if (Test-Path -LiteralPath $Destination) {
        Remove-Item -LiteralPath $Destination -Recurse -Force
        Write-Host ("Removed {0}" -f $Destination)
    } else {
        Write-Host ("Nothing to remove at {0}" -f $Destination)
    }
    Update-FSvcProfile -ProfilePath $ProfilePath -Block ""
    Write-Host ("Removed the fsvc block from {0}" -f $ProfilePath)
    if ($inMemory) { return } else { exit 0 }
}

# --- install -----------------------------------------------------------------

$localFiles = @(Get-FSvcInstallFiles -SourceDir $PSScriptRoot)
$useRemote = [bool]($Remote -or $localFiles.Count -eq 0)

if ($useRemote -and $script:FSvcScriptNames.Count -eq 0) {
    Write-Host "ERROR: the remote script list is empty." -ForegroundColor Red
    if ($inMemory) { return } else { exit 1 }
}

if (-not (Test-Path -LiteralPath $Destination)) {
    New-Item -ItemType Directory -Path $Destination -Force | Out-Null
}

if ($useRemote) {
    $result = Install-FSvcFromRemote -RemoteBaseUrl $RemoteBaseUrl -Names $script:FSvcScriptNames -Destination $Destination -Force:$Force
    Write-Host ("Installed scripts from {0} to {1} ({2} downloaded, {3} already present)" -f $RemoteBaseUrl, $Destination, $result.Copied, $result.Skipped)
} else {
    $copied = 0
    $skipped = 0
    foreach ($f in $localFiles) {
        $target = Join-Path $Destination (Split-Path -Leaf $f)
        if ((Test-Path -LiteralPath $target) -and -not $Force) {
            $skipped++
            continue
        }
        Copy-Item -LiteralPath $f -Destination $target -Force
        $copied++
    }
    Write-Host ("Installed scripts to {0} ({1} copied, {2} already present)" -f $Destination, $copied, $skipped)
}

$envMap = Get-FSvcEnvAssignments -Subdomain $Subdomain -Session $Session -CsrfToken $CsrfToken -LogPath $LogPath -TimeZoneId $TimeZoneId -UtcOffset $UtcOffset
$pathEntry = ""
if ($AddToPath) { $pathEntry = $Destination }
$block = New-FSvcProfileBlock -PathEntry $pathEntry -EnvMap $envMap

if ($block) {
    Update-FSvcProfile -ProfilePath $ProfilePath -Block $block
    Write-Host ("Wrote shared config to {0}" -f $ProfilePath)

    # Apply to the current session so it works immediately, without reopening.
    if ($AddToPath) { $env:PATH = Add-PathEntry -PathValue $env:PATH -Entry $Destination }
    foreach ($k in $envMap.Keys) { Set-Item -Path ("env:" + $k) -Value ([string]$envMap[$k]) }
}

Write-Host ""
Write-Host ("Done. Scripts are in {0}." -f $Destination)
if ($block) {
    Write-Host "Open a new shell (or run '. `$PROFILE') to pick up the PATH/config changes."
}
Write-Host ("Run them directly, e.g.  pwsh (Join-Path '{0}' 'Get-TicketOverview.ps1')" -f $Destination)