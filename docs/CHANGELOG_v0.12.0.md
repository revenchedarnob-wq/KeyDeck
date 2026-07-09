# KeyDeck Feasibility Lab v0.12.0 Changelog

## Added

- Proof 0.15 — Real Provider Conformance Framework.
- SHA-256-sealed provider evidence bundles.
- Exact provider identity records covering provider, API base/version, model and model revision.
- Evidence validity windows with expiry and revalidation behavior.
- Capture provenance with raw-capture SHA-256 binding.
- Durable provider evidence store with validation on save/load.
- Exact failure observations covering pre-output, mid-stream and unknown phases.
- Provider-specific usage/cache semantics records.
- Conservative evidence registry for exact-match policy activation.
- Conversion of still-valid pre-output evidence into the existing API-key-pool classifier.
- Fake-provider usage evidence on exact error responses and mid-stream events.

## Proven

- Tampered evidence is rejected.
- Expired evidence fails closed and requires revalidation.
- Exact pre-output credit exhaustion, invalid credential and key-specific rate-limit evidence may authorize replacement-key replay.
- Provider-wide busy/outage preserves backup keys.
- Ambiguous transport preserves backup keys and forbids replay.
- Mid-stream key exhaustion with partial output permits semantic continuation only; original request replay remains forbidden.
- Abrupt partial-stream EOF remains ambiguous.
- Unknown provider/model/version/behavior fails closed.
- Cache creation/read and usage fields can be preserved as provider-specific evidence.
- Only pre-output evidence enters the API-key-pool replay classifier.

## Safety boundary preserved

- This milestone does not claim production conformance for any paid provider.
- Unknown behavior remains conservative.
- Real provider profiles still require exact captured behavior, exact identity/version/date, provenance, expiry and revalidation.
- Provider-specific Optimization ON remains disabled without separate verified optimization evidence.

## Validation target

- `go test ./...`
- `go vet ./...`
- `go test -race ./...`
- regression Proofs 0.9 through 0.15.
