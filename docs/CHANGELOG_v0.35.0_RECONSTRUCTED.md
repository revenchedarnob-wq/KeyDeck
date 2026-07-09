# v0.35.0-RECONSTRUCTED

## Evidence boundary

- exact physical ancestor: v0.21.0 / Proof 0.24;
- reconstructed and re-proven current line: Proofs 0.25–0.38;
- no byte-identical recovery claim for lost historical post-v0.21 archives.

## Added

### Production Desktop Supervisor

- `internal/supervisor` production process lifecycle boundary;
- `cmd/keydeck-desktop` production desktop supervisor command;
- exact core and renderer hash injection at release build time;
- private per-instance verified execution copies;
- one supervisor owner identity shared with children through a non-secret environment binding;
- core readiness/identity/authentication before renderer launch;
- renderer HTTP/security attestation before in-memory open callback;
- bounded renderer-only restart policy;
- fatal core exit, runtime identity drift and supervisor-lease ownership loss;
- renderer-before-core shutdown;
- supervisor-pipe child ownership and graceful shutdown.

## Security hardening found before release

1. renderer private bytes are re-hashed before every restart launch;
2. inherited ownership environment entries are replaced, not duplicated;
3. `Start` and `Close` lifecycle transitions are serialized;
4. production desktop automatic OS opener was removed because passing the secret renderer URL to another process would leak it through a command line;
5. foreign supervisor lease ownership is preserved on fail-closed shutdown.

## Result

Proof 0.38: PASS 21/21.

Automatic secret-safe visual bootstrap is not claimed in this release.
