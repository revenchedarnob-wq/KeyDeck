# KeyDeck Deep Premade-Infrastructure Audit

**Audit basis:** all repository/package artifacts physically uploaded in this chat and extracted from the batch bundles, plus the quarantined prompt-reference file, compared against the sealed `KeyDeck v0.35.0-RECONSTRUCTED` source tree.

## Evidence boundary

- **102 archive/package artifacts** were inventoried across **100 checklist item numbers**, plus **1 quarantined text reference**.
- Every archive received SHA-256 inventorying and structural metadata analysis; corrupt-archive count after repair: **0**.
- I performed archive-wide hash, duplicate, size, language, root-license and top-level structure analysis on the complete set, then targeted source-level review of the high-impact candidates against the actual v0.35 KeyDeck packages.
- This is **not** a claim that every line in every repository was manually read. The deepest source review focused on candidates capable of changing KeyDeck architecture or replacing custom plumbing.

## Executive verdict

The uploads validate the original KeyDeck architecture more than they invalidate it. The unique safety/continuity core should stay KeyDeck-owned. The major mistake would be to keep building generic infrastructure around that core when mature components already exist.

**Keep KeyDeck-owned:** canonical state, tasks/timeline, Project Brain, route decisions, evidence-first failure classification, elastic key-pool authorization, Recovery Coordinator, Candidate Collection, Tool Journal, Secret Broker, Context Compiler identity/invalidation, authenticated core/presentation boundary, desktop supervisor, proof receipts and financial-safety policy.

**Replace or wrap generic plumbing:** provider drivers/format translation, low-level MCP wire client, extension sandbox runtime, agent-environment package/lock machinery, telemetry export vocabulary, installer/updater mechanics, network-fault injection, model/provider eval harnesses, browser/Windows UI QA, and development spec/context governance.

The highest-value architecture change is **Bifrost as a constrained provider data plane beneath KeyDeck policy**. The highest-value non-provider additions are **official MCP Go SDK behind the existing Adapter seam, Extism/Wasm, APM, AG-UI as projection only, OpenTelemetry GenAI, Toxiproxy, Promptfoo, Playwright, FlaUI, Velopack, Spec Kit, and completion of the exact codebase-memory-mcp gate**.

## Disposition totals

| Disposition | Artifact rows | Meaning |
|---|---:|---|
| `KEEP_PROVEN` | 4 | Already proven/used; preserve and deepen |
| `WRAP_OR_REPLACE_GENERIC` | 1 | Use premade infrastructure to replace/wrap generic KeyDeck plumbing |
| `ADOPT_BEFORE_SHIPPING` | 19 | High-value addition before public shipping |
| `PROTOTYPE_NEXT` | 17 | Strong candidate; requires an isolated proof before integration |
| `REFERENCE_ONLY` | 31 | Mine ideas/tests/UX, but no runtime dependency |
| `DEFER_BY_DESIGN` | 18 | Useful later; deliberately not current core |
| `REJECT` | 5 | Do not adopt in the planned role |
| `QUARANTINE` | 3 | Do not copy/integrate until trust/license issue is resolved |
| `DUPLICATE` | 5 | Exact duplicate artifact; deduplicate by SHA |

## The 12 strongest actions

### 1. Bifrost constrained data-plane proof

Use Bifrost only for provider protocol/driver plumbing. KeyDeck passes no fallback list, disables retries/cache, chooses key/provider itself and interprets all failures.

### 2. Official MCP Go SDK adapter proof

Replace only `internal/mcpbridge/client.go`-style JSON-RPC/stdio plumbing behind the existing `Adapter` seam. Keep all KeyDeck safety layers above it.

### 3. Extism/Wasm extension host

Default untrusted/community executable extensions to Wasm with no filesystem/network by default and explicit capabilities. Fetch/pin the missing `extism/go-sdk` before implementation.

### 4. APM agent-environment packaging

Use APM for manifests, lockfiles, integrity, drift and SBOM. KeyDeck remains the execution/permission authority.

### 5. AG-UI projection adapter

Project authenticated KeyDeck snapshot/timeline events into AG-UI for client interoperability. Never ingest AG-UI as canonical truth.

### 6. OpenTelemetry + GenAI conventions

Add standardized non-secret telemetry exports while retaining Proof Receipts as stronger canonical evidence.

### 7. Failure Lab stack

Use Toxiproxy for deterministic network faults, Promptfoo for provider/model/optimizer evals, Waza for skills/agents, Playwright for renderer QA and FlaUI for native Windows QA.

### 8. Velopack update/install proof

Use Velopack for delivery mechanics while KeyDeck keeps migration, repair, rollback, supervisor ownership and child-binary attestation.

### 9. Spec Kit + AgentRC governance

Use Spec Kit for constitution/spec/plan/tasks/implement and AgentRC experimentally to keep agent instructions/context current.

