# KeyDeck Feasibility Lab v0.13.0 Changelog

## Added

- Proof 0.16 — First Real Provider Evidence Capture.
- `ProviderObservationFragment`, an incremental real-provider evidence unit that does not require inventing unobserved cache/billing semantics.
- Exact request-shape and response-body-hash matching for narrowly scoped real-provider behavior.
- Durable provider-fragment storage with SHA-256 sealing and tamper detection.
- Fragment expiry/revalidation and conservative mismatch handling.
- Real Aerolink invalid-credential evidence captured on Windows with one intentionally invalid-key request and zero retries.

## Safety decision

Only the exact captured Aerolink 401 response/body hash for the exact provider/API/endpoint/model/version identity is trusted as `invalid_key`.

Any endpoint, model/version, response-body, expiry or tamper mismatch fails closed as ambiguous.

No other Aerolink failure, streaming, cache, billing or retry behavior is inferred.
