# KeyDeck Progress Proof v0.16.0

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
- Proof 0.14 — Microsoft APM + Waza Proving Ground passed with real APM and real checksum-verified Waza Windows evidence.
- Proof 0.15 — Real Provider Conformance Framework passed locally.
- Proof 0.16 — first real provider evidence capture passed with exact Aerolink invalid-credential evidence.
- Proof 0.17 — second real provider evidence capture passed with exact Aerolink HTTP 402 usage-window-limit evidence and conservative scope handling.
- Proof 0.18 — bounded paired scope capture imported safely; exact 402 was reproduced, replacement timed out, and KeyDeck preserved conservative policy without rerunning.
- Proof 0.19 — real MCP stdio tool execution is now governed by the durable Tool Journal, permission gate, Universal Activity Timeline, and Proof Receipt evidence.

## Proof 0.19 evidence

Passed 7/7 acceptance scenarios.

Protocol version:

`2025-11-25`

Transport:

`stdio-newline-delimited-json-rpc`

Durable Tool Journal operations:

`3`

MCP timeline events:

`8`

Proven:

```text
permission denied
→ no server execution
```

```text
completed non-repeatable operation
→ restart
→ result reused
→ side effect count remains one
```

```text
ambiguous non-repeatable operation
→ effect committed
→ server exits before response
→ restart
→ automatic replay blocked
```

```text
idempotent operation
→ first response interrupted
→ restart
→ safe retry
→ final state converges
```

The final Proof Receipt references all MCP tool timeline events and the deterministic server-state artifact SHA-256.

## Validation

```text
go test ./...
go vet ./...
race coverage across every stateful core package
Proofs 0.9–0.19
```

## Current next gate

Proof 0.20 — MCP Bridge Hardening:

- explicit permission profiles;
- bounded frame rejection;
- cancellation;
- production adapter seam;
- then one real local MCP server.
