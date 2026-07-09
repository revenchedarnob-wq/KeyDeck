# Proof 0.8 Hotfix v0.5.1

The first real Windows run reached the pinned structural engine but failed the harness adoption gate before Codex benchmarking.

The harness bug was in KeyDeck's focused `search_graph` regex. It used the inline `(?i)` flag, which is not portable to the pinned C-based regex path. v0.5.1:

- removes inline regex flags;
- expands a bounded set of case variants instead;
- retries graph search with the portable `.*` pattern if focused search fails;
- prints the exact tool, arguments, output, and error before a structural-gate failure;
- preserves the same pinned v0.8.1 binary and hash verification.

No savings claim is made until the real two-arm Codex benchmark passes.
