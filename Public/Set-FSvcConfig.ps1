function Set-FSvcConfig {
    <#
    .SYNOPSIS
        Stores Freshservice connection settings for this session and persists them for future sessions.
    .DESCRIPTION
        Settings are kept for the current session and also written to a managed
        block in your PowerShell profile, which sets the FSVC_* environment
        variables when the profile loads - so a new session picks them up without
        calling this again. In-session values take precedence over the persisted
        environment variables. Passing an empty value clears that setting.

        Note: SessionCookie and CsrfToken are persisted in plaintext, readable by
        any process running as you.
    .EXAMPLE
        Set-FSvcConfig -Subdomain acme -SessionCookie '<cookie>' -CsrfToken '<token>'
    #>
    [CmdletBinding()]
    param(
        [string]$Subdomain,
        [string]$SessionCookie,
        [string]$CsrfToken,
        [string]$BaseUrl,
        [string]$TimeZoneId,
        [string]$UtcOffset,
        [string]$LogPath,
        [string]$ProfilePath
    )

    # Only recognised settings are stored/persisted; -ProfilePath targets a
    # specific profile (used by tests and unusual setups).
    $values = @{}
    foreach ($pair in $PSBoundParameters.GetEnumerator()) {
        if ($script:FSvcEnvNames.ContainsKey($pair.Key)) {
            $values[$pair.Key] = [string]$pair.Value
        }
    }

    $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction SilentlyContinue
    if (-not $stored) {
        $script:FSvcConfig = @{}
        $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction Stop
    }
    foreach ($key in $values.Keys) {
        $stored.Value[$key] = $values[$key]
        $envName = $script:FSvcEnvNames[$key]
        if ($values[$key]) {
            Set-Item -Path ("env:" + $envName) -Value $values[$key]
        } else {
            Remove-Item -Path ("env:" + $envName) -ErrorAction SilentlyContinue
        }
    }

    $targetProfile = if ($PSBoundParameters.ContainsKey('ProfilePath') -and $ProfilePath) {
        $ProfilePath
    } elseif ($script:FSvcProfilePath) {
        $script:FSvcProfilePath
    } else {
        $PROFILE
    }

    Set-FSvcPersistentSettings -Settings $values -ProfilePath $targetProfile
}
