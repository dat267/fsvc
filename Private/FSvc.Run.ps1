# Scheduled-run helpers: a lock file so overlapping runs cannot double-apply,
# and optional transcript logging.

function Enter-FSvcRunLock {
    param([string]$Path, [int]$StaleMinutes = 240)
    for ($attempt = 0; $attempt -lt 2; $attempt++) {
        try {
            return [System.IO.File]::Open($Path, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        } catch {
            try {
                $age = (Get-Date) - (Get-Item -LiteralPath $Path -ErrorAction Stop).LastWriteTime
                if ($age.TotalMinutes -ge $StaleMinutes) {
                    Remove-Item -LiteralPath $Path -Force -ErrorAction Stop -WhatIf:$false
                    continue
                }
            } catch { }
            return $null
        }
    }
    return $null
}

function Exit-FSvcRunLock {
    param([AllowNull()]$Handle, [string]$Path)
    if ($null -ne $Handle) { try { $Handle.Close() } catch { } }
    try { if (Test-Path -LiteralPath $Path) { Remove-Item -LiteralPath $Path -Force -WhatIf:$false } } catch { }
}

function Start-FSvcLogging {
    param([string]$Path)
    if (-not $Path) { return $false }
    try {
        Start-Transcript -Path $Path -Append | Out-Null
        return $true
    } catch {
        Write-Warning ("Could not start transcript at {0}: {1}" -f $Path, $_.Exception.Message)
        return $false
    }
}

function Stop-FSvcLogging {
    param([bool]$Active)
    if ($Active) { try { Stop-Transcript | Out-Null } catch { } }
}
