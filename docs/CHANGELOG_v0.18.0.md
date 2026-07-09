# KeyDeck Feasibility Lab v0.18.0 Changelog

## Added

- Proof 0.21 — MCP Secret Broker + Schema-Aware Authorization.
- New `internal/secretbroker` package.
- Scoped `$secret_ref` argument representation.
- Immutable in-memory Broker with:
  - tool-to-scope authorization;
  - value-free preflight plans;
  - adapter-boundary secret resolution;
  - exact-value redaction.
- MCP argument schemas with:
  - required fields;
  - value types;
  - unknown-field rejection;
  - maximum string length;
  - secret-reference requirements;
  - sensitive-field redaction.
- New bridge ordering:
  - permission;
  - schema;
  - secret-scope plan;
  - Tool Journal decision;
  - secret resolution;
  - adapter invocation.
- `proofreceipt.BuildRedacted`, which preserves historical `Build` receipt identities while allowing redacted timeline summaries in new receipts.
- Separate Proof 0.21 local MCP server that stores only credential hashes.

## Proven behavior

```text
raw credential in model/tool args
→ schema denial
→ no secret plan
→ no adapter call
```

```text
unauthorized secret scope
→ denied before Tool Journal begin
→ no resolution
→ no adapter call
```

```text
completed secret-backed operation
→ policy rechecked
→ previous result reused
→ zero additional secret resolutions
```

```text
tool returns raw credential in error
→ exact-value redaction
→ [REDACTED_SECRET]
→ no raw value in return/journal/timeline/receipt
```

## Preserved safety boundary

MCP remains a protocol layer. KeyDeck still owns permissions, schema policy, secret scope, Tool Journal replay classification, ambiguity handling, canonical state, timeline evidence, and Proof Receipts.
