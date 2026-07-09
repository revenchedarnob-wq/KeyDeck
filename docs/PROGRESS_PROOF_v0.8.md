# KeyDeck Progress Proof v0.8.0

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
- Proof 0.11 — Universal Activity Timeline and evidence-based Proof Receipts passed locally.

## Proof 0.11 evidence

Passed scenarios:

1. Secret-like acceptance evidence was rejected before receipt generation.
2. Replaying all persisted event IDs after restart appended zero duplicate events.
3. Canonical ordering remained continuous after replay.
4. A human-readable receipt was generated from 4/4 passed acceptance checks, timeline references and SHA-256 artifact evidence.
5. Rebuilding the receipt after restart produced the same receipt ID and input digest.
6. Re-saving the same receipt after restart appended zero duplicate receipts.
7. Re-appending the proof event after restart appended zero duplicate events.
8. Task, engine, tool, artifact and proof domains all shared one canonical timeline.

Important boundary:

- Proof 0.11 creates the durable evidence substrate only;
- existing runtime subsystems are not yet all automatically projected into the timeline;
- cross-resource recovery coordination remains the next proof gate.

## Validation

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/proof09
go run ./cmd/proof10
go run ./cmd/proof11
```

## Current next gate

Proof 0.12 — Integrated Recovery Coordinator and exactly-once canonical commit:

- reconcile task, canonical session, engine runtime, Tool Journal, Activity Timeline, artifacts and Proof Receipt as one recovery unit;
- completed destructive work remains exactly-once;
- ambiguous work remains blocked across repeated restarts;
- idempotent work may retry safely;
- a completed engine result can survive crash windows and commit to canonical state exactly once;
- durable external threads become `resume_required`;
- interrupted non-resumable engines become `input_required`;
- recovery decisions are themselves durable timeline evidence.
