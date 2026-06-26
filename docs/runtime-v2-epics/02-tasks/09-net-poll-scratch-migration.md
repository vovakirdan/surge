# Epic 2 Task 09: Net Poll Scratch Migration

**Goal:** move or wrap current net poll scratch state behind the single shard
without introducing a persistent fd registry.

**Approach:** preserve the current poll-set rebuild and global waiter scan. This
task changes ownership shape, not net readiness semantics.

**Skills:** `c-pro`, `static-analysis`, `writing-clearly-and-concisely`

**Tech Details:** `runtime/native/rt_net.c`,
`runtime/native/rt_async_internal.h`, `scripts/bench_native_net.sh`

---

## Files

- Modify: `runtime/native/rt_net.c`
- Modify: `runtime/native/rt_async_internal.h`
- Modify: `runtime/native/rt_async_state.c` only for owner accessors.
- Modify: `docs/runtime-v2-epics/02-evidence.md`
- Modify: `docs/runtime-v2-epics/NOTES.md`

## Steps

1. Read Task 08 before evidence.
2. Move or wrap scratch fields approved by Task 02.
3. Keep `poll_net_waiters` semantics and global waiter scan intact.
4. Keep close/cancel behavior unchanged.
5. Run Task 08 net tests and benchmark.
6. Run `make c-check`, `make cppcheck`, and `make check`.
7. Run Sentrux root/runtime scans and record quality deltas.

## Done

- Net readiness and wake tests pass or record a blocker unrelated to this task.
- Benchmark rows are regenerated with a current-checkout binary.
- No fd registry lifecycle code appears in the diff.
