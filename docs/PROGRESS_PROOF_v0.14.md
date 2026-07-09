# KeyDeck Progress Proof v0.14.0

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

## Proof 0.17 evidence

Passed 22/22 integrated scenarios in the current proof runner.

Raw Windows capture SHA-256:

`f1b07af5a186f96709151bc70f541c2b097d9469c25281f726a9bfc44a1532f3`

Response body SHA-256:

`d3ab716be4468ad1fa15396e5ab20fcd475d553d003d8f4f562327e9b8a97dc4`

Normalized evidence fragment ID:

`aerolink-included-usage-window-limit-2026-07-07`

Normalized evidence fragment SHA-256:

`da65966cf99da86922668dc3457c027d2307d6218a5da64dd2a20398a4694b19`

Observed behavior:

```text
exact provider/API/endpoint/model/version
+
pre-output HTTP 402
+
exact captured response body hash
→ trusted observation: 5-hour included-usage window limit
```

Policy remains:

```text
scope = unknown
class = ambiguous
original replay = false
key rotation = false
semantic continuation = false
require input = true
```

## Framework correction

The fragment registry now supports multiple exact real behaviors for the same provider identity and endpoint. Proof 0.16's HTTP 401 invalid-key behavior and Proof 0.17's HTTP 402 usage-window behavior both match independently.

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
go run ./cmd/proof14
go run ./cmd/proof15
go run ./cmd/proof16
go run ./cmd/proof17
```

## Current next gate

Prove scope before allowing automatic rotation on the exact 402 behavior.

Use a bounded paired capture:

1. known-limited key returns the exact captured 402;
2. a separate known-usable replacement key/account succeeds immediately afterward on the same request shape.

Until that passes, KeyDeck preserves backup keys and requires input.
