# Proof 0.28 — Production Handoff Package Assembly (Reconstructed)

Acceptance contract:

- seal current Task Contract state, required outcomes, pending/passed checks and forbidden scope;
- bind exact verified context packet and source fingerprint;
- bind canonical MCP Server ID and schema SHA;
- bind Project Brain revision and inspection evidence;
- bind session passport and required engine capabilities;
- bind the exact engine-runtime request;
- reject stale task sequence, stale context, stale brain state and secret-like context before engine use;
- produce canonical tamper-evident package identity and Proof Receipt evidence.

Run: `go run ./cmd/proof28`

Status in this reconstructed line: PASS 10/10.