### 10. MCP Registry catalog integration

Use the official Registry to discover candidates, never to grant trust/permissions automatically.

### 11. codebase-memory-mcp exact binary gate

Finish exact v0.8.1 executable swap-in validation and keep it isolated as an MCP server rather than vendoring generated parser source.

### 12. Staged supply-chain trust

Near term: exact hashes + signed release evidence + Cosign/in-toto. Later: TUF delegation and ORAS/OCI pack transport after pack format freezes.

## Most important source-level findings

### Bifrost is the biggest engineering-time saver — with a hard safety boundary

- The uploaded Bifrost snapshot contains a large Go core, 28 provider directories, key handling, retries, fallbacks, MCP, plugins, telemetry and semantic-cache facilities.
- Its own core treats `AllowFallbacks == nil` as fallback-allowed and proceeds through configured fallback providers after most errors unless explicitly blocked. That is incompatible with KeyDeck’s evidence-first rule for ambiguous failures.
- Therefore Bifrost should be a **driver/data-plane layer only**. KeyDeck must retain provider/key selection, failure scope classification, replay authorization, key-pool rotation, fallback priority and cache policy.
- Bifrost requires Go 1.26.4 while KeyDeck v0.35 is Go 1.23. The first proof should use a supervised sidecar or isolated adapter, not a casual whole-core toolchain upgrade.

### 9Router contains useful ideas and an unsafe default for KeyDeck

- Its account-fallback function returns fallback=true for recognized rules and also for **any unmatched error**.
- Its RTK path explicitly compresses tool-result content **in place**. This may save tokens, but KeyDeck previously proved that transparent/pass-through behavior and evidence-preserving context matter more than blind mutation.
- Harvest quota UX, provider/account adapters, translation tests and token-saving experiments. Do not adopt its routing/fallback brain.

### The official MCP SDK fits the seam KeyDeck already designed

- `mcpbridge.Adapter` already states that a future official-SDK adapter can implement the interface, while Tool Journal and permissions stay above it.
- This is an unusually clean replacement opportunity: swap low-level JSON-RPC/stdio mechanics, not the proven safety stack.
- The uploaded official Go SDK requires Go 1.25, so the adapter proof and toolchain proof must be separate.

### AG-UI should extend the presentation edge, not replace KeyDeck state

- KeyDeck’s current presentation shell is a thin authenticated projection over atomic snapshots and task commands.
- AG-UI provides rich message, tool, run, state and reasoning event types. The correct direction is one-way projection from KeyDeck canonical state into AG-UI-compatible events.
- Never make client event replay or AG-UI event order a second source of canonical truth.

### Extism is the right default extension boundary, but the upload set is incomplete

- Extism/Wasm matches the approved design for untrusted third-party extensions with host-controlled capabilities.
- The uploaded `extism/extism` core repository is not the actual Go host SDK KeyDeck needs. Fetch and pin `extism/go-sdk` before implementation.
- XTP Bindgen is a strong schema-first SDK strategy, but the current upload is the core parser; exact language bindgen templates are still missing.
- HashiCorp go-plugin remains a trusted-native exception only, not the community default.

### The uploaded skills ecosystem should become curated packs, not 40 new runtime dependencies

- Agent Skills, AGENTS.md, APM and Waza are infrastructure. Supabase/Prisma/Auth0/Cloudflare/Expo/etc. are domain content examples.
- KeyDeck should curate and pin small capability-scoped packs, evaluate them, and record provenance. It should not inject the entire corpus into context or install everything globally.
- Item 032 is a collection error: it is the full Motion library repository, not a Framer Motion guidance skill.
- Item 041 is superseded/archived and should be replaced with the current successor only if later needed.

## Toolchain compatibility gate

| Candidate | Uploaded requirement | KeyDeck v0.35 | Decision |
|---|---:|---:|---|
| Bifrost | Go 1.26.4 | Go 1.23 | Sidecar/isolated proof first |
| Waza | Go 1.26.4 | Go 1.23 | Dev tool only; no core import |
| Tailscale/tsnet | Go 1.26.5 | Go 1.23 | Deferred Mesh gate |
| Official MCP Go SDK | Go 1.25 | Go 1.23 | Separate toolchain + adapter proof |
| CUE | Go 1.25 | Go 1.23 | External CLI or later upgrade |
| HashiCorp go-plugin | Go 1.25 | Go 1.23 | Deferred trusted-native path |
| go-workflows | Go 1.24.5 | Go 1.23 | Do not touch canonical recovery; later background jobs only |
| AG-UI community Go SDK | Go 1.24.4 | Go 1.23 | Protocol projection can precede SDK import |

## Exact KeyDeck custom-code decision

### Keep KeyDeck-owned

