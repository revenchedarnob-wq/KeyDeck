# KeyDeck Progress Proof v0.13.0

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

## Proof 0.16 evidence

Passed 17/17 scenarios.

Raw Windows capture SHA-256:

`b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3`

Normalized evidence fragment ID:

`aerolink-invalid-key-2026-07-07`

Normalized evidence fragment SHA-256:

`794a62c6d40ba1eb6fb47a8526ea95c4ce7768bb129b59f3a8c0b36886ddc2b4`

Trusted behavior:

```text
exact provider/API/endpoint/model/version
+
pre-output HTTP 401
+
exact captured response body hash
→ invalid_key
→ replacement-key selection allowed
```

Any mismatch remains conservative.

## Validation

```text
go test ./...
go vet ./...
go test -race on all stateful core packages
go run ./cmd/proof09
go run ./cmd/proof10
go run ./cmd/proof11
go run ./cmd/proof12
go run ./cmd/proof13
go run ./cmd/proof14
go run ./cmd/proof15
go run ./cmd/proof16
```

## Current next gate

Continue incremental real Aerolink evidence capture without spending backup keys blindly.

Priorities:

1. exact credit-exhaustion response;
2. exact key-specific rate-limit response;
3. provider-wide busy/outage evidence;
4. ambiguous transport behavior;
5. streaming interruption semantics;
6. cache and usage/billing semantics.

Do not promote Aerolink to a complete trusted provider profile until enough exact evidence exists.
