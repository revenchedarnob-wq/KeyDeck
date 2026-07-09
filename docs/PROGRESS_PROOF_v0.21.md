# KeyDeck Progress Proof v0.21.0

## New milestone

Proof 0.24 — Durable Local Runtime Bindings + MCP Server Manager Contract passed **10/10 integrated scenarios**.

Proven:

- portable MCP registration/discovery survive independently from local runtime availability;
- one canonical Server ID remains unchanged across local binding failure and repair;
- machine-local absolute paths are stored in a separate manager layer;
- conflicting local binding changes are blocked until an explicit rebind event is created;
- permission approvals are bound to the exact trusted discovery schema;
- wrong schema and unknown-tool approvals are rejected;
- only one explicitly approved read tool enters the effective policy;
- real pinned MCP initialize/tools-list health succeeds with 14 tools;
- enable/disable state survives restart;
- missing runtime produces `unavailable` without deleting portable state;
- explicit repair/rebind restores real health and final ready state;
- final manager state survives restart;
- local manager durable state contains no raw secret sentinel;
- Proof Receipt binds separate portable registry and local manager artifacts.

## Exact evidence

- Server ID: `mcp-2249d10f545df5e1a371589e`
- Identity SHA-256: `2249d10f545df5e1a371589efb2da91030a0eb6a3b8f866c29e33f5679ca76a6`
- Runtime SHA-256: `f5e3fafdf42658bb1f65f96018a68df2a0bfd4d5a771ce8c48a6b66cbd27cbc6`
- Schema SHA-256: `31b3d4e6042e08e91514e42eaddab25ec963664e63dd87081c25577cc48f185f`
- Registry SHA-256: `14ad7aebf598b265b57629e7b35b41b9ffd5b3130c71eab01a3cde436f2b27c4`
- Manager SHA-256 (reference local path): `66fb61fdd5c3756d782362cce2ef4caf6b0317d3f3052da0bf8b68d321e2c1f6`
- Healthy binding SHA-256 (reference local path): `a1977294e696c41b3827e1d156ba6f77b8a9c6675824190049b501bec370d25a`
- Broken binding SHA-256 (reference local path): `5ed3cec84fae8acc92ec5e84450cb7aed4c00e209368da5bd165d0e1bcf26433`
- Manager event count: 10

## Validation

Passed:

```text
go test ./...
go vet ./...
go test -race ./internal/mcpmanager ./internal/mcpregistry ./internal/mcpbridge
Proofs 0.9–0.24
```

## Next gate

Proof 0.25 — connect manager readiness and explicit approved-tool state to the existing MCP Bridge/Tool Journal execution path so disabled, unhealthy, unbound or unapproved servers cannot execute.
