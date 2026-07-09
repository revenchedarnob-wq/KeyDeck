# KeyDeck Progress Proof v0.10.0

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
- Proof 0.13 — Engine-Neutral Runtime Contract passed locally.

## Proof 0.13 evidence

Passed scenarios:

1. Missing required capability blocked before engine invocation.
2. Unhealthy engine blocked before engine invocation.
3. `continue` dispatched through the same common runtime contract without falling back to `start`.
4. Failed execution survived restart without replay.
5. Durable external binding survived restart and resumed.
6. Cancellation survived restart and prevented later resume.
7. Interrupted non-resumable execution became `input_required` without replay.
8. Engine result remained outside canonical session state before recovery.
9. Persisted result reconciled runtime state after interruption without replay.
10. Completed runtime results committed exactly once through repeated Recovery Coordinator runs.
11. Final Proof Receipt identity remained stable across repeated recovery.

The compatibility layer also has unit coverage proving an existing stateful KeyDeck `session.Engine` can be wrapped without discarding its native external binding.

## Validation

```text
go test ./...
go vet ./...
go test -race ./...
go run ./cmd/proof09
go run ./cmd/proof10
go run ./cmd/proof11
go run ./cmd/proof12
go run ./cmd/proof13
```

All passed before packaging v0.10.0.

## Current next gate

Proof 0.14 — Microsoft APM + Waza Proving Ground:

- prototype the declarative Agent Environment Contract before building a large custom Skill Compiler;
- test whether APM can handle skill/instruction/agent/MCP dependency packaging and lockfile behavior;
- use Waza to compare baseline vs skill behavior, token bloat, trigger accuracy and flakiness;
- keep APM/Waza development-time infrastructure separate from KeyDeck canonical state and runtime safety;
- prove actual engineering savings before adopting them as core dependencies.
