# KeyDeck Progress Proof v0.17.0

## New milestone

Proof 0.20 — MCP Bridge Hardening passed **7/7 acceptance scenarios**.

Proven:

- Read Only / Safe Edit / Full Control profiles block disallowed tools before adapter invocation.
- Oversized MCP frames are rejected at a configured hard bound.
- A cancelled non-repeatable effect remains ambiguous and blocked after restart.
- The same cancelled side effect is not executed twice.
- The Bridge can run through an explicit adapter interface without the command client.
- A separate compiled local MCP server process handles real initialize/list/call traffic.
- Hardening events remain in the Universal Activity Timeline.
- A Proof Receipt binds the timeline and final server-state artifact hash exactly once.

Final deterministic server-state artifact SHA-256:

`41b81131b6e7dd57174f62ce725d797257849f8b35bac0b13389e643bea3aa3c`

## Validation

Passed:

```text
go test ./...
go vet ./...
race coverage across every stateful core package
Proofs 0.9–0.20
```

## Honest boundary

The proof uses an independent KeyDeck proof server process, not a third-party production MCP server.

The official MCP Go SDK package is still not imported because the current execution sandbox could not resolve external Go modules.

## Next gate

Proof 0.21 — MCP Secret Broker + Schema-Aware Authorization:

- scoped secret handles rather than raw secrets in model/tool arguments;
- per-tool allowed secret scopes;
- schema-aware argument policy before adapter invocation;
- sensitive-field redaction in timeline and receipts;
- preserve all Proof 0.19–0.20 replay and cancellation guarantees.
