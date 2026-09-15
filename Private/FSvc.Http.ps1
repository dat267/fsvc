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

# Transport seam: tests (or an advanced caller) can replace the HTTP transport
# with a scriptblock that receives @{ Method; Path; Query; Body; Config } and
# returns the raw response body. $null means the real Invoke-WebRequest.
if (-not (Get-Variable -Name FSvcTransport -Scope Script -ErrorAction SilentlyContinue)) {
    $script:FSvcTransport = $null
}

# Single entry point for API calls. Resolves config, asserts the connection,
# and either calls the injected transport or performs the real HTTP request.
function Invoke-FSvcRequest {
    param(
        [string]$Method,
        [string]$Path,
        [hashtable]$Query,
        [hashtable]$Body,
        [hashtable]$Config
    )
    if (-not $Config) { $Config = Get-FSvcEffectiveConfig }
    Assert-FSvcConnection -Config $Config

    $transport = Get-Variable -Name FSvcTransport -Scope Script -ErrorAction SilentlyContinue
    if ($transport -and $null -ne $transport.Value) {
        return & $transport.Value @{ Method = $Method; Path = $Path; Query = $Query; Body = $Body; Config = $Config }
    }

    $url = Add-FSvcQuery -Path $Path -QueryString (Build-FSvcQueryString -Query $Query)
    $headers = @{
        "Accept" = "application/json"
        "Cookie" = "_itildesk_session=$($Config.ItildeskSession)"
    }
    $params = @{
        Uri             = ("{0}/api/_/{1}" -f $Config.BaseUrl.TrimEnd('/'), $url)
        Headers         = $headers
        Method          = $Method
        UseBasicParsing = $true
    }
    if ($Method -ne 'GET') {
        if (-not $Config.CsrfToken) {
            throw "No CSRF token configured. Run Set-FSvcConfig -CsrfToken <value>, or set FSVC_CSRF_TOKEN."
        }
        $headers['Content-Type'] = 'application/json; charset=utf-8'
        $headers['X-CSRF-Token'] = $Config.CsrfToken
        $params['Body'] = ($Body | ConvertTo-Json -Compress)
    }
    $resp = Invoke-WebRequest @params
    return $resp.Content
}

function Invoke-FSvcGet {
    param([string]$Path, [hashtable]$Query, [hashtable]$Config)
    return Invoke-FSvcRequest -Method GET -Path $Path -Query $Query -Config $Config
}

function Invoke-FSvcPut {
    param([string]$Path, [hashtable]$Body, [hashtable]$Config)
    return Invoke-FSvcRequest -Method PUT -Path $Path -Body $Body -Config $Config
}

# Walks every page of a list endpoint and returns the items from $ArrayKey.
# $BaseQuery is copied per page (page is added), and $Fetch defaults to
# Invoke-FSvcGet so tests can stub the whole traversal.
function Invoke-FSvcPagedQuery {
    param(
        [Parameter(Mandatory)][string]$Path,
        [hashtable]$BaseQuery,
        [Parameter(Mandatory)][string]$ArrayKey,
        [int]$StartPage = 1,
        [int]$MaxPages = 1000,
        [hashtable]$Config,
        [scriptblock]$Fetch
    )
    if (-not $BaseQuery) { $BaseQuery = @{} }
    if (-not $Fetch) {
        $Fetch = { param($P, $Q, $C) Invoke-FSvcGet -Path $P -Query $Q -Config $C }
    }

    $items = @()
    $page = $StartPage
    $fetched = 0
    do {
        $query = @{}
        foreach ($k in $BaseQuery.Keys) { $query[$k] = $BaseQuery[$k] }
        $query['page'] = $page
        $data = (& $Fetch $Path $query $Config) | ConvertFrom-FSvcJson
        $items += @($data.$ArrayKey)
        $hasNext = $data.meta.has_next
        $page++
        $fetched++
    } while ($hasNext -and $fetched -lt $MaxPages)
    return $items
}
