# Persisting Set-FSvcConfig values across sessions.
#
# Windows: writes user environment variables (registry-backed, visible to all
# new processes, including scheduled tasks). Other platforms: the .NET "User"
# environment target is a silent no-op, so a delimited block is written to the
# PowerShell profile instead. The current process is always updated by
# Set-FSvcConfig itself.

$script:FSvcBlockStart = '# >>> fsvc (managed) >>>'
$script:FSvcBlockEnd = '# <<< fsvc (managed) <<<'

# True on Windows, across Windows PowerShell 5.1 (no $IsWindows) and PS 7+.
function Test-IsWindowsHost {
    if ($null -ne $IsWindows) { return [bool]$IsWindows }
    return ($env:OS -eq 'Windows_NT')
}

# Maps friendly setting names to their FSVC_* environment variable names.
# Unknown keys are ignored; empty values are kept so they can clear a setting.
function ConvertTo-FSvcEnvironmentMap {
    param([hashtable]$Settings)
    $map = @{}
    if (-not $Settings) { return $map }
    foreach ($key in $Settings.Keys) {
        if ($script:FSvcEnvNames.ContainsKey($key)) {
            $map[$script:FSvcEnvNames[$key]] = [string]$Settings[$key]
        }
    }
    return $map
}

# Renders the managed profile block for an env-name -> value map.
function New-FSvcProfileBlock {
    param([hashtable]$Map)
    if (-not $Map -or $Map.Count -eq 0) { return "" }
    $lines = @()
    foreach ($key in ($Map.Keys | Sort-Object)) {
        $value = ([string]$Map[$key]).Replace("'", "''")
        $lines += ("`$env:{0} = '{1}'" -f $key, $value)
    }
    return ($script:FSvcBlockStart + "`n" + ($lines -join "`n") + "`n" + $script:FSvcBlockEnd + "`n")
}

# Reads the managed block back into an env-name -> value map.
function Read-FSvcProfileSettings {
    param([string]$ProfilePath)
    $map = @{}
    if (-not $ProfilePath -or -not (Test-Path -LiteralPath $ProfilePath)) { return $map }
    $text = Get-Content -LiteralPath $ProfilePath -Raw
    $pattern = "(?ms)" + [regex]::Escape($script:FSvcBlockStart) + "(.*?)" + [regex]::Escape($script:FSvcBlockEnd)
    $match = [regex]::Match($text, $pattern)
    if (-not $match.Success) { return $map }
    foreach ($line in ($match.Groups[1].Value -split "`n")) {
        $lm = [regex]::Match($line.Trim(), '^\$env:(?<n>[A-Za-z0-9_]+)\s*=\s*''(?<v>.*)''$')
        if ($lm.Success) {
            $map[$lm.Groups['n'].Value] = $lm.Groups['v'].Value.Replace("''", "'")
        }
    }
    return $map
}

# Inserts, replaces, or (with an empty $Block) removes the managed block,
# preserving the rest of the profile.
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

# Persists settings for future sessions. Windows writes user environment
# variables (via $SetUserEnvironment, injectable for tests); other platforms
# merge into the profile block. Empty values clear a persisted setting.
function Set-FSvcPersistentSettings {
    param(
        [hashtable]$Settings,
        [bool]$OnWindows,
        [string]$ProfilePath,
        [AllowNull()][scriptblock]$SetUserEnvironment
    )
    $map = ConvertTo-FSvcEnvironmentMap -Settings $Settings
    if ($map.Count -eq 0) { return }

    if ($OnWindows) {
        if (-not $SetUserEnvironment) {
            $SetUserEnvironment = {
                param($Name, $Value)
                if ($Value) { [Environment]::SetEnvironmentVariable($Name, $Value, 'User') }
                else { [Environment]::SetEnvironmentVariable($Name, $null, 'User') }
            }
        }
        foreach ($key in $map.Keys) { & $SetUserEnvironment $key $map[$key] }
        return
    }

    $merged = Read-FSvcProfileSettings -ProfilePath $ProfilePath
    foreach ($key in $map.Keys) {
        if ($map[$key]) { $merged[$key] = $map[$key] } else { [void]$merged.Remove($key) }
    }
    Update-FSvcProfile -ProfilePath $ProfilePath -Block (New-FSvcProfileBlock -Map $merged)
}
