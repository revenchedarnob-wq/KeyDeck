[CmdletBinding()]
param(
    [string]$Source = "C:\Users\Arnob\Desktop\KeyDeck-Drive-Upload-Staging",
    [string]$Destination = "keydeck-drive:KeyDeck",
    [switch]$DryRun,
    [string]$ReceiptPath = "receipts/DRIVE-UPLOAD-RECEIPT.json"
)
$ErrorActionPreference = "Stop"
if (-not (Get-Command rclone -ErrorAction SilentlyContinue)) { throw "rclone is required and must not store credentials in this repository" }
if (-not (Test-Path -LiteralPath $Source -PathType Container)) { throw "source directory not found: $Source" }
$manifest = Join-Path $Source "STAGING-MANIFEST.csv"
if (-not (Test-Path -LiteralPath $manifest -PathType Leaf)) { throw "STAGING-MANIFEST.csv is required in source directory" }
$files = Get-ChildItem -LiteralPath $Source -Recurse -File -Force
$totalBytes = ($files | Measure-Object -Property Length -Sum).Sum
$modeArgs = @("copy", $Source, $Destination, "--progress", "--transfers", "4", "--checkers", "8", "--retries", "3", "--low-level-retries", "10")
if ($DryRun) { $modeArgs += "--dry-run" }
$rcloneVersion = (rclone version | Select-Object -First 1)
rclone @modeArgs
$success = ($LASTEXITCODE -eq 0)
$verifyResult = if ($DryRun) { "dry-run only" } elseif ($success) { (rclone check $Source $Destination --one-way --size-only 2>&1 | Out-String).Trim() } else { "copy failed" }
$receiptDir = Split-Path -Parent $ReceiptPath
if ($receiptDir) { New-Item -ItemType Directory -Force -Path $receiptDir | Out-Null }
$receipt = [ordered]@{
    timestamp_utc = (Get-Date).ToUniversalTime().ToString("o")
    source_directory = $Source
    destination = $Destination
    local_file_count = $files.Count
    local_bytes = [int64]$totalBytes
    staging_manifest_sha256 = (Get-FileHash -Algorithm SHA256 -LiteralPath $manifest).Hash
    rclone_version = $rcloneVersion
    command_mode = if ($DryRun) { "dry-run copy" } else { "copy" }
    success = $success
    verification_result = $verifyResult
}
$receipt | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $ReceiptPath -Encoding UTF8
if (-not $success) { exit 1 }
Write-Host "Drive archive command completed: $Destination"
