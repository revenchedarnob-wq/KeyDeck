# Proof 0.25 — Manager-Gated MCP Execution Routing (Reconstructed)

## Recovery class

This proof is **reconstructed and re-proven**, not a byte-identical recovery of a lost historical v0.22.0 source archive.

It is built directly on the cryptographically verified physical ancestor:

- `KeyDeck-Feasibility-Lab-v0.21.0.zip`
- SHA-256 `ab9d8909cd5169f0b01fcfb727f246533dfb854c1ea4b3f2efe6182c48e7d6d6`
- Proof 0.24 physically recovered and replayed.

The reconstruction target comes from the saved v0.21.0 continuation contract.

## Acceptance contract

Execution may reach an MCP adapter only when the server is:

1. registered;
2. locally bound;
3. enabled;
4. healthy;
5. discovered with the current trusted schema;
6. explicitly approved for the requested tool;
7. allowed by the active permission profile.

The proof requires these denials before adapter construction:

- unbound;
- disabled;
- unavailable;
- unhealthy;
- unapproved;
- insufficient active profile.

Approved execution must preserve:

`manager gate → adapter construction → Bridge schema → Secret Broker plan → Tool Journal → secret resolution → adapter invocation`

Completed operations must reuse after restart without resolving secrets or invoking the adapter again. Interrupted non-repeatable operations must remain blocked.

## Integrated scenarios

Proof 0.25 passes 10/10 scenarios in `cmd/proof25`.

The real execution scenario uses the exact pinned third-party package:

- `@modelcontextprotocol/server-filesystem@2026.7.4`
- package tarball SHA-256 `7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54`
- package-lock SHA-256 `e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29`
- canonical Server ID `mcp-2249d10f545df5e1a371589e`
- trusted schema SHA-256 `31b3d4e6042e08e91514e42eaddab25ec963664e63dd87081c25577cc48f185f`

## Important evidence correction

Secret-scope denial correctly performs one **value-free** Secret Broker plan before denying the scope. It still performs zero secret resolutions, zero Tool Journal begins, and zero adapter invocations. The proof asserts this exact behavior.
