# Module configuration. Values set with Set-FSvcConfig take precedence over the
# matching FSVC_* environment variables, which fill anything not set there; an
# explicit per-call override beats both.
if (-not (Get-Variable -Name FSvcConfig -Scope Script -ErrorAction SilentlyContinue)) {
    $script:FSvcConfig = @{}
}


$script:FSvcEnvNames = @{
    Subdomain     = 'FSVC_SUBDOMAIN'
    SessionCookie = 'FSVC_ITILDESK_SESSION'
    CsrfToken     = 'FSVC_CSRF_TOKEN'
    BaseUrl       = 'FSVC_BASE_URL'
    TimeZoneId    = 'FSVC_TZ'
    UtcOffset     = 'FSVC_UTC_OFFSET'
    LogPath       = 'FSVC_LOG_PATH'
}

# Returns the effective config hashtable: per-call override, then the session
# value from Set-FSvcConfig, then the environment variable, then the persisted
# config file, then $null. Environment above file follows the usual convention,
# so an FSVC_* variable can override saved settings for one process. BaseUrl is
# derived from Subdomain when not set outright.
function Get-FSvcEffectiveConfig {
    param([hashtable]$Overrides)

    $stored = Get-Variable -Name FSvcConfig -Scope Script -ErrorAction SilentlyContinue
    $fileCfg = Read-FSvcConfigFile
    $cfg = @{}
    foreach ($key in $script:FSvcEnvNames.Keys) {
        $value = $null
        if ($Overrides -and $Overrides.ContainsKey($key)) { $value = $Overrides[$key] }
        if (-not $value -and $stored -and $stored.Value.ContainsKey($key)) { $value = $stored.Value[$key] }
        if (-not $value) { $value = [Environment]::GetEnvironmentVariable($script:FSvcEnvNames[$key]) }
        if (-not $value -and $fileCfg.ContainsKey($key)) { $value = $fileCfg[$key] }
        $cfg[$key] = $value
    }
    if (-not $cfg.BaseUrl -and $cfg.Subdomain) {
        $cfg.BaseUrl = "https://$($cfg.Subdomain).freshservice.com"
    }
    return $cfg
}

# Throws a helpful error when the connection cannot be made.
function Assert-FSvcConnection {
    param([hashtable]$Config)
    if (-not $Config.BaseUrl) {
        throw "No base URL configured. Run Set-FSvcConfig -Subdomain <domain> (or -BaseUrl), or set FSVC_SUBDOMAIN / FSVC_BASE_URL."
    }
    if (-not $Config.SessionCookie) {
        throw "No session configured. Run Set-FSvcConfig -SessionCookie <value>, or set FSVC_ITILDESK_SESSION."
    }
}
