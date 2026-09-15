function Set-FSvcConfig {
    <#
    .SYNOPSIS
        Stores Freshservice connection settings for the current session.
    .DESCRIPTION
        Values set here are used by every fsvc command. A non-empty FSVC_*
        environment variable overrides the stored value, so a shared environment
        configuration wins without re-running this command.
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
        [string]$LogPath
    )
    $values = @{}
    foreach ($pair in $PSBoundParameters.GetEnumerator()) {
        $values[$pair.Key] = [string]$pair.Value
    }
    $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction SilentlyContinue
    if (-not $stored) {
        $script:FSvcConfig = @{}
        $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction Stop
    }
    foreach ($key in $values.Keys) { $stored.Value[$key] = $values[$key] }
}
