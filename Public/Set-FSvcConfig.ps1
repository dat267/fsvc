function Set-FSvcConfig {
    <#
    .SYNOPSIS
        Stores Freshservice connection settings for the current session.
    .DESCRIPTION
        Values set here are used by every fsvc command and take precedence over
        the FSVC_* environment variables, which fill any value not set here.
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
