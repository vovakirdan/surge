# Epic 3 Task 16: Net Waiter Migration

**Goal:** move net waiter users to owner-local waiter APIs while preserving the
existing poll rebuild behavior.

**Approach:** migrate waiter access in `rt_net.c` without starting the fd
registry epic.

**Skills:** `c-pro`, `code-refactoring`, `static-analysis`

**Tech Details:** `runtime/native/rt_net.c`, waiter module,
`./scripts/bench_native_net.sh`

---

## Files

- Modify: `runtime/native/rt_net.c`
- Modify: waiter module if required.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Replace direct global waiter scans with owner-local APIs.
2. Keep current poll-set rebuild and wake-fd behavior.
3. Record line-count and trace-counter changes.
4. Run net tests, native net benchmark, static checks, and Sentrux scans.

## Done

- Net waiters no longer depend directly on executor-global waiter storage.
- No persistent fd registry was introduced.
