# KeyDeck Master Architecture v0.1

Status: **temporarily frozen for feasibility proofs**

## Product invariant

**One canonical project. One visible chat. One canonical memory/state. Engines are replaceable workers.**

## Unique KeyDeck core

KeyDeck owns:

- canonical project/chat/session state;
- elastic multi-key API pools;
- safe mid-answer continuation;
- cross-engine handoff;
- cost-safety firewall and cost autopsy;
- context compilation policy;
- task contract and Progress Proof;
- tool journal and replay-safety decisions;
- checkpoints, rewind and branches;
- durable orchestration and task receipts;
- evidence-based routing.

## Integration boundaries

- API models/providers: provider adapters; Bifrost may be reused underneath only if KeyDeck retains retry/fallback/cost decisions.
- Subscription coding agents: ACP where practical.
- Agent-to-agent collaboration: A2A.
- Tools: MCP.
- Frontend event stream: AG-UI candidate, not canonical state.
- Declarative agent environment: Microsoft APM candidate.
- Reusable skills: Agent Skills / SKILL.md style; evaluate with Waza.
- Executable community extensions: Extism/WebAssembly by default.
- Observability: OpenTelemetry GenAI conventions plus KeyDeck-specific safety attributes.
- Project execution environment: existing mise.toml and/or devcontainer.json where present.

## Safety core is always on

The user-facing **API Optimization ON/OFF** switch does not disable safety.

Always-on safety includes:

- no ambiguous replay;
- provider-wide outage protection;
- cost anomaly detection;
- tool journal and checkpoints;
- secret protection;
- strict local gateway identity/authentication;
- exact routing evidence.

Optimization OFF means safest provider-native pass-through behavior. Optimization ON may activate only provider-specific profiles proven by the Optimization Proving Ground.

## Proof order

1. Exact failure classification and elastic key pool.
2. Cost guard blocks a repeated burn pattern before a backup key is spent.
3. Mid-answer same-model key continuation.
4. Tool journal and ambiguous action handling.
5. Canonical API ↔ Codex handoff.
6. API pool exhausted → Codex fallback.
7. Google engine handoff.
8. Context Compiler savings measurement.
9. Crash/restart/resume.
10. Unified frontend event stream.

## No-guessing rule

Unknown providers remain pass-through. A provider optimizer, error classifier or retry policy is not enabled by default until exact provider behavior is captured, tested and dated. Lab thresholds are test fixtures only; production thresholds require evidence.
