# Proof 0.39 — Signed Production Bundle, First-Run Bootstrap, and Repair-Safe Desktop Launch

Status: **implementation gate; not yet passed**

## Goal

Prove that a Windows user can start KeyDeck from a signed production bundle on a clean machine, repair a damaged installation safely, and reach the existing authenticated visual shell without exposing the renderer launch secret or weakening the Proof 0.35–0.38 boundaries.

Proof 0.39 extends the production lifecycle. It does not move canonical state into the installer, bootstrapper, desktop host, or supervisor.

## Initial supported target

- Windows 11 x64;
- PowerShell 7 for operator and proof commands;
- Go version declared by `go.mod`;
- per-user installation with no administrator requirement;
- the existing `keydeck-core`, `keydeck-desktop-ui`, and `keydeck-desktop` process boundaries.

Other platforms and machine-wide installation remain unsupported until they receive equivalent platform-specific proofs.

## Trust model

1. A versioned bundle manifest is signed with an offline release key.
2. The bootstrapper contains only trusted public-key identities. No private signing key, certificate password, or signing token may be committed, logged, placed in process arguments, or stored in the bundle.
3. The signed manifest binds the bundle schema, product identity, release/build identity, monotonic release sequence, supported platform, and every shipped file's relative path, byte length, SHA-256, and role.
4. Signature verification and complete file verification happen before install, repair, activation, or launch.
5. Unknown keys, invalid signatures, unsafe paths, missing files, unexpected executable files, hash/size mismatches, unsupported platforms, and downgrade attempts fail closed.
6. The final release candidate's Windows executables and DLLs require valid Authenticode signatures in addition to the KeyDeck bundle signature. An isolated proof certificate may test mechanics but cannot be reported as production publisher trust.
7. Active-version metadata is a locator, not a trust root. The selected version is reverified from its signed manifest before launch.

## Installation layout and ownership

- Installation bytes are immutable per version and live below a per-user KeyDeck program root.
- New or repaired bytes are written to a unique staging directory, verified there, and only then activated.
- A running executable is never overwritten in place.
- The immediately previous verified version is retained until the new version passes first launch, enabling an explicit safe rollback policy.
- Canonical user data remains outside versioned application directories and is never deleted by install, repair, upgrade, or rollback.
- Incomplete staging directories are never launch candidates and can be removed only after they are classified as incomplete KeyDeck-owned state.

On Windows, Go `FileMode` values are not permission evidence. The bootstrapper must set and verify ownership and DACLs with Windows security APIs. The per-user install, staging, runtime, and credential paths must reject inherited or explicit access that grants unintended principals write access. The proof must record the current user SID, expected owner class, and normalized access result without dumping a full local account or ACL inventory.

## First-run flow

1. Resolve the bundle and install roots without following an unsafe reparse point.
2. Parse the manifest with duplicate-field and unknown-field rejection.
3. Verify the trusted signing-key identity and manifest signature.
4. Validate every relative path before filesystem access; reject absolute paths, traversal, alternate data streams, reserved device names, case-colliding entries, and duplicate normalized paths.
5. Verify the complete bundle inventory, file roles, sizes, and hashes.
6. Create a private staging directory and apply verified Windows ownership/DACLs.
7. Copy files without executing them, then reverify the staged bytes and paths.
8. Atomically activate the verified version without overwriting a running version.
9. Launch only the verified `keydeck-desktop.exe` from the activated directory.
10. Record a redacted receipt containing release identity, signing-key identity, source commit, installed file hashes, action, result, and timestamps. It must contain no credential or renderer launch secret.

## Secret-safe visual bootstrap

The production desktop path must use an in-process Windows web host, expected to be WebView2 unless a smaller equivalent passes the same contract. The renderer URL is delivered from the supervisor to that host only through an in-memory callback inside `keydeck-desktop.exe`.

The renderer launch secret must never appear in:

- any process command line;
- any environment block;
- logs, errors, crash text, receipts, screenshots, or durable state;
- browser history, external-browser profiles, shortcuts, registry protocol handlers, or static browser assets;
- supervisor lease/runtime records or core runtime metadata.

The desktop host must accept only the already-attested exact loopback renderer URL from the owning supervisor. It must not navigate arbitrary URLs, follow external redirects, expose developer tools in production, or fall back to launching an external browser with the secret URL. Renderer restart must rotate the secret and replace the in-memory navigation target without restarting the exact healthy core.

