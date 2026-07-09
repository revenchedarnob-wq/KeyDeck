# v0.22.0 Reconstructed

- Added manager-owned MCP `ExecutionRouter`.
- Added side-effect-free `Preflight` using the same readiness, schema-bound approval and permission-profile gates as execution.
- Preserved existing Bridge order: schema → Secret Broker → Tool Journal → adapter.
- Re-proven completed restart reuse and ambiguous non-repeatable blocking.
- Replayed real approved execution against exact pinned third-party MCP runtime.
- Proof 0.25: PASS 10/10.

This is reconstructed/re-proven from exact v0.21.0, not a byte-identical recovery of a lost archive.
