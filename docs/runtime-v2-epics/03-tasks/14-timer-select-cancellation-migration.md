# Epic 3 Task 14: Timer, Select, And Cancellation Migration

**Goal:** move timer, select, and cancellation waiter cleanup to owner-local
waiter APIs.

**Approach:** migrate cleanup paths after Task 13 proves multi-key and timeout
behavior.

**Skills:** `c-pro`, `code-refactoring`, `static-analysis`

**Tech Details:** `runtime/native/rt_async_task.c`,
`runtime/native/rt_async_state.c`, waiter module

---

## Files

- Modify: `runtime/native/rt_async_task.c`
- Modify: `runtime/native/rt_async_state.c`
- Modify: waiter module if required.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Move `wait_keys` and `select_timers` cleanup call sites.
2. Preserve cancellation and timeout semantics.
3. Record line-count changes for touched over-limit files.
4. Run Task 13 tests, static checks, and Sentrux scans.

## Done

- Multi-key wait cleanup no longer depends directly on global waiter storage.
