# KeyDeck Feasibility Lab v0.35.0 — Reconstructed

The physically recovered source anchor is immutable **v0.21.0 / Proof 0.24**. This tree reconstructs and re-proves the forward line through **Proof 0.38**, including a production desktop supervisor above the secure visual renderer and authenticated presentation boundary. It is not a claim of byte-identical recovery of the lost historical post-v0.21 archives. See `docs/RECONSTRUCTION_BOUNDARY_v0.34.0.md`.

KeyDeck’s invariant remains:

> **One canonical project. One visible chat. One canonical memory/state. Engines are replaceable workers.**

## Proven milestones

- Proof 0.1 — financially safe elastic API-pool policy.
- Proof 0.2 — same-provider mid-answer continuation and ambiguity protection.
- Proof 0.3 — persistent Tool Journal and replay safety.
- Proof 0.4 — KeyDeck-owned canonical session across engine switches.
- Proof 0.5 — real Codex handoff, restart/resume and switch-back.
- Proof 0.6 — automatic API-pool exhaustion to real Codex, restart/resume and recovered API.
- Proof 0.7 — API mid-answer exhaustion to persisted state, real Codex continuation, restart/resume and recovered API.
- Proof 0.8 — real Context Compiler benchmark reduced exploration and input usage without correctness loss.
- Proof 0.9 — durable Task Contract and evidence-based Progress Proof with crash reconciliation.
- Proof 0.10 — provider/optimizer conformance with byte-preserving Optimization OFF and verified-only Optimization ON.
- Proof 0.11 — durable Universal Activity Timeline and evidence-based Proof Receipts with restart exact-once behavior.
- Proof 0.12 — integrated Recovery Coordinator with process-level crash windows and exactly-once canonical result commit.
- Proof 0.13 — durable Engine-Neutral Runtime Contract with restart-safe bindings, dispositions and exactly-once recovery integration.
- Proof 0.14 — real Microsoft APM + Waza proving ground with scoped adoption and explicit limitations.
- Proof 0.15 — real-provider conformance evidence framework with exact identity, provenance, expiry and conservative fail-closed behavior.
- Proof 0.16 — exact Aerolink invalid-credential evidence fragment.
- Proof 0.17 — exact Aerolink HTTP 402 included-usage-window-limit evidence fragment with conservative scope.
- Proof 0.18 — bounded paired Aerolink scope capture preserved inconclusive replacement timeout without unsafe rotation.
- Proof 0.19 — real MCP stdio execution through permissions, Tool Journal, timeline and Proof Receipt safety.
- Proof 0.20 — MCP bridge hardening with permission profiles, bounded frames, cancellation safety and adapter seam.
- Proof 0.21 — MCP Secret Broker and schema-aware authorization with scoped references and redaction.
- Proof 0.22 — immutable third-party MCP package identity and replay/ambiguity safety.
- Proof 0.23 — durable MCP registration/discovery, drift detection and default-deny permission proposals.
- Proof 0.24 — exact recovered baseline: durable local runtime bindings and MCP manager.
- Proof 0.25 — reconstructed manager-gated MCP execution routing; unsafe server/tool states block before adapter construction.
- Proof 0.26 — reconstructed Production Context Scout/Compiler with source fingerprints, exact packet/cache identity, offline reuse, hygiene and provenance.
- Proof 0.27 — reconstructed canonical Project Brain revision stream bound to verified context/source/inspection evidence.
- Proof 0.28 — reconstructed production Handoff Package assembly with task, context, MCP, brain, inspection, passport, capability and runtime-request binding.
- Proof 0.29 — reconstructed durable handoff persistence, replay-safe engine execution, restart reconciliation and persistent cancellation.
- Proof 0.30 — reconstructed deterministic evidence-based routing and route-bound continuation safety.
- Proof 0.31 — reconstructed candidate collection and conservative reconciliation with no majority-vote truth.
- Proof 0.32 — reconstructed explicit decisive-evidence resolution and selected-only exact-once canonical recovery.
- Proof 0.33 — reconstructed integrated handoff/routing/reconciliation/recovery contract, PASS 12/12.
- Proof 0.34 — integrated Production Candidate Collection Coordinator, PASS 32/32, including durable recovery preparation, semantic replay hardening and exact evidence-chain binding.
- Proof 0.35 — production `keydeck-core` host and authenticated loopback control plane, PASS 16/16, with single-owner fail-closed lease authority, durable idempotent commands, canonical task/timeline projections, credential-safe attestation and deterministic proof replay.
- Proof 0.36 — typed authenticated core client and atomic desktop presentation projection boundary, PASS 20/20, with stale-instance refusal, no direct-store fallback, consistent concurrent snapshots and renderer-neutral shell commands.
- Proof 0.37 — secure visual desktop renderer, PASS 21/21, with secret launch paths, DNS-rebinding and cross-origin protection, sanitized browser state, canonical task commands through presentation only, restart-safe re-attestation and real Chromium rendering QA.
- Proof 0.38 — production desktop supervisor, PASS 21/21, with exact child binary identity, core-first attestation, bounded renderer restart, fatal ownership/identity drift and renderer-before-core shutdown.

