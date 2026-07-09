# Reconstruction Boundary — v0.31.0-RECONSTRUCTED

## Exact physically recovered ancestor

The only physically recovered immutable forward-line source anchor is:

- `KeyDeck-Feasibility-Lab-v0.21.0.zip`
- SHA-256: `ab9d8909cd5169f0b01fcfb727f246533dfb854c1ea4b3f2efe6182c48e7d6d6`
- integrated milestone: Proof 0.24

Its proof package and continuation evidence were also recovered and verified against their recorded hashes.

## Reconstructed and re-proven forward line

This tree reconstructs Proofs 0.25–0.34 from the exact v0.21.0 ancestor plus saved acceptance contracts, milestone evidence, and previously hardened Proof 0.34 staging source.

The reconstructed proofs are real current-source executions. They are **not** claims that lost historical post-v0.21 archives were recovered byte-for-byte.

Evidence classes must remain distinct:

1. **Exact recovered:** v0.21.0 / Proof 0.24.
2. **Reconstructed and re-proven:** Proofs 0.25–0.34 in this source line.
3. **Historical milestone records:** useful architecture/evidence references, not physical byte identity.

## Proof 0.34 integration boundary

The previously hardened Proof 0.34 v0.3.0 coordinator core is now physically present in this reconstructed source under `internal/candidatecollection` and connected to real KeyDeck task, handoff, routing, runtime, session engine-ledger, Recovery Coordinator, timeline and Proof Receipt APIs.

This removes the old “parallel staging overlay” limitation for the reconstructed line. It does not retroactively recover lost historical archives.

## Release naming

The current release name must include `RECONSTRUCTED`:

`v0.31.0-RECONSTRUCTED`

Do not shorten that to an exact historical v0.31.0 claim.
