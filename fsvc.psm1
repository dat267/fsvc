# fsvc module loader.
# Private helpers are dot-sourced first; public commands are dot-sourced and
# then exported. Tests dot-source the same files directly, so behaviour is
# identical inside and outside the module.

$privateDir = Join-Path $PSScriptRoot 'Private'
if (Test-Path -LiteralPath $privateDir) {
    Get-ChildItem -LiteralPath $privateDir -Filter '*.ps1' -File | Sort-Object Name | ForEach-Object { . $_.FullName }
}

$publicDir = Join-Path $PSScriptRoot 'Public'
$publicFunctions = @()
if (Test-Path -LiteralPath $publicDir) {
    $publicFiles = Get-ChildItem -LiteralPath $publicDir -Filter '*.ps1' -File | Sort-Object Name
    foreach ($file in $publicFiles) {
        . $file.FullName
        $publicFunctions += [System.IO.Path]::GetFileNameWithoutExtension($file.Name)
    }
}

Export-ModuleMember -Function $publicFunctions
