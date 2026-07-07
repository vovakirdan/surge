# Epic 10 Task 2: RV2-DEBT-003 — Dependency-Aware `rt_async_state.c` Split

**Status:** planned; verbatim-move tranche.
**Kind:** runtime refactor (no behavior change).

## Written model (epic Proof And Quality Contract)

- **Owned state and lifetime being changed:** none at runtime. This task moves
  three complete dependency clusters out of `runtime/native/rt_async_state.c`
  into files that own exactly one runtime concept each. All moves are verbatim;
  no lock order, atomic order, or control/shard-lane decision changes.
- **Contract before the task:** `rt_async_state.c` is 1426 raw / 1184 effective
  LOC under a `.loc-legacy-allowlist` ceiling of 1184. It mixes executor
  bootstrap, task/scope table slot accessors, the ready-queue cluster, the
  virtual clock/N=1 runner, task handle lifetime, and completion/cancel. The
  Sentrux ledger (RV2-DEBT-003) records the park↔wake↔ready↔completion coupling
  made visible by the Epic 8 Task 13 extraction.
- **Old ambiguous path:** a reader auditing completion or scheduler-queue
  behavior must scan an 1400-line file where five unrelated owners interleave;
  the DEBT-003 recovery clause requires the remaining splits to reduce that
  coupling, not relocate it.
- **New invariant:** each extracted file owns one concept and states its lane
  contract at the top: `rt_ready_queue.c` owns shard ready-queue mutation and
  the worker pop policy (all queue mutation under the owner shard lock);
  `rt_task_complete.c` owns terminal task transitions (cancel propagation,
  `mark_done`, poll-outcome application); `rt_task_lifetime.c` owns the task
  handle refcount and free (frees only on the control lane). What remains in
  `rt_async_state.c` is executor bootstrap/config, task/scope table slot
  accessors, and the virtual clock + N=1 runner loop.
- **Failing proof if regressed:** the full existing gate set — the moved
  functions are pinned by behavior tests (`runtime-v2-lifecycle-check`,
  `runtime-v2-lock-check`, `runtime-v2-accept-check`, perf gate) and by static
  gates that this task repoints: `check_sync_points.sh` filename map,
  `TestRuntimeV2SchedulerPlacementStealPathSourceGate` source path, and the
  `done_cv` confinement scan extended to the new completion file.

## Cluster map (from Task 1)

Derived from `runtime/native/rt_async_state.c` at the Epic 10 start commit:

| Cluster | Functions (l. = start line) | External callers |
| --- | --- | --- |
| Ready queue → `rt_ready_queue.c` | `scheduler_runnable_is_empty` (l.216), `rt_sched_idle_sample_locked` (l.238), `sched_next_u64` (l.258, static), `current_worker_scheduler` (l.513), `current_local_queue` (l.520, static), `pop_task_from_deque` (l.533, static), `ready_push_task_locked` (l.592), `ready_push_with_policy` (l.645, static), `ready_push_inner` (l.680, static), `ready_push` (l.684), `ready_take_current_local_tail` (l.689), `ready_push_yielded_task` (l.719, static→extern), `ready_pop` (l.725), `worker_next_ready` (l.740) | `rt_worker_turn.c`, `rt_async_task.c`, `rt_async_poll.c`, `rt_async_blocking.c`, `rt_task_park.c` |
| Completion/cancel → `rt_task_complete.c` | `clear_select_timers` (l.494), `current_task_cancelled` (l.1125), `cancel_task` (l.1131), `mark_done_needs_control` (l.1240, static), `mark_done` (l.1258), `apply_poll_outcome` (l.1346) | `rt_async_poll.c`, `rt_async_scope.c`, `rt_async_task.c`, `rt_worker_turn.c`, `rt_async_select.c` |
| Handle lifetime → `rt_task_lifetime.c` | `task_add_ref` (l.1052), `free_task` (l.1059, static), `task_release` (l.1085), `task_release_lane_aware` (l.1100) | `rt_async_task.c`, `rt_async_select.c`, `rt_task_complete.c` (`mark_done` pin/release) |

