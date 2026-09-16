@{
    RootModule           = 'fsvc.psm1'
    ModuleVersion        = '0.0.7'
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
            ReleaseNotes = '0.0.7 - Get-FSvcTicketOverview leaves unassigned tickets out of the report by default (one fewer view fetch); pass -IncludeUnassigned to include them again. 0.0.6 - default list view without Id (Id stays a property) showing the full, untruncated Link; rows grouped in report order with Days descending inside each group; Elapsed (humanized business duration such as 13d 14h, with minute resolution below a day) and Unanswered (customer messages the agent has not answered); Since renders as RFC 3339 with the account UTC offset; HTTP calls reuse one session per base URL with the auth cookie in the session jar, identical on PowerShell 5.1 and 7 (about 20 percent faster per request); the numeric Days property remains for sorting; packaging ships fsvc.format.ps1xml. 0.0.5 - ItildeskSession replaces SessionCookie (old config key still read); non-ASCII session values are rejected with the offending codepoint; HTTP errors are terminating; FSVC_* env vars override the saved config file; Set-FSvcConfig warns when called with no settings; TimeZoneId removed in favour of UtcOffset. 0.0.4 - empty-call warning and env precedence. 0.0.3 - persisted config moved to a JSON file. 0.0.2 - profile persistence. 0.0.1 - initial release.'
        }
    }
}
