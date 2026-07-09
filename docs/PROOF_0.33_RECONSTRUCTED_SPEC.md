# Proof 0.33 — Integrated Handoff, Routing, Reconciliation and Recovery (Reconstructed)

Integrated acceptance contract:

- restored handoff package safety;
- durable route-bound execution and restart reuse;
- deterministic evidence routing;
- safe route-bound mid-answer continuation;
- provider-busy continuation denial;
- reconciliation states `single_verified`, `agreement`, `disagreement`, `needs_review`, `resolved`;
- no majority-vote truth;
- stale task/package candidate rejection before persistence;
- duplicate candidate reuse after restart without producer rerun;
- secret-like evidence blocking;
- decisive-evidence resolution across restart;
- unresolved candidates cannot mutate canonical state;
- resolved result enters Recovery Coordinator exactly once;
- Proof Receipt binds disagreement, resolution, decisive evidence and reconciliation state.

Run: `go run ./cmd/proof33`

Status in this reconstructed line: PASS 12/12.
