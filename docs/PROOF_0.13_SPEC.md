# Proof 0.13 — Engine-Neutral Runtime Contract

## Goal

Prove that replaceable KeyDeck engines can share one durable runtime lifecycle without allowing an engine, provider, or external thread to own canonical KeyDeck state.

## Runtime contract

The common boundary exposes:

```text
start
continue
resume
cancel
capabilities
health
```

Durable runtime dispositions:

```text
completed
resume_required
input_required
failed
cancelled
```

An internal `running` state exists only while an invocation is in flight.

## Required proofs

1. Missing required capabilities block before engine work begins.
2. Unhealthy engines block before engine work begins.
3. Failed execution survives restart and is not replayed automatically.
4. Durable external handle/binding survives restart.
5. A resumable execution resumes through the same common runtime contract.
6. Cancellation survives restart and does not resume later.
7. Interrupted non-resumable work becomes `input_required` rather than being replayed.
8. A durable result found after runtime interruption reconciles to `completed` without replaying the adapter.
9. Completed engine output remains outside canonical session state until the KeyDeck Recovery Coordinator commits it.
10. Repeated recovery commits each completed runtime result to canonical state exactly once.
11. Final Proof Receipt identity remains stable across repeated recovery.

## Architecture rule

```text
Official deep integration
        ↓
thin KeyDeck runtime adapter
        ↓
engine-neutral runtime contract
        ↓
durable recovery/result ledger
        ↓
Recovery Coordinator
        ↓
canonical KeyDeck state
```

The official Codex App Server integration is not replaced by a generic protocol. Existing KeyDeck `session.Engine` implementations can be wrapped behind a thin compatibility adapter while preserving native durable bindings.

## Restart safety rule

If runtime state is still `running` after restart:

```text
durable result exists
→ reconcile completed without adapter replay
```

```text
durable resumable binding exists
→ resume_required
```

```text
neither exists
→ input_required
```

Unknown interrupted work is never replayed automatically.

## Exactly-once result rule

The runtime persists a completed result into the already-proven recovery engine ledger first.

Canonical session mutation is performed only by the Recovery Coordinator.

This preserves:

```text
engine = worker
KeyDeck = canonical state owner
```

## Safety boundary

Proof 0.13 uses deterministic local adapters and the existing local recovery substrate.

It proves the common runtime contract and its integration with exactly-once canonical commit.

It does **not** yet claim that:

- the real Codex App Server is fully driven through this new runtime in production;
- real API providers have passed provider-specific conformance under this runtime;
- Google AI Pro agentic tooling is integrated;
- Windows machine-restart recovery for this runtime layer is proven.

Those require later proof gates.

## Run

```bash
go run ./cmd/proof13
```
