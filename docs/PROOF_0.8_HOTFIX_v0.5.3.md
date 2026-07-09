# Proof 0.8 Hotfix v0.5.3

Observed real-Windows evidence from v0.5.2:

- structural indexing and focused packet generation succeeded;
- the baseline Codex arm entered the native Windows sandbox;
- the elevated sandbox produced `CreateProcessWithLogonW failed: 1056` once;
- Codex then attempted `go test ./...`, but the sandbox environment did not expose a Go toolchain.

The benchmark goal is static code diagnosis, not build/test execution. v0.5.3 therefore:

1. makes the benchmark explicitly toolchain-independent and forbids builds/tests/package-manager/compiler/linter runs;
2. launches Codex App Server with the official one-run `windows.sandbox="unelevated"` fallback for this disposable benchmark only;
3. keeps `workspace-write` boundaries and `approval_policy=never`; it does not use danger-full-access;
4. records diagnostics separately for each arm;
5. refuses any Context Compiler adoption claim when known Windows sandbox/toolchain contamination is detected.

This hotfix does not change the user's global Codex configuration.
