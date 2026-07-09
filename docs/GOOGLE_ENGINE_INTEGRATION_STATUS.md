# Google Engine Integration Status — v0.5.0

## Decision

Google AI Pro remains a required KeyDeck engine, but implementation is temporarily deferred rather than forced through a brittle integration.

## Reason

The architecture requires a worker boundary with all of these properties:

- supported authentication through the user's Google subscription;
- machine-readable task invocation;
- reliable result and failure status;
- scoped project permissions;
- non-interactive or explicitly mediated approvals;
- no terminal-screen scraping;
- no unsafe global access merely to make automation work.

The current consumer Google coding path has changed recently and the available automation boundary does not yet meet all of those requirements strongly enough for the KeyDeck safety core.

## Forbidden shortcuts

KeyDeck must not:

- scrape an interactive TUI as the canonical integration;
- steal browser cookies or tokens;
- run an unrestricted headless agent just to claim Google support;
- pretend stateless one-shot execution preserves hidden conversation state;
- make Google the owner of canonical project/session state.

## Planned shape

When a safe supported integration is available:

1. KeyDeck remains the canonical owner.
2. Google receives a scoped Agent Passport / task capsule.
3. Google returns events, artifacts and evidence.
4. KeyDeck records outcomes in the same task, tool and proof systems.
5. Subscription-backed Google execution remains separate from API-provider adapters.

Until then, work continues on independently provable KeyDeck layers rather than blocking the entire product.
