# KeyDeck Progress Proof — v0.5.1

This document reports acceptance evidence. It does not estimate an overall product-completion percentage.

## Proven on Zarif's real Windows machine

### Proof 0.5

- Official Codex CLI authenticated with ChatGPT Plus.
- Official Codex App Server integration.
- Real Codex thread creation.
- Real reasoning, command-execution and file-change events.
- Real project edits.
- Full App Server restart.
- Resume of the exact same real Codex thread.
- Switch back to the KeyDeck/API side.

### Proof 0.6

- Synthetic three-key API pool exhausted each key exactly once.
- Only `all keys unavailable` triggered cross-engine fallback.
- Provider-wide busy, ambiguous failure and cost-safety block remained ineligible.
- Same canonical task handed automatically to real Codex.
- Exact Codex thread survived restart/resume.
- Recovered API received the same canonical task state.

### Proof 0.7

- One visible API answer began in the synthetic API pool.
- Explicit key exhaustion rotated safely across keys.
- Partial visible response state was persisted while in-flight.
- A full KeyDeck restart occurred before handoff.
- Real Codex received the persisted partial response as continuation context.
- Real Codex continued the same visible response and changed project files.
- A second full KeyDeck/App Server restart occurred.
- The exact real Codex thread resumed.
- API recovery received the completed cross-engine state.

Final real result:

`PASS: API mid-answer exhaustion -> persisted partial state -> real Codex continuation -> restart/resume -> recovered API succeeded.`

## Proven locally in v0.5.1

### Context Compiler policy

- Hybrid packet combines structural evidence and exact source snippets.
- Packet is bounded by explicit character budget.
- Git status is bounded and cannot flood the packet.
- Structural CLI calls use documented arguments.
- Exact lower-ranked evidence omissions are counted.
- Local policy proof repeatedly identifies the relevant routing/cache/call-path evidence.
- Third-party structural engine bootstrap is pinned and checksum-verified by design.

Validation completed:

- `go test ./...`
- `go vet ./...`
- `go test -race ./...`
- critical Context Compiler / benchmark / Codex metrics tests repeated 20 times;
- local Proof 0.8 policy run repeated 10 times.

## Built, awaiting one real Windows run

### Proof 0.8

Real controlled benchmark:

- baseline fresh real Codex thread;
- context-assisted fresh real Codex thread;
- identical repositories;
- exact correctness checks;
- command-execution comparison;
- token comparison when App Server telemetry is available;
- no adoption claim unless measured evidence passes the gate.

## Intentionally deferred

### Google AI Pro worker

Current safe machine-readable integration is not yet strong enough to justify brittle terminal automation or unsafe headless execution. The Google engine remains in the architecture, but integration is deferred until a supported stable boundary is available or a narrowly-scoped safe adapter can be proven.

## Not yet proven

- Real Context Compiler efficiency on Zarif's Windows machine.
- Real provider-specific Aerolink conformance.
- Cross-engine continuation from a real paid API provider.
- Google AI Pro worker integration.
- Production durable workflow engine.
- Production UI and one-click universal installer.
