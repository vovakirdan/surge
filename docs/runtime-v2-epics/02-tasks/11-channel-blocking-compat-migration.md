# Epic 2 Task 11: Channel/Blocking Compatibility Migration

**Goal:** move or wrap channel-blocked worker counters, compensation counters,
and blocking fallback integration behind the single-shard shape.

**Approach:** preserve current direct async channel semantics and current sync
helper fallback. This task changes field ownership, not channel protocol.

**Skills:** `c-pro`, `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_async_channel.c`,
`runtime/native/rt_async_state.c`, `runtime/native/rt_async_internal.h`

---

## Files

- Modify: `runtime/native/rt_async_channel.c`
- Modify: `runtime/native/rt_async_state.c`
- Modify: `runtime/native/rt_async_internal.h`
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Read Task 10 evidence.
2. Move or wrap only approved channel/blocking compatibility fields.
3. Keep direct async channel paths direct.
4. Keep sync fallback counters and compensation trace explainable.
5. Run Task 10 tests and benchmark.
6. Run `make c-check`, `make cppcheck`, and `make check`.
7. Run Sentrux root/runtime scans and record quality deltas.

## Done

- Direct channel tests remain equivalent.
- Sync fallback evidence remains explainable.
- No remote channel request/ack behavior appears in the diff.
