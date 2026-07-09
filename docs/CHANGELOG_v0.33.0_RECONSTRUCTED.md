# v0.33.0-RECONSTRUCTED

## Evidence boundary

- Exact physically recovered ancestor: v0.21.0 / Proof 0.24.
- Reconstructed and re-proven forward line: Proofs 0.25–0.36.
- No claim of byte-identical recovery of lost historical post-v0.21 archives.

## Added

### Typed authenticated core API client

- exact build/API/install/instance attestation;
- explicit loopback runtime verification;
- hardened no-proxy/no-redirect transport;
- bounded strict JSON responses;
- runtime identity verification before and after every response;
- typed status/task/timeline/create operations;
- typed atomic projection operation.

### Atomic presentation projection

- authenticated `GET /v1/snapshot`;
- one consistent identity/status/task/timeline projection;
- serialized against canonical command execution;
- exact timeline cursor binding;
- independent client validation of counts, duplicate task summaries and timeline ordering.

### Renderer-neutral desktop shell boundary

- `internal/presentation` imports only `internal/corehost` from the KeyDeck core;
- no direct canonical store access or persistence methods;
- explicit connect/disconnect/re-attestation lifecycle;
- idempotent task creation through the authenticated core;
- `cmd/keydeck-desktop-shell` snapshot and task-create command surface.

## Security and correctness bugs found before release

1. failed reconnect could leave a stale client attached;
2. attestation lacked a post-response runtime check;
3. normal API calls lacked a post-response runtime check;
4. three-request refresh could mix state from different canonical instants;
5. authenticated projection responses were not independently checked for internal consistency.

All five were fixed and covered by tests/proof scenarios before sealing.

## Result

Proof 0.36: **PASS 20/20**.
