# KeyDeck Progress Proof — v0.3

This document reports acceptance evidence, not an overall guessed product percentage.

## Proven

- Exact v3.2.0 baseline audit.
- Elastic multi-key pool behavior in the fake-provider lab.
- Provider-wide busy preserves backup keys.
- Ambiguous failures do not rotate/replay automatically.
- Cost-safety guard can block further API spending before backup-key failover.
- Same-provider mid-answer continuation with stable-boundary buffering in the fake-provider lab.
- Tool journal replay rules across restart.
- One canonical session across engine switches.
- Real Codex App Server integration with ChatGPT Plus.
- Real Codex project edits.
- Real Codex thread persistence across full App Server restart.
- Real Codex resume of the exact same thread.
- Real Codex switch-back proof.
- Proof 0.6 local policy gate: all-keys exhaustion triggers fallback exactly once; provider busy and ambiguous 502 do not trigger fallback.

## Built, awaiting one real Windows run

- Proof 0.6 automatic fake API-pool exhaustion -> real Codex -> restart/resume -> recovered API.

## Not yet proven

- Cross-engine mid-answer API -> real Codex continuation.
- Real provider-specific Aerolink conformance.
- Real Google AI Pro worker integration.
- Production durable workflow engine.
- Production UI and one-click universal installer.
