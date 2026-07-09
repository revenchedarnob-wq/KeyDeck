# Proof 0.14 — Microsoft APM + Waza Proving Ground

## Goal

Determine whether mature premade infrastructure can replace a large amount of custom KeyDeck engineering for declarative Agent Environments and development-time skill/agent evaluation.

The proof must preserve the permanent ownership boundary:

- KeyDeck owns canonical state, runtime safety, recovery, financial policy, tool replay safety, permissions orchestration and secret brokering.
- APM may own declarative Agent Environment packaging/deployment metadata, lockfiles and drift detection only.
- Waza may own development-time skill/agent evaluation infrastructure only.

## Pinned tools

### Microsoft Agent Package Manager

- Version: `0.24.0`
- Execution: real local tool prototype.

### Microsoft Waza

- Version: `0.38.0`
- Windows x64 binary SHA-256:
  `ff7fe521d4f876de29d018a00fe282746109d8b788a6e9a9f288dbd8a3470364`
- The Windows runner verified the official checksum before execution.
- Returned raw evidence SHA-256:
  `595c381fe97c063e633aafe45f029702a91d10162d65df0faa0041255e849baf`

The source package stores a normalized evidence record that preserves this raw evidence hash without retaining user-specific Windows paths.

## APM acceptance checks

1. Represent declarative instructions.
2. Represent agent definitions.
3. Represent prompts.
4. Represent skills.
5. Represent hooks.
6. Produce a lockfile.
7. Run a clean audit.
8. Detect deliberate drift with a non-zero audit exit.
9. Restore the declared state with frozen install.
10. Reinstall cleanly in a second target.
11. Produce reproducible deployed hashes.
12. Produce a package/plugin bundle.

## Waza acceptance checks

1. Verify exact pinned version.
2. Verify exact binary checksum.
3. Validate the skill.
4. Verify positive and negative trigger/spec coverage.
5. Count tokens.
6. Enforce a token budget.
7. Run deterministic mock evaluation.
8. Use 4 tasks × 5 trials = 20 trials.
9. Cover 2 positive-trigger and 2 negative-trigger tasks.
10. Capture 20 snapshots.
11. Replay all 20 snapshots successfully offline.
12. Pass a no-regression gate.
13. Confirm the adversarial-pack catalog is available.

## Result

PASS.

Decision:

`ADOPT_APM_AND_WAZA_WITH_SCOPED_OWNERSHIP_AND_LIMITATIONS`

## Engineering savings proven

### APM

The real prototype replaced custom proof work for:

- declarative primitive deployment;
- lockfile creation;
- clean audit;
- drift detection;
- frozen restore;
- reproducible deployment checks;
- bundle creation.

### Waza

The real prototype replaced custom proof work for:

- skill validation;
- trigger/spec coverage checks;
- token counting;
- token-budget enforcement;
- deterministic evaluation;
- snapshot capture/replay;
- no-regression gating;
- adversarial catalog discovery.

Conclusion:

> Do not build a large custom KeyDeck Skill Compiler or a custom general skill-evaluation framework now.

## Adoption rules

1. Prefer APM for declarative Agent Environment packaging and lock/drift behavior.
2. Keep executable extension logic in Extism/WebAssembly.
3. Keep tools in MCP.
4. Use Waza as a development-time proving ground, not as production runtime infrastructure.
5. APM/Waza never own canonical KeyDeck state, runtime safety, recovery or financial policy.

## Limitations

The proof does not claim:

- remote Git ref pin resolution was proven;
- a non-empty MCP dependency was resolved end-to-end;
- live multi-model quality was measured;
- Waza is a production runtime dependency;
- APM owns executable extension logic;
- APM/Waza own any part of canonical state or recovery.

The Waza run intentionally used a deterministic mock executor so the proof measured evaluation infrastructure rather than model quality or billing.
