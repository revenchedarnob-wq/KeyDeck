# Proof 0.2 — Mid-answer Same-model Key Continuation

## Goal

Prove that an API key can be explicitly exhausted after partial streamed text and that the next key can continue the same visible answer without blindly replaying an ambiguous stream.

## Strategy

Balanced continuity mode holds the current unfinished sentence. Completed sentence boundaries are committed to the visible answer. On an explicit key-scoped exhaustion event:

1. mark the physical key exhausted;
2. preserve confirmed output;
3. preserve the unfinished fragment as directional context only;
4. create a structured continuation package;
5. call the next key;
6. append only the new continuation to the visible answer.

## Acceptance checks

- [x] Completed sentence is committed before failure.
- [x] Unfinished sentence is not exposed as final text.
- [x] Explicit mid-stream `key_exhausted` may move to the next key.
- [x] Continuation package contains original request, confirmed output and unstable fragment.
- [x] Next key produces one coherent visible answer.
- [x] Abrupt EOF after partial output is ambiguous and does not rotate/replay.
- [x] Provider-wide busy after partial output preserves backup keys.

## Honest limitation

This is **semantic continuation**, not transfer of hidden model state. It also does not yet permit automatic continuation after an ambiguous tool action. Tool journaling is the next proof.
