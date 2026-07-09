# v0.23.0 Reconstructed

- Added `internal/contextscout` production path.
- Added source-relevant local fingerprinting with `.env` exclusion.
- Added append-only hash-chained packet records and durable packet/rendered/provider-evidence artifacts.
- Added exact cache and packet identity binding.
- Added restart-safe fresh packet reuse, including provider-disabled offline reuse.
- Added manager-gated stale/missing rebuild ordering through `ExecutionRouter.Preflight`.
- Added Context Hygiene checks for budgets, uniqueness, containment, secret exclusion and structural receipts.
- Added explicit omitted-evidence accounting.
- Added Proof Receipt artifacts binding packet/store/provider identity evidence.
- Proof 0.26: PASS 10/10.

This is reconstructed/re-proven from exact v0.21.0 plus reconstructed Proof 0.25, not a byte-identical recovery of a lost archive.
