KEYDECK PROOF 0.14 — REAL WAZA EXTERNAL GATE

Why this exists
---------------
The engineering sandbox could fully execute Microsoft APM, but it refused to ingest Microsoft's 143 MB Waza executable and source archives by file type. This is the one external proof gate needed to finish Proof 0.14 honestly.

What the runner does
--------------------
1. Downloads the exact Microsoft Waza v0.38.0 Windows x64 release.
2. Downloads Microsoft's official checksums.txt.
3. Verifies SHA-256 exactly:
   ff7fe521d4f876de29d018a00fe282746109d8b788a6e9a9f288dbd8a3470364
4. Runs only deterministic/offline Waza operations:
   - version verification
   - skill validation
   - SKILL.md spec coverage gate
   - token count and token budget gate
   - 20 mock-executor trials (4 tasks x 5 trials)
   - snapshots
   - offline replay of every snapshot
   - no-regression gate
   - adversarial pack catalog discovery
5. Writes one return file:
   RETURN-THIS-waza-proof-evidence.json

No API key, paid provider, ChatGPT login, Codex login, or model account is required.

How to run
----------
1. Extract this ZIP.
2. Open PowerShell 7 in the extracted folder.
3. Run exactly:

   Set-ExecutionPolicy -Scope Process Bypass -Force; .\RUN-PROOF-0.14-WAZA.ps1

4. When it finishes, upload:

   RETURN-THIS-waza-proof-evidence.json

Do not edit the evidence file.
