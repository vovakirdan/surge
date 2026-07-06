# Epic 9 Task 4: RV2-DEBT-022 — External Await `done_cv` StoreLoad Protocol

**Status:** complete.
**Kind:** runtime code + deterministic proof + perf check.

## Why This Task Exists

`rt_task_await` is the external/main-thread compatibility await path when
`rt_worker_count() > 1`. It waits on `done_cv` under `ex->lock`, while worker
completion tries to avoid that control lane when no external awaiter is present.
That split is intentional, but the current handshake has a StoreLoad window:

1. awaiter increments `ex->done_waiters`, then loads `target->status`;
2. completer stores `TASK_DONE`, then loads `ex->done_waiters`;
3. without a seq-cst StoreLoad edge, both sides may miss each other: the awaiter
   observes not-DONE and parks, while the completer observes zero waiters and
   skips the only steady-state `done_cv` broadcast.

The old window is latent and narrow, but it is a correctness bug. Stress and TSan
are not sufficient evidence; this task must make the interleaving deterministic.

## Current Code Shape

| Surface | Current role |
| --- | --- |
| `runtime/native/rt_async_task.c:326-352` | `rt_task_await`: control lock, optional wake, seq-cst `done_waiters` increment, `SP_AWAIT_AFTER_INCREMENT`, seq-cst status loop, `SP_AWAIT_BEFORE_DONECV_WAIT`, `done_waiters--`. |
| `runtime/native/rt_async_state.c:1240-1256` | `mark_done_needs_control`: pre-`TASK_DONE` load keeps the old control-lane classification only; it is not the load-bearing wake proof. |
| `runtime/native/rt_async_state.c:1318-1338` | `mark_done`: writes result, seq-cst stores `TASK_DONE`, reaches `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD`, wakes join waiters, then delegates the guarded `done_cv` broadcast. |
| `runtime/native/rt_done_cv.c` | Owns the post-`DONE` seq-cst `done_waiters` load and the single completion `done_cv` broadcast under `ex->lock` / `RT_CTRL_SITE_AWAIT_COMPAT`. |
| `runtime/native/rt_sync_point.h` | Allows seven Epic 9 windows, including the added `SP_AWAIT_BEFORE_DONECV_WAIT` point for the condvar-window proof. |
| `check_sync_points.sh` | Allows the seven header enumerators and pins both await sync points to `rt_async_task.c`. |

## Required Protocol

Use a real seq-cst StoreLoad handshake or a strictly equivalent proof. A
half-seq-cst variant is not accepted: a seq-cst `TASK_DONE` store alone is not
equivalent if the following `done_waiters` load remains only acquire, and a
seq-cst `done_waiters` increment alone is not equivalent if the following status
load remains only acquire. The accepted shapes are either:

- an explicit `atomic_thread_fence(memory_order_seq_cst)` on both store→load
  edges; or
- all operations that form the two store→load edges participate in one seq-cst
  order, with a written C11 argument.

The implementation uses the second accepted shape: all operations that form the
two store-load pairs participate in one seq-cst order:

- awaiter: `rt_done_waiters_increment_for_external_await` performs a seq-cst
  increment, `SP_AWAIT_AFTER_INCREMENT` proves the point was reached, and
  `rt_task_status_load_for_external_await` performs the predicate load as
  seq-cst;
- completer: `rt_task_status_store_done_for_external_awaiters` stores
  `TASK_DONE` as seq-cst, `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD` proves the
  post-DONE position, and `rt_done_cv_broadcast_after_done` performs the
  post-DONE `done_waiters` load as seq-cst before broadcasting under the
  control mutex;
- the negative-control build deliberately removes the seq-cst participation and
  forces the old "missed waiter" read before `TASK_DONE`; the runtime proof then
  shows the unlocked/unguarded broadcast half loses the only wake.

The explicit-fence version remains an acceptable alternative, but it is not the
shape landed in this task:

- awaiter: increment `done_waiters` with seq-cst participation, reach
  `SP_AWAIT_AFTER_INCREMENT` in test builds, perform a seq-cst fence before the
  status predicate load, then wait only while the acquire/seq-cst status load
  still says not DONE;
- completer: store `TASK_DONE` with seq-cst participation or perform a seq-cst
  fence immediately after the release DONE store, reach
  `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD` before that fence in test builds, then
  load `done_waiters` with ordering sufficient for the proof;
- if waiters are observed, broadcast under `ex->lock` or while already holding
  it. The task must prove the broadcast cannot race a parked waiter and must
  keep the `done_cv` wait confined to `rt_task_await`.

Do not solve this by making all completions take `ex->lock`. Worker-side joins
must stay off `done_cv`; control-lane accounting must continue to classify
done-waiter-only completions as `AWAIT_COMPAT`, not steady-state completion.

