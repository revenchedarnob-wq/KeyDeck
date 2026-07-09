# KeyDeck Progress Proof v0.6.0

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

## Proof 0.8 evidence

Real Windows / ChatGPT Plus benchmark:

| Arm | Commands | Input tokens | Cached tokens | Correctness |
|---|---:|---:|---:|---|
| Baseline | 12 | 121,199 | 67,328 | Pass |
| Context-assisted | 5 | 57,374 | 41,600 | Pass |

Measured change:

- command exploration reduced from 12 to 5;
- input tokens reduced from 121,199 to 57,374;
- correctness preserved in both arms.

The benchmark used pinned `codebase-memory-mcp` v0.8.1 and a 7,200-character packet with six focused source snippets.

## Proof 0.9 evidence

Passed scenarios:

1. Completed non-repeatable tool action survives a crash window and is reconciled from the tool journal without replay.
2. Interrupted non-repeatable action becomes `input_required` and blocks automatic recovery.
3. Interrupted idempotent action remains retryable.
4. Progress is computed only from acceptance checks: 2/4 checks = exactly 50%.
5. 4/4 passed acceptance checks complete the task, and completion survives event-log replay after restart.

## Current next gate

Provider/Optimizer Conformance:

- provider-specific failure classification;
- byte-preserving Optimization OFF mode;
- optimizer activation only with exact verified evidence;
- no provider-wide outage key burn;
- no ambiguous retry/fallback;
- cost-thrash protection before backup-key consumption.
