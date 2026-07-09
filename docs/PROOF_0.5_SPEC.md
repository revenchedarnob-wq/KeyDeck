# Proof 0.5 — Codex App Server Session Binding

## Goal

Prove the KeyDeck-owned canonical session can bind to a Codex App Server thread, survive KeyDeck restart, resume the same external thread, and switch back to another engine without making Codex the canonical state owner.

## Why this uses App Server

The official Codex App Server exposes JSON-RPC over stdio, ChatGPT-managed authentication, persistent threads, thread resume, streamed item/turn events, approvals, and account/rate-limit information. The lab uses that official integration surface instead of scraping terminal output.

## Automated protocol proof

`go run ./cmd/proof05`

This uses a deterministic fake Codex App Server and proves:

- JSONL JSON-RPC initialization;
- ChatGPT/Plus account-state parsing;
- `thread/start` on first handoff;
- persisted external thread binding in KeyDeck canonical state;
- full KeyDeck session save/reload;
- `thread/resume` after restart;
- streamed agent text collection;
- file-change event normalization into KeyDeck completed actions and relevant files;
- switch back to the API engine with Codex changes visible;
- no API keys or secret tokens in the canonical handoff/session.

## Real Windows proof

Run `proof05-real.exe` on the user's Windows PC.

It will:

1. locate the official `codex` command;
2. start `codex app-server` over stdio;
3. initialize the protocol;
4. reuse existing ChatGPT-managed Codex login or start the official login flow;
5. create a disposable proof project unless `-project` is provided;
6. run API → real Codex;
7. persist the KeyDeck canonical session and Codex thread binding;
8. terminate the first App Server process;
9. launch a new App Server process;
10. reload the KeyDeck session;
11. resume the same Codex thread;
12. run a second Codex turn;
13. switch back to the API engine and verify the project changes.

### Commands

Default browser login:

```powershell
.\proof05-real.exe
```

Device-code login:

```powershell
.\proof05-real.exe -device-login
```

Use a specific disposable project:

```powershell
.\proof05-real.exe -project "C:\Path\To\DisposableRepo"
```

Only if Codex reports that Windows sandbox setup is required:

```powershell
.\proof05-real.exe -setup-windows-sandbox
```

## Pass criteria

- ChatGPT-managed Codex account detected.
- First Codex phase creates `codex-proof.txt` with phase 1 marker.
- Canonical KeyDeck session persists a non-empty Codex external thread id.
- A new App Server process resumes that thread after KeyDeck session reload.
- Second Codex phase appends phase 2 marker.
- API engine sees both phases after switching back.
- No ChatGPT credential, API key, or token appears in canonical session state.

## Current status

- Deterministic protocol/session proof: **PASS**.
- Real Windows ChatGPT Plus/Codex run: **PENDING USER-PC EXECUTION**.
