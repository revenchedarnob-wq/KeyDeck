param(
  [Parameter(Mandatory=$true)][string]$InputPath,
  [string]$OutputCsv = "STAGING-MANIFEST.csv"
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$root = (Resolve-Path $InputPath).Path
$rows = Get-ChildItem -LiteralPath $root -Recurse -File | Sort-Object FullName | ForEach-Object {
  [pscustomobject]@{
    relative_path = [IO.Path]::GetRelativePath($root, $_.FullName)
    size_bytes = $_.Length
    sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()
  }
}
$rows | Export-Csv -LiteralPath $OutputCsv -NoTypeInformation -Encoding UTF8
Write-Host "Wrote manifest: $OutputCsv"
