# Proof 0.36 — Authenticated Core Client + Desktop Presentation Projection Boundary

## Goal

Prove that a product-facing desktop shell can consume KeyDeck state and commands only through the authenticated `keydeck-core` contract, without reading or writing canonical stores directly.

## Required architecture

```text
future visual renderer
        ↓
internal/presentation
        ↓
typed authenticated core client
        ↓
keydeck-core /v1/snapshot + /v1/tasks
        ↓
canonical KeyDeck state
```

The presentation layer is non-canonical and must be disposable.

## Acceptance checks

Proof 0.36 passes only when all 20 scenarios pass:

1. exact identity attestation occurs before projection access;
2. wrong build identity blocks before backend projection;
3. non-loopback runtime metadata is rejected before credential or network use;
4. a core instance change during the identity round trip invalidates attestation;
5. a core instance change during any API round trip invalidates the response;
6. authenticated refresh projects canonical status/tasks/timeline;
7. presentation snapshots exclude the install credential token;
8. task creation routes through the core into canonical task/timeline state;
9. duplicate idempotent create reuses after shell reconnect;
10. conflicting idempotency reuse is rejected without canonical mutation;
11. timeline cursor pagination is monotonic and non-overlapping;
12. stale clients reject a restarted core before backend access;
13. explicit reconnect attests the new instance and restores canonical projection;
14. core unavailability causes an error with no direct-store fallback or mutation;
15. redirect and proxy paths cannot receive the local credential;
16. oversized and malformed responses fail closed;
17. authenticated but internally inconsistent projection payloads are rejected;
18. concurrent refreshes remain read-only and stable;
19. atomic projections remain internally consistent during concurrent commands;
20. explicit disconnect forgets the client and requires re-attestation.

## Core client security rules

- runtime metadata must name an explicit IP loopback endpoint;
- expected build and API identities are exact;
- credential is read only after runtime identity is locally acceptable;
- HTTP proxy use is disabled;
- redirects are disabled;
- response bodies are bounded;
- JSON is strict and single-valued;
- runtime identity is verified before and after every response;
- snapshot task counts, task uniqueness, timeline bounds/order and cursor identity are independently validated.

## Atomic presentation snapshot

`GET /v1/snapshot` returns one presentation-only projection containing:

- authenticated core identity;
- canonical status counts;
- all task summaries;
- one bounded timeline page;
- exact input and next cursors.

The host serializes snapshot construction with command execution so one snapshot cannot mix state from different canonical instants.

## Production shell boundary

`cmd/keydeck-desktop-shell` is intentionally renderer-neutral. It can:

- attest and print one presentation snapshot;
- optionally submit one task-create command from JSON using an explicit idempotency key;
- never open canonical task, timeline or request-journal stores.

A future visual desktop stack must consume this boundary instead of bypassing it.

## Run

```bash
go run ./cmd/proof36
```
