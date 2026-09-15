function Update-FSvcPlannedEndDates {
    <#
    .SYNOPSIS
        Sets planned_end_date to the ticket's last comment + N business days, at a chosen hour and timezone.
    .DESCRIPTION
        Every scanned ticket is recomputed so its planned end stays close to N
        business days after its latest comment (private note or public reply).
        A date that would be in the past is clamped to the nearest future
        business slot, so the planned end is always in the future. Identical
        dates are skipped. Supports -WhatIf / -Confirm; use -Confirm:$false for
        unattended runs. Runs are serialised with a lock file.
    .EXAMPLE
        Update-FSvcPlannedEndDates -BusinessDays 3 -TargetHour 17 -UtcOffset '+04:00' -WhatIf
    .EXAMPLE
        Update-FSvcPlannedEndDates -Confirm:$false -LogPath C:\logs\fsvc.log
    #>
    [CmdletBinding(SupportsShouldProcess, ConfirmImpact = 'Medium')]
    param(
        [string]$QueryHash = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]',
        [int]$BusinessDays = 3,
        [ValidateRange(0, 23)][int]$TargetHour = 17,
        [string]$TimeZoneId,
        [string]$UtcOffset,
        [int]$PerPage = 100,
        [string]$LogPath
    )
    $cfg = Get-FSvcEffectiveConfig
    $tz = if ($PSBoundParameters.ContainsKey('TimeZoneId')) { $TimeZoneId } else { $cfg.TimeZoneId }
    $off = if ($PSBoundParameters.ContainsKey('UtcOffset')) { $UtcOffset } else { $cfg.UtcOffset }
    $effectiveLog = if ($PSBoundParameters.ContainsKey('LogPath')) { $LogPath } else { $cfg.LogPath }

    $zone = Resolve-FSvcTimeZone -Id $tz
    $offset = ConvertTo-FSvcUtcOffset -Value $off

    $lockPath = Join-Path ([System.IO.Path]::GetTempPath()) 'fsvc-planned-end-dates.lock'
    $lock = Enter-FSvcRunLock -Path $lockPath
    if ($null -eq $lock) { throw "Another fsvc run is in progress (lock: $lockPath). Delete it if stale." }
    $logging = Start-FSvcLogging -Path $effectiveLog
    try {
        $tickets = @(Get-FSvcTickets -QueryHash $QueryHash -PerPage $PerPage -Config $cfg)
        $accountOffset = Get-FSvcAccountOffset -Tickets $tickets -Fallback ([datetimeoffset]::Now)
        $now = ([datetimeoffset]::Now).ToOffset($accountOffset)

        $changes = @()
        foreach ($t in $tickets) {
            $latest = Get-FSvcLatestConversation -TicketId $t.id -Config $cfg
            $base = $null
            if ($null -ne $latest) { $base = $latest.CreatedAt }
            if ($null -eq $base) { $base = ConvertTo-FSDateTimeOffset $t.created_at }
            if ($null -eq $base) { continue }

            $target = Get-FSvcTargetEndDate -Base $base -Days $BusinessDays -Hour $TargetHour -Zone $zone -Offset $offset -Now $now
            if (-not (Test-FSvcEndDateNeedsUpdate -PlannedEndDate $t.planned_end_date -Target $target)) { continue }
            $changes += [pscustomobject]@{
                Id    = $t.id
                Field = 'planned_end_date'
                From  = $t.planned_end_date
                To    = Format-Iso8601 $target
            }
        }

        foreach ($c in $changes) {
            $applied = $false
            if ($PSCmdlet.ShouldProcess(("ticket {0}" -f $c.Id), ("set planned_end_date to {0}" -f $c.To))) {
                Invoke-FSvcPut -Path ("tickets/{0}" -f $c.Id) -Body @{ planned_end_date = $c.To } -Config $cfg | Out-Null
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
