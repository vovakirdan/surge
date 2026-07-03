# Epic 7 Task 7: Scheduler Ready And Park/Wake Migration

**Kind:** runtime code. **Depends on:** Tasks 4, 6.

**Goal:** move worker sleep/wake ownership to the shard lane under the
sanctioned nested shape: the global `ready_cv` retires, every worker sleeps
on its own shard's `worker_cv` behind a `wake_pending` counter, wake paths
signal the owner shard only, and every task gets an owner shard at creation
(D3 universal assignment). The worker turn itself stays on the control lane
until the Task 11 peel; queue and waiter ownership follow in Tasks 8-11.

## Scope

- Universal owner assignment: `__task_create`, checkpoint/sleep spawns, and
  blocking submit assign `TASK_PLACEMENT_GENERIC` with the spawning worker's
  shard (shard 0 from non-worker threads) whenever the parent did not
  already provide placement (`rt_task_assign_spawn_owner`,
  `rt_scheduler_placement.c`).
- `rt_scheduler.wake_pending` (shard-lock-guarded) plus a new
  `rt_sched_wake.c` owning: owner-shard ready signal, all-shards compat
  broadcast, the worker sleep transition (release control, wait on
  `worker_cv` consuming `wake_pending`, reacquire control), and the
  shutdown wake sweep.
- Worker loop and `rt_wait_current_worker_wakeup` sleep on their shard's
  `worker_cv`; `signal_ready_workers` and every `ready_cv` site convert to
  owner-targeted signals or the compat broadcast; `ready_cv` is deleted.
- `ex->shutdown` becomes an atomic flag (writes stay control-lane) so
  shard-side wait predicates may read it; the shutdown path sweeps every
  shard's condvars under the nested order.
- `rt_net_wake_poll_for_task_wait_keys` signals the owner shards of the
  woken pollers instead of broadcasting to every worker in the process.

## Correctness Argument (D5 applied to worker sleep)

A wake between the worker's control-lane "no work" decision and its shard
`cond_wait` is not lost: the wake path bumps `wake_pending` under the shard
lock before signaling, and the worker re-checks `wake_pending` under the
same lock before waiting. Shutdown is safe the same way: the flag is atomic
and the sweep's broadcast takes each shard lock, so it serializes with
every waiter's predicate check. Leftover `wake_pending` counts cause one
extra rescan, never a missed one.

## Behavior Contract

Observable behavior unchanged: Task 4 suite (both configs), the full
`runtime-v2-check` chain (including MT wakeups/cancellation, channel
park/unpark, seeded scheduler, accept gates), and `make check` stay green.
Static gates: no new flips expected (worker-loop gate flips at Task 11).

## Checks

`make c-check`, `make cppcheck`, Task 4 suite, `timeout 600s make
runtime-v2-check`, `make check` (pre-commit), `./check_file_sizes.sh -a`,
`git diff --check`, Sentrux root/runtime/native.

## Success Criteria

- No `ready_cv` field or reference remains; worker sleep is shard-local.
- Every task has a valid owner shard from creation.
- All behavior gates green; evidence/NOTES/index updated; own commit.
