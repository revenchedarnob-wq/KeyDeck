param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [switch]$SkipProofReplay
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
& (Join-Path $PSScriptRoot "secret-scan.ps1") -RepoRoot $RepoRoot
& (Join-Path $PSScriptRoot "fast.ps1") -RepoRoot $RepoRoot
if (-not $SkipProofReplay) {
  $proofDirs = Get-ChildItem -Path (Join-Path $RepoRoot "cmd") -Directory | Where-Object { $_.Name -match '^proof\d\d$' } | Sort-Object Name
  foreach ($dir in $proofDirs) {
    Write-Host "Replaying $($dir.Name)"
    go run "./cmd/$($dir.Name)"
  }
}
Write-Host "KEYDECK deep CI: PASS"
