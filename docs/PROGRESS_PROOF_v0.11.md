# KeyDeck Progress Proof v0.11.0

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

## Proof 0.14 evidence

### APM v0.24.0

Passed:

1. declarative instructions represented;
2. agent definition represented;
3. prompt represented;
4. skill represented;
5. hook definition represented;
6. lockfile created;
7. clean audit passed;
8. deliberate drift detected with exit code 1;
9. frozen restore passed;
10. second clean install passed;
11. deployed hashes were reproducible;
12. package/plugin bundle created.

### Waza v0.38.0

Passed:

1. exact version verified;
2. exact Windows x64 binary checksum verified;
3. skill check passed;
4. positive/negative trigger spec coverage passed;
5. token count passed;
6. token-budget gate passed;
7. deterministic mock evaluation passed;
8. 4 tasks × 5 trials = 20 trials;
9. 2 positive and 2 negative trigger tasks;
10. 20 snapshots captured;
11. all 20 snapshots replayed successfully;
12. regression gate passed;
13. adversarial-pack catalog available.

Raw returned Waza evidence SHA-256:

`595c381fe97c063e633aafe45f029702a91d10162d65df0faa0041255e849baf`

## Evidence-based adoption decision

`ADOPT_APM_AND_WAZA_WITH_SCOPED_OWNERSHIP_AND_LIMITATIONS`

This proof shows enough generic engineering savings to defer:

- a large custom KeyDeck Skill Compiler;
- a custom lockfile/drift system for declarative agent content;
- a custom general skill/agent evaluation framework.

KeyDeck still owns:

- canonical state;
- runtime safety;
- recovery;
- financial safety;
- Tool Journal replay policy;
- permissions orchestration;
- secret brokering.

## Limitations

Not yet proven:

- remote Git ref pin resolution;
- non-empty MCP dependency resolution;
- live-model Waza quality comparison.

These are focused later checks, not blockers for the scoped adoption decision.

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
```

All must pass before packaging v0.11.0.

## Current next gate

Proof 0.15 — Real Provider Conformance Framework.

Start from the existing Proof 0.10 safety model and build the evidence path required for real provider profiles:

- exact provider identity and version;
- captured exhaustion behavior;
- invalid-credential behavior;
- key-specific rate-limit behavior;
- provider-wide busy/outage behavior;
- ambiguous transport failure behavior;
- streaming interruption semantics;
- cache/billing semantics where exposed;
- dated evidence and profile provenance.

Do not enable provider-specific Optimization ON or aggressive retry/failover behavior without exact real evidence.
