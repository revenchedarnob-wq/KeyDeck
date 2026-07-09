# Proof 0.18 — Bounded Paired Scope Capture: Inconclusive Safe Handling

## Goal

Import the real paired Aerolink scope capture without converting an ambiguous replacement timeout into a claim about key, account, or provider scope.

The proof must preserve the exact successful first half of the gate, preserve the exact failed second half, and show that KeyDeck does **not** reward an inconclusive experiment with unsafe replay or key rotation.

## Real paired capture

Exact target:

```text
Provider: Aerolink
API base: https://capi.aerolink.lat
Endpoint: /v1/messages
API format: Anthropic Messages
Anthropic version: 2023-06-01
Model: claude-opus-4-8
```

Bounded ordering:

```text
Credential A: already limited
→ exactly one request
→ reproduced exact known HTTP 402 body hash

ONLY THEN

Credential B: separate known-usable replacement
→ exactly one request
→ zero retries
→ no HTTP response
→ 20-second HttpClient timeout
```

Capture safety:

```text
total_request_count = 2
total_retry_count = 0
max_tokens = 1 per credential
secrets_persisted = false
request_shape_identical = true
```

Raw paired capture SHA-256:

`ee2ebaa92ab8bf97661a1a594939ede3f0109442b28190f5f21080a9fdbf81b8`

## Required acceptance checks

1. The raw paired capture SHA-256 matches the Windows return file.
2. Prior real-capture hashes remain unchanged.
3. The paired schema and bounded sequence validate.
4. Exact provider/API/endpoint/model/version identity is preserved.
5. The gate used two total requests, zero retries, `max_tokens=1`, and persisted no secrets.
6. Credential A reproduced the exact known 402 before B was touched.
7. Credential B was attempted exactly once.
8. Credential B produced status `0`, no HTTP response, and a preserved timeout error.
9. The capture correctly remains `passed=false`.
10. The request shape remained identical across both credentials.
11. The capture normalizes into a SHA-256-sealed paired-scope evidence record.
12. Raw-capture provenance is bound by SHA-256.
13. The replacement outcome is `no_http_response`.
14. The replacement transport class is `timeout`.
15. Scope remains unproven.
16. Original replay remains forbidden.
17. Key rotation remains forbidden.
18. Semantic continuation remains forbidden.
19. Input remains required.
20. Durable save/load preserves evidence identity.
21. Expired evidence requires revalidation.
22. Tampered paired evidence is rejected.
23. Prior HTTP 401 and HTTP 402 fragments still coexist.
24. Prior exact HTTP 401 behavior remains unchanged.
25. Prior exact HTTP 402 behavior remains trusted but conservative.
26. The replacement timeout fails closed before fragment matching.
27. The timeout does not claim key, account, or provider scope.
28. Source evidence contains no user path or persisted credential.

## Safety decision

The paired capture proves:

```text
Credential A
→ exact known Aerolink HTTP 402 usage-window response
```

It also proves:

```text
Credential B
→ one bounded attempt
→ no HTTP response
→ 20-second timeout
→ zero retries
```

It does **not** prove:

- the 402 is key-specific;
- the 402 is account-specific;
- the 402 is provider-wide;
- replacement-key rotation is safe;
- original replay is safe.

Therefore:

```text
scope = unproven
class = ambiguous
original replay = false
key rotation = false
semantic continuation = false
require input = true
```

## Next gate

Do not automatically rerun the same paired test.

Preserve the exact 402 as scope-unknown and the B timeout as ambiguous transport evidence. Continue local product work. Repeat a real paired scope capture only later under a separately justified bounded gate when transport conditions or new provider evidence make the experiment high value.