## v0.35.0 reconstructed release validation

The source line is sealed as **v0.34.0-RECONSTRUCTED** only after:

- full repository tests and vet;
- race coverage across stateful forward-line packages;
- complete Proof 0.9–0.38 regression replay;
- exact pinned third-party MCP runtime replay for Proofs 0.22–0.25;
- repeated anti-flake checks;
- clean-source replay;
- deterministic Windows x64 rebuild;
- precise source/binary leak scans;
- deterministic source/proof/continuation packages.


## Proof 0.38 — Production Desktop Supervisor

```bash
go run ./cmd/proof38
```

Production desktop supervisor (release builds inject exact child SHA-256 values):

```bash
go run ./cmd/keydeck-desktop --data-dir /path/to/keydeck-data
```

The production command intentionally does not pass the secret renderer URL to an OS opener command line. See `docs/PROOF_0.38_SPEC.md` and `docs/RECONSTRUCTION_BOUNDARY_v0.35.0.md`.

## Proof 0.37 — Secure Visual Desktop Renderer

```bash
go run ./cmd/proof37
```

Product-facing visual shell:

```bash
go run ./cmd/keydeck-desktop-ui --data-dir /path/to/keydeck-data --expected-build keydeck-v0.34.0-reconstructed
```

See `docs/PROOF_0.37_SPEC.md` and `docs/RECONSTRUCTION_BOUNDARY_v0.34.0.md`.

## Proof 0.36 — Authenticated Core Client + Desktop Presentation Projection Boundary

```bash
go run ./cmd/proof36
```

Renderer-neutral product shell:

```bash
go run ./cmd/keydeck-desktop-shell --data-dir /path/to/keydeck-data --expected-build keydeck-v0.34.0-reconstructed
```

See `docs/PROOF_0.36_SPEC.md` and `docs/RECONSTRUCTION_BOUNDARY_v0.33.0.md`.

## Proof 0.35 — Production Core Host + Authenticated Loopback Control Plane

```bash
go run ./cmd/proof35
```

Production host:

```bash
go run ./cmd/keydeck-core --data-dir /path/to/keydeck-data --listen 127.0.0.1:0
```

See `docs/PROOF_0.35_SPEC.md` and `docs/RECONSTRUCTION_BOUNDARY_v0.32.0.md`.

## Proof 0.34 — Production Candidate Collection Coordinator (Integrated Reconstructed Line)

```bash
go run ./cmd/proof34
```

See `docs/PROOF_0.34_INTEGRATED_SPEC.md` and `docs/RECONSTRUCTION_BOUNDARY_v0.32.0.md`.

## Proof 0.33 — Integrated Handoff, Routing, Reconciliation and Recovery (Reconstructed)

```bash
go run ./cmd/proof33
```

See `docs/PROOF_0.33_RECONSTRUCTED_SPEC.md`.

## Proof 0.32 — Evidence-Backed Resolution + Selected-Only Canonical Recovery (Reconstructed)

```bash
go run ./cmd/proof32
```

See `docs/PROOF_0.32_RECONSTRUCTED_SPEC.md`.

## Proof 0.31 — Candidate Collection + Conservative Reconciliation (Reconstructed)

```bash
go run ./cmd/proof31
```

See `docs/PROOF_0.31_RECONSTRUCTED_SPEC.md`.

## Proof 0.30 — Deterministic Evidence Routing + Safe Continuation (Reconstructed)

