# KeyDeck Feasibility Lab v0.7.0 Changelog

Base: verified `KeyDeck-Feasibility-Lab-v0.6.0.zip`.

## Added

- Proof 0.10 — Provider / Optimizer Conformance.
- `internal/conformance` provider-profile and optimizer-evidence layer.
- Exact provider/version/date/evidence gating for provider-specific behavior.
- Byte-preserving Optimization OFF behavior.
- Verified-only Optimization ON activation.
- Provider-specific classifier injection into the elastic key pool.
- Fake-provider custom error fixture for exact provider conformance proofs.
- Proof 0.10 machine-readable report and specification.

## Safety behavior proven

- unknown 429 remains ambiguous;
- ambiguous 502 is not replayed or rotated;
- provider-wide busy preserves backups;
- cost-thrash blocks before backup consumption;
- exact evidenced key-scoped exhaustion may rotate;
- mismatched optimizer evidence fails closed without changing request bytes.

## Not claimed

This release does not claim real-provider conformance for Aerolink, Anthropic, OpenAI, Google, xAI, or any other production provider. The fixture profile proves architecture and control flow only.
