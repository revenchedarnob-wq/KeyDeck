# Proof 0.22 — Immutable Third-Party Local MCP Server

## Goal

Prove that KeyDeck can execute an exact immutable third-party local MCP package through the hardened MCP bridge while KeyDeck retains authority over permissions, argument schemas, Secret Broker policy, Tool Journal replay safety, timeline provenance, and Proof Receipts.

## Pinned third-party package

- Registry: `npm`
- Package: `@modelcontextprotocol/server-filesystem`
- Version: `2026.7.4`
- Canonical source reference: `npm:@modelcontextprotocol/server-filesystem@2026.7.4`
- npm integrity: `sha512-JwEaH4dRRzwcNMwX8WJVCJyXfFxXjFKdgwHxjQhFLhi02kszgyyj611LV9puBLDO1IiDQSCjfKFSPaemegnvwg==`
- Exact package tarball SHA-256: `7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54`
- Exact installed package-lock SHA-256: `e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29`
- Exact package identity SHA-256: `2249d10f545df5e1a371589efb2da91030a0eb6a3b8f866c29e33f5679ca76a6`

The package is executed only against a temporary allowlisted fixture root.

## Acceptance checks

1. **Immutable identity** — exact version, npm integrity, package tarball SHA-256, package-lock SHA-256, installed package metadata, and server entrypoint are verified.
2. **Real third-party call** — real MCP `initialize`, `tools/list`, and `read_text_file` execute through `mcpbridge.Adapter`.
3. **Permission preflight** — a read-only KeyDeck profile denies `write_file` before third-party process invocation.
4. **Schema preflight** — invalid arguments are denied before third-party process invocation.
5. **Completed replay** — a completed third-party read is reused after restart with zero additional server calls.
6. **Ambiguous effect safety** — a real third-party `write_file` effect followed by synthetic response loss remains blocked after restart and is not replayed.
7. **Secret boundary** — a Secret Broker is configured, but filesystem operations cause zero secret plans, zero resolutions, and no raw-secret persistence.
8. **Provenance** — timeline events and a Proof Receipt bind the exact immutable package identity and package artifacts.

## Adapter identity binding

Proof 0.22 adds an explicit `ServerIdentity` model and a bound-adapter check.

A Bridge configured with trusted third-party identity must verify:

```text
Bridge expected identity hash
=
Adapter-bound identity hash
```

Mismatch or an unbound adapter fails before:

- Tool Journal begin;
- timeline execution events;
- third-party process invocation.

This prevents trusted package provenance from being attached to an unrelated adapter.

## Deterministic schema preflight fix

The full regression chain exposed a map-iteration nondeterminism in schema validation. The same invalid object could report different first-denial fields across runs.

Proof 0.22 fixes this by:

- sorting schema field names before validation;
- preserving specific errors such as `ErrSecretReferenceRequired`;
- also wrapping every field-level schema rejection in `ErrArgumentSchemaDenied`.

This makes preflight classification deterministic and restored Proof 0.21 reproducibility.

## Real replay-safety scenario

The non-repeatable write scenario is intentionally strict:

```text
real third-party write_file succeeds
→ file effect exists
→ wrapper loses response
→ Tool Journal remains started/ambiguous
→ process restarts
→ same operation ID is attempted
→ replay forbidden
→ no second adapter call
```

Final written-file SHA-256:

`34ebc435b34ed0ba6fc1ba33c61f561e7702176c7b98e66f0e1150bd202688e2`

## Fixture evidence

Fixture SHA-256:

`94d606993a9b207930e0c925f4e43e1afe4ba185a7ae66cba738918ee73cca60`

The real read must contain the canonical keyword:

`ORANGE-CONTINUITY-22`

## Result

Proof 0.22 passed **8/8 integrated scenarios**.

## Preserved safety boundary

The third-party server remains a tool implementation. It does not own:

- canonical KeyDeck state;
- permissions;
- argument schemas;
- Secret Broker policy;
- Tool Journal replay classification;
- ambiguity handling;
- timeline provenance;
- Proof Receipt completion policy.

## Non-claims

Proof 0.22 does not claim:

- the third-party reference server itself is KeyDeck's production security boundary;
- broad SaaS connector readiness;
- official MCP Go SDK package integration;
- production MCP discovery/configuration UX;
- `codebase-memory-mcp` executable integration in this proof.

The already-pinned `codebase-memory-mcp` v0.8.1 remains the dedicated context-server target when its exact executable can be retrieved without weakening immutable identity verification.
