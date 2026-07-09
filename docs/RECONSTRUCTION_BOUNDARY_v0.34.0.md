# Reconstruction Boundary — v0.34.0-RECONSTRUCTED

## Exact physically recovered ancestor

- `KeyDeck-Feasibility-Lab-v0.21.0.zip`
- SHA-256: `ab9d8909cd5169f0b01fcfb727f246533dfb854c1ea4b3f2efe6182c48e7d6d6`
- integrated milestone: Proof 0.24

## Reconstructed and re-proven line

This source reconstructs and re-proves Proofs 0.25–0.37 from the exact ancestor plus saved contracts, preserved evidence and current-source validation.

Evidence classes remain distinct:

1. **Exact recovered:** v0.21.0 / Proof 0.24.
2. **Reconstructed and re-proven:** Proofs 0.25–0.37.
3. **Historical milestone records:** architecture/evidence references only; not physical byte identity.

## Proof 0.37 boundary

`internal/visualshell`, its embedded assets, `cmd/keydeck-desktop-ui` and `cmd/proof37` are new reconstructed-line product code.

No lost historical GUI implementation is claimed as recovered.

No third-party GUI framework is frozen. The visual renderer is intentionally implemented with embedded assets and the Go standard library so the proof can establish the product boundary before a future native window technology is chosen.

The browser renderer remains presentation-only and non-canonical. It must not access canonical stores, runtime metadata or credentials directly.

## Browser QA limitation

Direct Chromium navigation to local HTTP URLs is blocked by the execution environment's administrator policy. The release browser QA therefore renders the exact embedded assets and bridges fetches to the real KeyDeck renderer API. Real HTTP loopback/Host/Origin/CSP/CORS boundaries are independently exercised by Proof 0.37.

## Release naming

The release name must include `RECONSTRUCTED`:

`v0.34.0-RECONSTRUCTED`
