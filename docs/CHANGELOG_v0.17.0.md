# KeyDeck Feasibility Lab v0.17.0 Changelog

## Added

- Proof 0.20 — MCP Bridge Hardening.
- Explicit `mcpbridge.Adapter` production transport seam.
- `CommandAdapter` for the existing local stdio wire client.
- Permission profiles:
  - `read_only`;
  - `safe_edit`;
  - `full_control`.
- Per-tool minimum permission profile mapping.
- Explicit bounded MCP frame reader and `ErrFrameTooLarge`.
- Context-aware cancellation classification.
- Cancellation timeline dispositions:
  - `mcp_tool_cancelled_ambiguous` for non-repeatable work;
  - `mcp_tool_cancelled_retryable` for declared idempotent work.
- Independent proof20 local MCP server process.
- Unit tests for permission profiles and bounded frame behavior.

## Proven behavior

```text
Read Only profile
→ safe.write denied
→ adapter not called
```

```text
oversized MCP response
→ deterministic ErrFrameTooLarge
```

```text
non-repeatable tool commits side effect
→ caller cancels before response
→ journal remains started / ambiguous
→ restart blocks replay
→ side effect count remains one
```

```text
Bridge
→ mcpbridge.Adapter
→ transport implementation
```

## Preserved safety boundary

MCP remains a protocol layer. KeyDeck continues to own permissions, Tool Journal policy, ambiguity handling, canonical state, timeline evidence, and Proof Receipts.
