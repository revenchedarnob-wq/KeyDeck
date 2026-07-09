# Proof 0.10 — Provider / Optimizer Conformance

## Goal

Prove that KeyDeck can add provider-specific behavior without weakening the universal safety core.

## Required proofs

1. Optimization OFF preserves the exact request bytes and never invokes an optimizer.
2. Optimization ON activates only for an exact provider/version match backed by dated evidence marked Verified, with correctness preserved and measurable benefit.
3. An exact provider-specific key-scoped exhaustion signal may rotate to the next key.
4. An unknown 429 remains ambiguous and does not spend a backup key.
5. A provider-wide busy signal preserves every backup key.
6. An ambiguous 502 is not replayed and does not rotate keys.
7. Cost-thrash protection blocks the next request before a backup key can be consumed.

## Safety boundary

This proof does not claim production conformance for Aerolink, Anthropic, OpenAI, Google, xAI, or any other real provider.

The fixture profile proves the architecture and control flow only. A real provider profile still requires exact captured behavior, provider/version/date evidence, and dedicated conformance testing.

## Run

```bash
go run ./cmd/proof10
```
