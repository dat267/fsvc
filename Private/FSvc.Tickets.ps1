# Ticket querying and triage helpers.

# Named saved views. Defined once so the "self-assigned unresolved" and
# "unassigned unresolved" query_hash strings cannot drift between commands.
$script:FSvcViews = [ordered]@{
    SelfAssigned = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["0"],"type":"default"}]'
    Unassigned   = '[{"condition":"status","operator":"is_in","value":["0"],"type":"default"},{"condition":"responder_id","operator":"is_in","value":["-1"],"type":"default"}]'
}

# The query_hash for a named view.
function Get-FSvcViewQueryHash {
    param([Parameter(Mandatory)][ValidateSet('SelfAssigned', 'Unassigned')][string]$View)
    return $script:FSvcViews[$View]
}

# Ticket selection for a command: a raw query hash when given, otherwise a
# named saved view. Owns the view -> query_hash mapping and the paging, so
# callers only say which tickets they want.
function Get-FSvcTargetTickets {
    param(
        [ValidateSet('SelfAssigned', 'Unassigned')][string]$View = 'SelfAssigned',
        [string]$QueryHash,
        [int]$PerPage = 100,
        [int]$MaxPages = 1000,
        [hashtable]$Config
    )
    $hash = if ($QueryHash) { $QueryHash } else { Get-FSvcViewQueryHash -View $View }
    return Invoke-FSvcPagedQuery -Path "tickets" -ArrayKey "tickets" -MaxPages $MaxPages -Config $Config -BaseQuery @{
        "order_by"   = "created_at"
        "order_type" = "asc"
        "per_page"   = $PerPage
        "query_hash" = $hash
    }
}

# One page of a ticket's conversations, newest first, normalised to the shared
# view shape. HasNext reports whether the API holds more pages.
function Get-FSvcConversationPage {
    param([int64]$TicketId, [int]$Page = 1, [int]$PerPage = 50, [hashtable]$Config)
    $query = @{
        "order_by"   = "created_at"
        "order_type" = "desc"
        "per_page"   = $PerPage
        "page"       = $Page
    }
    $data = (Invoke-FSvcGet -Path ("tickets/{0}/conversations" -f $TicketId) -Query $query -Config $Config) | ConvertFrom-FSvcJson
    $items = @(@($data.conversations) | Where-Object { $null -ne $_ } | ForEach-Object { ConvertTo-FSvcConversationView $_ })
    return [pscustomobject]@{
        Items   = $items
        HasNext = [bool]$data.meta.has_next
    }
}

# Most recent conversation for a ticket - any kind, private note or public
# reply - or $null when it has none.
function Get-FSvcLatestConversation {
    param([int64]$TicketId, [hashtable]$Config)
    $page = Get-FSvcConversationPage -TicketId $TicketId -Page 1 -PerPage 1 -Config $Config
    return (@($page.Items) | Select-Object -First 1)
}

# Leading run of incoming (customer) messages before the first outgoing one.
function Get-FSvcUnansweredCount {
    param([AllowEmptyCollection()][object[]]$Conversations)
    $count = 0
    foreach ($c in $Conversations) {
        if ($c.Direction -eq 'incoming') { $count++ } else { break }
    }
    return $count
}

# Newest-first conversation views for a ticket, plus how many customer messages
# the agent has not answered yet (the consecutive incoming run at the tail).
# Pages only while a whole page is unanswered, so an answered thread costs one
# request; MaxPages caps a pathological all-customer thread.
function Get-FSvcTicketThread {
    param([int64]$TicketId, [hashtable]$Config, [int]$PerPage = 50, [int]$MaxPages = 5)
    $items = @()
    $unanswered = 0
    $pageNumber = 1
    while ($true) {
        $page = Get-FSvcConversationPage -TicketId $TicketId -Page $pageNumber -PerPage $PerPage -Config $Config
        $pageItems = @($page.Items)
        $items += $pageItems
        $incoming = Get-FSvcUnansweredCount -Conversations $pageItems
        $unanswered += $incoming
        if ($incoming -lt $pageItems.Count) { break }
        if (-not $page.HasNext) { break }
        if ($pageNumber -ge $MaxPages) { break }
        $pageNumber++
    }
    return [pscustomobject]@{
        Items      = $items
        Latest     = (@($items) | Select-Object -First 1)
        Unanswered = $unanswered
    }
}

