# Real Codex Windows Proof — User Action

No password, ChatGPT cookie, OpenAI API key, or access token should be copied into KeyDeck Lab.

## What you need

- Windows x64.
- Official Codex CLI available as `codex` on PATH.
- ChatGPT Plus account.
- Internet connection for Codex authentication and execution.

## Run

Open PowerShell in the extracted `dist\windows-x64` folder:

```powershell
.\proof05-real.exe
```

The runner first checks current Codex auth. If you are not already signed in with ChatGPT, it starts the official App Server login flow and opens the official URL. Complete the sign-in in the browser, then the proof continues automatically.

## Safer alternative login

```powershell
.\proof05-real.exe -device-login
```

This prints the official verification URL and user code.

## Expected final line

```text
PASS: real Codex handoff, restart/resume, and switch-back proof succeeded.
```

The machine-readable report is written inside the disposable proof project at:

```text
.keydeck-lab\proof05-real-report.json
```

## Important

Do not test this on the real KeyDeck source repository. The default mode creates a disposable temporary proof project.
