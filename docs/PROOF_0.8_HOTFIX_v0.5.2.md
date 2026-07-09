# Proof 0.8 Hotfix v0.5.2

The v0.5.1 Windows run proved repository indexing and architecture extraction succeeded, but the proof gate later reported that graph search had not succeeded. Root cause: the Context Compiler trimmed `StructuralEvidence` entries to meet the 12,000-character packet budget before the proof gate inspected them. A successful `search_graph` receipt could therefore be deleted from the packet and misreported as a search failure.

v0.5.2 fixes the harness and the product design:

- structural index/search success is recorded as durable packet metadata before rendering/budget trimming;
- budget compaction never deletes structural tool receipts; it compacts verbose outputs while preserving tool, arguments, success/failure, and errors;
- Proof 0.8 verifies the durable success metadata rather than inferring execution history from already-compacted presentation evidence;
- a regression test forces an oversized architecture payload and proves successful index/search receipts survive packet compaction.

This does not claim Context Compiler efficiency. The real baseline-vs-assisted Codex benchmark must still pass.
