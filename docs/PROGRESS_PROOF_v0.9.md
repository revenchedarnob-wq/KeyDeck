# KeyDeck Progress Proof v0.9.0

## Proven milestones

- Proof 0.1 — financially safe elastic API-pool policy.
- Proof 0.2 — same-provider mid-answer continuation and ambiguity protection.
- Proof 0.3 — persistent Tool Journal and replay safety.
- Proof 0.4 — KeyDeck-owned canonical session across engine switches.
- Proof 0.5 — real Codex handoff, restart/resume and switch-back on a real Windows PC.
- Proof 0.6 — automatic API-pool exhaustion to real Codex, restart/resume and recovered API.
- Proof 0.7 — API mid-answer exhaustion to persisted partial state, real Codex continuation, restart/resume and recovered API.
- Proof 0.8 — Context Compiler benchmark passed on real Codex with correctness preserved.
- Proof 0.9 — durable Task Contract and Progress Proof semantics passed locally.
- Proof 0.10 — provider/optimizer conformance architecture passed locally.
- Proof 0.11 — Universal Activity Timeline and evidence-based Proof Receipts passed locally.
- Proof 0.12 — Integrated Recovery Coordinator and exactly-once canonical commit passed with process-level crash windows.

## Proof 0.12 evidence

Passed scenarios:

1. Completed destructive work converged exactly once without tool replay.
2. Ambiguous non-repeatable work remained blocked across repeated restarts.
3. Interrupted idempotent work remained safely retryable.
4. Two separate engine-result crash windows each converged to one canonical result commit.
5. Durable external engine state became `resume_required`.
6. Interrupted non-resumable engine state became `input_required`.
7. Task, session, engine ledger, Tool Journal, Activity Timeline, artifact ledger and Proof Receipt reconciled through one coordinator.
8. Final proof output remained exactly once across repeated recovery.

Important architectural correction discovered during the proof:

- a stale non-resumable execution must not downgrade an already terminal completed task back to `input_required`;
- terminal task state remains terminal while the recovery decision remains durable evidence.

## Validation

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/proof09
go run ./cmd/proof10
go run ./cmd/proof11
go run ./cmd/proof12
```

All passed before packaging v0.9.0.

## Current next gate

Proof 0.13 — Engine-Neutral Runtime Contract:

- formalize `start`, `continue`, `resume`, `cancel`, `capabilities`, `health` and recovery disposition;
- preserve official deep integrations such as Codex App Server behind the common runtime boundary rather than replacing them;
- persist engine runtime handles/bindings;
- map completed results into the existing exactly-once canonical commit path;
- prove `completed`, `resume_required`, `input_required`, `failed` and `cancelled` behavior without transferring canonical-state ownership to the engine.
