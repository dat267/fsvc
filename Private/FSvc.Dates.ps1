# Date and timezone helpers. Instants are absolute; wall-clock maths happens in
# the target zone/offset, so the host machine's timezone never changes results.

# Converts an API timestamp (string, DateTime or DateTimeOffset) to a
# DateTimeOffset that preserves the account's UTC offset. $null when absent or
# unparseable.
function ConvertTo-FSDateTimeOffset {
    param([AllowNull()]$Value)
    if ($null -eq $Value) { return $null }
    if ($Value -is [datetimeoffset]) { return $Value }
    if ($Value -is [datetime]) { return [datetimeoffset]$Value }
    $parsed = [datetimeoffset]::MinValue
    if ([datetimeoffset]::TryParse([string]$Value, [ref]$parsed)) { return $parsed }
    return $null
}

# Renders a DateTimeOffset as RFC 3339, using Z for a zero offset.
function Format-Iso8601 {
    param([datetimeoffset]$Value)
    if ($Value.Offset -eq [timespan]::Zero) {
        return $Value.ToString("yyyy-MM-ddTHH:mm:ss") + "Z"
    }
    return $Value.ToString("yyyy-MM-ddTHH:mm:sszzz")
}

# Adds n weekdays, skipping weekends.
function Add-FSvcBusinessDays {
    param([datetimeoffset]$Start, [int]$Days)
    $t = $Start
    $added = 0
    while ($added -lt $Days) {
        $t = $t.AddDays(1)
        if ($t.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $t.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
            $added++
        }
    }
    return $t
}

# Rounds up to the next quarter hour, keeping the timestamp's own offset.
function Round-FSvcQuarterHour {
    param([datetimeoffset]$Value)
    $total = $Value.Hour * 60 + $Value.Minute
    if ($Value.Second -ne 0 -or $Value.Millisecond -ne 0 -or ($total % 15) -ne 0) {
        $total = [math]::Floor($total / 15) * 15 + 15
    }
    $base = [datetimeoffset]::new($Value.Year, $Value.Month, $Value.Day, 0, 0, 0, $Value.Offset)
    return $base.AddMinutes($total)
}

# Business days between two instants, weekends skipped, partial days counting
# fractionally. Both are normalised to one offset so the result is host
# timezone independent.
function Get-FSvcBusinessDaysBetween {
    param([datetimeoffset]$From, [datetimeoffset]$To)
    $from = $From.ToOffset($From.Offset)
    $to = $To.ToOffset($From.Offset)
    if ($to -lt $from) { $swap = $from; $from = $to; $to = $swap }
    $start = [datetimeoffset]::new($from.Year, $from.Month, $from.Day, 0, 0, 0, $from.Offset)
    $end = [datetimeoffset]::new($to.Year, $to.Month, $to.Day, 0, 0, 0, $to.Offset)
    $full = 0.0
    for ($d = $start; $d -lt $end; $d = $d.AddDays(1)) {
        if ($d.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $d.DayOfWeek -ne [System.DayOfWeek]::Sunday) { $full++ }
    }
    $fracFrom = 0.0
    if ($from.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $from.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
        $fracFrom = ($from - $start).TotalMinutes / 1440
    }
    $fracTo = 0.0
    if ($to.DayOfWeek -ne [System.DayOfWeek]::Saturday -and $to.DayOfWeek -ne [System.DayOfWeek]::Sunday) {
        $fracTo = ($to - $end).TotalMinutes / 1440
    }
    return $full - $fracFrom + $fracTo
}

# Resolves a Windows or IANA timezone id to a TimeZoneInfo. $null when empty.
function Resolve-FSvcTimeZone {
    param([string]$Id)
    if (-not $Id) { return $null }
    try { return [System.TimeZoneInfo]::FindSystemTimeZoneById($Id) }
    catch { throw ("Unknown TimeZoneId '{0}'. Use a Windows id (e.g. 'Arabian Standard Time') or IANA id (e.g. 'Asia/Dubai')." -f $Id) }
}

# Parses a "+04:00" style UTC offset. $null when empty.
function ConvertTo-FSvcUtcOffset {
    param([string]$Value)
    if (-not $Value) { return $null }
    try { return [datetimeoffset]::Parse("2000-01-01T00:00:00" + $Value).Offset }
    catch { throw ("Invalid UtcOffset '{0}'. Use a value like '+04:00'." -f $Value) }
}

# Converts an instant to the target timezone (by id) or fixed UTC offset.
function ConvertTo-FSvcTargetZone {
    param([datetimeoffset]$Value, [AllowNull()]$Zone, [AllowNull()]$Offset)
    if ($null -ne $Zone) { return [System.TimeZoneInfo]::ConvertTime($Value, $Zone) }
    if ($null -ne $Offset) { return $Value.ToOffset([timespan]$Offset) }
    return $Value
}

# Computes a planned_end_date from a base instant: convert to the target
# zone/offset, add business days, set the hour, round up, and (when -Now is
# supplied) clamp so the result is always in the future.
function Get-FSvcTargetEndDate {
    param(
        [datetimeoffset]$Base,
        [int]$Days,
        [int]$Hour,
        [AllowNull()]$Zone,
        [AllowNull()]$Offset,
        [AllowNull()]$Now
    )
    $b = ConvertTo-FSvcTargetZone -Value $Base -Zone $Zone -Offset $Offset
    $t = Add-FSvcBusinessDays -Start $b -Days $Days
    $t = [datetimeoffset]::new($t.Year, $t.Month, $t.Day, $Hour, 0, 0, $t.Offset)
    $t = Round-FSvcQuarterHour $t

    if ($null -ne $Now) {
        $n = ConvertTo-FSvcTargetZone -Value $Now -Zone $Zone -Offset $Offset
        if ($t -le $n) {
            $slot = [datetimeoffset]::new($n.Year, $n.Month, $n.Day, $Hour, 0, 0, $n.Offset)
            if ($slot -le $n) { $slot = Add-FSvcBusinessDays -Start $slot -Days 1 }
            $t = $slot
        }
    }
    return $t
}

# True when a ticket's date differs from the computed target (instant compare,
# so equal moments in different offsets do not count as a change).
function Test-FSvcEndDateNeedsUpdate {
    param([AllowNull()]$PlannedEndDate, [datetimeoffset]$Target)
    $cur = ConvertTo-FSDateTimeOffset $PlannedEndDate
    if ($null -eq $cur) { return $true }
    return ($cur.UtcDateTime -ne $Target.UtcDateTime)
}

# True when planned_start_date is unset and created_at can supply it.
function Test-FSvcFillStart {
    param([AllowNull()][string]$PlannedStartDate, [AllowNull()][string]$CreatedAt)
    if ($PlannedStartDate) { return $false }
    if (-not $CreatedAt) { return $false }
    return $true
}