```bash
go run ./cmd/proof30
```

See `docs/PROOF_0.30_RECONSTRUCTED_SPEC.md`.

## Proof 0.29 — Durable Handoff Execution + Restart Reconciliation (Reconstructed)

```bash
go run ./cmd/proof29
```

See `docs/PROOF_0.29_RECONSTRUCTED_SPEC.md`.

## Proof 0.28 — Production Handoff Package Assembly (Reconstructed)

```bash
go run ./cmd/proof28
```

See `docs/PROOF_0.28_RECONSTRUCTED_SPEC.md`.

## Proof 0.27 — Canonical Project Brain + Context Inspection (Reconstructed)

```bash
go run ./cmd/proof27
```

See `docs/PROOF_0.27_RECONSTRUCTED_SPEC.md`.

## Proof 0.26 — Production Context Scout/Compiler (Reconstructed)

```bash
go run ./cmd/proof26
```

See `docs/PROOF_0.26_SPEC.md` and `docs/RECONSTRUCTION_BOUNDARY_v0.32.0.md`.

## Proof 0.25 — Manager-Gated MCP Execution Routing (Reconstructed)

The real third-party scenario requires the same exact pinned runtime evidence as Proof 0.24 plus `KEYDECK_PROOF25_NODE`. See `docs/PROOF_0.25_SPEC.md`.

## Proof 0.24 — Durable Local Runtime Bindings + MCP Server Manager

Local integrated proof requires the exact pinned third-party runtime/evidence paths:

```bash
KEYDECK_PROOF24_SERVER_JS=/path/to/server-filesystem/dist/index.js \
KEYDECK_PROOF24_PACKAGE_TARBALL=/path/to/modelcontextprotocol-server-filesystem-2026.7.4.tgz \
KEYDECK_PROOF24_PACKAGE_LOCK=/path/to/runtime/package-lock.json \
go run ./cmd/proof24
```

See `docs/PROOF_0.24_SPEC.md`.

The proof keeps portable registration/discovery separate from machine-local paths, persists explicit approvals and enable/disable state, performs a real 14-tool health check, preserves portable state when the runtime disappears, and requires explicit rebind events for repair.


## Proof 0.23 — Production MCP Server Registration and Discovery Contracts

Local integrated proof requires the exact pinned third-party runtime/evidence paths:

```bash
KEYDECK_PROOF23_SERVER_JS=/path/to/server-filesystem/dist/index.js \
KEYDECK_PROOF23_PACKAGE_TARBALL=/path/to/modelcontextprotocol-server-filesystem-2026.7.4.tgz \
KEYDECK_PROOF23_PACKAGE_LOCK=/path/to/runtime/package-lock.json \
go run ./cmd/proof23
```

See `docs/PROOF_0.23_SPEC.md`.

The proof registers one immutable server identity, keeps runtime configuration separate, persists a real 14-tool discovery snapshot, detects runtime/capability drift, and generates permission suggestions without auto-granting any tool.


## Proof 0.22 — Immutable Third-Party Local MCP Server

Local integrated proof requires the pinned package runtime/evidence paths:

```bash
KEYDECK_PROOF22_SERVER_JS=/path/to/runtime/node_modules/@modelcontextprotocol/server-filesystem/dist/index.js \
KEYDECK_PROOF22_PACKAGE_TARBALL=/path/to/modelcontextprotocol-server-filesystem-2026.7.4.tgz \
KEYDECK_PROOF22_PACKAGE_LOCK=/path/to/runtime/package-lock.json \
KEYDECK_PROOF22_PACKAGE_JSON=/path/to/runtime/node_modules/@modelcontextprotocol/server-filesystem/package.json \
go run ./cmd/proof22
```

See `docs/PROOF_0.22_SPEC.md`.

The proof uses exact package identity and executes only against a temporary allowlisted filesystem root. KeyDeck remains responsible for permissions, schema policy, Secret Broker behavior, Tool Journal replay safety, timeline provenance, and Proof Receipts.


## Proof 0.21 — MCP Secret Broker + Schema-Aware Authorization

Local deterministic proof:

```bash
go run ./cmd/proof21
```

See `docs/PROOF_0.21_SPEC.md`.

