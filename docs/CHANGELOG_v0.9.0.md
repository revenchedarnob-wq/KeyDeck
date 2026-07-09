# KeyDeck Feasibility Lab v0.9.0 Changelog

## Added

- `internal/recovery` integrated recovery package.
- Durable engine execution/result ledger.
- Durable artifact evidence ledger.
- Exactly-once canonical engine-result commit marker.
- Recovery Coordinator spanning task, session, engine, tool, timeline, artifact and proof state.
- Process-level Proof 0.12 crash harness.
- Recovery unit tests.

## Proven

- Completed destructive work is not replayed after crash.
- Ambiguous non-repeatable work remains blocked after repeated restarts.
- Idempotent interrupted work remains safely retryable.
- Completed engine results survive two distinct crash windows and commit exactly once.
- Durable external threads become `resume_required`.
- Interrupted non-resumable engines become `input_required`.
- Terminal completed tasks are not reopened by stale engine records.
- Integrated Proof Receipt generation remains exactly once.

## Validation

- `go test ./...` — PASS.
- `go vet ./...` — PASS.
- `go test -race ./...` — PASS.
- Proofs 0.9, 0.10, 0.11 and 0.12 regression — PASS.