## Required Proof Coverage

- already-RUNNING target: deterministic positive/negative proof of the
  StoreLoad window using `SP_AWAIT_AFTER_INCREMENT` and
  `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD`;
- parked target: awaiter parks on a WAITING target, an independent wake
  completes it, and the same `done_cv` protocol does not strand;
- already-DONE target: await returns without parking or requiring a broadcast;
- multiple external awaiters on the same target, with broadcast (not signal)
  justified;
- `RV2-DEBT-022` x `RV2-DEBT-023`: cancellation/completion still wakes the
  external awaiter and does not resurrect DONE tasks;
- `SURGE_SHARDS=1,2,8` where the path is shard-sensitive.

The pthread-backed sync-point scaffold cannot by itself be the load-bearing
negative control for the pure StoreLoad miss: a mutex/cond rendezvous placed
after the first store can add synchronization that masks the weak ordering being
tested. Therefore the task must use one of these explicit negative-control
shapes:

- a focused C-level StoreLoad litmus that starts both store→load pairs from a
  synchronization point before the stores, uses the same fence macro/helper as
  the runtime path, proves the no-fence build can observe the forbidden both-miss
  outcome, and proves the fenced build cannot within the bounded run; or
- a stricter deterministic static/proof gate that compiles the negative-control
  runtime without the required fences and fails because the exact fence/helper
  contract is absent, plus a runtime condvar-window negative control for the
  broadcast-under-lock half.

Do not claim that `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD` +
`SP_AWAIT_AFTER_INCREMENT` alone proves the old no-fence StoreLoad failure.

## Implemented Code/Test Changes

- `runtime/native/rt_async_state.c`
  - added the completer half of the StoreLoad protocol;
  - kept `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD`;
  - delegated the broadcast to `rt_done_cv_broadcast_after_done`;
  - kept `rt_async_state.c` at `1183` effective LOC, below its `1184` legacy
    ceiling.
- `runtime/native/rt_async_task.c`
  - added the awaiter half of the StoreLoad protocol;
  - kept `SP_AWAIT_AFTER_INCREMENT`;
  - added `SP_AWAIT_BEFORE_DONECV_WAIT`;
  - kept `done_cv` as external-await compatibility only.
- `runtime/native/rt_async_internal.h`, `runtime/native/rt_done_cv.c`
  - added the seq-cst helper names pinned by the static gate;
  - added `RV2_DEBT_022_NEGATIVE_CONTROL` in one helper cluster;
  - moved the single completion `done_cv` broadcast to a focused helper file.
- `internal/vm/runtime_v2_lifecycle_behavior_*`
  - added focused positive and negative proof modes;
  - added matrix modes for multi-awaiters, already-DONE, parked target, and
    cancelled parked target;
  - updated static gates so the seq-cst handshake cannot silently degrade back to
    acquire/release-only ordering.
- `Makefile`
  - wired the new positive/negative/matrix tests into
    `runtime-v2-lifecycle-check`.
- `docs/runtime-v2-epics/09-evidence.md`, `DEBT.md`, `NOTES.md`, and this task
  doc
  - record the interleaving model, commands, results, and debt closeout.

## Performance Requirement

Because the completer-side ordering runs on every completion, this task must
measure the cost. Run `make runtime-v2-perf-check` and record
`TestRuntimeV2PerfControlLaneGate` counters. If the chosen protocol adds a
seq-cst fence on every completion, run the native net benchmark row used by the
Epic 9 baseline or explain why the CI perf gate is the accepted measurement for
this slice. Any material increase in `control_lock_acquired`,
`ctrl_await_compat`, lifecycle-control/request, or steady-state-control/request
must be explained or assigned to debt.

Task result: `make runtime-v2-perf-check` passed after the protocol change.
Recorded counters were `control_lock_acquired=11819` (`11.542/req`),
`ctrl_await_compat=3458` (`3.377/req`), steady-state-control `8361`
(`8.165/req`, ceiling `20.0`), lifecycle-control `6143` (`5.999/req`, ceiling
`9.0`), and `placement_adoptions=253`. The chosen shape adds seq-cst atomics on
the existing completion/status/waiter handshake but does not add a steady-path
control-lock acquisition.

## Exit Criteria

- `RV2-DEBT-022` is closed with deterministic positive and negative evidence.
- The written proof covers already-running, parked, already-DONE, and multi
  external-await cases.
- Worker join remains `done_cv`-free and external-await compat stays counted
  separately.
- Required gates pass and are recorded:
  `git diff --check`, `make c-check`, `make cppcheck`,
  `make runtime-v2-syncpoint-check`, focused positive/negative tests,
  `make runtime-v2-lifecycle-check`, `make runtime-v2-perf-check`,
  `./check_file_sizes.sh -a`, and final `make check` before commit.
