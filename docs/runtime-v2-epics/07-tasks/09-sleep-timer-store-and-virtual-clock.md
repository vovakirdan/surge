# Epic 7 Task 9: Sleep/Timer Store And Virtual Clock

**Kind:** runtime code. **Depends on:** Tasks 7, 8.

**Goal:** kill the whole-task-table sleep scans and make the virtual clock
an atomic (spike D7): per-shard sleep stores sorted by `(deadline,
task_id)`, an atomic `min_deadline` mirror per store, `now_ms` as a relaxed
atomic with `fetch_add` ticks and a monotonic CAS idle advance.

## Scope

- New `rt_async_sleep.c`: store init/add/remove/pop-due/min/destroy, clock
  now/tick/advance, and `rt_sleep_fire_due_on_shard`. The store is a second
  structure next to the timer-key waiter entries by recorded design (Rule 5
  reason): the waiter store is unsorted per-key park registration; deadline
  discovery needs an ordered index, and scanning waiter entries per tick
  would be the same O(everything) class this task deletes.
- `poll_sleep_task` arms once into the owner shard's store; `mark_done`
  removes cancelled sleepers' index entries; fired sleepers were already
  popped.
- `tick_virtual`: atomic tick, fire own shard's due sleepers inline, hand
  other shards a wake token when their mirror shows due work; the worker
  loop pops its own due sleepers at the top of its scan, so a token from a
  foreign tick turns into owner-local wakes (fixes the busy-other-shard
  timer lag the pure per-shard tick would have had).
- `next_sleep_deadline`: min over shard mirrors. `advance_time_to_next_timer`:
  monotonic CAS then a fire sweep over all shards (idle path, global as
  today).
- Empty-store mirrors must read `UINT64_MAX`: `rt_sleep_store_init` runs in
  `rt_shard_init`, because zeroed storage would read as a phantom deadline
  at 0 and spin the idle paths.

## Semantics

`SURGE_SHARDS=1` is bit-for-bit: one store, `(deadline, id)` order equals
the old ascending-id scan, ticks count the same events, idle advance jumps
identically. Multi-shard equal-deadline order across shards follows the
sweep order (recorded implementation artifact, spike D7).

## Checks

c-check/cppcheck; full lock-split suite (9 behavior modes green; static
gates `ClockAndSleepStoreShape` + `NoWholeTableSleepScan` flipped green;
the four Task-10/11 gates stay red by design); `runtime-v2-check` twice;
`make check` pre-commit; LOC; Sentrux.

## Success Criteria

Both Task 9 static gates green, behavior unchanged, evidence/NOTES/index
updated, own commit.
