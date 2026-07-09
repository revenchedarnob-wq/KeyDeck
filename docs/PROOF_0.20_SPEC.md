# Proof 0.20 — MCP Bridge Hardening

## Goal

Prove that KeyDeck can harden the real MCP stdio tool bridge without weakening the replay-safety guarantees established by Proof 0.19.

## Acceptance checks

1. **Permission profiles** — Read Only, Safe Edit, and Full Control enforce tool access before adapter invocation.
2. **Bounded frames** — a server response larger than the configured MCP frame limit is rejected deterministically.
3. **Cancellation safety** — cancelling a non-repeatable tool after its side effect but before the response leaves the operation ambiguous and blocked after restart.
4. **Production adapter seam** — the bridge executes through `mcpbridge.Adapter`, independent of the command-wire client.
5. **Independent local server process** — a separately compiled local MCP server handles real initialize/list/call traffic.
6. **Timeline evidence** — hardening lifecycle events remain in the durable Universal Activity Timeline.
7. **Proof Receipt** — the receipt binds the MCP timeline and final server-state artifact SHA-256 exactly once.

## Permission profiles

```text
Read Only
→ read-only tools

Safe Edit
→ read-only + safe deterministic writes

Full Control
→ read-only + safe writes + destructive/full-control tools
```

A denied tool must be rejected before `Adapter.Invoke`.

## Frame safety

The stdio client uses an explicit configured maximum frame size. Oversized newline-delimited JSON-RPC responses fail with:

`ErrFrameTooLarge`

The proof uses a 512-byte bound and a server that deliberately emits a larger initialize response.

## Cancellation safety

The proof tool `slow.commit`:

1. persists a side effect;
2. delays its response;
3. is cancelled by the caller.

Because it is non-repeatable, KeyDeck leaves the Tool Journal record in `started`. After restart the same operation ID must return `ErrAmbiguousOperation`. The effect count must remain one.

## Adapter boundary

`mcpbridge.Adapter` is the production seam:

```text
KeyDeck Bridge
→ permission policy
→ Tool Journal
→ Adapter.Invoke
→ MCP transport implementation
```

A future official MCP Go SDK adapter may replace the local command adapter without changing permissions or replay policy.

## Local server boundary

The proof communicates with a separately compiled local MCP server process over newline-delimited JSON-RPC stdio. This is an independent proof server process, not a claim that a third-party production MCP server has been integrated.

## Non-claims

Proof 0.20 does not claim:

- official MCP Go SDK package integration;
- third-party MCP server compatibility;
- Secret Broker integration;
- schema-aware argument authorization;
- SaaS connector readiness.
