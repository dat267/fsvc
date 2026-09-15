function Get-FSvcTicketContent {
    <#
    .SYNOPSIS
        Returns a ticket and its full conversation trace as an object.
    .EXAMPLE
        Get-FSvcTicketContent -Id 10100 | Format-FSvcTicketContent
    .EXAMPLE
        Get-FSvcTicketContent -Id 10100 | ConvertTo-Json -Depth 10
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory, Position = 0)][int64]$Id
    )
    $ticket = ((Invoke-FSvcGet -Path ("tickets/{0}" -f $Id)) | ConvertFrom-FSvcJson).ticket
    if (-not $ticket) { throw ("Ticket {0} not found." -f $Id) }
    $conversations = Invoke-FSvcPagedQuery -Path ("tickets/{0}/conversations" -f $Id) -ArrayKey 'conversations' -BaseQuery @{
        order_by   = 'created_at'
        order_type = 'asc'
        per_page   = 100
    }
    [pscustomobject]@{ Ticket = $ticket; Conversations = $conversations }
}
