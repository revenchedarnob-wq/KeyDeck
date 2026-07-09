# Proof 0.30 — Deterministic Evidence Routing + Safe Continuation (Reconstructed)

Acceptance contract:

- only available, healthy and capability-qualified engines enter ranking;
- explicit exclusions are honored;
- route identity is deterministic across candidate input order;
- evidence score ranks qualified candidates with deterministic tie-breaking;
- route binds task, session, handoff package and candidate-set SHA;
- stale/tampered route is rejected before adapter invocation;
- key-exhaustion continuation may move only to an eligible different route;
- provider-wide busy denies same-provider continuation;
- route and continuation evidence enter timeline and Proof Receipt.

Run: `go run ./cmd/proof30`

Status in this reconstructed line: PASS 10/10.
