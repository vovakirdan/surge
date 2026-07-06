# Epic 8 Task 13: Large-File And Quality Tranche

**Kind:** refactor / quality. **Depends on:** Tasks 10, 11, 12.
**Behavior contract:** identical before and after. This task is a
MOVE/RENAME/COMMENT tranche; it changes no locking, ordering, atomics, or
scheduler semantics. The full `runtime-v2-check` battery (including the
no-keepalive `CompletionPinInterleavingTSan` at shards 1/2/8 and the
`runtime-v2-perf-check` control-lane gate) is the before/after behavior proof.

## Scope

`RULES.md` Global Rule 4 (file size and modularity) is the rule this task
serves. Four deliverables:

1. Rewrite the stale executor-invariant comment in `rt_async_internal.h` to the
   post-Epic-7/8 lane model.
2. Reduce the touched over-limit file `rt_async_state.c` (`RV2-DEBT-003`) by a
   single dependency-bounded extraction, and record the LOC outcome.
3. Quality sweep of the epic's additions (stale comments, naming, dead code) in
   the touched files.
4. Sentrux scoped scans before and after (root + `runtime` + `runtime/native`).

Non-goals (unchanged this task): any completion/scope/await behavior, the select
slow lane, `rt_term.c`/`rt_fs.c`/`rt_string.c`/`rt_bignum_*` (`RV2-DEBT-005`,
non-Epic-8), and a mega-split of `rt_async_state.c` for its own sake.

## Deliverable 1: Stale Invariant Comment

The "Executor invariants" block in `rt_async_internal.h` (the epic document
flagged it at its own lines 98-99; the block had drifted from the epic's old
`:292-304` pointer to `:346-358` after Tasks 6-10 grew the executor struct) still
described the pre-Epic-7 executor-wide ownership model: it claimed `ex->lock`
owns "tasks/scopes, shard stores, scheduler queues/counters, channel/blocking
compatibility counters, net polling, timers, and shutdown", that "queue/waiter
transitions still happen under `ex->lock`", and that `running_count` moves under
`ex->lock`. All false post-Epic-7/8: scheduler queues, waiter stores, sleep
timers, net poll, `running_count`, and `channel_blocking_compat` are shard-lane;
task/scope steady-state bookkeeping is owner-shard; `shutdown` is an atomic.

The rewrite names the three ownership lanes explicitly so a reader must place
every mutation in exactly one, and it names the two remaining control-lane
residuals (cross-owner `scope_on_child_done`/failfast/cancel walk, and the
external/main-thread await `done_cv`/`compat_cv` compat) so it does not go stale
next epic:

- **Control lane (`ex->lock`):** task/scope table growth; external-await compat
  (`done_cv` gated on `done_waiters`, `compat_cv`); cross-owner residuals
  (`scope_on_child_done` when a child's owner shard != the scope's pinned shard,
  failfast `scope_cancel_children_controlled`, the `cancel_task` sibling walk);
  checkpoint/sleep/blocking submit; compensation bookkeeping. `control_waiters`
  now backs only the unknown-waker-kind default and the diagnostic dump.