The proof uses scoped secret references and a separate local MCP proof server. Raw secret values are resolved only after the Tool Journal chooses execution and are redacted before persistence.


## Proof 0.20 — MCP Bridge Hardening

Local deterministic proof:

```bash
go run ./cmd/proof20
```

See `docs/PROOF_0.20_SPEC.md`.

The proof uses a separately compiled local MCP proof server process. It does not claim third-party production server or official Go SDK package integration.


## Proof 0.19 — Real MCP Tool Execution + Durable Tool Journal Bridge

Local deterministic proof:

```bash
go run ./cmd/proof19
```

See `docs/PROOF_0.19_SPEC.md`.

This proof uses real MCP stdio wire semantics with a local deterministic subprocess server. It does not claim the official Go SDK package itself was integrated because the sandbox could not fetch its module dependencies.


## Proof 0.18 — Bounded Paired Scope Capture

Local evidence integration proof:

```bash
go run ./cmd/proof18
```

See `docs/PROOF_0.18_SPEC.md`.

This proof intentionally treats the replacement timeout as inconclusive. It does not authorize replay or key rotation and explicitly forbids automatic rerun of the same real-provider experiment.


## Proof 0.17 — Second Real Provider Evidence Capture

Local evidence integration proof:

```bash
go run ./cmd/proof17
```

See `docs/PROOF_0.17_SPEC.md`.

This proof trusts the exact HTTP 402 response as an observed Aerolink usage-window-limit behavior, but does not yet allow automatic replay or key rotation because key/account scope has not been proven.


## Proof 0.16 — First Real Provider Evidence Capture

Local evidence integration proof:

```bash
go run ./cmd/proof16
```

See `docs/PROOF_0.16_SPEC.md`.

This proof trusts only the exact captured Aerolink invalid-token response for the exact endpoint/model/version identity. It does not claim exhaustion, rate-limit, busy/outage, streaming, cache or billing semantics.


## Proof 0.15 — Real Provider Conformance Framework

Local deterministic proof:

```bash
go run ./cmd/proof15
```

See `docs/PROOF_0.15_SPEC.md`.

This proof validates the framework only. It does not mark any paid provider conformant.


## Proof 0.14 — Microsoft APM + Waza Proving Ground

Local integrated evidence proof:

```bash
go run ./cmd/proof14
```

See `docs/PROOF_0.14_SPEC.md`.

Decision:

`ADOPT_APM_AND_WAZA_WITH_SCOPED_OWNERSHIP_AND_LIMITATIONS`

The proof does not claim remote Git pin resolution, non-empty MCP dependency resolution, or live-model quality comparison.


## Proof 0.13 — Engine-Neutral Runtime Contract

Local proof:

```bash
go run ./cmd/proof13
```

See `docs/PROOF_0.13_SPEC.md`.


## Proof 0.12 — Integrated Recovery Coordinator

Local process-crash proof:

```bash
go run ./cmd/proof12
```

See `docs/PROOF_0.12_SPEC.md`.

## Proof 0.11 — Universal Activity Timeline and Proof Receipts

Local proof:

```bash
go run ./cmd/proof11
```

See `docs/PROOF_0.11_SPEC.md`.

## Proof 0.10 — Provider / Optimizer Conformance

Local proof:

```bash
go run ./cmd/proof10
```

See `docs/PROOF_0.10_SPEC.md`.

## Proof 0.9 — Durable Task Contract and Progress Proof

Local proof:

```bash
go run ./cmd/proof09
```

See `docs/PROOF_0.9_SPEC.md`.

## Proof 0.8 — Context Compiler

Local policy proof:

```bash
go run ./cmd/proof08
```

Real Windows / ChatGPT Plus benchmark:

```powershell
.\RUN-REAL-PROOF-0.8.ps1
```

See:

- `docs/PROOF_0.8_SPEC.md`
- `docs/PROGRESS_PROOF_v0.5.md`
- `docs/GOOGLE_ENGINE_INTEGRATION_STATUS.md`

## Safety boundary

Proof 0.8 does not run the third-party install script and does not modify Codex, Claude, Gemini, MCP, or agent configuration files. The runner downloads one pinned Windows archive, verifies the published checksum manifest against a hard-pinned SHA-256, verifies the archive hash, and runs the binary with a private disposable cache directory.
