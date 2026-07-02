# Epic 5 Evidence: Per-Shard Heap Accounting

This file records task evidence for Epic 5. Keep durable conclusions here and
keep `NOTES.md` as the handoff log.

## Task Status

| Task | Status | Evidence |
| --- | --- | --- |
| 1 | Draft | Kickoff baseline not started. |
| 2 | Draft | Heap accounting dependency map not started. |
| 3 | Draft | Heap stats contract tests not started. |
| 4 | Draft | Static shape tests not started. |
| 5 | Draft | Accounting cell skeleton not started. |
| 6 | Draft | Alloc/free/realloc accounting migration not started. |
| 7 | Draft | Heap stats aggregation not started. |
| 8 | Draft | Concurrency and performance evidence not started. |
| 9 | Draft | Runtime V2 heap CI gate not started. |
| 10 | Draft | Epic closeout not started. |

## Open Evidence Questions

- Which accounting cell model removes the hot global counter cache line without
  hiding a new shared shard-local bottleneck under current multi-worker `N=1`?
- Which cold allocation paths run before runtime initialization, and how do they
  report into `rt_heap_stats()`?
- Which focused tests are stable enough for `runtime-v2-heap-check` and CI?
- Does heap accounting change covered Runtime V2 net or channel benchmark rows?
