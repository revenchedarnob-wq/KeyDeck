# Proof 0.7 — API Mid-Answer Exhaustion to Real Codex

## Goal

Prove that one visible KeyDeck response can begin in an API pool, survive explicit exhaustion across every API key, persist the confirmed/unstable boundary across a KeyDeck restart, continue through real Codex on ChatGPT Plus without duplicating confirmed text or repeating a completed action, survive a full Codex App Server restart, and return to a recovered API pool with the full canonical state intact.

## Safety boundary

Cross-engine continuation is staged only when the streaming API engine returns explicit `ErrAllKeysUnavailable` after key-scoped exhaustion evidence.

It must not be staged for provider-wide busy or ambiguous stream failure. Confirmed output is canonical visible text. The unstable fragment is directional context only and is never committed directly.

## Real proof sequence

1. Create one canonical KeyDeck session and one protected file representing an already-completed API action.
2. Start one visible API response.
3. Key 1 emits one confirmed sentence, then explicitly exhausts mid-sentence.
4. Key 2 receives a continuation package and emits the next confirmed sentence, then explicitly exhausts.
5. Key 3 receives a continuation package and emits the third confirmed sentence plus an unfinished fragment, then explicitly exhausts.
6. Persist the in-flight response state.
7. Simulate a full KeyDeck restart before any cross-engine handoff.
8. Reload the exact confirmed output and unstable fragment.
9. Launch the official Codex App Server with ChatGPT Plus authentication.
10. Hand the persisted continuation state to real Codex.
11. Real Codex creates the phase-1 proof artifact while preserving the protected completed-action file byte-for-byte.
12. Merge the confirmed API output and Codex continuation into one visible assistant response, with confirmed API text appearing exactly once.
13. Persist the Codex thread binding and canonical state.
14. Shut down the first App Server.
15. Start a fresh App Server and resume the exact same Codex thread.
16. Real Codex appends the phase-2 marker while preserving the protected file.
17. Simulate API capacity recovery.
18. Switch the same canonical session back to API.
19. Verify the recovered API request contains the confirmed cross-engine response, both Codex phase markers, and protected-file evidence.

## PASS requirements

- each initial API key is called exactly once;
- Key 2 and Key 3 receive valid same-provider continuation packages;
- confirmed output and unstable fragment survive a KeyDeck restart;
- real Codex receives a model-agnostic continuation passport;
- confirmed API text appears exactly once in the final visible response;
- unstable fragment is not committed as confirmed output;
- protected completed-action file hash is unchanged;
- the original canonical user task appears exactly once;
- the real Codex external thread ID survives App Server restart;
- recovered API sees the confirmed cross-engine response and both Codex proof phases;
- final active engine is API pool;
- no in-flight response remains after successful completion.
