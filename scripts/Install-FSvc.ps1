#!/usr/bin/env pwsh
# Install-FSvc.ps1
#
# Installs the standalone fsvc scripts into a stable folder, optionally adds
# that folder to PATH, and optionally persists shared FSVC_* configuration in
# your PowerShell profile so every script picks it up without editing.
#
# WHAT THIS IS: a small installer for this repo's scripts. It does not contact
# Freshservice or need any cookie; it only copies files and writes a managed
# block into your profile.
#
# Usage:
#   # install to ~/fsvc and record shared config for future shells
#   pwsh scripts/Install-FSvc.ps1 -AddToPath `
#        -Subdomain acme -Session "<cookie>" -CsrfToken "<token>" -LogPath "C:\logs\fsvc.log"
#
#   # install somewhere else / refresh an existing install
#   pwsh scripts/Install-FSvc.ps1 -Destination C:\tools\fsvc -Force
#
#   # remove the installed folder and the profile block
#   pwsh scripts/Install-FSvc.ps1 -Uninstall
#
# The managed block in your profile is delimited by markers and is replaced on
# every install, so it never duplicates. Use -ProfilePath to target a different
# profile (handy for testing).

param(
    [string]$Destination = (Join-Path $HOME "fsvc"),
    [string]$ProfilePath = $PROFILE,
    [switch]$AddToPath,
    [switch]$Uninstall,
    [switch]$Force,
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

# The .ps1 files to install from a source directory.
function Get-FSvcInstallFiles {
    param([string]$SourceDir)
    if (-not (Test-Path -LiteralPath $SourceDir)) { return @() }
    return @(Get-ChildItem -LiteralPath $SourceDir -Filter "*.ps1" -File | ForEach-Object { $_.FullName })
}

# Allow dot-sourcing: `path . Install-FSvc.ps1` defines the helper functions
# without performing an install.
if ($MyInvocation.InvocationName -eq '.') { return }

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
    exit 0
}

# --- install -----------------------------------------------------------------

$sourceDir = $PSScriptRoot
$files = Get-FSvcInstallFiles -SourceDir $sourceDir
if (-not $files -or $files.Count -eq 0) {
    Write-Host ("ERROR: no .ps1 files found next to the installer ({0})." -f $sourceDir) -ForegroundColor Red
    exit 1
}

if (-not (Test-Path -LiteralPath $Destination)) {
    New-Item -ItemType Directory -Path $Destination -Force | Out-Null
}

$copied = 0
$skipped = 0
foreach ($f in $files) {
    $target = Join-Path $Destination (Split-Path -Leaf $f)
    if ((Test-Path -LiteralPath $target) -and -not $Force) {
        $skipped++
        continue
    }
    Copy-Item -LiteralPath $f -Destination $target -Force
    $copied++
}
Write-Host ("Installed scripts to {0} ({1} copied, {2} already present)" -f $Destination, $copied, $skipped)

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