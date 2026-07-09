# Proof 0.3 — Tool Journal Replay Safety

## Goal

Prove that engine/key switching and crash recovery cannot blindly repeat a tool action whose real-world outcome may already have occurred.

## Rules

- Every operation has a stable operation ID, tool name and argument hash.
- Completed operations return the recorded result instead of executing again.
- A started-but-not-completed **non-replayable** operation becomes ambiguous after restart and blocks automatic replay.
- A started-but-not-completed operation explicitly classified as idempotent may retry.
- Reusing an operation ID with different arguments is rejected.
- Journal records are append-only and synced to disk in the lab proof.

## Acceptance checks

- [x] Completed destructive action is not re-executed after restart.
- [x] Interrupted destructive action stops with an ambiguous-operation error.
- [x] Interrupted idempotent read may retry.
- [x] Operation ID/argument collision is rejected.

## Honest limitation

This proves replay decisions and restart persistence, not yet full integration with real MCP/ACP tools. Cross-engine continuation should consume this journal before any tool action can be reissued.
