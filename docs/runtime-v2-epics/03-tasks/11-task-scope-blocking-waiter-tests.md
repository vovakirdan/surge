# Epic 3 Task 11: Task, Scope, And Blocking Waiter Tests

**Goal:** prove task await, scope, and blocking waiter behavior before moving
those users.

**Approach:** add focused probes for join wake, failfast scope cancellation,
blocking completion, and blocking cancellation.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_task.c`,
`runtime/native/rt_async_scope.c`, `runtime/native/rt_async_blocking.c`,
`internal/vm/`

---

## Files

- Test: focused `internal/vm/` tests.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Cover join waiters and completed-task stale waiters.
2. Cover scope owner wake and failfast child cancellation.
3. Cover blocking job completion and cancellation wake behavior.
4. Record missing probes that cannot be stabilized yet.

## Done

- Task 12 has a behavior proof for every moved user group.
