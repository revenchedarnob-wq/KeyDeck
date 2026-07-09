KeyDeck Proof 0.7 — Windows x64

Goal:
Prove one visible response can start in a synthetic API pool, survive explicit key exhaustion mid-answer, persist confirmed/unstable response state across a KeyDeck restart, continue through real Codex on ChatGPT Plus, survive a full Codex App Server restart, and return to a recovered API pool.

Run:
  Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force; Unblock-File .\RUN-REAL-PROOF-0.7.ps1; .\RUN-REAL-PROOF-0.7.ps1

The API side is synthetic and spends no API credit.
The proof uses a disposable project by default.
The script preserves the proven direct standalone Codex runtime path so Windows sandbox resources resolve correctly.
