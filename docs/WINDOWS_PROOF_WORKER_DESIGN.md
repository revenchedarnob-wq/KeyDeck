# Windows Proof Worker Design

## Purpose

Provide a future restricted Windows runner for proofs that are Windows-dependent, UI-dependent, credential-dependent, or provider-dependent.

## Guardrails

- No hardcoded credentials.
- No unscoped environment dumps.
- No unsafe tool replay.
- No blind retry, key rotation, or provider fallback.
- Evidence must classify deterministic, network-dependent, credential-dependent, Windows-dependent, UI-dependent, and provider-dependent outcomes separately.

## Execution boundary

The proof worker should run from the canonical clone only:

```powershell
cd A:\KeyDeck-Workspace\repo
.\KEYDECK.ps1 deep
```

For provider proofs, the worker must write redacted receipts and never expose raw secrets in process arguments, durable state, logs, screenshots, or artifacts.

## Future implementation notes

- Prefer a dedicated low-privilege Windows user.
- Use a locked workspace root.
- Emit proof receipts into a staging directory before upload.
- Hash every artifact before upload.
- Keep network/provider failures classified as evidence, not as generic retries.
