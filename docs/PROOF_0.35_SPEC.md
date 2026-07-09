# Proof 0.35 — Production Core Host + Authenticated Loopback Control Plane

## Goal

Prove that KeyDeck can run as one real local core process behind a narrow authenticated loopback boundary without exposing canonical stores directly to future desktop UI code.

## Required architecture

```text
Desktop/UI client
        ↓
authenticated loopback API
        ↓
keydeck-core
        ↓
canonical KeyDeck stores and managers
```

The UI must not read or mutate task, timeline, request-journal, or credential files directly.

## Acceptance checks

Proof 0.35 passes only when all 16 scenarios pass:

1. install credential generated once and reused after restart;
2. non-loopback binding rejected before listen;
3. unauthenticated health is generic and authenticated APIs block before backend access;
4. wrong token denied and correct token authorized;
5. identity attestation binds exact build/API/install/instance identity;
6. authenticated task creation uses the canonical Task Manager and Activity Timeline;
7. duplicate idempotent command reuses one durable result without duplicate state;
8. conflicting reuse of an idempotency key is rejected without mutation;
9. bounded strict JSON and pending-only Task Contract rules block invalid mutation;
10. timeline pagination and status project canonical state;
11. active single owner is blocked and stale lease can be reclaimed;
12. lease ownership loss forces the host fail-closed and preserves the foreign owner lease;
13. crash-window canonical task creation reconciles into the request journal;
14. restart reopens canonical state and reuses completed command results;
15. install credential is absent from runtime metadata, canonical stores, HTTP responses and Proof Receipt evidence;
16. Proof Receipt binds core-host runtime, canonical task, timeline and authenticated request journal artifacts.

## Security properties

- explicit IP loopback only;
- one per-install 256-bit random bearer token;
- unauthenticated `/healthz` returns only `ok`;
- every `/v1/*` endpoint requires authentication;
- bearer comparison uses fixed-length SHA-256 digests and constant-time comparison;
- runtime attestation rejects non-loopback addresses before reading/sending the credential;
- attestation disables HTTP redirects and proxy use;
- request bodies are size bounded and strict JSON;
- idempotent commands are durably journaled;
- active owner lease is heartbeat-backed;
- heartbeat verifies exact ownership before refresh;
- lost ownership or HTTP-server failure stops ownership heartbeat and becomes fatal;
- release never removes a lease it cannot prove it owns.

## Durable command boundary

The current production command surface is intentionally narrow:

```text
POST /v1/tasks
```

It uses:

```text
Idempotency-Key
→ exact request hash
→ canonical task state
→ canonical timeline event
→ durable request journal
```

Crash windows reconcile from already-created canonical task/timeline state instead of duplicating the command.

## Current limitation

The dedicated credential file is permission-restricted and excluded from canonical evidence. Final Windows secure-at-rest hardening with DPAPI and explicit ACL validation is not yet claimed by this Linux-hosted proof and remains a future Windows release gate.

## Run

```bash
go run ./cmd/proof35
```
