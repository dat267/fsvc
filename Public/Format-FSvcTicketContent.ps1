function Format-FSvcTicketContent {
    <#
    .SYNOPSIS
        Renders the object from Get-FSvcTicketContent as readable text.
    .EXAMPLE
        Get-FSvcTicketContent -Id 10100 | Format-FSvcTicketContent
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory, ValueFromPipeline)]$InputObject
    )
    process {
        $ticket = $InputObject.Ticket
        $conversations = @($InputObject.Conversations)
        $lines = [System.Collections.Generic.List[string]]::new()

        $display = if ($ticket.display_id) { $ticket.display_id } else { $ticket.id }
        $lines.Add(("Ticket #{0} - {1}" -f $display, $ticket.subject))
        $lines.Add("")
        foreach ($f in @(
            @{ Label = 'Status'; Key = 'status' },
            @{ Label = 'Priority'; Key = 'priority' },
            @{ Label = 'Urgency'; Key = 'urgency' },
            @{ Label = 'Impact'; Key = 'impact' },
            @{ Label = 'Group'; Key = 'group' },
            @{ Label = 'Requester'; Key = 'requester' },
            @{ Label = 'Responder'; Key = 'responder' },
            @{ Label = 'Department'; Key = 'department' },
            @{ Label = 'Created'; Key = 'created_at' },
            @{ Label = 'Updated'; Key = 'updated_at' }
        )) {
            $value = $null
            foreach ($suffix in @('_name', '_id', '')) {
                $k = $f.Key + $suffix
                if ($ticket.PSObject.Properties.Name -contains $k -and "$($ticket.$k)" -ne "") { $value = "$($ticket.$k)"; break }
            }
            $lines.Add(("{0,-10} : {1}" -f $f.Label, $value))
        }
        $lines.Add("")
        $desc = $ticket.description_text
        if ($desc) { $lines.Add($desc); $lines.Add("") }

        $lines.Add(("Conversations ({0})" -f $conversations.Count))
        $lines.Add("")
        $n = 0
        foreach ($c in $conversations) {
            $n++
            $author = if ($c.user -and $c.user.name) { $c.user.name } else { $c.user_id }
            $dir = if ($c.incoming) { 'incoming' } else { 'outgoing' }
            $lines.Add(("--- [{0}] {1} ({2}, {3})" -f $n, $author, $dir, $c.created_at))
            $body = if ($c.body_text) { $c.body_text } else { $c.body }
            if ($body) { $lines.Add("$body") } else { $lines.Add("(no body)") }
            $lines.Add("")
        }
        ($lines -join "`n")
    }
}
