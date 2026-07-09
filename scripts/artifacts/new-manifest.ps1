[CmdletBinding()]
param([Parameter(Mandatory=$true)][string]$Path, [string]$OutputPath = "artifact-manifest.csv", [string]$Category = "general")
$ErrorActionPreference = "Stop"
$root = (Resolve-Path -LiteralPath $Path).Path
$rows = Get-ChildItem -LiteralPath $root -Recurse -File -Force | Sort-Object FullName | ForEach-Object {
    [pscustomobject]@{
        relative_path = [System.IO.Path]::GetRelativePath($root, $_.FullName)
        bytes = $_.Length
        sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash
        creation_time_utc = $_.CreationTimeUtc.ToString("o")
        category = $Category
    }
}
$parent = Split-Path -Parent $OutputPath
if ($parent) { New-Item -ItemType Directory -Force -Path $parent | Out-Null }
$rows | Export-Csv -LiteralPath $OutputPath -NoTypeInformation -Encoding UTF8
Write-Host "Manifest written: $OutputPath ($($rows.Count) files)"
