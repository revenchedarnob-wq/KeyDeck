[CmdletBinding()]
param(
    [Parameter(Mandatory=$true)][ValidatePattern('^v\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$')][string]$Version,
    [switch]$DryRun,
    [switch]$Publish,
    [switch]$AllowBranch,
    [switch]$SkipDeep
)
$ErrorActionPreference = "Stop"
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $repoRoot
$branch = (git branch --show-current).Trim()
$status = (git status --short | Out-String).Trim()
if ($status) { throw "release requires a clean working tree" }
if (-not $AllowBranch -and $branch -ne "main") { throw "release must run on main unless -AllowBranch is supplied" }
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "Go is required for release builds" }
& .\scripts\ci\fast.ps1
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
if (-not $SkipDeep) { & .\scripts\ci\deep.ps1; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }
& .\scripts\ci\secret-scan.ps1
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$stage = Join-Path "release-staging" $Version
if (Test-Path $stage) { throw "release staging already exists: $stage" }
New-Item -ItemType Directory -Force -Path $stage | Out-Null
$binDir = Join-Path $stage "windows-x64"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$env:GOOS = "windows"; $env:GOARCH = "amd64"
foreach ($cmd in @("keydeck-core", "keydeck-desktop-shell", "keydeck-desktop-ui", "proof38")) {
    go build -trimpath -o (Join-Path $binDir "$cmd.exe") "./cmd/$cmd"
    if ($LASTEXITCODE -ne 0) { throw "go build ./cmd/$cmd failed" }
}
Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $stage -Recurse -File | Sort-Object FullName | ForEach-Object {
    "$((Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash)  $([System.IO.Path]::GetRelativePath($stage, $_.FullName) -replace '\\','/')"
} | Set-Content -LiteralPath (Join-Path $stage "SHA256SUMS.txt") -Encoding ASCII
$zipPath = Join-Path "release-staging" "KeyDeck-$Version-windows-x64.zip"
Compress-Archive -LiteralPath $binDir -DestinationPath $zipPath -CompressionLevel Optimal
$manifest = [ordered]@{
    timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
    version = $Version
    branch = $branch
    commit = (git rev-parse HEAD).Trim()
    dry_run = [bool]$DryRun
    publish = [bool]$Publish
    assets = @(
        [ordered]@{ name = [System.IO.Path]::GetFileName($zipPath); path = $zipPath; bytes = (Get-Item $zipPath).Length; sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $zipPath).Hash },
        [ordered]@{ name = "SHA256SUMS.txt"; path = (Join-Path $stage "SHA256SUMS.txt"); bytes = (Get-Item (Join-Path $stage "SHA256SUMS.txt")).Length; sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $stage "SHA256SUMS.txt")).Hash }
    )
}
$manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath (Join-Path $stage "RELEASE-MANIFEST.json") -Encoding UTF8
$manifest | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath "receipts/release-receipt-$Version.json" -Encoding UTF8
if ($Publish) {
    if ($DryRun) { throw "cannot publish in dry-run mode" }
    gh release view $Version *> $null
    if ($LASTEXITCODE -eq 0) { throw "release $Version already exists" }
    gh release create $Version $zipPath (Join-Path $stage "SHA256SUMS.txt") --title "KeyDeck $Version" --notes "Automated KeyDeck release package for $Version."
}
Write-Host "Release pipeline completed for $Version (dry-run=$DryRun publish=$Publish)"
