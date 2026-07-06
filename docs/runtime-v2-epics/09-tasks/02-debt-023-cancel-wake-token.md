# Epic 9 Task 2: RV2-DEBT-023 — Cancel vs RUNNING→WAITING Park Ordering

**Status:** fix landed, scaffold-stable, and deterministic cancel-vs-park proof
green for the first slice.
**Kind:** runtime code (smallest surface first).

## Interleaving model

- **Owners/locks:** the target task's `status`/`cancelled`/`wake_token` are
  per-task atomics; `park_current` commits `TASK_WAITING` under the target's
  owner shard lock; `cancel_task` runs control-held and calls `wake_task`, which
  takes the owner shard lock internally.
- **Protected transition:** the target's `RUNNING -> TASK_WAITING` park commit.
- **Old failure window:** `cancel_task` stored the cancelled flag but woke only
  when its lock-free status read saw `TASK_WAITING` (`rt_async_state.c`, old
  `:1159` guard). A RUNNING target that already passed its poll's
  `current_task_cancelled` check (`rt_async_poll.c` -> `POLL_PARKED`) and is
  committing to `TASK_WAITING` in `park_current` (`rt_task_park.c:270`, token
  re-check `:271`) could have the cancel read `RUNNING` in the window before the
  `WAITING` store, skip the wake, and never set the token. On a never-firing
  `park_key` (join of a never-completing target, channel with no sender,
  indefinite wait) the cancellation was lost.
- **New guarantee:** the wake is now UNCONDITIONAL. `wake_task_on_shard_locked`
  (`rt_task_park.c:45`) sets the wake token unconditionally under the owner
  shard lock before its status gate, and only enqueues when WAITING and not
  already enqueued. So the token is set even for a RUNNING target; when it
  commits to `TASK_WAITING`, `park_current`'s token re-check (`:271`) aborts and
  re-runs it, and its next poll observes `cancelled=1` and unwinds.
- **Test that fails if it regresses:** the deterministic cancel-vs-park proof
  arms `SURGE_SYNC_POINT=SP_PARK_BEFORE_WAITING:block`, then
  `POLL_CANCEL_PARK_PROOF` registers a never-firing channel recv and returns
  `POLL_PARKED` without cancelling itself. The harness main thread waits until
  the parked outcome is blocked while the target is still `TASK_RUNNING`, calls
  the real external `rt_task_cancel(proof)`, verifies
  `SP_CANCEL_BEFORE_WAKE` was reached, then calls `rt_sync_point_open()` to
  release the parker. Positive build unwinds cancelled. The
  `RV2_DEBT_023_NEGATIVE_CONTROL` build restores the old status-gated wake and
  must fail only after both syncpoint counts prove the intended window ran.

## Fix

`rt_async_state.c`, `cancel_task`: replaced

```c
if (task_status_load(task) == TASK_WAITING) { wake_task(ex, task->id, 1); }
```

with

```c
RT_SYNC_POINT(SP_CANCEL_BEFORE_WAKE);
wake_task(ex, task->id, 1);
```

`rt_async_poll.c`, `run_ready_one` user path: added
`RT_SYNC_POINT_IF(..., SP_PARK_BEFORE_WAITING)` after `poll_task()` returns
`POLL_PARKED` and before reacquiring the control lane to commit `TASK_WAITING`.

`rt_worker_turn.c`: added the same hook on the shard-worker user-task paths.
The lifecycle harness normally executes ready work through shard workers at
`SURGE_SHARDS=2,8`; without this equivalent worker hook, the CI proof can
strand in the real worker path without exercising the syncpoint. The conditional
macro evaluates its predicate only in `RT_TEST_SYNC_POINTS` builds; release
builds keep only the enum allowlist cast, with no branch and no predicate
evaluation. The static gate allowlists `SP_PARK_BEFORE_WAITING` only in
`rt_async_poll.c` and `rt_worker_turn.c`.

