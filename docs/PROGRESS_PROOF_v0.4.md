# KeyDeck Progress Proof — v0.4

This document reports acceptance evidence, not an overall guessed product percentage.

## Proven on the real Windows machine

- Official Codex CLI authentication through ChatGPT Plus.
- Official Codex App Server integration.
- Real Codex reasoning, command-execution and file-change events streamed into KeyDeck.
- Real Codex project edits.
- Exact Codex thread persistence across a full App Server restart.
- Resume of the exact same real Codex thread.
- Switch-back from Codex to the KeyDeck/API side.
- Proof 0.6: synthetic three-key API pool exhaustion automatically hands one canonical task to real Codex, survives restart/resume, then returns to recovered API.

## Proven locally in v0.4

- Proof 0.7 policy: one API response can rotate across explicitly exhausted keys while preserving confirmed sentence boundaries.
- When all API keys exhaust mid-answer, KeyDeck stages model-agnostic in-flight continuation state.
- Confirmed visible output and unstable fragment survive save/reload.
- A second engine can continue the same visible response without duplicating confirmed output.
- Provider-wide busy and ambiguous stream failure remain ineligible for automatic cross-engine continuation through the existing safety policy.

## Proven on the real Windows machine in v0.4

- Proof 0.7: API mid-answer exhaustion -> persisted partial response -> real Codex continuation -> restart/resume -> recovered API.
- The final real result was: `PASS: API mid-answer exhaustion -> persisted partial state -> real Codex continuation -> restart/resume -> recovered API succeeded.`

## Not yet proven

- Real provider-specific Aerolink conformance.
- Cross-engine continuation from a real paid API provider.
- Real Google AI Pro worker integration.
- Production durable workflow engine.
- Production UI and one-click universal installer.
