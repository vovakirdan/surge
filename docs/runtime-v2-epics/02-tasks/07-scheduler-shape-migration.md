# Epic 2 Task 07: Scheduler Shape Migration

**Goal:** move or wrap scheduler container state behind the single-shard shape
without changing scheduling behavior.

**Approach:** move only the field group approved by Task 02. Preserve current
global synchronization and current trace semantics.

**Skills:** `c-pro`, `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_state.c`,
`runtime/native/rt_async_internal.h`, `make c-check`, `make cppcheck`

---

## Files

- Modify: `runtime/native/rt_async_internal.h`
- Modify: `runtime/native/rt_async_state.c`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Read Task 06 test evidence.
2. Move or wrap only scheduler fields approved for Epic 2.
3. Keep current queue ordering, worker accounting, trace counters, and lock
   behavior unchanged.
4. Avoid adding new abstraction unless it removes a real ownership ambiguity.
5. Run Task 06 selected tests.
6. Run direct channel and net smoke probes if the moved fields affect wake
   placement.
7. Run `make c-check`, `make cppcheck`, and `make check`.
8. Run Sentrux root/runtime scans and record quality deltas.

## Done

- Scheduler behavior remains source-visible equivalent.
- Trace rows remain comparable to baseline or the rename is documented.
- No owner-local waiter semantics appear in this task.
