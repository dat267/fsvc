function Test-FSvcSession {
    <#
    .SYNOPSIS
        Verifies the configured session by fetching one ticket.
    #>
    [CmdletBinding()]
    param()
    $data = (Invoke-FSvcGet -Path "tickets" -Query @{ per_page = 1 }) | ConvertFrom-FSvcJson
    [pscustomobject]@{
        Ok             = $true
        VisibleTickets = $data.meta.count
    }
}
