# HTTP plumbing for the Freshservice private API (/api/_/).

# Builds a query string (without leading ?) from a hashtable of params.
function Build-FSvcQueryString {
    param([hashtable]$Query)
    if (-not $Query -or $Query.Count -eq 0) { return "" }
    return ($Query.GetEnumerator() | ForEach-Object { "{0}={1}" -f $_.Key, [uri]::EscapeDataString([string]$_.Value) }) -join "&"
}

# Appends a query string to a path as ?key=value&... . Uses -f rather than
# "$Path?$qs" because `?` is legal in PowerShell variable names and would be
# swallowed into an undefined variable.
function Add-FSvcQuery {
    param([string]$Path, [string]$QueryString)
    if ($QueryString) { return "{0}?{1}" -f $Path, $QueryString }
    return $Path
}

# Parses API JSON, keeping ISO timestamp strings verbatim on PS 7.5+
# (-DateKind String) so the account offset is never converted away.
function ConvertFrom-FSvcJson {
    param([Parameter(Mandatory, ValueFromPipeline)][string]$Json)
    if ((Get-Command ConvertFrom-Json).Parameters.ContainsKey("DateKind")) {
        return $Json | ConvertFrom-Json -DateKind String
    }
    return $Json | ConvertFrom-Json
}

function Invoke-FSvcGet {
    param([string]$Path, [hashtable]$Query, [hashtable]$Config)
    if (-not $Config) { $Config = Get-FSvcEffectiveConfig }
    Assert-FSvcConnection -Config $Config
    $url = Add-FSvcQuery -Path $Path -QueryString (Build-FSvcQueryString -Query $Query)
    $headers = @{
        "Accept" = "application/json"
        "Cookie" = "_itildesk_session=$($Config.SessionCookie)"
    }
    $resp = Invoke-WebRequest -Uri ("{0}/api/_/{1}" -f $Config.BaseUrl.TrimEnd('/'), $url) -Headers $headers -UseBasicParsing
    return $resp.Content
}

function Invoke-FSvcPut {
    param([string]$Path, [hashtable]$Body, [hashtable]$Config)
    if (-not $Config) { $Config = Get-FSvcEffectiveConfig }
    Assert-FSvcConnection -Config $Config
    if (-not $Config.CsrfToken) {
        throw "No CSRF token configured. Run Set-FSvcConfig -CsrfToken <value>, or set FSVC_CSRF_TOKEN."
    }
    $headers = @{
        "Accept"       = "application/json"
        "Cookie"       = "_itildesk_session=$($Config.SessionCookie)"
        "Content-Type" = "application/json; charset=utf-8"
        "X-CSRF-Token" = $Config.CsrfToken
    }
    $json = $Body | ConvertTo-Json -Compress
    $resp = Invoke-WebRequest -Uri ("{0}/api/_/{1}" -f $Config.BaseUrl.TrimEnd('/'), $Path) -Method Put -Headers $headers -Body $json -UseBasicParsing
    return $resp.Content
}
