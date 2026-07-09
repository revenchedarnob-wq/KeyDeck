# Windows Proof Worker Design

This is preparation only. Do not install, register, or run a GitHub self-hosted runner from this document.

## Goals

- Dedicated local Windows account for proof execution.
- Least privilege and no access to the daily user profile or personal documents.
- Dedicated working directory such as `C:\KeyDeck-Proof-Worker`.
- Restricted secrets with explicit allowlists and short lifetimes.
- Allowlisted KeyDeck workflows only.
- Append-only proof receipts.
- Controlled artifact upload.
- No execution of arbitrary PR code.
- No public-repository jobs.
- Explicit separation from the daily user account.

## Future safe architecture

1. Create a dedicated Windows account with no administrator rights.
2. Deny access to personal folders and unrelated drives.
3. Create `C:\KeyDeck-Proof-Worker\work`, `receipts`, and `artifacts` with restricted ACLs.
4. Configure a repository-scoped runner only after branch protections and workflow allowlists are reviewed.
5. Accept only dedicated proof workflows that checkout trusted refs and never run arbitrary pull-request code with secrets.
6. Emit append-only JSON receipts for every proof run.
7. Upload artifacts through a controlled path after secret scanning.
8. Disable immediately by removing the runner token, stopping any service, and revoking any worker secrets.

Use `scripts/worker/prepare-windows-proof-worker.ps1` for dry-run planning receipts only.
