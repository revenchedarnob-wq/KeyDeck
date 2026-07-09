# KeyDeck Feasibility Lab — Progress Proof

Generated from executable acceptance checks, not model-estimated completion.

## Baseline

**Uploaded baseline:** Aerolink KeyDeck v3.2.0 source/release.

Verified defects frozen as forbidden regressions:

- no real cost-burn guard;
- universal hardcoded local gateway credential;
- ambiguous automatic retry behavior;
- weak gateway identity/version readiness check;
- mutable `main`-branch skill download;
- heuristic health probe before authentication;
- detailed unauthenticated local status endpoints;
- source ZIP missing embedded setup payloads required for a self-contained rebuild.

## Proven milestones

| Proof | Acceptance status | Evidence |
|---|---|---|
| 0.1 Financially safe elastic API pool | PASS | Explicit exhausted/invalid keys fail over; provider busy, ambiguous 502, unknown 429 and cost anomaly preserve backups. |
| 0.2 Mid-answer same-model key continuation | PASS | Explicit streamed key exhaustion continues on next key using confirmed output + unstable-fragment handoff. |
| 0.3 Tool journal replay safety | PASS | Completed destructive action is not re-executed; interrupted non-repeatable action blocks replay after restart. |
| 0.4 Canonical one-chat engine switching | PASS | Persisted session switches API → fake Codex → API with transcript, decisions, actions and checkpoint intact. |

## Validation actually run

- `go test ./...` — PASS
- `go vet ./...` — PASS
- `go test -race ./...` — PASS
- 20 repeated runs of core proof packages — PASS
- Windows amd64 cross-build of fake provider + Proof 0.1–0.4 runners — PASS

## Not yet proven

These are explicitly **not complete**:

- real Aerolink exhaustion/busy/rate-limit response conformance;
- production cache-thrash thresholds;
- real Codex authentication/session handoff via ACP/App Server;
- Google AI Pro/Antigravity engine integration;
- tool journal wired into real MCP/ACP tool execution;
- strict per-install gateway credential and gateway executable/version attestation;
- pinned one-click installer dependency chain;
- crash-resume durable workflow across real external agents.

## Next proof gate

**Proof 0.5:** connect the canonical session to a real Codex engine on a machine already authenticated with the user's ChatGPT Plus account. No password or API key should enter KeyDeck.
