param(
  [ValidateSet("status", "bootstrap", "fast", "deep", "proof-registry", "manifest")]
  [string]$Command = "status",
  [string]$Path = ".",
  [string]$Out = "STAGING-MANIFEST.csv"
)
$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$RepoRoot = $PSScriptRoot
switch ($Command) {
  "status" {
    Set-Location $RepoRoot
    git status --short
    git rev-parse --abbrev-ref HEAD
    git rev-parse HEAD
  }
  "bootstrap" { & (Join-Path $RepoRoot "scripts\bootstrap\bootstrap-dev.ps1") -RepoRoot $RepoRoot }
  "fast" { & (Join-Path $RepoRoot "scripts\ci\fast.ps1") -RepoRoot $RepoRoot }
  "deep" { & (Join-Path $RepoRoot "scripts\ci\deep.ps1") -RepoRoot $RepoRoot }
  "proof-registry" { & (Join-Path $RepoRoot "scripts\proof\build-proof-registry.ps1") -RepoRoot $RepoRoot }
  "manifest" { & (Join-Path $RepoRoot "scripts\artifacts\new-manifest.ps1") -InputPath $Path -OutputCsv $Out }
}
