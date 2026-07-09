# Proof 0.16 — First Real Provider Evidence Capture

## Goal

Import one real provider capture into the Proof 0.15 conformance framework without broadening its meaning beyond the exact observed behavior.

The real capture came from a Windows x64 one-request gate against the Aerolink Anthropic-compatible endpoint using an intentionally invalid fixture key.

## Captured facts

- provider: `Aerolink`
- API base: `https://capi.aerolink.lat`
- endpoint: `/v1/messages`
- API format: `Anthropic Messages`
- Anthropic version: `2023-06-01`
- requested model: `claude-opus-4-8`
- request count: `1`
- retries: `0`
- automatic retries: `0`
- real API key used: `false`
- `max_tokens`: `1`
- HTTP status: `401`
- exact response body: `{"error":"Unauthorized - Invalid token"}`

Raw capture SHA-256:

`b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3`

Response body SHA-256:

`6fb94d69ebe670927f7223646cff659217301ac3173998e1875842fa1584bf76`

## Required proofs

1. Raw capture SHA-256 matches the Windows gate.
2. Capture schema and embedded response-body hash validate.
3. The capture used exactly one invalid-key request, zero retries and no real key.
4. Exact provider/API/endpoint/version/model identity is preserved.
5. Exact 401 invalid-token response is preserved.
6. The raw capture normalizes into a scoped provider-observation fragment.
7. The normalized fragment binds the raw-capture SHA-256 as provenance.
8. The fragment contains one scoped pre-output invalid-credential observation only.
9. Durable fragment save/load preserves the same evidence identity.
10. The exact captured response authorizes `invalid_key` classification and replacement-key selection.
11. Response-body mismatch fails closed.
12. Endpoint mismatch fails closed.
13. Model/version mismatch fails closed.
14. Expired evidence requires revalidation.
15. Tampered fragment is rejected.
16. The original limitations remain attached and no other provider behavior is claimed.
17. The packaged source evidence contains no user-specific path or real API key.

## Safety boundary

This proof establishes only the exact current invalid-credential behavior for the captured Aerolink endpoint/model request shape.

It does **not** prove:

- credit exhaustion;
- key-specific rate limits;
- provider-wide busy or outage;
- ambiguous transport behavior;
- streaming interruption;
- cache semantics;
- billing semantics;
- general retry policy.

Unknown or mismatched behavior remains conservative.

## Run

```bash
go run ./cmd/proof16
```
