# Proof 0.15 — Real Provider Conformance Framework

## Goal

Build the local evidence and policy framework required before KeyDeck can trust any real provider-specific failover, retry, continuation, cache or billing behavior.

This proof does **not** claim that OpenAI, Anthropic, Google, xAI, Aerolink, or any other paid provider is conformant. It proves the evidence path and conservative policy boundary using deterministic fake-provider captures.

## Required proofs

1. Provider evidence is exact by provider, API base, API version, model and model revision.
2. Evidence is dated and has an explicit expiry/revalidation boundary.
3. Evidence provenance is bound to a SHA-256 of the raw capture matrix.
4. The evidence bundle itself is SHA-256 sealed and tampering is rejected.
5. Durable evidence save/load preserves the same evidence identity.
6. Exact pre-output credit exhaustion may authorize replay on a replacement key.
7. Exact invalid credentials may authorize replacement-key selection.
8. Exact key-specific rate limits may authorize replacement-key selection.
9. Provider-wide busy/outage never burns backup keys.
10. Ambiguous transport failure never authorizes replay or key rotation.
11. Mid-stream key exhaustion with partial output may authorize semantic continuation, but never original request replay.
12. Abrupt partial-stream EOF remains ambiguous.
13. Unknown provider/model/version behavior fails closed.
14. Expired evidence requires revalidation and fails closed.
15. Cache and usage fields are captured as provider-specific evidence rather than universal billing assumptions.
16. Only pre-output evidence can compile into the existing API-key-pool classifier.

## Evidence model

A provider evidence bundle records:

- exact provider identity;
- API base and API version;
- model and model revision;
- tested-at and expires-at timestamps;
- capture provenance and raw-capture SHA-256;
- exact failure observations;
- phase (`pre_output`, `mid_stream`, `post_output`, `unknown`);
- status/error/scope/transport facts;
- partial-output and terminal-event facts;
- observed usage/cache fields;
- the narrow policy decision justified by that exact evidence.

Unknown or expired behavior returns a conservative decision:

```text
class = ambiguous
allow_original_replay = false
allow_key_rotation = false
allow_semantic_continuation = false
```

## Mid-stream safety rule

Partial output forbids original request replay.

Exact mid-stream key exhaustion may permit:

```text
replacement worker/key selection
+
semantic continuation from canonical state
```

It may not permit:

```text
blind replay of the original request
```

## Run

```bash
go run ./cmd/proof15
```

## Safety boundary

A real provider profile must still be captured and validated separately for the exact provider/API/model/version/date.

No real provider profile should be marked trusted until the local framework passes and exact provider evidence exists.
