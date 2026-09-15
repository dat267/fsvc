@{
    RootModule           = 'fsvc.psm1'
    ModuleVersion        = '0.0.6'
    GUID                 = 'a2aea9f2-df4e-4762-a982-b6fa9be5b5e4'
    Author               = 'dat267'
    Copyright            = '(c) dat267. All rights reserved.'
    Description          = 'Freshservice private-API toolkit: ticket triage, ticket content, and planned-date hygiene.'
    PowerShellVersion    = '5.1'
    CompatiblePSEditions = @('Desktop', 'Core')
    FormatsToProcess     = @('fsvc.format.ps1xml')
    FunctionsToExport    = @(
        'Get-FSvcConfig',
        'Set-FSvcConfig',
        'Test-FSvcSession',
        'Get-FSvcTicketList',
        'Get-FSvcTicketContent',
        'Format-FSvcTicketContent',
        'Get-FSvcTicketOverview',
        'Set-FSvcPlannedStartDates',
        'Update-FSvcPlannedEndDates'
    )
    CmdletsToExport      = @()
    VariablesToExport    = @()
    AliasesToExport      = @()
    PrivateData          = @{
        PSData = @{
            Tags         = @('Freshservice', 'Helpdesk', 'Tickets', 'API', 'PrivateAPI')
            ProjectUri   = 'https://github.com/dat267/fsvc'
            LicenseUri   = 'https://github.com/dat267/fsvc/blob/main/LICENSE'
            ReleaseNotes = '0.0.6 - overview rows get a default list view (no Id, full Link) and are grouped by category with Days descending within each group. 0.0.5 - ItildeskSession replaces SessionCookie (old config key still read); non-ASCII session values are rejected with the offending codepoint; HTTP errors are terminating; FSVC_* env vars override the saved config file; Set-FSvcConfig warns when called with no settings; TimeZoneId removed in favour of UtcOffset. 0.0.4 - empty-call warning and env precedence. 0.0.3 - persisted config moved to a JSON file. 0.0.2 - profile persistence. 0.0.1 - initial release.'
        }
    }
}
