# Proof 0.24 — Durable Local Runtime Bindings + MCP Server Manager Contract

## Goal

Prove that KeyDeck can keep portable MCP package registration/discovery separate from machine-local execution state, then expose one durable manager view over registration, discovery, local binding, health, enable state and explicit permission approvals.

A missing or broken machine-local runtime must never delete or rewrite the portable registration/discovery record. Repair must be an explicit rebind event.

## Portable identity retained from Proof 0.23

- Canonical Server ID: `mcp-2249d10f545df5e1a371589e`
- Identity SHA-256: `2249d10f545df5e1a371589efb2da91030a0eb6a3b8f866c29e33f5679ca76a6`
- Runtime contract SHA-256: `f5e3fafdf42658bb1f65f96018a68df2a0bfd4d5a771ce8c48a6b66cbd27cbc6`
- Tool-schema SHA-256: `31b3d4e6042e08e91514e42eaddab25ec963664e63dd87081c25577cc48f185f`
- Portable registry SHA-256: `14ad7aebf598b265b57629e7b35b41b9ffd5b3130c71eab01a3cde436f2b27c4`

## New local manager layer

`internal/mcpmanager` adds a separate append-only machine-local state store.

It owns only local execution state:

- absolute runtime path;
- absolute entrypoint path;
- values for declared non-secret runtime argument slots;
- explicit enable/disable state;
- explicit schema-bound tool approvals;
- binding-specific health observations;
- explicit bind/rebind history.

It does not own or mutate:

- immutable package identity;
- canonical Server ID;
- portable runtime contract;
- trusted capability discovery;
- Secret Broker values;
- Tool Journal replay policy;
- canonical task/session state.

## Execution ordering and safety rules

```text
portable registration/discovery
        ↓
local binding
        ↓
explicit approvals
        ↓
enable state
        ↓
binding-specific health
        ↓
one manager view
```

Conflicting local paths cannot replace an existing binding through `Bind`. A different binding requires an explicit `Rebind` event and non-empty reason.

Permission approvals are tied to the exact trusted discovery schema digest. Unknown tools and stale/wrong schema digests are rejected. No discovered tool is granted by default.

Health observations are tied to the exact current binding SHA-256. Rebinding invalidates the previous effective health state.

## Real health check

The proof uses the exact pinned package:

- `@modelcontextprotocol/server-filesystem@2026.7.4`
- tarball SHA-256 `7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54`
- package-lock SHA-256 `e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29`

A machine-local binding launches the real server over stdio and performs:

1. initialize;
2. initialized notification;
3. tools/list.

Healthy result: 14 tools.

## Acceptance checks

1. Portable registration/discovery survive with no local binding.
2. Machine-local binding preserves canonical Server ID and blocks silent mutation.
3. Permission approvals are explicit, schema-bound and default-deny.
4. Real pinned third-party runtime health check succeeds.
5. One manager view combines portable and machine-local state.
6. Enable/disable state survives restart.
7. Missing local runtime becomes unavailable without deleting portable state.
8. Explicit repair/rebind restores health without changing canonical identity.
9. Final manager state survives restart with no raw secret persistence.
10. Proof Receipt binds separate portable-registry and local-manager artifacts.

## Reference release-run local-state evidence

- Healthy binding SHA-256 (machine-local reference): `a1977294e696c41b3827e1d156ba6f77b8a9c6675824190049b501bec370d25a`
- Broken binding SHA-256 (machine-local reference): `5ed3cec84fae8acc92ec5e84450cb7aed4c00e209368da5bd165d0e1bcf26433`
- Local manager event-log SHA-256 (machine-local reference): `66fb61fdd5c3756d782362cce2ef4caf6b0317d3f3052da0bf8b68d321e2c1f6`
- Manager events: 10

These three local-state hashes intentionally include absolute machine-local binding paths. They are deterministic for the reference release run but are expected to differ when the same proof package is installed at a different local path. Portable Server ID, identity, runtime-contract, schema and registry hashes remain path-independent.

## Result

Proof 0.24 passed **10/10 integrated scenarios**.

## Non-claims

Proof 0.24 does not yet prove:

- execution routing gated by the manager's `Ready` state;
- automatic package installation/update;
- user-facing desktop management UI;
- broad SaaS integrations;
- `codebase-memory-mcp` executable integration.

Those remain later gates.
