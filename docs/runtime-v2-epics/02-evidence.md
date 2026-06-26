# Epic 2 Evidence

This file records task-by-task evidence for
`02-n1-runtime-shard-structure.md`.

Do not use this file to hide known debt. The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` is accepted backend-test debt
until the later test/backend matrix epic fixes or replaces it. Epic 2 evidence
must separate that debt from new runtime regressions.

## Task Evidence Index

| Task | Evidence status | Notes |
| --- | --- | --- |
| 1. Kickoff Evidence | Pending | Record baseline, accepted VM debt, and Sentrux state. |
| 2. Field Ownership Map | Pending | Link the ownership map and deferred field groups. |
| 3. Runtime V2 Test And CI Contract | Pending | Define stable local and CI probes. |
| 4. Runtime/Shard Skeleton Tests | Pending | Record failing or selected skeleton checks. |
| 5. Runtime/Shard Skeleton | Pending | Record implementation checks and Sentrux deltas. |
| 6. Scheduler Shape Tests | Pending | Record selected scheduler liveness checks. |
| 7. Scheduler Shape Migration | Pending | Record scheduler migration checks and traces. |
| 8. Net Poll Scratch Tests | Pending | Record net wake and benchmark baseline. |
| 9. Net Poll Scratch Migration | Pending | Record net migration checks and benchmark rows. |
| 10. Channel/Blocking Compatibility Tests | Pending | Record channel and fallback checks. |
| 11. Channel/Blocking Compatibility Migration | Pending | Record migration checks and trace rows. |
| 12. CI Runtime V2 Gates | Pending | Record new CI target/job and local result. |
| 13. Accessor Cleanup And Static Gates | Pending | Record static checks and quality deltas. |
| 14. Epic Closeout | Pending | Record final gates and handoff to Epic 3. |
