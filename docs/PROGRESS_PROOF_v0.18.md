# KeyDeck Progress Proof v0.18.0

## New milestone

Proof 0.21 — MCP Secret Broker + Schema-Aware Authorization passed **8/8 integrated scenarios**.

Proven:

- models/tool operations can use scoped secret references rather than raw credentials;
- permission denial happens before Secret Broker planning or adapter invocation;
- schema denial happens before Secret Broker planning or adapter invocation;
- unauthorized secret scope is denied before Tool Journal begin, resolution, or adapter invocation;
- a completed secret-backed operation is reused after restart with zero additional secret resolutions;
- a real MCP tool error containing the runtime credential is redacted before return, journal, timeline, and receipt;
- raw secret value is absent from persisted journal, timeline, task, and server-state files;
- a Proof Receipt can include a redacted timeline summary and is stored exactly once.

Final deterministic Proof 0.21 server-state artifact SHA-256:

`f57820100db2470936313598dacfe819c6652a703f8b27385c7ad8873ca2a49a`

## Validation

Passed:

```text
go test ./...
go vet ./...
go test -race ./...
Proofs 0.9–0.21
```

## Important design result

The proven preflight order is:

```text
permission
→ argument schema
→ secret-scope plan
→ Tool Journal
→ secret resolution
→ adapter
```

This means completed replays can return a previous result without resolving the secret again.

## Honest boundary

The Secret Broker proof uses an immutable in-memory secret store. Production integration still needs to connect this abstraction to KeyDeck's existing encrypted Windows secret storage without changing the proven authorization and replay order.

The MCP server is still an independent KeyDeck proof server, not a third-party production server.

## Next gate

Proof 0.22 — one immutable third-party local MCP server through the hardened bridge and Secret Broker.

Requirements:

- pin the package/version immutably;
- verify package/artifact identity;
- use no broad SaaS integration;
- preserve KeyDeck-owned permissions, schemas, secret scope, Tool Journal replay safety, and timeline evidence;
- if no suitable immutable third-party server can be obtained without compromising the frozen no-broad-browsing plan, use a local adapter conformance fixture and do not overclaim third-party compatibility.
