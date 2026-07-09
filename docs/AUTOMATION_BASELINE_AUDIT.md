# Automation Baseline Audit

## Source inspected

This audit is based on the repository contents present after cloning `https://github.com/revenchedarnob-wq/KeyDeck.git` at the imported reconstructed baseline.

## Existing automation

- No existing `.github/workflows` directory was present at audit time.
- No existing `scripts` hierarchy was present at audit time.
- The repository has Go package tests under `internal/**` and command proof harnesses under `cmd/proof*`.
- Release artifacts and historical proof packages exist under `_artifacts` and `dist`.

## Missing automation

- Local bootstrap verifier, conservative secret scanner, fast/deep CI scripts, proof registry generation, release pipeline, Drive upload wrapper, root command center, and GitHub Actions.

## Safe deterministic tests

Safe candidates are ordinary Go unit tests under `internal/**` and deterministic proof commands that do not require live provider credentials, UI interaction, external pinned runtimes, or real Codex sign-in.

## Tests requiring Windows

Windows-sensitive commands include desktop/product shell commands, real Codex proof harnesses, and proofs whose specs or code mention Windows-only execution.

## Tests requiring credentials

Proofs involving real Codex, live providers, Aerolink captures, OAuth/device login, or MCP pinned external runtime evidence are not safe for GitHub-hosted CI without explicit non-secret fixtures.

## Tests requiring external providers

The docs and command code mention Aerolink, real-provider evidence, Codex CLI/App Server authentication, and pinned third-party MCP runtime evidence.

## Tests requiring UI interaction

Desktop UI/renderer proofs and real browser/Chromium QA are treated as UI-sensitive.

## Proofs safe for GitHub-hosted CI

The generated `docs/PROOF_REGISTRY.json` is the source of truth. CI uses conservative deterministic packages and avoids real-provider, credential, interactive, and arbitrary downloaded-code execution.

## Proofs unsafe for cloud CI

Unsafe classes include live credential tests, real Codex handoff proofs, external paid API/provider tests, OAuth/device-login flows, interactive UI tests, and any proof that would execute downloaded premade infrastructure archives.

## Current release packaging behavior

The repository contains historical release/proof artifacts, but no one-command safe release pipeline was present. The new release script stages deterministic repository-owned builds, hashes assets, produces a manifest, and only publishes through `gh` when explicitly requested.