## Repair, upgrade, and rollback rules

- `repair` reuses only a complete bundle whose signature and every file verify at repair time.
- Repair never trusts or copies bytes from the damaged active installation.
- Repair to the same release identity is idempotent and preserves canonical user data.
- Upgrade stages and verifies a strictly newer signed release before activation.
- Silent downgrade is forbidden. A future explicit rollback command may activate only a retained, previously verified release allowed by signed rollback policy and must emit a receipt.
- A corrupt active version, broken active locator, interrupted activation, foreign file, stale staging directory, unsafe reparse point, or locked running executable must produce a classified result and preserve the last verified recoverable version.
- Uninstall behavior is outside the executable repair transaction: application bytes may be removed explicitly, but canonical user data requires a separate explicit user choice.

## Acceptance contract

Proof 0.39 must demonstrate all of the following on Windows:

1. a correctly signed complete bundle verifies;
2. an unknown signing key and an invalid signature fail before any install write;
3. manifest tampering and file tampering fail before execution;
4. missing, extra executable, duplicate, case-colliding, traversal, device-name, alternate-data-stream, symlink, and reparse-point paths fail closed;
5. the bundle binds the exact source commit and production build identity;
6. first run installs into a private per-user version directory and launches the exact verified desktop binary;
7. Windows ownership and DACL checks use Windows security evidence rather than Go `FileMode` inference;
8. partial installation and interrupted activation cannot become active;
9. a repeated first run is idempotent;
10. same-version repair replaces damaged bytes from the verified bundle without touching canonical data;
11. repair does not overwrite a running executable and completes safely after shutdown;
12. upgrade activates only a newer verified signed release;
13. an unsigned or signed-but-older downgrade is refused;
14. a failed upgrade preserves the last verified launchable version;
15. the desktop supervisor retains exact sibling binary identity checks from Proof 0.38;
16. the visual shell opens inside the production desktop host with no external-browser secret handoff;
17. the renderer launch secret is absent from command lines, environment, logs, receipts, durable state, static assets, and inspected process metadata;
18. renderer restart rotates the secret, re-attests the same core, and updates only the in-memory host navigation;
19. missing or untrusted desktop runtime dependencies fail with a factual repair instruction and no unsafe fallback;
20. foreign, stale, corrupt, or tampered runtime/installation state is classified and handled without adopting foreign processes or bytes;
21. clean shutdown preserves renderer-before-core ordering and removes owned runtime state;
22. generated install/repair receipts are redacted, schema-validated, and hash-bound to the release;
23. the final bundle manifest, detached signature, artifacts, and hashes are reproducibly generated or any reproducibility limitation is documented;
24. `go test ./...`, `go vet ./...`, fast CI, applicable deep CI, and the Windows Proof 0.39 runner pass from a clean canonical clone.

## Required evidence

The proof report must include:

- source commit and branch;
- supported Windows architecture and observed OS build;
- bundle/release identity and monotonic sequence;
- manifest SHA-256, detached-signature SHA-256, trusted public-key identity, and Authenticode validation result;
- every acceptance scenario with durable observable evidence;
- redacted install, repair, launch, and failure receipts;
- exact commands and exit codes;
- final artifact names, sizes, and SHA-256 values;
- explicit limitations and deferred work.

No report may claim publisher trust from a test certificate or claim Windows ACL safety from POSIX-style permission bits.

## Implementation slices

1. Freeze this acceptance contract and threat model.
2. Add a platform-neutral signed-manifest parser/verifier with adversarial unit tests and no production private key.
3. Add Windows path, ownership, DACL, staging, activation, repair, and anti-downgrade primitives behind narrow interfaces.
4. Add the in-process Windows desktop web host and supervisor callback without exposing the renderer URL outside memory.
5. Add deterministic production bundle construction, signature injection, Authenticode verification hooks, and redacted receipts.
6. Add `cmd/proof39`, focused regression tests, Windows proof packaging, and release documentation.
7. Run the full clean-source validation matrix before opening the release-candidate gate.

## Non-claims

This specification does not claim Proof 0.39 has passed, does not publish a release, does not provision a production code-signing certificate, does not enable automatic network update, and does not define machine-wide installation or non-Windows desktop hosting.
