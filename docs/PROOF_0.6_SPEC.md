# Proof 0.6 — Automatic Exhausted API Pool to Real Codex

## Goal

Prove that one canonical KeyDeck task can automatically leave an API pool only after every key is explicitly exhausted, continue with real Codex on ChatGPT Plus, survive a full App Server restart, resume the exact same Codex thread, and then return to API when capacity recovers.

## Safety boundary

Automatic cross-engine fallback is allowed only when the API pool returns `ErrAllKeysUnavailable` after explicit key-scoped exhaustion evidence.

It must NOT trigger on:

- provider-wide busy;
- ambiguous 502/network outcomes;
- unknown 429 outcomes;
- cost-safety blocks.

## Real proof sequence

1. Create one canonical KeyDeck session.
2. Start with a synthetic three-key API pool.
3. Explicitly exhaust Key 1.
4. Explicitly exhaust Key 2.
5. Explicitly exhaust Key 3.
6. Verify each key was attempted exactly once.
7. Automatically hand the same canonical user task to real Codex.
8. Real Codex creates `codex-proof-06.txt` with phase 1 marker.
9. Persist canonical state and Codex thread binding.
10. Shut down the first App Server.
11. Reload canonical state.
12. Start a fresh App Server.
13. Resume the exact same Codex thread.
14. Append phase 2 marker.
15. Simulate API capacity recovery.
16. Switch back to API.
17. Verify the recovered API request contains evidence of both Codex phases.
18. Verify the original user task appears exactly once in canonical transcript.

## PASS requirements

- all three initial keys called exactly once;
- fallback reason is exact and explicit;
- no duplicate canonical user turn;
- real Codex phase 1 file change succeeds;
- App Server restart succeeds;
- exact same external Codex thread ID resumes;
- real Codex phase 2 succeeds;
- recovered API request sees both Codex phase markers;
- final active engine is API pool;
- no API key or ChatGPT credential is persisted in canonical state.
