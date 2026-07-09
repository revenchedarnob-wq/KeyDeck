# KeyDeck Progress Proof v0.20.0

## New milestone

Proof 0.23 — Production MCP Server Registration and Discovery Contracts passed **8/8 integrated scenarios**.

Proven:

- immutable MCP registration is created once and deduplicated;
- one canonical Server ID derives only from immutable package identity;
- runtime configuration has a separate digest and cannot silently drift;
- a real third-party server exposes 14 tools through real MCP discovery;
- capability discovery is persisted with exact identity/runtime/schema hashes;
- restart reuses the cached discovery with zero server calls;
- runtime drift is blocked before process invocation;
- capability/schema drift is rejected without replacing trusted cache;
- permission proposals auto-grant nothing;
- explicit approval of one read tool leaves all other tools denied;
- registry and Proof Receipt evidence bind canonical Server ID and schema digest.

## Exact evidence

- Server ID: `mcp-2249d10f545df5e1a371589e`
- Identity SHA-256: `2249d10f545df5e1a371589efb2da91030a0eb6a3b8f866c29e33f5679ca76a6`
- Runtime SHA-256: `f5e3fafdf42658bb1f65f96018a68df2a0bfd4d5a771ce8c48a6b66cbd27cbc6`
- Tool-schema SHA-256: `31b3d4e6042e08e91514e42eaddab25ec963664e63dd87081c25577cc48f185f`
- Registry SHA-256: `14ad7aebf598b265b57629e7b35b41b9ffd5b3130c71eab01a3cde436f2b27c4`
- Tool count: 14

## Validation

Passed:

```text
go test ./...
go vet ./...
go test -race ./internal/mcpregistry ./internal/mcpbridge
Proofs 0.9–0.23
```

The broader stateful race suite was already clean in immutable v0.19.0; v0.20.0 changes are isolated to the new registry package/proof and were race-checked directly.

## Next gate

Proof 0.24 — durable local runtime bindings and a server-manager contract above the registry, followed by the pinned `codebase-memory-mcp` v0.8.1 executable when immutable retrieval is available.
