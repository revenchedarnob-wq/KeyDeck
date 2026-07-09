# Proof 0.37 — Secure Visual Desktop Renderer

## Goal

Prove a real visual KeyDeck surface without allowing the renderer to become canonical state or to bypass the authenticated presentation boundary.

The renderer must consume `internal/presentation` only. It must not open task stores, timelines, request journals, runtime metadata or credentials directly.

## Architecture

```text
browser renderer
    ↓ secret in-memory launch path + same-origin API
internal/visualshell
    ↓
internal/presentation
    ↓ authenticated typed core client
keydeck-core
    ↓
canonical KeyDeck managers/stores
```

The visual renderer is presentation-only and non-canonical.

## Security contract

- explicit loopback-only renderer binding;
- 256-bit in-memory launch secret;
- public root and wrong secret paths reveal nothing;
- exact Host validation blocks DNS rebinding;
- mutating requests require the exact renderer Origin;
- no CORS grant;
- strict CSP, no-referrer, anti-frame and no-sniff headers;
- embedded deterministic assets only;
- no core credential, install ID or instance ID in browser state;
- bounded strict JSON command bodies;
- generic command errors without credential/path disclosure;
- renderer reconnect re-attests the core through `internal/presentation`;
- stale core instances fail closed with no direct-store fallback;
- session launch secret is never persisted and rotates on renderer restart;
- canonical text uses safe DOM text sinks, not HTML injection APIs.

## Acceptance result

Proof 0.37 passes only if all 21 scenarios pass:

1. renderer binds loopback with an unpersisted secret launch path;
2. wrong secret and DNS-rebinding Host are blocked before presentation;
3. embedded visual assets are deterministic and external-resource free;
4. secret launch token never appears in visual assets;
5. canonical text uses safe DOM text sinks without HTML injection APIs;
6. strict security headers are present and no CORS access is granted;
7. browser state excludes core credential/install/instance secrets;
8. visual projection consumes the authenticated presentation snapshot;
9. task command reaches canonical core only through presentation;
10. duplicate visual command reuses the idempotent canonical result;
11. conflicting idempotency key is generic and non-mutating;
12. cross-origin mutation is blocked before presentation;
13. oversized and malformed commands fail closed;
14. stale core instance blocks projection without store fallback;
15. explicit reconnect re-attests the replacement core;
16. cross-origin reconnect is blocked before re-attestation;
17. renderer session secret is absent from core durable state;
18. snapshots stay internally consistent during concurrent commands;
19. renderer restart rotates the secret and invalidates the old path;
20. renderer restart reconnects to the same canonical state;
21. renderer close disconnects presentation and stops serving.

## Browser QA boundary

The release gate additionally renders and interacts with the exact embedded HTML/CSS/JS bytes in Chromium 144.

The execution environment blocks direct browser navigation to local HTTP URLs by administrator policy. Therefore Chromium QA uses a harness-only fetch bridge to the real KeyDeck visual API. The portable proof independently exercises the real renderer's loopback, Host, Origin, CSP, CORS and secret-path security boundaries.

Browser QA must prove:

- page title and connected state render;
- zero browser console errors;
- a real task can be created through the UI;
- the canonical task appears visually;
- task and timeline counts update;
- no external resource dependency is required.
