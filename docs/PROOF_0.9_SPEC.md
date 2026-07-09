# Proof 0.9 — Durable Task Contract and Progress Proof

## Goal

Prove the KeyDeck-owned long-running Task primitive before integrating a generic workflow runtime.

The Task primitive aligns with the planned MCP Tasks states:

- working
- input_required
- completed
- failed
- cancelled

## Required proofs

1. A completed non-repeatable tool action whose journal commit survives but whose task-step commit is lost must be reconciled after restart without replaying the action.
2. A non-repeatable tool action left in `started` state across a crash must be treated as ambiguous and block automatic recovery.
3. An interrupted idempotent tool action may retry.
4. Progress must be derived from acceptance-check evidence only, never from model estimates.
5. Task completion must survive restart through append-only event replay.

## Backend boundary

This proof intentionally validates semantics using a local append-only event store first. The task API is designed so the persistence backend can later be replaced by SQLite and/or a proven durable-workflow library without changing safety semantics.
