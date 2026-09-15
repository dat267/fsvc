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
    $tickets = @()
    do {
        $query = @{
            order_by   = $OrderBy
            order_type = $OrderType
            per_page   = $PerPage
            page       = $Page
        }
        if ($FilterId) { $query['filter'] = $FilterId }
        if ($QueryHash) { $query['query_hash'] = $QueryHash }
        $data = (Invoke-FSvcGet -Path "tickets" -Query $query) | ConvertFrom-FSvcJson
        $tickets += @($data.tickets)
        $hasNext = $data.meta.has_next
        $Page++
    } while ($hasNext -and $Page -lt $MaxPages)
    return $tickets
}