- **Shard lane (`rt_shard.lock`):** ready queues (local + inject),
  `running_count`, `wake_pending`, `waiter_store` (join/net/channel keys plus the
  scope owner's `scope_key`), `sleep_store`, net poll / fd registry, per-shard
  `channel_blocking_compat`, and steady-state task/scope lifecycle (slot
  publish/read, park/wake, scope-object bookkeeping on the pinned owner shard).
- **Atomic, no lock:** `task->status` (acquire/release; the `TASK_DONE` store
  publishes `result_kind`/`result_bits` written before it), the other per-task
  atomics, `next_id`/`next_scope_id`/`now_ms`/`shutdown`, the task/scope table
  slots, the `sleep_store` min-deadline mirror, `done_waiters`, and
  `channel_blocked_workers`.

Comment-only; zero code change.

## Deliverable 2: `rt_async_state.c` Extraction

**Decision: single dependency-bounded extraction (not a mega-split, not
hold-flat).** The task park/unpark + key-wake primitive cluster moved verbatim
from `rt_async_state.c` into a new owner-oriented module
`runtime/native/rt_task_park.c`. Owner concept (Global Rule 8): how a RUNNING
task suspends itself behind a `waker_key` and how a parked task is made
schedulable again. Functions moved: `wake_task_on_shard_locked`,
`wake_task_with_policy`, `wake_task`, `wake_net_task`, `park_requeue_locked`,
`wake_key_all_with_policy`, `wake_key_all`, `park_current`. It is a closed call
group (`park_current` <-> `wake_task` <-> `wake_task_on_shard_locked` <->
`wake_key_all`) that only calls already-header-exported helpers, so the move is
clean; `park_state`/`park_key`/`waiter` are canonical Global-Rule-7 ownership
names.

**Load-bearing invariant comments moved verbatim** (diffed here per the review
requirement — content byte-identical, only the file changed):

- The "Leaf wake: caller holds the owner shard's lock ... at most one absorbed
  spurious wake" block above `wake_task_on_shard_locked`.
- The `wake_task_on_shard_locked` ownership invariant ("RV2-DEBT-019:
  park_key/park_prepared belong to the RUNNING task's own thread until it commits
  to WAITING ... the wake path may only read or clear those fields once the task
  is provably parked (WAITING, not enqueued)").
- The `wake_task_with_policy` generation-capture rationale ("the deferred removal
  below runs after this lock is released, and the woken task can re-register the
  same channel key in that window; the generation confines the removal ...").
- The `park_current` D5 register-then-commit + wake-token double-check comments
  and the generation-qualified abort-removal rationale.

**The only non-pure-move line** is in `park_requeue_locked`: the direct read of
the file-scope `channel_wake_force_inject` static became a call to its existing
`channel_wake_force_inject_enabled()` accessor. Behavior-identical: the accessor
is `return channel_wake_force_inject != 0;`, so `channel_wake_force_inject_enabled()`
evaluates to exactly the prior `channel_wake_force_inject != 0` expression, with
no added ordering (a plain read of the same `static uint8_t`). The static itself
stays in `rt_async_state.c` (still set in `exec_init_once`, still read by the
accessor), so there is no duplicate definition and no dead static.

**One linkage change** required by the move: `wake_key_all_with_policy` was
`static`; `mark_done` (staying in `rt_async_state.c`) drains join waiters via
`wake_key_all_with_policy(ex, join_key(task->id), 0)` across the new module
boundary, so the helper gains external linkage (a prototype added to
`rt_async_internal.h` beside `wake_key_all`, matching the existing
`wake_task_on_shard_locked` extern-leaf-helper pattern). Every call site is
byte-identical; `mark_done` is untouched. The other two moved statics
(`wake_task_with_policy`, `park_requeue_locked`) have callers only inside the
cluster and stay `static`.

**LOC outcome (effective, `check_file_sizes.sh`):**

| File | Before | After | Delta |
| --- | --- | --- | --- |
| `runtime/native/rt_async_state.c` | 1386 | 1184 | −202 |
| `runtime/native/rt_task_park.c` | — | 203 | new (<=500) |
| `runtime/native/rt_async_internal.h` | 555 | 556 | +1 (one prototype) |

`rt_async_state.c` stays over the 500 hard line, so `RV2-DEBT-003` stays **open**;
the `.loc-legacy-allowlist` ceiling is lowered from `1580` to the exact measured
post-move `1184` (zero headroom, so any regrowth trips the gate). Named remaining
split candidates for future tasks under `RV2-DEBT-003`: the ready-queue cluster
(`ready_push*`/`ready_pop`/`worker_next_ready`/deque helpers), the
completion/cancel cluster (`cancel_task`/`mark_done*`/`apply_poll_outcome`), and
the handle-lifetime cluster (`task_add_ref`/`free_task`/`task_release*`). The
completion cluster is deliberately NOT extracted this task: it is the active
`RV2-DEBT-022` `done_cv` hot path, and `runtime_v2_lifecycle_static_test.go` pins
"rt_async_state.c broadcasts done_cv exactly once / never waits on done_cv" by
filename.

**Build/test wiring:** the Makefile `C_SOURCES` glob, the `//go:embed
native/*.c native/*.h` directive, and the VM test harnesses all glob
`runtime/native/*.c`, so the new file is auto-discovered — no Makefile, embed, or
harness edits needed. One static-gate pin was updated: the Epic 6
`TestRuntimeV2AcceptNetOwnershipNoShard0Shortcut` no-shard-0 check pinned
`park_current` (and `next_ready`) to `rt_async_state.c` by filename; the pin is
split so `park_current` points at `rt_task_park.c` and `next_ready` stays. The
lifecycle static gate extracts function bodies by globbing all native `*.c`, so
it needed no change.

## Deliverable 3: Quality Sweep

Bounded to the touched files (`rt_async_state.c`, `rt_task_park.c`,
`rt_async_internal.h`). Result: no edits required.

- **Stale attribution comments (the Task 8->9->10 `ctrl_completion` saga):** the
  completion-helper comment above `mark_done_needs_control` already carries Task
  10's final, corrected attribution (WAKER_JOIN removed in Task 8; scope reason +
  WAKER_SCOPE removed in Task 9; final form net-key + `done_waiters`; the
  `completion_reason_out`/AWAIT_COMPAT complement from the Task 10 review). No
  stale/wrong attribution remains in C comments (the wrong Task 8/9 attributions
  live only in the `DEBT.md` correction chain, which is intentionally preserved).
- **Naming (Global Rule 7):** the moved functions and the new module use
  owner-oriented names (`park`/`wake`/`waker_key`/`owner_shard`); no rename
  needed.
- **Dead code:** none introduced or left; the strict-warning `make c-check`
  compile confirms no unused static or unreferenced symbol.

The Task-14-parked reviewer notes (e.g. the `RV2-DEBT-020` `rt_waiter_route.c`
migration re-derivation) were NOT touched — not adjacent to the extracted files.

## Deliverable 4 + Gates

### Sentrux (CLI `sentrux check`; MCP not connected, per the Epic 8 mechanism)

| Scope | Before | After | Rules |
| --- | --- | --- | --- |
| root `.` | 6174 | 6173 | 10, all pass |
| `runtime` | 5295 | 5255 | 7, all pass |
| `runtime/native` | 5382 | 5341 | 7, all pass |

All rules pass at every scope before and after. The code-scope quality signal
dropped −40/−41 (0.76%). Recorded rationale (accepted per `RULES.md` Global Rule
3, which permits a drop when the epic records the recovery owner):

- The extraction split the runtime's hottest interconnect. `park` <-> `wake` <->
  `ready` <-> `completion` are mutually recursive: `mark_done`/`cancel_task`/
  `apply_poll_outcome` (staying) call into the moved wake/park, and the moved
  code calls back into `ready_push`/waiter helpers. Splitting turns intra-file
  coupling into visible inter-module coupling, which the modularity signal
  penalizes ~41. Earlier Epic splits (`rt_scope_table`, `rt_waiter_route`,
  `rt_sched_wake`) RAISED the signal because those clusters were self-contained;
  this cluster is uniquely bidirectionally coupled to what stays.
- The tool's own regression gate (`sentrux gate` vs the committed baseline the
  epic's `sentrux gate --save` mechanism uses) shows the QUALITY dimension
  IMPROVED: `runtime/native` 5159 -> 5341. Its "DEGRADED" verdict is on Coupling
  (0.00->0.06) and Complex-functions (21->23), both of which are cumulative drift
  since the Jul-2 baseline (all of Epic 5-8), not introduced by this verbatim
  move.
- The task's actual goal — file-size/reviewability (`RV2-DEBT-003`: 1386->1184)
  — is met, and `RV2-DEBT-003` is the recorded recovery owner for the remaining
  split work. The quality sweep found no dead code or stale comment to remove for
  an offset.

### Gates

| Command | Result |
| --- | --- |
| `git diff --check` | clean |
| `make c-check` | PASS (strict-warning C compile) |
| `make cppcheck` | PASS (56 files incl. rt_task_park.c) |
| `make check` | PASS (all Go packages ok, lint clean, file sizes OK) |
| `make runtime-v2-check` | PASS (green run): `CompletionPinInterleavingTSan` PASS shards 1/2/8; `PerfControlLaneGate` steady-state-control 8.059/req << 20.0 ceiling (no control-lane regression); `NoShard0Shortcut` pin-split PASS |
| `./check_file_sizes.sh -a` | PASS; deltas above; rt_async_state.c 1184 at ceiling |

**Transient handling (`RV2-DEBT-018`/`RV2-DEBT-011`):** two earlier
`runtime-v2-check` runs each hit one accepted transient on a different test — the
`RV2-DEBT-018` empty-output `exit=1 ~2.9ms` on `SelectTimeoutCleansLosingChannelWaiter`,
and a net-timing flake (`io_poll_calls=0`, `fd_ready_batches=0`) on the accept
`OwnerShardLifecycleTraceContract`. Each proven non-reproducible: the accept test
5/5 green focused; the select test 5/5 green as separate `-count=1` runs (its
`-count=5` failure was `RV2-DEBT-011` same-test artifact-dir reuse, not logic).
A third clean `runtime-v2-check` run was fully green (recorded above). The
verbatim wake/park move is not implicated: it passed the strict-warning
`make c-check` compile, and a broken net wake would hang, not skip polling.
