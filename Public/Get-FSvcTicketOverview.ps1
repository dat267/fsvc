function Get-FSvcTicketOverview {
    <#
    .SYNOPSIS
        Three-list triage: unassigned, waiting on customer, awaiting agent.
    .DESCRIPTION
        Returns one object per ticket with a Category property, so pipe it to
        Where-Object / Group-Object / Format-Table.
    .EXAMPLE
        Get-FSvcTicketOverview -OlderThanDays 2 | Format-Table Category, Id, Subject, Days
    #>
    [CmdletBinding()]
    param(
        [string]$UnassignedQueryHash,
        [string]$AssignedQueryHash,
        [double]$OlderThanDays = 2,
        [int]$PerPage = 100
    )
    $cfg = Get-FSvcEffectiveConfig
    $now = [datetimeoffset]::Now

    $out = @()
    $unassigned = if ($UnassignedQueryHash) {
        Get-FSvcTickets -QueryHash $UnassignedQueryHash -PerPage $PerPage -Config $cfg
    } else {
        Get-FSvcViewTickets -View Unassigned -PerPage $PerPage -Config $cfg
    }
    foreach ($t in @($unassigned)) {
        $created = ConvertTo-FSDateTimeOffset $t.created_at
        $days = 0.0
        if ($null -ne $created) { $days = Get-FSvcBusinessDaysBetween -From $created -To $now }
        $out += [pscustomobject]@{
            PSTypeName = 'FSvc.TicketOverviewRow'
            Category   = 'unassigned'
            Id       = $t.id
            Subject  = $t.subject
            Days     = [math]::Round($days, 1)
            Link     = ("{0}/a/tickets/{1}" -f $cfg.BaseUrl, $t.id)
        }
    }

    $assigned = if ($AssignedQueryHash) {
        Get-FSvcTickets -QueryHash $AssignedQueryHash -PerPage $PerPage -Config $cfg
    } else {
        Get-FSvcViewTickets -View SelfAssigned -PerPage $PerPage -Config $cfg
    }
    foreach ($t in @($assigned)) {
        $latest = Get-FSvcLatestConversation -TicketId $t.id -Config $cfg
        $lastMessage = $null
        $lastUser = [int64]0
        if ($null -ne $latest) { $lastMessage = $latest.At; $lastUser = $latest.UserId }
        $category = Get-FSvcTicketCategory -ResponderID $t.responder_id -LastMessage $lastMessage -LastUserID $lastUser -CreatedAt (ConvertTo-FSDateTimeOffset $t.created_at) -OlderThanDays $OlderThanDays -Now $now
        if ($category -ne 'waiting' -and $category -ne 'awaiting_agent') { continue }
        $ref = ConvertTo-FSDateTimeOffset $t.created_at
        if ($null -ne $lastMessage) { $ref = $lastMessage }
        $days = 0.0
        if ($null -ne $ref) { $days = Get-FSvcBusinessDaysBetween -From $ref -To $now }
        $out += [pscustomobject]@{
            PSTypeName = 'FSvc.TicketOverviewRow'
            Category   = $category
            Id       = $t.id
            Subject  = $t.subject
            Days     = [math]::Round($days, 1)
            Link     = ("{0}/a/tickets/{1}" -f $cfg.BaseUrl, $t.id)
        }
    }
    return $out
}
