# Epic 3 Task 10: Channel Waiter Migration

**Goal:** move channel waiter users to the owner-local waiter container.

**Approach:** update channel call sites only. Preserve current handoff order and
close semantics unless Task 09 explicitly allows a change.

**Skills:** `c-pro`, `code-refactoring`, `static-analysis`

**Tech Details:** `runtime/native/rt_async_channel.c`, waiter module,
`make c-check`, `make cppcheck`

---

## Files

- Modify: `runtime/native/rt_async_channel.c`
- Modify: waiter module if required.
- Modify: `docs/runtime-v2-epics/03-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Replace channel waiter call sites with owner-local APIs.
2. Keep close wake behavior for both senders and receivers.
3. Record line-count changes and behavior proof.
4. Run channel tests, static checks, benchmarks if affected, and Sentrux scans.

## Done

- Channel waiters no longer depend directly on executor-global waiter storage.
