[CmdletBinding()]
param([Parameter(Position=0)][string]$Command = "help", [Parameter(Position=1)][string]$Argument)
$ErrorActionPreference = "Stop"
$repoRoot = $PSScriptRoot
Set-Location $repoRoot
New-Item -ItemType Directory -Force -Path "logs", "receipts", "local-only" | Out-Null
function Help {
@"
KeyDeck command center

Commands:
  .\KEYDECK.ps1 status
  .\KEYDECK.ps1 update
  .\KEYDECK.ps1 test
  .\KEYDECK.ps1 test-deep
  .\KEYDECK.ps1 proof-registry
  .\KEYDECK.ps1 manifest <path>
  .\KEYDECK.ps1 release-dry-run <version>
  .\KEYDECK.ps1 drive-dry-run
  .\KEYDECK.ps1 drive-upload
"@ | Write-Host
}
switch ($Command) {
    "help" { Help }
    "status" { git status -sb; git remote -v; git log --oneline -5 }
    "update" { git fetch origin; git switch main; git pull --ff-only }
    "test" { & .\scripts\ci\fast.ps1 }
    "test-deep" { & .\scripts\ci\deep.ps1 }
    "proof-registry" { & .\scripts\proof\build-proof-registry.ps1 }
    "manifest" { if (-not $Argument) { throw "manifest requires a path" }; & .\scripts\artifacts\new-manifest.ps1 -Path $Argument -OutputPath "receipts/artifact-manifest.csv" }
    "release-dry-run" { if (-not $Argument) { throw "release-dry-run requires a version" }; & .\scripts\release\release.ps1 -Version $Argument -DryRun -AllowBranch }
    "drive-dry-run" { & .\scripts\drive\upload-drive-archive.ps1 -DryRun }
    "drive-upload" { & .\scripts\drive\upload-drive-archive.ps1 }
    default { Help; exit 2 }
}
