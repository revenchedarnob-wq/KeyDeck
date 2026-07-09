# KeyDeck Feasibility Lab v0.11.0 Changelog

## Added

- `internal/agentenvproof` evidence-validation package.
- Proof 0.14 integrated acceptance harness.
- Real Microsoft APM v0.24.0 prototype fixture and evidence.
- Real Microsoft Waza v0.38.0 normalized evidence record with source-evidence SHA-256 chain.
- APM declarative fixture covering instructions, agents, prompts, skills and hooks.
- Waza skill/evaluation fixture covering positive and negative trigger boundaries.
- Proof 0.14 specification and report.

## Proven

### Microsoft APM

- Clean audit passed.
- Deliberate drift was detected.
- Frozen restore returned the deployment to the declared state.
- A second clean installation passed.
- Deployed hashes were reproducible.
- A package/plugin bundle was created.
- Lockfile behavior was exercised.
- Declarative instructions, agents, prompts, skills and hooks were represented.

### Microsoft Waza

- Exact v0.38.0 Windows x64 binary checksum was verified.
- Skill validation passed.
- Trigger/spec coverage gate passed.
- Token counting and token-budget gate passed.
- 4 tasks × 5 deterministic mock trials completed.
- 20 snapshots were captured.
- All 20 snapshots replayed successfully.
- No-regression gate passed.
- Adversarial-pack catalog discovery passed.

## Decision

`ADOPT_APM_AND_WAZA_WITH_SCOPED_OWNERSHIP_AND_LIMITATIONS`

- APM becomes the preferred prototype for the declarative Agent Environment layer.
- Waza becomes the preferred development-time skill/agent evaluation proving ground.
- A large custom Skill Compiler and custom general evaluation stack remain deferred.

## Architecture boundary preserved

- KeyDeck still owns canonical state.
- KeyDeck still owns engine runtime safety.
- KeyDeck still owns Recovery Coordinator behavior.
- KeyDeck still owns financial policy.
- KeyDeck still owns Tool Journal replay safety.
- Executable community extensions remain Extism/WebAssembly.
- Tools remain MCP.

## Limitations preserved

- Remote Git pin resolution was not proven.
- Non-empty MCP dependency resolution was not proven end-to-end.
- Waza live-model quality comparison was not part of this proof.
- Waza remains development-only infrastructure.

## Validation target

- `go test ./...`
- `go vet ./...`
- `go test -race ./...`
- regression Proofs 0.9 through 0.14.
