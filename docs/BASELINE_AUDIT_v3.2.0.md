# KeyDeck v3.2.0 Baseline Audit

This document records what is present in the exact uploaded v3.2.0 source/release baseline. The baseline remains untouched; KeyDeck Feasibility Lab is separate.

## Verified present in source

1. **No real credit-burn guard.** The gateway does not parse or enforce cache creation/read usage patterns before allowing later requests.
2. **Hardcoded local gateway credential.** `LocalToken = "keydeck-local"` is embedded in source and used by Claude configuration and gateway authentication.
3. **Ambiguous automatic retries.** The gateway permits up to four attempts and retries transient statuses plus temporary network errors; this includes financially ambiguous outcomes.
4. **Weak gateway readiness check.** setup status primarily treats HTTP 200 from the local status endpoint as readiness instead of proving the exact approved gateway executable/version/cache mode.
5. **Unpinned skill download.** UI/UX Pro Max is downloaded from the repository `main` branch.
6. **Heuristic health probe before authentication.** the health-probe classifier runs before local request authentication.
7. **Unauthenticated status endpoints.** status aliases are handled before authentication and expose operational details.
8. **Source archive is not self-reproducible as uploaded.** `cmd/setup/main_windows.go` embeds `assets/*`, but the uploaded source ZIP contains no setup assets. A full extracted-source Windows test/build path fails until assets are created/populated; the focused gateway/migration tests do pass.

## Verified good baseline properties

- Real API keys are stored through the existing DPAPI vault path.
- Gateway binds to loopback in the source.
- The prompt-cache hotfix source uses pass-through cache policy (`prepareCachePolicy` returns the original body unchanged).
- Focused unit tests for `internal/gateway` and `internal/migration` pass on Go 1.23.2.

## Frozen forbidden regressions

The next KeyDeck architecture must never:

- inject or move cache breakpoints without provider-specific proof;
- rotate through backup keys during provider-wide failures;
- replay an ambiguous request automatically;
- replay a tool action without a journaled safety decision;
- use a universal hardcoded local gateway credential;
- report a gateway as safe without proving version, mode and executable identity;
- answer a heuristic probe before authentication;
- expose detailed local status without authorization;
- download mutable setup dependencies without pinning and integrity verification;
- enable automatic key failover before cost-safety controls can stop a repeated burn pattern.
