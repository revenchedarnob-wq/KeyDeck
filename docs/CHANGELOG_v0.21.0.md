# KeyDeck Feasibility Lab v0.21.0 Changelog

## Added

- Proof 0.24 — Durable Local Runtime Bindings + MCP Server Manager Contract.
- New `internal/mcpmanager` package.
- Machine-local absolute runtime/entrypoint bindings separate from portable registration.
- Explicit `Bind` versus `Rebind` semantics.
- Binding SHA-256 identities.
- Durable enable/disable state.
- Explicit schema-bound permission approval state.
- Binding-specific health observations.
- One server-manager view over registration, discovery, binding, health, enable state and grants.
- Restart-safe append-only local manager events.

## Proven behavior

```text
portable MCP registration/discovery
→ survives with no local runtime binding
```

```text
new machine-local path conflicts
→ silent Bind rejected
→ explicit Rebind(reason) required
```

```text
missing runtime
→ health = unavailable
→ portable registration/discovery preserved
```

```text
repair
→ explicit rebind event
→ real initialize/tools-list health check
→ ready state restored
```

```text
discovered tools
→ no grants by default
→ exact schema-bound approval only
```
