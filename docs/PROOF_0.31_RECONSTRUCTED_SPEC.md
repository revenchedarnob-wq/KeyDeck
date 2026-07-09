# Proof 0.31 — Candidate Collection + Conservative Reconciliation (Reconstructed)

Acceptance contract:

- validate exact current task/handoff/route/runtime/engine/result state before persistence;
- reuse duplicate execution/result candidates without producer rerun;
- reject conflicting duplicate identities, stale state and secret-like evidence;
- restart replays durable candidates without rerunning producers;
- distinguish authentic provenance from verification evidence;
- support `single_verified`, `agreement`, `disagreement` and `needs_review`;
- majority agreement never establishes truth over conflicting evidence.

Run: `go run ./cmd/proof31`

Status in this reconstructed line: PASS 14/14.
