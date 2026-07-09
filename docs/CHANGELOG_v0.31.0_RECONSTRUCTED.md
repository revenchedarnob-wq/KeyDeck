# v0.31.0-RECONSTRUCTED

## Evidence boundary

- Exact physically recovered ancestor: v0.21.0 / Proof 0.24.
- Reconstructed and re-proven forward line: Proofs 0.25–0.34.
- No claim of byte-identical recovery of lost historical post-v0.21 archives.

## Added in the reconstructed line

- manager-gated MCP execution routing;
- Production Context Scout/Compiler;
- canonical Project Brain revisions and inspection evidence;
- production Handoff Package assembly;
- durable replay-safe handoff execution and restart reconciliation;
- deterministic evidence routing and safe continuation;
- candidate collection and conservative reconciliation;
- explicit decisive-evidence resolution;
- selected-only exact-once Recovery Coordinator integration;
- integrated Production Candidate Collection Coordinator with 32 safety scenarios.

## Key bugs caught and fixed while reconstructing

- Proof 0.29 live-state fixture did not expose mutation to the validator; fixed before pass.
- Provenance was incorrectly treated as verification evidence, making `needs_review` unreachable; separated provenance from `verification:*` evidence.
- Selected-result recovery used split in-memory engine-ledger ownership; fixed to use the Recovery Coordinator-owned ledger instance.
- Pinned Proof 0.22 regression initially used wrapper package metadata instead of the exact MCP server package JSON; corrected without weakening the proof.
