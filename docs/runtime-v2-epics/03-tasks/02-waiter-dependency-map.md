# Epic 3 Task 02: Waiter Dependency Map

**Goal:** map current waiter behavior before any waiter storage move.

**Approach:** inspect every waiter key, add/pop/remove caller, wake path, and
cleanup path. Classify each dependency by owner and by behavior it protects.

**Skills:** `task-breakdown`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_internal.h`,
`runtime/native/rt_async_state.c`, `runtime/native/rt_async_channel.c`,
`runtime/native/rt_async_task.c`, `runtime/native/rt_net.c`

---

## Files

- Create: `docs/runtime-v2-epics/03-waiter-dependency-map.md`
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Map `waker_key` kinds to producer and consumer paths.
2. Map `add_waiter`, `remove_waiter`, `pop_waiter`, `wake_key_all`, and
   `park_current` call sites.
3. Record cleanup paths for done, cancellation, timeout, close, shutdown, and
   net wake.
4. Mark which behavior is contract and which is implementation detail.

## Done

- The owner-local waiter design has a concrete dependency map.
- Unknown or ambiguous behavior is recorded as an open decision.
