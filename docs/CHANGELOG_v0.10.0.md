# KeyDeck Feasibility Lab v0.10.0 Changelog

## Added

- `internal/engineruntime` durable engine-neutral runtime package.
- Common runtime operations: `start`, `continue`, `resume`, `cancel`.
- Capability and health gates before engine work begins.
- Durable runtime execution ledger.
- Durable external binding/handle persistence.
- Runtime restart reconciliation for completed results, resumable work and non-resumable interruptions.
- Thin `SessionEngineAdapter` for existing KeyDeck engines, preserving native stateful bindings.
- Proof 0.13 integrated acceptance harness.
- Runtime unit tests and restart-safety tests.

## Proven

- Missing capabilities block before adapter invocation.
- Unhealthy engines block before adapter invocation.
- `continue` dispatches through the common runtime operation rather than replaying `start`.
- Failed execution survives restart without replay.
- Durable bindings survive restart and resume through one contract.
- Cancellation survives restart.
- Interrupted non-resumable work becomes `input_required` without replay.
- Persisted engine results reconcile runtime state after interruption without adapter replay.
- Engine output does not mutate canonical state directly.
- Completed runtime results commit exactly once through the existing Recovery Coordinator.
- Proof Receipt identity remains stable across repeated recovery.

## Architecture boundary preserved

- Engines remain replaceable workers.
- Canonical KeyDeck state remains outside the engine runtime.
- Official deep integrations such as Codex App Server remain intact and may be wrapped behind a thin runtime adapter.

## Validation

- `go test ./...` — PASS.
- `go vet ./...` — PASS.
- `go test -race ./...` — PASS.
- Proofs 0.9, 0.10, 0.11, 0.12 and 0.13 regression — PASS.
