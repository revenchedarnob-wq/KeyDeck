# KeyDeck Feasibility Lab v0.8.0 Changelog

Base: verified `KeyDeck-Feasibility-Lab-v0.7.0.zip`.

## Added

- Proof 0.11 — Universal Activity Timeline and Proof Receipts.
- `internal/timeline` durable append-only cross-domain event store.
- Explicit event identity across task, engine, tool, artifact and proof domains.
- Restart-safe `AppendOnce` semantics with conflict detection for reused event IDs.
- Canonical event ordering with sequence-gap and duplicate-ID validation during replay.
- `internal/proofreceipt` deterministic evidence-based receipt builder.
- Human-readable Markdown Proof Receipts.
- Receipt references to Task Contract acceptance evidence, timeline event IDs and SHA-256 artifact evidence.
- Secret-like material rejection before receipt generation.
- Durable receipt store with restart-safe `SaveOnce` semantics.
- Proof 0.11 machine-readable report and specification.

## Safety behavior proven

- replaying the same persisted event IDs after restart creates zero duplicate timeline entries;
- reusing an event ID with different content is rejected;
- all required activity domains share one canonical ordered timeline;
- receipt identity is derived deterministically from persisted proof inputs;
- rebuilding and re-saving a receipt after restart does not duplicate it;
- re-appending the receipt's proof event after restart does not duplicate it;
- secret-like acceptance evidence is blocked before a receipt can be created.

## Safety boundary

This release establishes the evidence substrate for the future integrated Recovery Coordinator. It does not yet claim one transactionally coordinated recovery decision across task, session, engine runtime, Tool Journal, artifacts, timeline and Proof Receipt.
