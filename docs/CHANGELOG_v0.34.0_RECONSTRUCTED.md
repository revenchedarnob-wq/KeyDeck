# v0.34.0-RECONSTRUCTED

## Evidence boundary

- Exact physically recovered ancestor: v0.21.0 / Proof 0.24.
- Reconstructed and re-proven forward line: Proofs 0.25–0.37.
- No claim of byte-identical recovery of lost historical post-v0.21 archives.

## Added

### Secure visual renderer

- `internal/visualshell` with embedded HTML/CSS/JS assets;
- `cmd/keydeck-desktop-ui` product-facing visual shell command;
- explicit loopback-only renderer listener;
- 256-bit in-memory secret launch path;
- exact Host validation against DNS rebinding;
- exact Origin requirement for commands and reconnect;
- strict CSP and security headers;
- no CORS grant;
- strict bounded JSON command bodies;
- sanitized browser projection that omits install and instance identities;
- task create and reconnect routed only through `internal/presentation`;
- session secret rotation on renderer restart;
- deterministic responsive visual assets;
- safe DOM text sinks only.

### Real browser QA

Chromium 144 rendered and executed the exact embedded UI assets with zero console errors. A task created through the visual form appeared in the canonical task list and timeline.

The execution environment blocks direct local HTTP navigation by browser policy, so the browser QA harness bridged UI fetch calls to the real KeyDeck renderer API. Proof 0.37 independently covers the real HTTP security boundary.

## Result

Proof 0.37: **PASS 21/21**.
