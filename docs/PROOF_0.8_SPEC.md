# Proof 0.8 — Context Compiler Efficiency

## Goal

Determine whether KeyDeck can reduce real Codex repository exploration work by compiling a small, auditable context packet before the expensive solver starts, without reducing correctness.

This proof must not assume that a structural index saves tokens. It must measure the result.

## Candidate architecture

The Context Compiler is a KeyDeck-owned policy layer. It combines:

1. a local structural graph from `codebase-memory-mcp`;
2. Git state;
3. exact source scanning and snippets;
4. an explicit character budget;
5. an omission count and raw evidence receipt.

The structural engine does not own canonical state and does not replace source access. KeyDeck decides which evidence enters the packet.

## Third-party tool boundary

Proof 0.8 pins `codebase-memory-mcp` v0.8.1.

The Windows runner:

- downloads the release `checksums.txt`;
- requires exact SHA-256 `142399e4e552fb559ede866b2549dbacc942d56f1c8718b52bc701b21f3f94c6` for that manifest;
- obtains the Windows archive hash only from the verified manifest;
- verifies the downloaded archive;
- extracts it with ZIP-slip protection;
- runs the binary directly;
- uses a private `CBM_CACHE_DIR` inside the disposable proof workspace;
- never runs the upstream install script;
- never modifies agent configuration or global MCP settings.

## Controlled benchmark

The proof creates one template Go repository with a real multi-tenant isolation bug and many decoy files.

The defect is:

- the routing engine resolves the currently active tenant;
- the policy cache is keyed only by route;
- after an active-tenant switch, a route cached for tenant A may be reused for tenant B.

The exact minimal fix spans:

- `internal/routing/engine.go`
- `internal/policy/cache.go`

The repository contains 180 decoy packages and 60 telemetry files using overlapping tenant/cache/route/fallback vocabulary.

## Two-arm experiment

### Baseline arm

A fresh real Codex thread receives only the task and investigates the identical repository normally.

### Context-assisted arm

A separate fresh real Codex thread receives the same task plus the bounded KeyDeck Context Packet and an instruction to use the packet first, inspecting additional source only when needed.

The two arms never share a Codex thread or project directory.

## Correctness acceptance checks

Each arm must write exactly one `KEYDECK_PROOF_08_RESULT.json` artifact.

The evaluator requires:

- exact proof marker;
- root cause identifies tenant-isolation leakage;
- root cause identifies a cache keyed only by route / missing tenant identity;
- call path includes `HandleRequest -> Resolve -> GetOrLoad`;
- minimal fix set contains exactly the required routing and cache source files;
- Git evidence proves the agent did not modify tracked source or add unexpected files.

A symptom-only answer does not pass.

## Efficiency evidence

The App Server event stream records:

- command executions;
- file changes;
- other tool-item counts;
- token-usage notifications when exposed by the current App Server;
- total input tokens;
- cached input tokens;
- derived uncached input tokens;
- wall-clock duration.

Raw token-usage payloads are preserved alongside parsed counters so schema changes cannot silently become zero.

## Adoption gate

Proof 0.8 passes only when:

1. both arms pass all correctness acceptance checks;
2. the assisted arm reduces at least one measured exploration metric:
   - command executions, or
   - total input tokens, or
   - uncached input tokens;
3. when token telemetry is available, total input tokens do not regress by more than 25%.

If correctness fails, KeyDeck rejects/redesigns the approach.

If correctness holds but savings are not proven, the result is `INCONCLUSIVE`; KeyDeck makes no savings claim and does not treat the compiler as validated.
