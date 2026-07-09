# Reconstruction Boundary — v0.32.0-RECONSTRUCTED

## Exact physically recovered ancestor

The immutable physical forward-line anchor remains:

- `KeyDeck-Feasibility-Lab-v0.21.0.zip`
- SHA-256: `ab9d8909cd5169f0b01fcfb727f246533dfb854c1ea4b3f2efe6182c48e7d6d6`
- integrated milestone: Proof 0.24

## Reconstructed and re-proven line

This source reconstructs and currently re-proves Proofs 0.25–0.35 from that exact ancestor plus saved contracts, milestone evidence, the hardened Proof 0.34 staging provenance, and current-source validation.

Evidence classes remain distinct:

1. **Exact recovered:** v0.21.0 / Proof 0.24.
2. **Reconstructed and re-proven:** Proofs 0.25–0.35.
3. **Historical milestone records:** architecture/evidence references only; not physical byte identity.

## Proof 0.35 boundary

`internal/corehost` and `cmd/keydeck-core` are new reconstructed-line product code. Proof 0.35 does not claim that a historical lost host implementation existed or was recovered.

The host consumes real canonical reconstructed-line APIs and stores. It is not a standalone mock and does not create a competing canonical state model.

## Release naming

The release name must include `RECONSTRUCTED`:

`v0.32.0-RECONSTRUCTED`