Stays in `rt_async_state.c`: executor globals/TLS (l.14-98), env/config parsing
(l.100-154), `exec_init_once`/`ensure_exec`/`rt_start_workers`/worker-ctx debug
validation (l.156-355), task/scope slot accessors + child caps (l.357-492;
pinned to this file by `TestRuntimeV2LifecycleStaticTaskTableAtomicSnapshot`
and `TestRuntimeV2LifecycleStaticScopeOwnerLane` needles — moving them is a
separate decision this task does not take), virtual clock/sleep tick + N=1
runner (`tick_virtual`, `rt_next_sleep_deadline`, `advance_time_to_next_timer`,
`next_ready`, l.886-993; `next_ready` is pinned to this file by
`runtime_v2_accept_static_test.go:109`), and the task-handle/child helpers
(`task_from_handle`, `task_id_from_handle`, `task_add_child`,
`scope_add_child`, `scope_remove_child`, l.995-1044).

## Dependency/coupling argument (DEBT-003 close condition)

The recovery clause requires each split to REDUCE the visible coupling, not
relocate lines:

- The ready-queue cluster's only inbound dependencies are the five consumer
  files above; its outbound dependencies are the deque primitives
  (`rt_async_deque.c`), shard lock accessors, and trace hooks. Extracting it
  turns "scheduler queue mutation" into one reviewable unit whose lock rule
  (owner shard lock around every queue mutation) is stated once, instead of
  being distributed among bootstrap and completion code.
- The completion/cancel cluster's inbound edges are poll/turn/scope/select; its
  outbound edges are waiter removal, scope completion, park/wake
  (`rt_task_park.c`), the `done_cv` helper (`rt_done_cv.c`), and the ready
  queue (yield re-push). After the move, the park↔wake↔ready↔completion
  recursion that DEBT-003 records becomes a file-level cycle visible to
  Sentrux between `rt_task_park.c`, `rt_ready_queue.c`, and
  `rt_task_complete.c` — the same property, now measured on three
  single-owner files instead of hiding inside one multi-owner file.
- The handle-lifetime cluster has no outbound dependency on scheduler or
  completion internals other than `clear_wait_keys` (waiter surface) and the
  control-lane rule for `free_task`; separating it makes the "who may free a
  task and on which lane" question answerable from one 90-line file.

## Static gates repointed by this task (verbatim contract preserved)

- `check_sync_points.sh:35,37`: `SP_CANCEL_BEFORE_WAKE` and
  `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD` move `rt_async_state.c` →
  `rt_task_complete.c`.
- `internal/vm/runtime_v2_scheduler_placement_source_test.go`:
  `readRuntimeV2SchedulerStateSource` reads `rt_async_state.c` for
  `pop_task_from_deque` → reads `rt_ready_queue.c`.
- `internal/vm/runtime_v2_lifecycle_static_test.go`
  (`TestRuntimeV2LifecycleStaticAwaitCompatCountedSeparately`): the `done_cv`
  confinement scan over `rt_async_state.c` is EXTENDED to also scan
  `rt_task_complete.c` (strengthening: the file that now holds `mark_done` must
  also delegate `done_cv` broadcasting to `rt_done_cv.c`).
- `.loc-legacy-allowlist`: the `rt_async_state.c` entry is removed if the
  remaining effective LOC is at or under the normal 575 gate, else lowered to
  the new measured value.

## Header change

`rt_async_internal.h` gains one declaration: `ready_push_yielded_task`
(previously static; `apply_poll_outcome` now calls it across the
`rt_task_complete.c` → `rt_ready_queue.c` boundary). No other signature
changes.

## Non-goals

- No behavior change, no lock/atomic reordering, no comment rewrites beyond
  the file-top ownership banners.
- No move of the task/scope slot accessors (`get_task`, `rt_task_slot_store`,
  `rt_task_table_snapshot`, `get_scope`, `rt_scope_slot_store`): they are
  needle-pinned to `rt_async_state.c` by lifecycle static gates and their
  ownership question (table vs bootstrap) is not part of the three DEBT-003
  clusters. Recorded as a possible later cleanup, not debt.
- No net-handle or stdlib changes (Tasks 3-4).

## Gates to run

`git diff --check`, `make c-check`, `make cppcheck`, `make runtime-v2-check`,
`make check`, `./check_file_sizes.sh -a`, root + `runtime/` + `runtime/native`
`sentrux check` (baselines: 6178 / 5326 / 5428), before/after effective LOC for
`rt_async_state.c` and each new file.
