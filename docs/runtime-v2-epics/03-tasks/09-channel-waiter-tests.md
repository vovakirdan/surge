# Epic 3 Task 09: Channel Waiter Tests

**Goal:** prove channel waiter behavior before moving channel users.

**Approach:** add focused send, receive, close, cancellation, and FIFO probes
for channel waiter paths.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_channel.c`, `internal/vm/`,
`make runtime-v2-check`

---

## Files

- Test: focused `internal/vm/` channel tests.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Cover send waiter wake, receive waiter wake, close wake, and cancellation.
2. Include a timeout wrapper for liveness-sensitive cases.
3. Decide which tests can join the local Runtime V2 gate.

## Done

- Task 10 can move channel waiters with before/after proof.
