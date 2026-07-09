# Proof 0.26 — Production Context Scout/Compiler (Reconstructed)

## Recovery class

This proof is **reconstructed and re-proven** from the exact recovered v0.21.0 / Proof 0.24 source ancestor plus reconstructed Proof 0.25. It is not a claim that the lost historical v0.23.0 archive was recovered byte-for-byte.

## Acceptance contract

The production Context Scout/Compiler must prove:

1. local-only project source fingerprinting where `.env` changes are irrelevant and relevant source edits invalidate;
2. cache identity binds exact provider Server ID, provider schema SHA, project root, objective, max chars and max files;
3. packet identity binds cache key, current source fingerprint and packet SHA-256;
4. a fresh verified packet reuses after restart with zero provider calls;
5. a fresh verified packet still reuses while the provider is disabled;
6. a relevant source change invalidates the old packet;
7. a stale or missing packet cannot silently degrade into local-source fallback when the configured provider is blocked;
8. `ExecutionRouter.Preflight` applies the same manager readiness/current-schema/tool/profile gates without adapter construction, Tool Journal begin, secret resolution or server invocation;
9. a disabled provider blocks stale rebuild before provider execution or packet persistence;
10. re-enabling rebuilds a new source-bound packet with fresh operation identities;
11. hygiene enforces character/file budgets, duplicate rejection, path containment, forbidden exact-value exclusion and structural receipts;
12. lower-ranked omitted evidence is recorded explicitly;
13. Proof Receipt provenance binds the packet store, packet artifacts and exact provider Server ID/schema evidence.

## Integrated proof

`cmd/proof26` freezes the contract into 10 integrated scenarios and uses an identity-bound deterministic in-process provider fixture with the four production Context Compiler tools:

- `index_repository`
- `get_architecture`
- `search_graph`
- `trace_path`

The provider still enters through `ExecutionRouter`, `mcpbridge.Bridge`, schema validation, Tool Journal and durable timeline evidence.

## No silent fallback rule

The lower-level Context Compiler may retain its historical exact-source fallback behavior for generic use. The production Coordinator must preflight the configured provider before rebuilding any stale or missing packet. This preserves fresh offline reuse while making stale rebuilds fail closed when provider safety/readiness is denied.
