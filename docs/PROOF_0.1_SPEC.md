# Proof 0.1 — Financially Safe Elastic API Pool

## Goal

Prove that multiple low-credit keys can act as one logical API engine without turning a provider-wide problem, ambiguous failure, or cache-thrash pattern into a chain reaction that burns backup keys.

## Acceptance checks

- [x] Explicit key exhaustion can move to the next key.
- [x] Explicit invalid key can move to the next key.
- [x] Provider-wide busy stops the request and preserves backup keys.
- [x] Ambiguous 502 does not retry or rotate.
- [x] Unclassified 429 does not rotate; provider adapters must supply exact evidence before the universal core treats it as key-specific.
- [x] Repeated large-cache-miss pattern blocks later API spending before another key is touched.
- [x] Every attempt, key-state change and safety stop is evented.

## Important limitation

This proof covers failures **before any streamed output has been committed**. It does not yet prove mid-answer continuation. That is Proof 0.2.

## Cost-guard thresholds

The current numeric values in tests are deliberately marked **LAB FIXTURE ONLY**. They prove control flow and backup-key protection. Production thresholds must be derived from provider-specific evidence and the Optimization Proving Ground.
