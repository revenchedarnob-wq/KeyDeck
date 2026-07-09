# Proof 0.4 — Canonical One-chat Engine Switching

## Goal

Prove the KeyDeck-owned session model before connecting a real Codex process.

## Scenario

1. API engine analyzes a gateway problem.
2. Canonical session is saved and reopened.
3. Codex worker receives an Agent Passport containing the goal, transcript, decisions, pending work, relevant files and checkpoint state.
4. Codex records a completed action and checkpoint.
5. Session switches back to the API engine.
6. API engine can see the Codex action/checkpoint in the same transcript and project state.

## Acceptance checks

- [x] One transcript remains across engine changes.
- [x] Session survives save/reload between engines.
- [x] Handoff contains prior findings and decisions.
- [x] Switching back contains Codex actions and checkpoint.
- [x] Passport contains no API key material.

## Honest limitation

The engines in this proof are deterministic fake workers. The canonical state/handoff model is proven; real Codex ACP/App Server authentication and session behavior still require an integration proof on a machine signed into ChatGPT Plus.
