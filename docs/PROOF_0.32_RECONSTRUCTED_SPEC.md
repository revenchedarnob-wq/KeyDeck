# Proof 0.32 — Evidence-Backed Resolution + Selected-Only Canonical Recovery (Reconstructed)

Acceptance contract:

- unresolved candidates cannot mutate canonical state;
- emit a UI-neutral review contract;
- resolution requires decisive evidence and a selected candidate from the assessed set;
- resolution survives restart;
- only the selected resolved candidate enters Recovery Coordinator;
- repeated canonical commit converges exactly once;
- direct raw recovery bypass fails closed;
- receipt binds collection, assessment, resolution, recovery preparation/operation and canonical commit;
- late candidates invalidate stale assessment before review/commit.

Run: `go run ./cmd/proof32`

Status in this reconstructed line: PASS 12/12.
