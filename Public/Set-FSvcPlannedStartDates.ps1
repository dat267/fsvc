function Set-FSvcPlannedStartDates {
    <#
    .SYNOPSIS
        Fills a null planned_start_date from created_at (rounded up to the quarter hour).
    .DESCRIPTION
        Defaults to the SelfAssigned view (self-assigned unresolved tickets);
        pass -View Unassigned or a raw -QueryHash to target something else.
        Only touches tickets whose planned_start_date is null. Supports -WhatIf /
        -Confirm; use -Confirm:$false for unattended runs. Runs are serialised
        with a lock file.
    .EXAMPLE
        Set-FSvcPlannedStartDates -WhatIf
    .EXAMPLE
        Set-FSvcPlannedStartDates -Confirm:$false -LogPath C:\logs\fsvc.log
    #>
    [CmdletBinding(SupportsShouldProcess, ConfirmImpact = 'Medium')]
    param(
        [ValidateSet('SelfAssigned', 'Unassigned')][string]$View = 'SelfAssigned',
        [string]$QueryHash,
        [int]$PerPage = 100,
        [string]$LogPath
    )
    $cfg = Get-FSvcEffectiveConfig
    $effectiveLog = if ($PSBoundParameters.ContainsKey('LogPath')) { $LogPath } else { $cfg.LogPath }

    $tickets = if ($QueryHash) {
        Get-FSvcTickets -QueryHash $QueryHash -PerPage $PerPage -Config $cfg
    } else {
        Get-FSvcViewTickets -View $View -PerPage $PerPage -Config $cfg
    }

    $changes = @()
    foreach ($t in @($tickets)) {
        if (-not (Test-FSvcFillStart -PlannedStartDate $t.planned_start_date -CreatedAt $t.created_at)) { continue }
        $at = ConvertTo-FSDateTimeOffset $t.created_at
        if ($null -eq $at) { continue }
        $changes += [pscustomobject]@{
            Id    = $t.id
            Field = 'planned_start_date'
            From  = $t.planned_start_date
            To    = Format-Iso8601 (Round-FSvcQuarterHour $at)
        }
    }

    Invoke-FSvcChangeSet -Change $changes -Config $cfg -LogPath $effectiveLog `
        -LockName 'fsvc-planned-start-dates' `
        -Should { param($Target, $Action) $PSCmdlet.ShouldProcess($Target, $Action) } `
        -Apply { param($c) Invoke-FSvcPut -Path ("tickets/{0}" -f $c.Id) -Body @{ planned_start_date = $c.To } -Config $cfg }
}
