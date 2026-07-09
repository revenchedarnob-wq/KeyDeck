# Real Codex Compatibility Fix v0.2.1

## Observed live failure

On Windows with Codex CLI 0.142.5 authenticated through ChatGPT Plus, Proof 0.5 reached the real App Server and failed at `thread/start` with:

`Invalid request: unknown variant workspaceWrite, expected one of read-only, workspace-write, danger-full-access`

## Root cause

The Codex App Server protocol currently uses two different representations for workspace-write permissions:

- Legacy `thread/start.sandbox` shorthand: `workspace-write`
- Structured `turn/start.sandboxPolicy.type`: `workspaceWrite`

The v0.2 runner incorrectly sent the structured camelCase variant in the legacy thread field.

## Fix

- `thread/start.sandbox` now sends `workspace-write`.
- `turn/start.sandboxPolicy.type` remains `workspaceWrite`.
- Added regression validation for both representations.

## Validation completed before packaging

- `go test ./...` PASS
- `go vet ./...` PASS
- `go test -race ./internal/codexapp ./internal/session` PASS
- Windows x64 `proof05-real.exe` cross-build PASS

## Proof status

The real ChatGPT Plus authentication gate is proven. The full real Codex handoff/restart/resume proof remains pending until this hotfix is rerun on the Windows machine.
