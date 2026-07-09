# KeyDeck Progress Proof v0.15.0

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

## Proof 0.18 evidence

Passed 28/28 integrated scenarios.

Raw paired Windows capture SHA-256:

`ee2ebaa92ab8bf97661a1a594939ede3f0109442b28190f5f21080a9fdbf81b8`

Normalized paired evidence ID:

`aerolink-paired-scope-inconclusive-2026-07-07`

Normalized paired evidence SHA-256:

`94f854fb229722e31cc20448948f1f3caad45344474435ca4712920375c1cc3e`

Observed sequence:

```text
Credential A
→ exact known HTTP 402 body hash

Credential B
→ one request
→ zero retries
→ no HTTP response
→ 20-second timeout
```

Policy remains:

```text
scope = unproven
class = ambiguous
original replay = false
key rotation = false
semantic continuation = false
require input = true
```

## Validation

```text
go test ./...
go vet ./...
full race coverage across all stateful core packages
Proofs 0.9–0.18
```

## Current next gate

Do not automatically repeat the paired provider experiment.

Return to local product work. The next high-value local gate should close a weaker product workstream without requiring paid-provider evidence, with real MCP + Tool Journal integration and production Context System hardening as leading candidates.
