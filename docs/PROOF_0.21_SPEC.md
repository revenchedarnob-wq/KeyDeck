# Proof 0.21 — MCP Secret Broker + Schema-Aware Authorization

## Goal

Prove that KeyDeck can pass secrets to MCP tools without placing raw secret values in model/tool arguments, the Tool Journal, the Universal Activity Timeline, server-state artifacts, or Proof Receipts.

## Acceptance checks

1. **Scoped references** — tool arguments use `$secret_ref` objects instead of raw values.
2. **Permission order** — permission denial happens before secret planning or adapter invocation.
3. **Schema order** — argument schema denial happens before secret planning or adapter invocation.
4. **Scope order** — secret-scope denial happens before Tool Journal begin, secret resolution, or adapter invocation.
5. **Replay safety** — a completed secret-backed operation is reused without resolving the secret again.
6. **Redaction** — a tool result/error containing the runtime secret is redacted before return, journal persistence, or timeline persistence.
7. **Persistence boundary** — the raw secret is absent from journal, timeline, task, and MCP server-state artifacts.
8. **Proof Receipt** — a redacted timeline summary is included without the raw value and the receipt is stored exactly once.

## Preflight and execution order

```text
permission
→ argument schema
→ secret-scope plan (value-free)
→ Tool Journal decision
→ secret resolution
→ Adapter.Invoke
```

This order matters.

- Permission and schema denials touch neither the Secret Broker nor the adapter.
- Scope denial creates no Tool Journal operation.
- Completed replay rechecks policy but returns the completed result without resolving the secret again.
- Raw secret values exist only after the journal decides the operation must execute.

## Scoped reference format

```json
{
  "$secret_ref": {
    "scope": "provider.read",
    "name": "primary"
  }
}
```

The reference is safe to hash and persist. The value is not.

## Secret Broker ownership

The Broker owns:

- in-memory secret values;
- tool-to-scope authorization;
- value-free preflight plans;
- adapter-boundary resolution;
- exact-value redaction helpers.

It does not own:

- Tool Journal replay policy;
- canonical task/session state;
- permission profiles;
- MCP transport;
- Proof Receipt completion policy.

## Schema-aware authorization

Each tool can define:

- required fields;
- value types;
- allowed/unknown fields;
- maximum string length;
- fields that must be scoped secret references;
- sensitive fields for timeline summary redaction.

A raw string in a field requiring a secret reference is rejected before any secret planning or adapter call.

## Redaction proof

The proof server deliberately returns an MCP tool error containing the received runtime credential.

KeyDeck must convert it to:

```text
upstream rejected credential [REDACTED_SECRET]
```

before it can enter:

- the returned error;
- Tool Journal failure state;
- timeline summary;
- redacted Proof Receipt.

## Real MCP boundary

The proof uses a separately compiled local MCP server over newline-delimited JSON-RPC stdio. The server validates only the SHA-256 of the received runtime secret and stores only credential hashes.

## Preserved historical safety

Proofs 0.9–0.21 must all pass after this change.

The proof must not weaken:

- completed non-repeatable result reuse;
- ambiguous non-repeatable blocking;
- declared idempotent retry;
- cancellation ambiguity;
- permission-profile enforcement;
- bounded MCP frame rejection.

## Non-claims

Proof 0.21 does not claim:

- DPAPI-backed production secret storage for this new Broker abstraction;
- third-party MCP server integration;
- official MCP Go SDK package integration;
- broad SaaS connector readiness;
- multi-user or remote secret federation.
