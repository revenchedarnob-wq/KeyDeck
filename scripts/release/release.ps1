param(
  [string]$Version = "v0.35.0-RECONSTRUCTED",
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [string]$OutDir = (Join-Path $RepoRoot "_out\release")
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
& (Join-Path $RepoRoot "scripts\ci\secret-scan.ps1") -RepoRoot $RepoRoot
& (Join-Path $RepoRoot "scripts\proof\build-proof-registry.ps1") -RepoRoot $RepoRoot
$zip = Join-Path $OutDir ("KeyDeck-source-$Version.zip")
if (Test-Path $zip) { Remove-Item -LiteralPath $zip -Force }
git archive --format zip --output $zip HEAD
& (Join-Path $RepoRoot "scripts\artifacts\new-manifest.ps1") -InputPath $OutDir -OutputCsv (Join-Path $OutDir "RELEASE-MANIFEST.csv")
Write-Host "Release artifacts staged under $OutDir"
