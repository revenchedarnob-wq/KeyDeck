param(
  [string]$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path,
  [switch]$Deep
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
Set-Location $RepoRoot
if (-not (Test-Path "go.mod")) { throw "go.mod not found; run from the canonical KeyDeck repository clone." }
Write-Host "KEYDECK bootstrap: repo=$RepoRoot"
go version
& (Join-Path $RepoRoot "scripts\proof\build-proof-registry.ps1") -RepoRoot $RepoRoot
if ($Deep) {
  & (Join-Path $RepoRoot "scripts\ci\deep.ps1") -RepoRoot $RepoRoot
} else {
  & (Join-Path $RepoRoot "scripts\ci\fast.ps1") -RepoRoot $RepoRoot
}
