# KeyDeck Progress Proof v0.7.0

## Proven milestones

- Proof 0.1 — financially safe elastic API-pool policy.
- Proof 0.2 — same-provider mid-answer continuation and ambiguity protection.
- Proof 0.3 — persistent tool journal and replay safety.
- Proof 0.4 — KeyDeck-owned canonical session across engine switches.
- Proof 0.5 — real Codex handoff, restart/resume and switch-back on Zarif's Windows PC.
- Proof 0.6 — automatic API-pool exhaustion to real Codex, restart/resume and recovered API.
- Proof 0.7 — API mid-answer exhaustion to persisted partial state, real Codex continuation, restart/resume and recovered API.
- Proof 0.8 — Context Compiler benchmark passed on real Codex with correctness preserved.
- Proof 0.9 — durable Task Contract and Progress Proof semantics passed locally.
- Proof 0.10 — provider/optimizer conformance architecture passed locally.

## Proof 0.10 evidence

Passed scenarios:

1. Optimization OFF preserved request bytes exactly and did not invoke an optimizer.
2. Optimization ON activated only for exact verified provider/version evidence; a version mismatch left the request unchanged and failed closed.
3. An exact provider-specific key-scoped exhaustion rule rotated safely to the next key.
4. An unknown 429 remained ambiguous and did not spend a backup key.
5. Provider-wide busy preserved the backup key.
6. Ambiguous 502 did not replay and did not rotate.
7. Cost-thrash protection blocked before backup-key consumption.

Important boundary:

- fixture provider rules prove control flow only;
- no real provider-specific optimizer is enabled by this proof;
- production profiles still require exact provider/version/date evidence.

## Validation

```text
go test ./...
go vet ./...
go test -race ./internal/pool ./internal/continuity ./internal/tooljournal ./internal/session ./internal/tasks ./internal/apiengine ./internal/codexapp ./internal/conformance
go run ./cmd/proof09
go run ./cmd/proof10
```

## Current next gate

Durable Proof Receipts + Universal Activity Timeline:

- one append-only event identity across task, engine, tool, artifact and proof events;
- durable event IDs and canonical ordering;
- human-readable Proof Receipt generated from acceptance evidence and timeline references;
- no secrets in receipts;
- restart replay preserves receipt inputs exactly once.
