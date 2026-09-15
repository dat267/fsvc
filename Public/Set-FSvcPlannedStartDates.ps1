function Set-FSvcPlannedStartDates {
    <#
    .SYNOPSIS
        Fills a null planned_start_date from created_at (rounded up to the quarter hour).
    .DESCRIPTION
        Only touches self-assigned unresolved tickets whose planned_start_date is
        null. Supports -WhatIf / -Confirm; use -Confirm:$false for unattended runs.
        Runs are serialised with a lock file.
    .EXAMPLE
        Set-FSvcPlannedStartDates -WhatIf
    .EXAMPLE
        Set-FSvcPlannedStartDates -Confirm:$false -LogPath C:\logs\fsvc.log
    #>
    [CmdletBinding(SupportsShouldProcess, ConfirmImpact = 'Medium')]
    param(
        [string]$QueryHash = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]',
        [int]$PerPage = 100,
        [string]$LogPath
    )
    $cfg = Get-FSvcEffectiveConfig
    $effectiveLog = if ($PSBoundParameters.ContainsKey('LogPath')) { $LogPath } else { $cfg.LogPath }

    $lockPath = Join-Path ([System.IO.Path]::GetTempPath()) 'fsvc-planned-start-dates.lock'
    $lock = Enter-FSvcRunLock -Path $lockPath
    if ($null -eq $lock) { throw "Another fsvc run is in progress (lock: $lockPath). Delete it if stale." }
    $logging = Start-FSvcLogging -Path $effectiveLog
    try {
        $changes = @()
        foreach ($t in @(Get-FSvcTickets -QueryHash $QueryHash -PerPage $PerPage -Config $cfg)) {
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

        foreach ($c in $changes) {
            $applied = $false
            if ($PSCmdlet.ShouldProcess(("ticket {0}" -f $c.Id), ("set planned_start_date to {0}" -f $c.To))) {
                Invoke-FSvcPut -Path ("tickets/{0}" -f $c.Id) -Body @{ planned_start_date = $c.To } -Config $cfg | Out-Null
                $applied = $true
            }
            $c | Add-Member -NotePropertyName Applied -NotePropertyValue $applied
            $c
        }
    } finally {
        Stop-FSvcLogging -Active $logging
        Exit-FSvcRunLock -Handle $lock -Path $lockPath
    }
}
