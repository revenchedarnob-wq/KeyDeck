# Main Branch Protection Status

Generated UTC: `2026-07-09T21:16:00Z`

## Current observed state

`main` was observed through the connected GitHub branch API with protection disabled before this automation branch was created.

## Required target state

Before treating `main` as a hardened canonical branch, configure protection/rulesets to require:

- pull request review or owner-controlled merge discipline;
- passing `ci-fast` before merge;
- no direct force pushes;
- no deletion of `main`;
- signed/tagged release discipline when publishing large deliverables.

## Manual blocker

The current connector can inspect this state, but repository ruleset/branch-protection mutation is not confirmed available in this session. Configure this in GitHub settings if no connected tool becomes available.
