@{
    RootModule           = 'fsvc.psm1'
    ModuleVersion        = '0.0.4'
    GUID                 = 'a2aea9f2-df4e-4762-a982-b6fa9be5b5e4'
    Author               = 'dat267'
    Copyright            = '(c) dat267. All rights reserved.'
    Description          = 'Freshservice private-API toolkit: ticket triage, ticket content, and planned-date hygiene.'
    PowerShellVersion    = '5.1'
    CompatiblePSEditions = @('Desktop', 'Core')
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
            ReleaseNotes = '0.0.4 - Set-FSvcConfig warns with no settings; FSVC_* env vars now override the saved config file. 0.0.3 - persisted config moved to a JSON file (%LOCALAPPDATA% or ~/.config). 0.0.2 - profile persistence. 0.0.1 - initial release.'
        }
    }
}
