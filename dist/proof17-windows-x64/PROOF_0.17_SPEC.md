# Proof 0.17 — Second Real Provider Evidence Capture

## Goal

Import the first real Aerolink HTTP 402 usage-window-limit capture without overclaiming that the behavior is key-specific, account-specific, provider-wide, replay-safe, or rotation-safe.

The proof must also show that multiple exact real behaviors for the same provider identity and endpoint can coexist in the incremental fragment registry.

## Real capture

Exact target:

```text
Provider: Aerolink
API base: https://capi.aerolink.lat
Endpoint: /v1/messages
API format: Anthropic Messages
Anthropic version: 2023-06-01
Model: claude-opus-4-8
```

Capture constraints:

```text
request_count = 1
retry_count = 0
automatic_retries = 0
max_tokens = 1
real_api_key_used = true
secret_persisted = false
```

Observed response:

```text
HTTP 402 Payment Required
```

Exact response body:

```json
{"error":"5-hour included-usage limit reached. You have used the $10.00 allowance for this window. Wait for the 5-hour reset, add balance, or upgrade to continue."}
```

## Required acceptance checks

1. The raw capture SHA-256 matches the returned Windows evidence file.
2. The prior Proof 0.16 invalid-key capture remains valid.
3. The new real capture schema and response body hash validate.
4. The capture used exactly one request and zero retries.
5. A real already-limited key was used, but the secret was not persisted.
6. The exact Aerolink provider/API/endpoint/model/version identity is preserved.
7. The exact HTTP 402 body is preserved by SHA-256.
8. The prior invalid-key fragment still normalizes.
9. The 402 capture normalizes to a separate incremental fragment.
10. The fragment binds raw capture provenance.
11. The fragment does not claim key scope.
12. Automatic original replay remains forbidden.
13. Automatic key rotation remains forbidden.
14. Semantic continuation remains forbidden for this pre-output ambiguous scope.
15. The fragment survives durable save/load with stable identity.
16. Multiple real fragments for the same provider/endpoint coexist.
17. The prior HTTP 401 invalid-key behavior still matches exactly.
18. The exact HTTP 402 behavior matches its own fragment.
19. Body mismatch fails closed.
20. Endpoint mismatch fails closed.
21. Model/version mismatch fails closed.
22. Expired evidence requires revalidation.
23. Tampered evidence is rejected.
24. Limitations remain attached.
25. No user path or persisted secret enters source evidence.

## Safety decision

The exact HTTP 402 response is a trusted observation of an Aerolink 5-hour included-usage window limit for the captured request shape.

It is **not yet trusted as key-specific exhaustion**.

Until a paired capture proves that a separate known-usable replacement key/account succeeds while the limited key returns the same exact 402:

```text
original replay = forbidden
automatic key rotation = forbidden
automatic semantic continuation = forbidden
require input = true
```

## Next gate

Run a bounded paired scope capture:

```text
known-limited key
→ exact same 402

then immediately

separate known-usable replacement key/account
→ successful response on same request shape
```

Use exactly one request per key, zero retries, `max_tokens=1`, SecureString input, and never persist either key.
