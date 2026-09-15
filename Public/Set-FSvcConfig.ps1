function Set-FSvcConfig {
    <#
    .SYNOPSIS
        Stores Freshservice connection settings for this session and persists them for future sessions.
    .DESCRIPTION
        Settings are kept for the current session and saved to a small JSON file
        (%LOCALAPPDATA%\fsvc\config.json on Windows, ~/.config/fsvc/config.json
        elsewhere), so a new session picks them up without calling this again.
        In-session values take precedence. Passing an empty value clears the
        setting from the file.

        Note: ItildeskSession and CsrfToken are persisted in plaintext, readable by
        any process running as you.
    .EXAMPLE
        Set-FSvcConfig -Subdomain acme -ItildeskSession '<cookie>' -CsrfToken '<token>'
    #>
    [CmdletBinding()]
    param(
        [string]$Subdomain,
        [string]$ItildeskSession,
        [string]$CsrfToken,
        [string]$BaseUrl,
        [string]$UtcOffset,
        [string]$LogPath,
        [string]$ConfigPath
    )

    # Only recognised settings are stored/persisted; -ConfigPath targets a
    # specific file (used by tests and unusual setups).
    $values = @{}
    foreach ($pair in $PSBoundParameters.GetEnumerator()) {
        if ($script:FSvcEnvNames.ContainsKey($pair.Key)) {
            $values[$pair.Key] = [string]$pair.Value
        }
    }

    if ($values.Count -eq 0) {
        Write-Warning "No settings supplied; nothing changed. Pass one or more settings (for example -Subdomain) or run Get-FSvcConfig to see the current values."
        return
    }

    $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction SilentlyContinue
    if (-not $stored) {
        $script:FSvcConfig = @{}
        $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction Stop
    }
    foreach ($key in $values.Keys) {
        $stored.Value[$key] = $values[$key]
    }

    $targetPath = if ($PSBoundParameters.ContainsKey('ConfigPath') -and $ConfigPath) { $ConfigPath } else { $null }
    Write-FSvcConfigFile -Settings $values -Path $targetPath
}
