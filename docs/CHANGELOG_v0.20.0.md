# KeyDeck Feasibility Lab v0.20.0 Changelog

## Added

- Proof 0.23 — Production MCP Server Registration and Discovery Contracts.
- New `internal/mcpregistry` package.
- Identity-derived canonical MCP Server IDs.
- Runtime contracts with separate SHA-256 identity.
- Append-only restart-safe registration/discovery event store.
- Real MCP capability discovery cache.
- Tool schema/capability SHA-256.
- Runtime drift detection before process invocation.
- Capability drift rejection without replacing trusted cache.
- Permission proposals with explicit `default_granted=false`.
- Explicit approval conversion into existing KeyDeck permission policy.

## Proven behavior

```text
immutable package identity
→ one canonical Server ID
→ runtime contract stored separately
```

```text
restart
→ registry reload
→ cached tool discovery reused
→ zero server calls
```

```text
runtime drift
→ blocked before discovery process
```

```text
capability/schema drift
→ rejected
→ trusted cache preserved
```

```text
discovered tools
→ suggestions only
→ no automatic grants
```
