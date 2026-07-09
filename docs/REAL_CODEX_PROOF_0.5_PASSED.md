# Real Codex Proof 0.5 — PASSED

Date: 2026-07-06
Platform: Windows x64
Codex CLI: 0.142.5
Authentication: ChatGPT Plus through official Codex authentication

## What was genuinely proven

- KeyDeck connected to the official Codex App Server.
- A real Codex thread was created.
- Real reasoning, agent-message, command-execution, and file-change events streamed through the integration.
- Codex edited a disposable project successfully.
- KeyDeck persisted the external Codex thread binding inside canonical state.
- The first App Server process was shut down to simulate a full KeyDeck/App Server restart.
- A fresh App Server process launched.
- KeyDeck resumed the exact same Codex thread.
- Codex continued work in phase 2.
- The proof switched back to the KeyDeck/API side and completed successfully.

Final observed result:

`PASS: real Codex handoff, restart/resume, and switch-back proof succeeded.`

## Compatibility findings preserved

1. `thread/start.sandbox` requires `workspace-write`.
2. `turn/start.sandboxPolicy.type` requires `workspaceWrite`.
3. On the tested Windows standalone installation, the launcher could not resolve `codex-windows-sandbox-setup.exe`.
4. The successful proof invoked the actual packaged runtime from `~/.codex/packages/standalone/current/bin/codex.exe` and placed the bundled resource directory on PATH.
5. Sandbox safety was not disabled.

## Architectural meaning

The result proves that one KeyDeck-owned canonical session can use real Codex as a worker, survive an App Server restart, resume the same Codex thread, and switch back while preserving project/task continuity.
