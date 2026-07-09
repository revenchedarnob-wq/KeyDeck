# Proof 0.38 — Production Desktop Supervisor

## Goal

Prove one non-canonical desktop lifecycle can safely own the production `keydeck-core` and secure visual renderer without weakening their existing boundaries.

## Acceptance contract

The supervisor must:

1. reject wrong child binary identity before launch;
2. reject foreign/stale existing core runtime before any child launch;
3. execute verified private child copies that survive source-file mutation;
4. preserve exact child identity inside the private supervisor runtime;
5. launch core first and renderer only after authenticated core readiness;
6. bind core runtime to exact supervisor owner, PID, build and API;
7. HTTP-attest the renderer before any in-memory open callback;
8. keep core credential and renderer secret out of child command lines/environment;
9. give both children only the same non-secret supervisor ownership binding;
10. keep credentials/secrets out of durable supervisor state;
11. block a second active supervisor before child launch;
12. restart only renderer after renderer failure and rotate renderer secret;
13. re-attest the same exact core before renderer restart;
14. stop renderer before core and remove runtime on clean shutdown;
15. reclaim an expired supervisor lease without reusing foreign children;
16. stop restart storms at a bounded renderer restart limit;
17. treat core runtime identity drift as fatal and shut down owned children;
18. treat unexpected core exit as fatal and never blindly restart core;
19. fail closed on supervisor lease ownership loss while preserving the foreign lease;
20. re-hash the private renderer before restart and block tampered bytes;
21. stop a supervised core gracefully when the supervisor ownership pipe closes.

## Security boundary

- The supervisor owns process lifecycle only. It is not canonical state.
- It imports no task/session/recovery/tool-journal/candidate canonical stores.
- Child binary identity is verified before private copy and again immediately before launch/relaunch.
- Renderer starts only after current core runtime + authenticated API attestation.
- Only a non-secret supervisor instance ID is inherited by children.
- Core bearer credentials and renderer launch secrets never enter supervisor command lines, logs or durable state.
- UI restarts are bounded. Core exits and identity drift are fatal.
- Shutdown order is renderer first, core second.

## Important release correction

The production `keydeck-desktop` command intentionally does not invoke `rundll32`, `xdg-open`, `open`, or another OS helper with the secret renderer URL in a child process command line. Secret-safe automatic visual bootstrap is deferred to the next product gate.

## Result

Proof 0.38: PASS 21/21.

The reconstructed proof harness is behaviorally re-proven after a sandbox reset. Its new deterministic report SHA-256 is recorded by the v0.35 release validation; no byte-identical harness recovery claim is made.
