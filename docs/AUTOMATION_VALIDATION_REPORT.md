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
## GitHub Actions result

PR #2 was opened at `https://github.com/revenchedarnob-wq/KeyDeck/pull/2`.

GitHub-hosted Windows CI reached Go successfully. The automation scripts ran through secret scan, required-file checks, `go version`, and `go env`. The failure is in the repository test suite:

- `internal/corehost.TestCredentialCreatedOnceAndReused` failed because the credential file mode on Windows hosted runner was reported as `-rw-rw-rw-`.
- `internal/corehost.TestLeaseOwnershipLossForcesHostFailClosed` failed because the host did not fail closed after lease ownership loss.

This is documented as a pre-existing Windows test/product blocker rather than an automation script regression. Fast/deep CI should not be treated as passing until this blocker is reviewed or fixed.