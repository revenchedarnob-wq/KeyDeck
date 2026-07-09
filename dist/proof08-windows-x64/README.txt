KeyDeck Proof 0.8 — Windows x64

Goal:
Measure whether the KeyDeck Context Compiler reduces real Codex repository exploration work without reducing correctness.

The proof creates two identical disposable Go repositories:
  1. baseline: fresh real Codex investigates normally;
  2. context-assisted: a separate fresh real Codex thread receives a bounded KeyDeck Context Packet first.

Both arms must pass the same exact acceptance checks.

Safety:
- No API provider or paid API key is used.
- Real Codex uses your existing official ChatGPT authentication.
- The structural engine is codebase-memory-mcp v0.8.1.
- The proof downloads checksums.txt and requires its exact hard-pinned SHA-256.
- The Windows archive hash is accepted only from that verified manifest.
- The archive is verified before extraction.
- The upstream installer is never run.
- No Codex, Claude, Gemini, MCP, or agent configuration is modified.
- Structural indexing runs against a separate disposable scout copy, not the baseline or assisted benchmark repository.
- Git verifies the solver did not modify source or add unexpected files.

Run:
  Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass -Force; Unblock-File .\RUN-REAL-PROOF-0.8.ps1; .\RUN-REAL-PROOF-0.8.ps1

PASS requires:
- baseline correctness PASS;
- assisted correctness PASS;
- assisted arm reduces at least one measured exploration metric;
- no major total-input-token regression when token telemetry is available.

An INCONCLUSIVE result is intentionally a non-zero exit. KeyDeck makes no savings claim unless the evidence passes the adoption gate.
