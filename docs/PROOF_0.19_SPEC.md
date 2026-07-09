# Proof 0.19 — Real MCP Tool Execution + Durable Tool Journal Bridge

## Goal

Prove that KeyDeck can execute real MCP tool calls over a local stdio transport while retaining ownership of permissions, replay safety, durable tool state, timeline evidence, and Proof Receipts.

MCP is the tool protocol.

KeyDeck remains the safety and canonical-state owner.

## Protocol boundary

The proof uses the MCP `2025-11-25` connection-era flow:

```text
subprocess stdio
→ newline-delimited JSON-RPC
→ initialize
→ notifications/initialized
→ tools/list
→ tools/call
```

The local proof server is deterministic and runs as a subprocess of the proof executable.

No paid service, external network, API key, ChatGPT login, Codex login, or SaaS account is required.

## Important implementation boundary

The proof validates the MCP wire boundary directly.

The official MCP Go SDK remains the preferred production adapter direction, but its module could not be fetched in the execution sandbox because outbound Go module resolution was unavailable. Proof 0.19 therefore does **not** claim that the official SDK package itself was integrated.

This does not weaken the replay-safety proof, because the KeyDeck-owned bridge sits above the transport adapter seam.

## Local proof tools

### `counter.increment`

Non-repeatable.

Used to prove:

```text
completed result
→ restart
→ journal returns previous result
→ MCP tool is not called again
```

### `ambiguous.append`

Non-repeatable.

The server commits the effect and terminates before returning an MCP response.

Used to prove:

```text
effect may have happened
→ transport ends before response
→ restart
→ automatic replay forbidden
```

### `idempotent.put`

Explicitly idempotent.

The server writes the value and terminates before the first response. The same operation ID may retry after restart and converge safely.

## Required acceptance checks

1. A disallowed MCP tool is blocked before server execution.
2. Real MCP `initialize`, `tools/list`, and `tools/call` succeed over stdio.
3. A completed non-repeatable MCP operation is reused after restart without replay.
4. An ambiguous non-repeatable MCP operation remains blocked after restart.
5. A declared idempotent MCP operation may retry safely after interrupted transport.
6. MCP tool lifecycle events enter the Universal Activity Timeline.
7. A Proof Receipt references MCP tool timeline evidence and a SHA-256 artifact exactly once.

## Safety model

For every allowed effectful operation:

```text
permission check
→ Tool Journal begin
→ timeline started event
→ MCP tool call
```

### Completed non-repeatable operation

```text
journal = completed
→ future same operation ID
→ return previous result
→ no MCP replay
```

### Ambiguous non-repeatable operation

```text
journal = started
→ transport dies before result
→ restart
→ ErrAmbiguousOperation
→ no MCP replay
```

### Idempotent interrupted operation

```text
policy = idempotent
→ interrupted transport
→ journal failure recorded
→ restart
→ same operation ID may execute again
```

## Evidence integration

MCP events are normalized into the Universal Activity Timeline:

- `mcp_tool_started`
- `mcp_tool_completed`
- `mcp_tool_result_reused`
- `mcp_tool_ambiguous`
- `mcp_tool_retryable_failure`

The final Proof Receipt binds:

- Task Contract acceptance checks;
- MCP timeline references;
- durable local server-state artifact SHA-256.

## Next gate

Harden the bridge before broad app integrations:

1. explicit permission profiles;
2. bounded-frame failure proof;
3. cancellation semantics;
4. production adapter interface;
5. then one real local MCP server integration.

Do not connect broad SaaS integrations before the local safety bridge is hardened.
