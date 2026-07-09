# Proof 0.11 — Universal Activity Timeline and Proof Receipts

## Goal

Prove one durable cross-domain activity timeline and a human-readable Proof Receipt generated only from persisted acceptance evidence, timeline references and artifact hashes.

## Required proofs

1. Task, engine, tool, artifact and proof events share one append-only event identity and one canonical sequence.
2. Durable event IDs survive restart without duplicate insertion.
3. Reusing an event ID with different content is rejected rather than silently overwritten.
4. A human-readable Proof Receipt is derived from the Task Contract, acceptance evidence, timeline references and SHA-256 artifact evidence.
5. Secret-like evidence is rejected before receipt creation.
6. Receipt inputs replay after restart to the same deterministic receipt ID and input digest.
7. Re-saving the same receipt and re-appending its proof event after restart creates no duplicate.

## Safety boundary

This proof creates the durable evidence substrate required by the future integrated Recovery Coordinator. It does not yet claim that every existing KeyDeck subsystem automatically projects all runtime events into the timeline, nor does it implement cross-resource recovery transactions.

## Run

```bash
go run ./cmd/proof11
```