# Normalises a raw API conversation (or the latest-conversation response) into
# the single shape consumers use: Id, Author (nested user name else user_id),
# UserId, Direction, At, Body (body_text preferred over body).
function ConvertTo-FSvcConversationView {
    param([AllowNull()]$Conversation)
    if ($null -eq $Conversation) { return $null }
    $author = if ($Conversation.user -and $Conversation.user.name) { [string]$Conversation.user.name } else { [string]$Conversation.user_id }
    $direction = if ($Conversation.incoming) { 'incoming' } else { 'outgoing' }
    $body = if ($Conversation.body_text) { [string]$Conversation.body_text } elseif ($Conversation.body) { [string]$Conversation.body } else { '' }
    return [pscustomobject]@{
        Id        = $Conversation.id
        Author    = $author
        UserId    = [int64]$Conversation.user_id
        Direction = $direction
        At        = ConvertTo-FSDateTimeOffset $Conversation.created_at
        Body      = $body
    }
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

# Computes the planned_end_date a ticket should have, or $null when it should be
# left alone. The base is the latest conversation timestamp (any kind) when
# present, else created_at. The target is base + N business days at the target
# hour in the target zone/offset, clamped so it is never in the past. Returns
# $null when there is no base date, or when the ticket already holds that exact
# instant (so re-running is a no-op).
function Get-FSvcPlannedEndDate {
    param(
        [Parameter(Mandatory)]$Ticket,
        [AllowNull()]$LatestConversationAt,
        [datetimeoffset]$Now,
        [int]$BusinessDays = 3,
        [int]$TargetHour = 17,
        [AllowNull()]$Offset
    )
    $base = ConvertTo-FSDateTimeOffset $LatestConversationAt
    if ($null -eq $base) { $base = ConvertTo-FSDateTimeOffset $Ticket.created_at }
    if ($null -eq $base) { return $null }

    $target = Get-FSvcTargetEndDate -Base $base -Days $BusinessDays -Hour $TargetHour -Offset $Offset -Now $Now
    if (-not (Test-FSvcEndDateNeedsUpdate -PlannedEndDate $Ticket.planned_end_date -Target $target)) { return $null }
    return $target
}

# Orders overview rows: groups in report order (unassigned, waiting,
# awaiting_agent), and within a group by Days descending (longest-waiting
# first). Returns a new array; the input is not mutated.
function Sort-FSvcOverviewRows {
    param([AllowEmptyCollection()][object[]]$Rows)
    if (-not $Rows) { return @() }
    $rank = @{ 'unassigned' = 0; 'waiting' = 1; 'awaiting_agent' = 2 }
    return @($Rows | Sort-Object -Property @{ Expression = { if ($rank.ContainsKey($_.Category)) { $rank[$_.Category] } else { 99 } } }, @{ Expression = { [double]$_.Days }; Descending = $true })
}

# Humanizes a business-day count. At a day or more the display is "Nd Nh";
# below a day it keeps minute resolution ("1h 30m", "20m"), because sub-day
# ages are the ones where hours alone are misleading. Callers should pass the
# unrounded business-day value (the numeric Days property is rounded to 1 dp).
function Format-FSvcDuration {
    param([double]$Days)
    if ($Days -lt 0) { $Days = 0 }
    $minutes = [int][math]::Round($Days * 1440, 0, [System.MidpointRounding]::AwayFromZero)
    $days = [int][math]::Floor($minutes / 1440)
    $rem = $minutes - ($days * 1440)
    $hours = [int][math]::Floor($rem / 60)
    $mins = $rem - ($hours * 60)
    if ($days -gt 0) {
        if ($hours -gt 0) { return ('{0}d {1}h' -f $days, $hours) }
        return ('{0}d' -f $days)
    }
    if ($hours -gt 0) {
        if ($mins -gt 0) { return ('{0}h {1}m' -f $hours, $mins) }
        return ('{0}h' -f $hours)
    }
    return ('{0}m' -f $mins)
}

# Builds one overview row. RawDays is the unrounded business-day count: the
# numeric Days property is rounded to one decimal (stable for sorting), while
# Elapsed keeps sub-day resolution.
function New-FSvcOverviewRow {
    param(
        [string]$Category,
        [object]$Ticket,
        [double]$RawDays,
        [AllowNull()]$Since,
        [AllowNull()]$Unanswered,
        [string]$BaseUrl
    )
    return [pscustomobject]@{
        PSTypeName = 'FSvc.TicketOverviewRow'
        Category   = $Category
        Id         = $Ticket.id
        Subject    = $Ticket.subject
        Days       = [math]::Round($RawDays, 1)
        Elapsed    = Format-FSvcDuration -Days $RawDays
        Since      = $Since
        Unanswered = $Unanswered
        Link       = ("{0}/a/tickets/{1}" -f $BaseUrl, $Ticket.id)
    }
}

# Triage decision for one ticket: which bucket it belongs to and the timestamp
# its clock runs from (the anchor Since/Elapsed are measured from). Returns
# $null when the ticket needs no attention. Owning bucket and anchor together
# is the point: they must never disagree.
function Get-FSvcTriage {
    param(
        [Parameter(Mandatory)]$Ticket,
        [AllowNull()]$LatestConversation,
        [double]$OlderThanDays,
        [datetimeoffset]$Now
    )
    $responderId = $Ticket.responder_id
    if ($null -eq $responderId -or [int64]$responderId -lt 0) {
        $created = ConvertTo-FSDateTimeOffset $Ticket.created_at
        $days = 0.0
        if ($null -ne $created) { $days = Get-FSvcBusinessDaysBetween -From $created -To $Now }
        return [pscustomobject]@{
            Category = 'unassigned'
            Since    = $created
            Days     = $days
        }
    }
    $lastAt = $null
    $lastUser = [int64]0
    if ($null -ne $LatestConversation) {
        $lastAt = $LatestConversation.At
        $lastUser = [int64]$LatestConversation.UserId
    }
    if ($null -ne $lastAt -and $lastUser -ne [int64]$responderId) {
        return [pscustomobject]@{
            Category = 'awaiting_agent'
            Since    = $lastAt
            Days     = (Get-FSvcBusinessDaysBetween -From $lastAt -To $Now)
        }
    }
    $ref = $lastAt
    if ($null -eq $ref) { $ref = ConvertTo-FSDateTimeOffset $Ticket.created_at }
    if ($null -ne $ref) {
        $days = Get-FSvcBusinessDaysBetween -From $ref -To $Now
        if ($days -gt $OlderThanDays) {
            return [pscustomobject]@{
                Category = 'waiting'
                Since    = $ref
                Days     = $days
            }
        }
    }
    return $null
}
