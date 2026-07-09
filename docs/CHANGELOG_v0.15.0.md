# KeyDeck Feasibility Lab v0.15.0 Changelog

## Added

- Proof 0.18 — Bounded Paired Scope Capture: Inconclusive Safe Handling.
- Durable `PairedScopeCapture` decoder/validator for two-credential bounded real-provider evidence.
- SHA-256-sealed `PairedScopeEvidence` for inconclusive scope experiments.
- Durable `PairedScopeEvidenceStore`.
- Explicit replacement transport classification for timeout versus other ambiguous transport failure.

## Real evidence imported

Credential A reproduced the exact known Aerolink HTTP 402 usage-window response.

Credential B was attempted once with the identical request shape and produced no HTTP response before the configured 20-second timeout.

The capture used:

```text
2 total requests
0 retries
max_tokens=1 per credential
no persisted secrets
```

## Safety decision

The replacement timeout is not interpreted as:

- provider-wide outage;
- key-specific failure;
- account-specific failure;
- proof that replacement rotation works.

The exact 402 remains a trusted scope-unknown observation. Automatic replay, key rotation, and semantic continuation remain forbidden.

## Policy

Do not automatically rerun the same paired proof. Move back to local product work and only repeat a real scope experiment when a separately justified bounded gate makes it high value.
