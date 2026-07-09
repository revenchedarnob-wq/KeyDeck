# KeyDeck Progress Proof v0.12.0

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
- Proof 0.14 — Microsoft APM + Waza Proving Ground passed with a real APM prototype and a real checksum-verified Waza Windows run.
- Proof 0.15 — Real Provider Conformance Framework passed locally.

## Proof 0.15 evidence

Passed:

1. exact provider/API/model/version identity;
2. tested-at and expiry/revalidation window;
3. raw-capture SHA-256 provenance;
4. evidence-bundle SHA-256 sealing;
5. durable evidence round-trip;
6. tamper rejection;
7. exact pre-output credit exhaustion policy;
8. invalid-credential policy;
9. key-specific rate-limit policy;
10. provider-wide busy policy;
11. provider-wide outage policy;
12. ambiguous transport policy;
13. mid-stream key exhaustion semantic continuation policy;
14. abrupt partial-stream ambiguity policy;
15. unknown behavior conservative fallback;
16. identity/version mismatch conservative fallback;
17. expired evidence revalidation requirement;
18. cache/usage semantics capture;
19. evidence-derived integration with the existing API-key pool classifier.

Fixture evidence ID:

`proof-0.15-fixture-provider-r1`

Fixture evidence SHA-256:

`113c0c287421e11d8821a3a92b7fd5fbc239dc21b8a2039346afd370423e8344`

Raw capture SHA-256:

`5da2d7c1f499314823407b211c85c26b37fb7b080cda9ec92a1b30c0436915ef`

## Safety decision

The local conformance framework is ready for exact real-provider evidence capture.

No paid provider is yet marked conformant by this proof.

Unknown, mismatched, expired or unobserved behavior remains conservative.

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
```

All must pass before packaging v0.12.0.

## Current next gate

Proof 0.16 — First Real Provider Evidence Capture.

Do not immediately spend a paid API key. First choose the highest-value provider based on the user's actual KeyDeck/Aerolink usage path, verify current official error/usage semantics, build a narrowly scoped capture runner with strict spend limits, then request one manual real-provider run only if required.

The real-provider capture must record:

- exact provider/API/model/version;
- exact request shape without secret material;
- exhaustion or other target behavior;
- rate-limit/busy/outage evidence if safely reproducible;
- stream interruption semantics where practical;
- usage/cache fields;
- raw capture hashes;
- expiry/revalidation date.
