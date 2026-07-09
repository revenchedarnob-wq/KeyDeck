# Automation Validation Report

## Summary

Validation was run on `automation/time-saver-bootstrap-20260710` in `A:\KeyDeck-Workspace\repo`.

## Passed locally

- `scripts/proof/build-proof-registry.ps1` generated `docs/PROOF_REGISTRY.json` and `docs/PROOF_REGISTRY.md`.
- `scripts/artifacts/new-manifest.ps1 -Path docs -OutputPath receipts/docs-manifest.csv` completed.
- `scripts/ci/secret-scan.ps1` scanned tracked files and reported no credible live secrets after excluding binary artifacts and narrow fake/test fixture literals.
- `scripts/ci/fast.ps1 -SkipGo` passed.
- `scripts/ci/deep.ps1 -SkipGo` passed.

## Blocked locally

- Full `bootstrap-dev.ps1`, normal `fast.ps1`, normal `deep.ps1`, and release dry-run require Go. `go` is not currently available on PATH in the local environment.
- `scripts/drive/upload-drive-archive.ps1 -DryRun` requires `rclone`. `rclone` is not currently available on PATH.

## Classification

- No bootstrap-caused Go test failure was observed locally because the Go toolchain is unavailable.
- No bootstrap-caused Drive upload failure was observed locally because rclone is unavailable.
- The PowerShell-only automation bugs found during validation were fixed before this report was written.
- The remaining failures are environment/tooling limitations, not proven repository regressions.

## Receipts

Local receipts are written under `receipts/`, which is intentionally ignored by Git.