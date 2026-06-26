# Epic 3 Task 13: Timer, Select, And Cancellation Tests

**Goal:** prove timeout, select, and cancellation cleanup before moving those
waiter paths.

**Approach:** add focused tests around `wait_keys`, `select_timers`,
timeout-task cancellation, and cancelled waiter cleanup.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_task.c`,
`runtime/native/rt_async_state.c`, `internal/vm/`

---

## Files

- Test: focused `internal/vm/` tests.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Cover select with channel and timeout branches.
2. Cover cancellation while parked on multiple keys.
3. Cover cleanup of timer tasks created only for select timeout.
4. Record any known flaky timeout behavior separately.

## Done

- Task 14 can move cleanup paths with before/after proof.
