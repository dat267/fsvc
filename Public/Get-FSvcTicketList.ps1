function Get-FSvcTicketList {
    <#
    .SYNOPSIS
        Returns tickets from a saved-filter id or a raw query_hash.
    .DESCRIPTION
        Outputs ticket objects, so format or pipe them yourself:
        Format-Table, ConvertTo-Json, Export-Csv, Where-Object, ...
    .EXAMPLE
        Get-FSvcTicketList -FilterId 1100 | Format-Table id, subject, status, priority
    .EXAMPLE
        Get-FSvcTicketList -QueryHash '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"}]'
    #>
    [CmdletBinding()]
    param(
        [int64]$FilterId,
        [string]$QueryHash,
        [string]$OrderBy = 'created_at',
        [ValidateSet('asc', 'desc')][string]$OrderType = 'asc',
        [int]$Page = 1,
        [int]$PerPage = 100,
        [int]$MaxPages = 1000
    )
    $baseQuery = @{
        order_by   = $OrderBy
        order_type = $OrderType
        per_page   = $PerPage
    }
    if ($FilterId) { $baseQuery['filter'] = $FilterId }
    if ($QueryHash) { $baseQuery['query_hash'] = $QueryHash }

    return Invoke-FSvcPagedQuery -Path "tickets" -ArrayKey "tickets" -BaseQuery $baseQuery -StartPage $Page -MaxPages $MaxPages
}
