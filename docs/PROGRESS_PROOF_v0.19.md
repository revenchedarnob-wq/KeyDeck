# KeyDeck Progress Proof v0.19.0

## New milestone

Proof 0.22 — Immutable Third-Party Local MCP Server passed **8/8 integrated scenarios**.

Proven:

- an exact npm package tarball and installed lock are pinned and SHA-256 verified;
- a real third-party MCP server executes through KeyDeck's existing stdio client and adapter seam;
- `initialize`, `tools/list`, and `read_text_file` work against a temporary allowlisted root;
- KeyDeck permission denial prevents third-party process invocation;
- KeyDeck schema denial prevents third-party process invocation;
- a completed real third-party read is reused after restart with zero additional server calls;
- a real non-repeatable third-party write followed by response loss remains ambiguous and blocked after restart;
- Secret Broker configuration causes zero plans and zero resolutions when tools need no secrets;
- timeline and Proof Receipt evidence bind exact third-party package identity and immutable artifacts;
- trusted package identity cannot be attached to an unrelated adapter.

## Immutable package evidence

- Package: `@modelcontextprotocol/server-filesystem@2026.7.4`
- Tarball SHA-256: `7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54`
- Package-lock SHA-256: `e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29`
- Server identity SHA-256: `2249d10f545df5e1a371589efb2da91030a0eb6a3b8f866c29e33f5679ca76a6`
- Server entrypoint SHA-256: `79cbfe681d9c31b5268036be5740c613f6082a6ac106eb8c091b6eaf35e91573`

## Replay-safety evidence

- Fixture SHA-256: `94d606993a9b207930e0c925f4e43e1afe4ba185a7ae66cba738918ee73cca60`
- Ambiguous write artifact SHA-256: `34ebc435b34ed0ba6fc1ba33c61f561e7702176c7b98e66f0e1150bd202688e2`

## Regression issue found and fixed

The regression chain exposed nondeterministic schema validation caused by Go map iteration. Validation order is now sorted and all field-level rejections preserve the top-level schema-denied classification.

This was a real framework bug found before release, not a third-party MCP failure.

## Validation status before freeze

Passed:

```text
go test ./...
go vet ./...
stateful internal race coverage in package groups
Proofs 0.9–0.20
Proof 0.21 after deterministic-schema fix
Proof 0.22: 8/8
```

## Honest boundary

This milestone proves one exact immutable third-party local MCP package through KeyDeck's safety layers.

It does not yet provide production MCP server discovery/configuration UI, broad connector integration, or the dedicated `codebase-memory-mcp` executable path.

## Next gate

Proof 0.23 — production MCP server registration/discovery and configuration contracts, then connect the already-pinned `codebase-memory-mcp` v0.8.1 executable when immutable retrieval is available without weakening verification.
