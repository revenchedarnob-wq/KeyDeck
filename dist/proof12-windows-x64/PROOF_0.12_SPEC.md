# Proof 0.12 — Integrated Recovery Coordinator and Exactly-Once Canonical Commit

## Goal

Prove that KeyDeck can reopen one crashed task as a single recovery unit spanning task state, canonical session state, engine runtime records, the Tool Journal, Activity Timeline, artifact evidence and Proof Receipts.

## Required proofs

1. Completed destructive tool work is reconciled from the durable Tool Journal without replay.
2. Repeating recovery does not append duplicate task or timeline recovery records.
3. Ambiguous non-repeatable work remains blocked across repeated restarts.
4. Interrupted idempotent work remains eligible for safe retry.
5. A completed engine result survives a crash before canonical commit and is committed exactly once.
6. A completed engine result survives a second crash window after canonical session save but before result-ledger commit and is not duplicated.
7. A durable external thread becomes `resume_required`.
8. An interrupted non-resumable engine becomes `input_required`.
9. Terminal task completion is not reopened by stale interrupted execution records.
10. Recovery decisions are durable Activity Timeline evidence.
11. Final task evidence produces exactly one Proof Receipt and one proof timeline event across repeated recovery.

## Process-level crash model

The proof runner launches itself as separate child processes. Each child durably writes a controlled partial state and exits with a dedicated crash code before the next state transition.

Crash windows include:

```text
Tool Journal completed
→ crash before task step completion
```

```text
Engine result persisted
→ crash before canonical session commit
```

```text
Canonical session saved with result marker
→ crash before engine-result ledger commit
```

The parent process then reopens every store from disk and runs the Recovery Coordinator.

## Exactly-once canonical result rule

Each durable engine result has a deterministic canonical commit marker:

```text
keydeck-engine-result:<result-id>
```

Recovery behavior:

```text
result uncommitted + marker absent
→ apply result to canonical session
→ atomically save session
→ mark result committed
```

```text
result uncommitted + marker already present
→ do not apply result again
→ reconcile result ledger commit only
```

## Safety boundary

This proof validates the local durable recovery substrate and real process crash windows. It does not yet claim that every production engine adapter implements the same lifecycle interface, nor that real Codex/API processes are automatically driven by the coordinator. That is the next proof gate.

## Run

```bash
go run ./cmd/proof12
```
