KeyDeck Proof 0.12 — Integrated Recovery Coordinator + Exactly-Once Canonical Commit

This Windows x64 runner is a local synthetic durability proof.
It requires no API key, paid provider, ChatGPT login, Codex login, or network access.

Run from PowerShell or Command Prompt:

  .\KeyDeck-Proof-0.12.exe

Expected result:

  "passed": true

The proof launches child copies of itself and intentionally terminates them at controlled durability boundaries. It then reopens the persisted stores and verifies recovery behavior.

This package does not modify KeyDeck installation settings, Claude settings, Codex settings, API keys, or provider configuration.
