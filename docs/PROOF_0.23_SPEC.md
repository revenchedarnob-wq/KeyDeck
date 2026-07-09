# Proof 0.23 — Production MCP Server Registration and Discovery Contracts

## Goal

Prove that KeyDeck can register an immutable MCP server once, keep package identity separate from runtime configuration, discover real tool capabilities, persist and reuse the discovery safely after restart, detect runtime/capability drift, and propose permissions without granting any tool automatically.

## Exact server identity

- Canonical Server ID: `mcp-2249d10f545df5e1a371589e`
- Identity SHA-256: `2249d10f545df5e1a371589efb2da91030a0eb6a3b8f866c29e33f5679ca76a6`
- Package: `@modelcontextprotocol/server-filesystem@2026.7.4`
- Package tarball SHA-256: `7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54`
- Package-lock SHA-256: `e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29`

The canonical Server ID derives only from immutable package identity. Runtime configuration has a separate digest.

## Runtime contract

Runtime SHA-256:

`f5e3fafdf42658bb1f65f96018a68df2a0bfd4d5a771ce8c48a6b66cbd27cbc6`

The contract contains logical execution requirements rather than user-specific absolute paths:

- transport: stdio;
- runtime: node;
- entrypoint: `dist/index.js`;
- protocol: `2025-11-25`;
- max frame bytes: 4 MiB;
- argument slot: `allowed_root`.

Changing the runtime contract does not change Server ID, but it creates a registration conflict/drift condition.

## Acceptance checks

1. Immutable registration is appended once and exact duplicate registration is deduplicated.
2. Canonical Server ID stays stable across runtime changes while conflicting runtime contracts are rejected.
3. Real third-party discovery persists exact identity, runtime and tool-schema digests.
4. Restart reuses the persisted discovery without launching the server again.
5. Runtime drift is rejected before discovery invocation.
6. Capability/schema drift is rejected without replacing the trusted cache.
7. Permission proposals grant nothing until explicit approval.
8. Registry artifact and Proof Receipt bind Server ID and schema digest.

## Real discovery evidence

- Tool count: 14
- Schema SHA-256: `31b3d4e6042e08e91514e42eaddab25ec963664e63dd87081c25577cc48f185f`
- Registry event-log SHA-256: `14ad7aebf598b265b57629e7b35b41b9ffd5b3130c71eab01a3cde436f2b27c4`

The real discovery included `read_text_file` and `write_file`.

## Permission proposal boundary

Discovery may suggest a minimum permission profile, but every proposal has:

```text
default_granted = false
```

In the proof, only `read_text_file` is explicitly approved. `write_file`, `edit_file`, and every other discovered tool remain denied.

Unknown capability names are conservatively suggested as full-control, still with no automatic grant.

## Persistence model

The registry is append-only JSONL with versioned, sequence-checked events:

- `registered`;
- `discovered`.

On restart, registrations and discovery snapshots are revalidated before use.

A capability revalidation mismatch returns `ErrCapabilityDrift` and leaves the previously trusted cache untouched.

## Result

Proof 0.23 passed **8/8 integrated scenarios**.

## Preserved ownership

The registry does not own:

- permission grants;
- argument schemas used at execution;
- Secret Broker values;
- Tool Journal replay classification;
- canonical task/session state;
- timeline or Proof Receipt completion policy.

## Non-claims

Proof 0.23 does not yet provide:

- user-facing MCP server management UI;
- durable machine-specific runtime bindings;
- automatic package installation/update;
- broad SaaS integration;
- `codebase-memory-mcp` executable integration.