The candidate `rt_trace_cancel_wake_forced` counter was DROPPED (see below);
`rt_sync_point_reached_count(SP_CANCEL_BEFORE_WAKE)` supplies the same
"the wake path engaged" assertion without growing an over-limit file.

## No-resurrection proof: every `cancel_task` caller holds the control lane

`free_task` is control-lane only, so if `cancel_task` runs control-held, no free
can interleave between `get_task` and the now-unconditional wake. All six call
sites verified control-held:

| Call site | Enclosing | Control evidence |
| --- | --- | --- |
| `rt_async_state.c:1206` | `cancel_task` recursion | control-held by construction (the tree walk holds control) |
| `rt_async_task.c:378` | `rt_task_cancel` | `rt_control_lock(ex)` at `:375` |
| `rt_async_select.c:101` | `rt_timeout_poll` | control held; `rt_control_unlock(ex)` at `:109` |
| `rt_async_select.c:383` | `rt_select_poll_tasks` | control held; `rt_control_unlock(ex)` at `:390` |
| `rt_async_state.c:504` | `clear_select_timers` | reached from `mark_done` only when `select_timers_len>0`, which forces `need_control`; select callers (`:264/278/281/386`) hold control |
| `rt_async_scope.c:74` | `scope_cancel_children_controlled` | all 4 callers hold control via the `need_control = !rt_lane_holds_control(); if (need_control) rt_control_lock` guard: `rt_async_state.c:1373` (apply_poll_outcome), `rt_async_scope.c:178`, `:201`, `:368` (scope_on_child_done) |

No caller is control-free; the fix is safe in both the control-held tree-walk
and every leaf call shape. No DONE enqueue (triple-guarded: `cancel_task:1135`,
`wake_task_with_policy` `rt_task_park.c:83`, `wake_task_on_shard_locked:51`); no
global scan (per-task atomic token + one owner shard).

## Counter substitution (flagged to architect)

The brief's `rt_trace_cancel_wake_forced` counter would grow `rt_async_trace.c`
from 666 (ACCEPTABLE) to 676 (BAD, >675), which the file-size gate hard-fails
and Global Rule 4 forbids (must not grow an over-limit file), and its call site
tipped `rt_async_state.c` to 1185 (>1184 ceiling). Because
`rt_sync_point_reached_count(SP_CANCEL_BEFORE_WAKE)` already provides the exact
assertion the counter was for ("cancel took the wake path"), the counter was
dropped. The deterministic test asserts via the reached-count plus the
behavioral cancelled-unwind. Architect can override if a counter is still
wanted (would need an approved `.loc-legacy-allowlist` entry or an offset).

## Gates (per-slice; recorded in ../09-evidence.md)

`git diff --check` clean; `make c-check` pass; `make cppcheck` pass (57/57);
`make runtime-v2-syncpoint-check` pass; `./check_file_sizes.sh -a` pass with
`rt_async_state.c` = 1184 (at ceiling), `rt_async_trace.c` = 666 (unchanged),
`rt_async_poll.c` = 313, `rt_worker_turn.c` = 243, `rt_task_park.c` = 203,
`rt_sync_point.c` = 178, `rt_sync_point.h` = 37.

Focused proof command:

```bash
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LifecycleDebt023CancelParkWakeToken(Proof|NegativeControl)$' -count=1 -parallel=1 -p=1 -v --timeout 60s
```

Result: exit 0. Positive proof passed at `SURGE_SHARDS=1,2,8`. Negative-control
build passed the Go test by failing the harness run as expected with
`debt023 proof target stranded after release`; this failure occurs after both
`SP_PARK_BEFORE_WAITING` and `SP_CANCEL_BEFORE_WAKE` reached-count assertions.

`make runtime-v2-lifecycle-check` now includes the proof and negative-control
tests and passed locally (exit 0, `surge/internal/vm` in 67.637s).

Not run in this slice: full `make runtime-v2-check`, full `make check`, a
DEBT-023-specific TSan proof, and the broader proof-matrix row for cancel racing
`wake_key_all` mid-drain.
