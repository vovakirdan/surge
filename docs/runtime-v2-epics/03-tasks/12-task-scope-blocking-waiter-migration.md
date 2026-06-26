# Epic 3 Task 12: Task, Scope, And Blocking Waiter Migration

**Goal:** move task, scope, and blocking waiter users to the owner-local waiter
container.

**Approach:** migrate the related users together only if Task 11 proves their
shared cleanup behavior. Otherwise split this task before implementation.

**Skills:** `c-pro`, `code-refactoring`, `static-analysis`

**Tech Details:** `runtime/native/rt_async_task.c`,
`runtime/native/rt_async_scope.c`, `runtime/native/rt_async_blocking.c`

---

## Files

- Modify: `runtime/native/rt_async_task.c`
- Modify: `runtime/native/rt_async_scope.c`
- Modify: `runtime/native/rt_async_blocking.c`
- Modify: waiter module if required.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Move join, scope, and blocking waiter call sites.
2. Preserve stale waiter cleanup and single-wake behavior.
3. Run Task 11 tests, static checks, and Sentrux scans.

## Done

- Task/scope/blocking waiters no longer depend directly on global waiter
  storage.
