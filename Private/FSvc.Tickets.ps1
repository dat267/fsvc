# Ticket querying and triage helpers.

# Paginates a tickets query, returning every ticket as an object.
function Get-FSvcTickets {
    param(
        [string]$QueryHash,
        [int]$PerPage = 100,
        [int]$MaxPages = 1000,
        [hashtable]$Config
    )
    return Invoke-FSvcPagedQuery -Path "tickets" -ArrayKey "tickets" -MaxPages $MaxPages -Config $Config -BaseQuery @{
        "order_by"   = "created_at"
        "order_type" = "asc"
        "per_page"   = $PerPage
        "query_hash" = $QueryHash
    }
}

# Most recent conversation for a ticket - any kind, private note or public
# reply - or $null when it has none.
function Get-FSvcLatestConversation {
    param([int64]$TicketId, [hashtable]$Config)
    $query = @{
        "order_by"   = "created_at"
        "order_type" = "desc"
        "per_page"   = 1
        "page"       = 1
    }
    $data = (Invoke-FSvcGet -Path ("tickets/{0}/conversations" -f $TicketId) -Query $query -Config $Config) | ConvertFrom-FSvcJson
    $c = @($data.conversations) | Select-Object -First 1
    if ($null -eq $c) { return $null }
    return [pscustomobject]@{
        Id        = $c.id
        CreatedAt = ConvertTo-FSDateTimeOffset $c.created_at
        UserID    = [int64]$c.user_id
        Body      = $c.body_text
        Incoming  = [bool]$c.incoming
    }
}

# Triage bucket for a self-assigned ticket: unassigned / awaiting_agent /
# waiting / none. Mirrors the standalone overview logic.
function Get-FSvcTicketCategory {
    param(
        [AllowNull()]$ResponderID,
        [AllowNull()]$LastMessage,
        [int64]$LastUserID,
        [datetimeoffset]$CreatedAt,
        [double]$OlderThanDays,
        [datetimeoffset]$Now
    )
    if ($null -eq $ResponderID -or [int64]$ResponderID -lt 0) { return "unassigned" }
    $last = ConvertTo-FSDateTimeOffset $LastMessage
    if ($null -ne $last -and $LastUserID -ne [int64]$ResponderID) { return "awaiting_agent" }
    $ref = $CreatedAt
    if ($null -ne $last) { $ref = $last }
    if ((Get-FSvcBusinessDaysBetween -From $ref -To $Now) -gt $OlderThanDays) { return "waiting" }
    return "none"
}

# The UTC offset evidenced by the tickets' own dates (planned_end_date
# preferred, created_at fallback), or the fallback's offset.
function Get-FSvcAccountOffset {
    param($Tickets, [datetimeoffset]$Fallback)
    foreach ($t in @($Tickets)) {
        $d = ConvertTo-FSDateTimeOffset $t.planned_end_date
        if ($null -ne $d) { return $d.Offset }
    }
    foreach ($t in @($Tickets)) {
        $d = ConvertTo-FSDateTimeOffset $t.created_at
        if ($null -ne $d) { return $d.Offset }
    }
    return $Fallback.Offset
}

# Renders a business-days number with one decimal, invariant culture.
function Format-FSvcDays {
    param([double]$Days)
    $rounded = [math]::Round($Days, 1, [System.MidpointRounding]::AwayFromZero)
    return $rounded.ToString("0.0", [System.Globalization.CultureInfo]::InvariantCulture)
}
