# Persisted configuration: a small JSON text file, by default
#   Windows      %LOCALAPPDATA%\fsvc\config.json
#   non-Windows  ~/.config/fsvc/config.json
# Set-FSvcConfig writes it; Get-FSvcEffectiveConfig reads it.

# Older config files used these keys; read them as their current names.
$script:FSvcLegacyConfigKeys = @{
    SessionCookie = 'ItildeskSession'
}

if (-not (Get-Variable -Name FSvcConfigPath -Scope Script -ErrorAction SilentlyContinue)) {
    $script:FSvcConfigPath = $null   # when set (tests, -ConfigPath), overrides the default
}

# True on Windows, across Windows PowerShell 5.1 (no $IsWindows) and PS 7+.
function Test-IsWindowsHost {
    if ($null -ne $IsWindows) { return [bool]$IsWindows }
    return ($env:OS -eq 'Windows_NT')
}

# Default config file path for the platform.
function Get-FSvcDefaultConfigPath {
    param(
        [AllowNull()][string]$LocalAppData,
        [AllowNull()][string]$UserHome,
        [AllowNull()]$OnWindows
    )
    if ($null -eq $OnWindows) { $OnWindows = Test-IsWindowsHost }
    if ($OnWindows -and $LocalAppData) {
        return [System.IO.Path]::Combine($LocalAppData, 'fsvc', 'config.json')
    }
    if ($UserHome) {
        return [System.IO.Path]::Combine($UserHome, '.config', 'fsvc', 'config.json')
    }
    return [System.IO.Path]::Combine('.', 'fsvc', 'config.json')
}

# Effective config file path: the script override, else the platform default.
function Get-FSvcConfigPath {
    if ($script:FSvcConfigPath) { return $script:FSvcConfigPath }
    return Get-FSvcDefaultConfigPath -LocalAppData $env:LOCALAPPDATA -UserHome $HOME -OnWindows (Test-IsWindowsHost)
}

# Reads the config file into a friendly-name -> value map. A missing or
# unreadable file yields an empty map.
function Read-FSvcConfigFile {
    param([string]$Path)
    if (-not $Path) { $Path = Get-FSvcConfigPath }
    $map = @{}
    if (-not $Path -or -not (Test-Path -LiteralPath $Path)) { return $map }
    try {
        $raw = Get-Content -LiteralPath $Path -Raw
        if (-not $raw.Trim()) { return $map }
        $obj = $raw | ConvertFrom-Json
    } catch {
        return $map
    }
    $names = @($obj.PSObject.Properties.Name)
    foreach ($key in $script:FSvcEnvNames.Keys) {
        if ($names -contains $key -and "$($obj.$key)" -ne '') {
            $map[$key] = [string]$obj.$key
        }
    }
    foreach ($legacy in $script:FSvcLegacyConfigKeys.Keys) {
        $current = $script:FSvcLegacyConfigKeys[$legacy]
        if (-not $map.ContainsKey($current) -and $names -contains $legacy -and "$($obj.$legacy)" -ne '') {
            $map[$current] = [string]$obj.$legacy
        }
    }
    return $map
}

# Merges settings into the config file. Empty values remove a key; the file is
# deleted once nothing is left.
function Write-FSvcConfigFile {
    param([hashtable]$Settings, [string]$Path)
    if (-not $Path) { $Path = Get-FSvcConfigPath }
    $current = Read-FSvcConfigFile -Path $Path
    foreach ($key in $Settings.Keys) {
        if ($Settings[$key]) { $current[$key] = [string]$Settings[$key] }
        else { [void]$current.Remove($key) }
    }
    if ($current.Count -eq 0) {
        if (Test-Path -LiteralPath $Path) { Remove-Item -LiteralPath $Path -Force }
        return
    }
    $dir = Split-Path -Parent $Path
    if ($dir -and -not (Test-Path -LiteralPath $dir)) {
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
    }
    ($current | ConvertTo-Json -Depth 5) | Set-Content -LiteralPath $Path -Encoding UTF8
}
