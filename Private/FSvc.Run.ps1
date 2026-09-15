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

# Runs a set of ticket changes as one guarded batch: acquires the run lock,
# optionally starts a transcript, asks the injected decision scriptblock (which
# the caller binds to $PSCmdlet.ShouldProcess) per change, applies approved
# changes, tags each with Applied, and always releases lock/logging.
#
# -Change : objects with at least Id, Field, From, To
# -Apply  : scriptblock run per approved change, receives the change
# -Should : scriptblock (Target, Action) -> [bool]; defaults to always apply
function Invoke-FSvcChangeSet {
    param(
        [AllowEmptyCollection()][object[]]$Change,
        [Parameter(Mandatory)][scriptblock]$Apply,
        [scriptblock]$Should = { param($Target, $Action) $true },
        [hashtable]$Config,
        [string]$LockName = 'fsvc-change-set',
        [string]$LogPath
    )

    if (-not $Change -or $Change.Count -eq 0) { return }

    $lockPath = Join-Path ([System.IO.Path]::GetTempPath()) ($LockName + '.lock')
    $lock = Enter-FSvcRunLock -Path $lockPath
    if ($null -eq $lock) {
        throw "Another fsvc run is in progress (lock: $lockPath). Delete it if stale."
    }
    $logging = Start-FSvcLogging -Path $LogPath
    try {
        foreach ($c in $Change) {
            $applied = $false
            $target = "ticket {0}" -f $c.Id
            $action = "set {0} to {1}" -f $c.Field, $c.To
            if (& $Should $target $action) {
                $null = & $Apply $c
                $applied = $true
            }
            $c | Add-Member -NotePropertyName Applied -NotePropertyValue $applied -Force
            $c
        }
    } finally {
        Stop-FSvcLogging -Active $logging
        Exit-FSvcRunLock -Handle $lock -Path $lockPath
    }
}
