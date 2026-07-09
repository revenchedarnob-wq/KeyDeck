# KeyDeck Feasibility Lab v0.16.0 Changelog

## Added

- Proof 0.19 — Real MCP Tool Execution + Durable Tool Journal Bridge.
- `internal/mcpbridge` protocol-boundary client and KeyDeck safety bridge.
- MCP stdio subprocess transport with newline-delimited JSON-RPC and bounded frame size.
- MCP permission allowlist enforced before server execution.
- Tool Journal integration for MCP operation IDs, argument hashes, replay policy, completion reuse, ambiguity blocking, and idempotent retry.
- Universal Activity Timeline events for MCP tool lifecycle.
- Proof Receipt evidence linking MCP timeline events and a durable artifact SHA-256.
- Deterministic local MCP proof server with non-repeatable, ambiguous, and idempotent tools.

## Proven behavior

```text
completed non-repeatable MCP tool
→ restart
→ previous result returned
→ no replay
```

```text
non-repeatable tool commits effect
→ server exits before response
→ restart
→ replay blocked
```

```text
idempotent tool commits value
→ server exits before first response
→ restart
→ safe retry
→ converged result
```

## Integration boundary

The proof uses real MCP stdio wire semantics directly.

The official MCP Go SDK remains the preferred production adapter direction, but was not imported because outbound module resolution was unavailable in the execution sandbox.

KeyDeck continues to own:

- permissions;
- Tool Journal replay policy;
- canonical task state;
- timeline evidence;
- Proof Receipts;
- recovery decisions.
