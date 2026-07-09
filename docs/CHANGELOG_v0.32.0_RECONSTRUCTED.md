# v0.32.0-RECONSTRUCTED

## Evidence boundary

- Exact physically recovered ancestor: v0.21.0 / Proof 0.24.
- Reconstructed and re-proven forward line: Proofs 0.25–0.35.
- No claim of byte-identical recovery of lost historical post-v0.21 archives.

## Added

### Production `keydeck-core` host

- one real product-facing core process;
- explicit loopback-only HTTP listener;
- per-install random local bearer credential;
- generic unauthenticated liveness only;
- authenticated identity, status, task and timeline endpoints;
- exact runtime attestation;
- canonical task creation through existing Task Manager and Activity Timeline;
- durable request journal and idempotent command reuse;
- restart-safe crash-window reconciliation;
- single-owner lease with stale reclaim;
- strict bounded request decoding;
- fatal host-error channel for process supervision.

## Security hardening found during Proof 0.35

1. **Lease heartbeat ownership gap**
   - Refresh originally rewrote the heartbeat without proving the lease was still owned by the same instance.
   - Refresh now verifies the exact owner identity first.
   - Ownership loss is fatal and shuts down serving.

2. **Ignored heartbeat failures**
   - Heartbeat errors could previously leave the host serving.
   - Any refresh failure now reports a fatal host error and fails closed.

3. **Unsafe lease release on unverifiable ownership**
   - Release could remove the lease directory after an owner-read failure.
   - Release now refuses to delete anything unless exact ownership is proven.

4. **Runtime-attestation credential exfiltration risk**
   - A tampered runtime address could previously point attestation away from loopback.
   - Runtime metadata must now contain an explicit loopback address before credential use.
   - Redirects and proxies are disabled for attestation.

5. **Unexpected HTTP-server failure retained ownership heartbeat**
   - Server failure now stops the heartbeat so dead ownership can become reclaimable.

6. **Proof determinism**
   - Runtime receipt identities legitimately include real event timestamps.
   - The acceptance report now proves receipt existence/binding without embedding run-variable receipt IDs, allowing byte-for-byte deterministic Proof 0.35 reports.

## Result

Proof 0.35: **PASS 16/16**.
