function Get-FSvcConfig {
    <#
    .SYNOPSIS
        Shows the effective fsvc configuration (stored values merged with FSVC_* environment variables).
    #>
    [CmdletBinding()]
    param()
    $cfg = Get-FSvcEffectiveConfig
    [pscustomobject]@{
        Subdomain     = $cfg.Subdomain
        BaseUrl       = $cfg.BaseUrl
        ItildeskSession = if ($cfg.ItildeskSession) { '<set>' } else { $null }
        CsrfToken     = if ($cfg.CsrfToken) { '<set>' } else { $null }
        UtcOffset     = $cfg.UtcOffset
        LogPath       = $cfg.LogPath
    }
}
