# KeyDeck Feasibility Lab v0.14.0 Changelog

## Added

- Proof 0.17 — Second Real Provider Evidence Capture.
- Real Aerolink HTTP 402 included-usage-window-limit evidence from a one-request, zero-retry Windows capture.
- `CaptureRequest.SecretPersisted` evidence field.
- `NormalizeUsageWindowLimitCapture`, which preserves the exact observed behavior without inventing key scope.
- Multi-fragment matching for multiple real behaviors on the same provider identity and endpoint.

## Fixed

The incremental `FragmentRegistry` previously returned after the first same-provider/same-endpoint fragment mismatch. That meant a second exact behavior for the same endpoint could never be reached.

The registry now:

1. scans all matching provider/endpoint fragments;
2. returns an exact valid response match;
3. requires revalidation for an exact expired match;
4. fails closed on invalid/tampered matching evidence;
5. preserves conservative behavior when no exact response was observed.

## Safety decision

The exact Aerolink 402 response proves a 5-hour included-usage window limit for the captured provider/API/model request shape.

It does **not** by itself prove:

- key-specific scope;
- account-specific scope;
- provider-wide scope;
- safe replacement-key rotation;
- safe original replay.

Therefore the exact 402 is stored as trusted evidence with a conservative `ambiguous` policy and `require_input=true` until a paired replacement-key scope proof passes.