- `candidatecollection` — lifecycle barrier, stale-assessment protection and canonical commit evidence
- `conformance` — exact provider observation fragments and fail-closed unknown behavior
- `recovery`, `routing`, `continuity`, `handoff`, `session` — canonical continuity and evidence-based recovery
- `tasks`, `timeline`, `projectbrain`, `contextcompiler`, `contextscout` — canonical project state and bounded context identity
- `tooljournal`, `secretbroker` — replay safety and secret resolution/redaction
- `pool`, `costguard` — KeyDeck-specific key-pool and financial-safety policy
- `corehost`, `supervisor`, `visualshell` — authenticated local ownership and desktop safety boundaries
- `proofreceipt` — durable provenance tying safety evidence together

### Replace or wrap generic parts

| Current KeyDeck area | Premade candidate | Exact boundary |
|---|---|---|
| `providerhttp` + parts of `apiengine` and future provider adapters | Bifrost | Replace generic request/format/provider plumbing only; KeyDeck owns policy |
| `mcpbridge/client.go` low-level wire | Official MCP Go SDK | Keep Adapter, identity, schema, permissions, manager, Secret Broker and Tool Journal |
| presentation event compatibility | AG-UI | Add projection adapter; keep atomic KeyDeck snapshot authoritative |
| agent environment packaging/drift | APM | APM packages; KeyDeck authorizes execution |
| future plugin runtime | Extism + XTP Bindgen | Wasm default; native subprocess only by exception |
| telemetry export | OpenTelemetry GenAI | Export only; Proof Receipts remain canonical |
| update/install mechanics | Velopack + WinGet | Delivery mechanics only; KeyDeck keeps repair/rollback/attestation |
| network/model/UI test scaffolding | Toxiproxy + Promptfoo + Playwright + FlaUI | Test/development only |
| spec/context-development workflow | Spec Kit + AgentRC | Dev governance only; product Context Compiler remains canonical |

## Exact duplicates and collection anomalies

- Items 009 and 013 are exact byte duplicates.
- Items 017, 019 and 021 are exact byte duplicates.
- Items 058 and 059 are exact byte duplicates.
- Items 090 and 091 are exact byte duplicates.
- Item 032 is the Motion codebase, not a Framer Motion guidance-skill artifact.
- Item 041 says it is superseded/archived.
- Item 077 says the repository is no longer maintained and core development moved private.
- Item 024 is the exact MCP filesystem package used in proofs, but its tarball declares a license file that is absent from the package.
- Batch labels 5 and 6 did not add standalone files, but earlier uploaded bundle ZIPs contained many nested archives; the audit covers the physically available 102 artifact files.

## Full adoption ledger

The CSV deliverable contains one row per uploaded archive/package variant plus the prompt reference, including SHA-256, license audit, priority, disposition, recommended action and risk boundary.

### Priority legend

- **P0:** next proof/integration sequence before resuming broad product work.
- **P1:** before public shipping, after isolated proofs.
- **P2:** post-core optional/future architecture.
- **P3:** reference/archive only.
- **HOLD:** trust, license, collection or obsolescence problem.

## Recommended execution sequence

1. **Gate A — Freeze the audit:** Preserve this ledger and exact artifact hashes. Do not add dependencies merely because they were uploaded.
2. **Gate B — Bifrost constrained data plane:** Prove one provider path with zero Bifrost retries/fallback/cache and compare exact responses, streaming, token/cost metadata and failure evidence against current providerhttp.
3. **Gate C — MCP official SDK seam:** After a separate Go-toolchain decision, implement one official-SDK Adapter and replay Proofs 0.22–0.25 plus Secret Broker/Tool Journal regressions.
4. **Gate D — Complete codebase-memory gate:** Validate the exact v0.8.1 executable against the production manager/registry/context provider path.
5. **Gate E — Finish Proof 0.39:** Resume secret-safe visual bootstrap with no architecture churn from unrelated repos.
6. **Gate F — Failure/QA stack:** Add Toxiproxy, Playwright, FlaUI and Promptfoo to independent test harnesses.
7. **Gate G — Packaging and observability:** Prototype OTel export and Velopack install/update without weakening ownership, rollback or attestation.
8. **Gate H — Extension platform:** Fetch exact extism/go-sdk, freeze plugin capability schema, prove Wasm deny-by-default, then add XTP SDK generation.
9. **Gate I — Interop protocols:** Add AG-UI projection, OASF capability records, official MCP Registry catalog and ACP adapters in that order.
10. **Gate J — Supply-chain hardening:** Add Cosign/in-toto now; defer TUF/ORAS until pack/update delegation is real.

## Final conclusion

The correct outcome is **not** “install all repos.” It is a thinner KeyDeck surrounded by mature infrastructure while its unique safety kernel remains sovereign. The strongest uploaded components can save substantial future engineering work, but only if they are forced behind KeyDeck-owned policy seams and proven one at a time. The first high-value change is the Bifrost constrained data-plane proof; the first high-value no-risk improvements are the test/governance tools and the exact codebase-memory validation.