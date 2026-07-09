# KeyDeck Feasibility Lab v0.19.0 Changelog

## Added

- Proof 0.22 — Immutable Third-Party Local MCP Server.
- Exact pinned npm package identity for `@modelcontextprotocol/server-filesystem@2026.7.4`.
- `internal/mcpbridge.ServerIdentity` with:
  - validation;
  - canonical package reference;
  - stable identity SHA-256.
- Adapter-side identity binding through `ServerIdentityProvider`.
- `NewIdentifiedCommandAdapter`.
- Bridge rejection when expected identity and adapter-bound identity differ.
- Exact third-party package identity in MCP timeline `SourceRef` values.
- Real third-party read and write safety scenarios.

## Fixed

- Deterministic schema-validation order by sorting field names.
- Specific schema errors now also preserve the top-level `ErrArgumentSchemaDenied` classification.
- This removed a regression flake where Proof 0.21 could report different first-denial fields depending on Go map iteration order.

## Proven behavior

```text
exact npm tarball + lock + installed metadata verified
→ identified adapter bound to same identity hash
→ real third-party initialize/list/call
→ KeyDeck timeline records exact package source
```

```text
read-only profile
→ write_file denied
→ zero third-party process calls
```

```text
real write_file effect commits
→ response is lost
→ restart
→ replay remains forbidden
→ effect is not repeated
```

## Preserved safety boundary

KeyDeck still owns permissions, schemas, Secret Broker policy, Tool Journal replay safety, ambiguity handling, canonical state, timeline evidence, and Proof Receipts.
