# Epic 8 Task 8: Completion Epilogue And Done Path

Task 8 output. Runtime-code task. This document is self-contained: it restates
the runtime state it depends on with `file:line` evidence and points back to
`08-task-lifecycle-lane-and-net-fairness.md` (Boundary Decisions, Proof And
Quality Contract) and `08-lifecycle-lane-proving-spike.md` (the six written
rules and S6-Q1) for the rest.

Baseline commit for anchors: `d998df20` (Task 7, join-poll + F2, reviewed
APPROVE-WITH-NOTES). Performance baseline row: Task 7's 8x1024 row
(`control_lock_acquired` ~195600 total for 8192 requests, `ctrl_completion`
28673), NOT Task 6's — Task 7's row is the first with genuinely-distributed
8-worker execution (RV2-DEBT-016 reinterpretation, Task 7 handoff).

## Scope

1. Fix RACE 2 of `RV2-DEBT-019`: the unlocked `park_key` read in
   `mark_done_needs_control` racing the wake path's write under the owner shard
   lock.
2. Reduce `mark_done_needs_control` per S6-Q1 by removing the `WAKER_JOIN`
   park-key reason (Task 7 made join removal owner-local). The scope reasons
   stay until Task 9 (see "Task 8/Task 9 split" below).
3. Un-skip `TestRuntimeV2LifecycleCompletionPinInterleavingTSan` in the required
   no-keepalive mode and wire it into `runtime-v2-lifecycle-check`.
4. Peel pending static gate P8.
5. Fix the two stale lock-comments assigned by the Task 7 review (Global Rule 7).
6. Per-site `RT_CTRL_SITE_HANDLE` sub-attribution to resolve the Task 7
   `ctrl_handle` rise honestly (reviewer Note 3).

Non-goals: the scope owner-lane migration (Task 9), await/runner/blocking compat
narrowing (Task 10), the select slow lane (named non-goal), and any Phase 4
surface.

## The Task 8/Task 9 Split (approved by main)

S6-Q1's verdict is that `mark_done_needs_control` ends the epic keeping only the
net-key removal and `done_waiters > 0` reasons. Its table annotates the scope
reason's removal against S5-Q8/Q10 — which are **Task 9's** implementation
(scope object bookkeeping on the scope owner shard lock, `ex->scopes` atomic
snapshot, and the `scope_key` waiter store moving off `ex->control_waiters`).
Task 8 therefore removes only the reason whose enabling migration already
shipped:

| `mark_done_needs_control` reason | Site | Task 8 disposition |
| --- | --- | --- |
| residual `wait_keys_len` / `select_timers_len` | select non-goal / compat | stays (compat) |
| `parent_scope_id != 0` / `scope_registered` | scope bookkeeping | **stays — Task 9** |
| `park_key` is `WAKER_JOIN` | join removal | **removed (Task 8)** — Task 7 moved the join store to the target owner shard |
| `park_key` is `WAKER_SCOPE` | `scope_key` on control | **stays — Task 9** (`scope_key` still on `ex->control_waiters`) |
| `park_key` is net | net registry scan | stays (net contract) |
| `done_waiters > 0` | external-await compat | stays (rule 5) |

Why the scope reason cannot move in Task 8: the scope bookkeeping in `mark_done`
(`get_scope` at `rt_async_state.c:1598`, `scope_child_done_locked` at `:1612`,
the failfast mutation of `scope->failfast_*` at `:1603-1608`,
`scope_cancel_children_locked` at `:1606`, and a `WAKER_SCOPE` `remove_waiter`)
is all control-lane state today. `get_scope` (`rt_async_state.c`, a control-
guarded `ex->scopes[id]` read) is not safe to call control-free until Task 9's
atomic-snapshot scope table exists, and the scope mutators race `register` /
`join_all` / sibling child-done until they serialize on the scope owner shard
lock (S5-Q8). Dropping the scope reason before that migration would run scope
mutations unserialized — a race, not a lane slice.

Consequence for performance (recorded honestly, main's precedent): the Task 7
`ctrl_completion` = 28673 (3.500/req) is driven by these scope-registered
net-wrapper child completions taking control via the **scope** reason, not the
join reason. So Task 8's S6-Q1 slice does not claw back that 28673; the clawback
lands in Task 9 when the scope reason is removed. Task 8's honest contract:
RACE 2 closed, JOIN reason removed, zero regression, and a clean per-site row
prepared as Task 9's clawback anchor. This is recorded in three places:
`DEBT.md` (RV2-DEBT-016 progress note), this ledger's Task 8 section, and the
Task 9 doc's scope line when it is expanded.

## RACE 2 Fix — The `park_key` Family (RV2-DEBT-019)

The debt named two races, but the no-keepalive completion-pin TSan stress showed
the real root is a whole FAMILY, all one class: Epic 8 Task 7 moved join
registration off the control lock onto the source SHARD lock, and two consumers
kept control-era assumptions. All are closed in this task's commit.

### The races

- **Reader (the debt's "race 2"):** `mark_done_needs_control` read
  `task->park_key` with no lock (was `rt_async_state.c:1494`/`:1511`).
- **The root, found by the no-keepalive stress:** the wake path
  (`wake_task_on_shard_locked` at `rt_async_state.c:937-941`;
  `wake_task_with_policy`'s `park_seq` read at `:982`) read and cleared a task's
  `park_key`/`park_prepared` BEFORE checking status, i.e. even for a RUNNING
  task. When a completing target's join-completion DRAIN
  (`wake_key_all_with_policy` from `mark_done`) wakes a JOINER that is still
  RUNNING — mid `prepare_park` (`rt_async_waiter.c:627-628` via `rt_task_poll`'s
  register-then-verify) or mid `park_current` (via `apply_poll_outcome`), before
  it commits to `TASK_WAITING` — the waker touches `park_key` under the joiner's
  owner shard lock while the joiner writes it unlocked on its own thread. The D5
  register-then-commit protocol makes the LOGICAL outcome correct (the wake
  token forces the mid-park task to abort and re-run), but `park_key` is a
  non-atomic field written by two threads, so it is a real TSan data race.

Pre-existence is confirmed, not argued: the no-keepalive pin at clean baseline
`585e3c5c`, `SURGE_SHARDS=8`, FAILED under TSan (exit 1, `tsan_warnings=1`,
~92s). None of `wake_task_on_shard_locked` / `prepare_park` / `park_current` are
in the Task 8 diff except for the gate added below.

### The fix — gate the wake path's `park_key` work on WAITING (main-approved)

Ownership invariant (Global Rule 7 comment at `wake_task_on_shard_locked`):
`park_key`/`park_prepared` belong to the RUNNING task's own thread until it
commits to `TASK_WAITING` under the owner shard lock; the wake path may read or
clear them only once the task is provably parked (WAITING and not enqueued).
Concretely:

- `wake_task_on_shard_locked`: set the wake token (atomic) UNCONDITIONALLY as
  before — it is the D5 abort signal and is valid whether or not the task is
  parked — then check status FIRST; only when the task is WAITING-and-not-
  enqueued does it capture the stale key and clear `park_key`/`park_prepared`
  and enqueue. For any other status it returns without touching those fields.
- `wake_task_with_policy`: the same WAITING-and-not-enqueued gate guards the
  `park_seq`/`park_key.kind` read at `:982`.

This REMOVES work for non-parked tasks rather than adding locking, matching Task
1's "don't eat a fresh registration" philosophy: a stale registration a task
left is cleaned by its OWN `remove_waiter` path (the register-then-verify DONE
branch, `rt_async_task.c:223`; `park_current`'s token-abort requeue,
`park_requeue_locked`), never by an orphaned deferred removal from a wake that
never owned the park.

### mark_done stays lock-free (main-approved, contingent on the wake gate)

Because no waker touches a RUNNING task's `park_key`, and `mark_done`'s task is
always RUNNING during its `park_key` access (TASK_DONE is stored later),
`mark_done` reads `park_key` as a plain thread-own read with NO owner shard lock
— keeping the completion path shard-local-cheap (S6-Q1). The result-write
reorder (Task 7) and the original sleep-store handling are unchanged. The
`park_key` snapshot feeds `park_needs_control` (net or `WAKER_SCOPE`) and the
`remove_waiter(park)` cleanup exactly as before.

### F2 migrate sibling race (fixed in-commit; higher-level assumption = RV2-DEBT-020)

The same stress also hit `rt_waiter_migrate_join_waiters` (`rt_waiter_route.c`):
it read `from->len` UNLOCKED before taking the source shard lock. Same root
cause (Task 7 moved registration to the source shard lock; the migrate kept its
control-era "no registration interleaves" assumption). Triggered here because
`spawn_pinned` marks tasks `TASK_PLACEMENT_CONNECTION`, so a joiner consuming a
DONE pin target fires F2 adoption -> `rt_task_replace_owner` -> this migrate.
Fixed by dropping the unlocked `from->len == 0` early-out (the batch loop already
reads `from->len` under the source shard lock; an empty source returns after the
first locked pass). The migrate's HIGHER-LEVEL correctness assumption (comment
lines 82-84) is now also stale post-Task-7, but that is a logical interleaving,
not a TSan data race, and is genuinely F2 territory — recorded as RV2-DEBT-020
(owner: Epic 8 closeout) with the suspected-benign mechanism.

### Post-change `park_key` reader audit (main's verification point)

`grep task->park_key runtime/native`: the only cross-thread reader that was
unlocked *in the completion/lifecycle path* (`mark_done_needs_control`) is gone.
Remaining accesses are all safe: `mark_done` (plain thread-own read on the
RUNNING task, per the wake gate); `wake_task_on_shard_locked:934` /
`wake_task_with_policy:982` (now gated on WAITING, under the owner shard lock);
`channel_lane.h:89` — this is `channel_candidate_valid`'s `park_key` peek, and
it has TWO callers: `channel_deliver_same_shard_locked` reads it under the
channel owner shard lock (safe), but `channel_deliver_foreign` peeks it
pre-lock (under the control lock only, before taking the peer's owner shard
lock and re-validating), so that first read is a documented-benign candidate/
validate cross-lock read (`rt_channel_lane.h:83-86`: a mismatch just drops the
candidate, an exact key match means the peer still parks on this key), NOT an
owner-shard-locked read; `park_current:1137` and `prepare_park:627` (thread-own,
RUNNING); `rt_async_trace.c` SIGUSR1 dump (best-effort, trace-only). So no
cross-thread unlocked reader remains in the completion/lifecycle path; the one
surviving cross-lock reader is `channel_deliver_foreign`'s pre-lock peek, which
is the pre-existing, documented-benign channel candidate/validate read (not a
regression and not a `park_key`-ownership violation), the epic's channel lane
being an explicit non-goal here.

## S6-Q1 Reduction

`mark_done_needs_control` (`rt_async_state.c:1512-1534`) now takes a
`park_needs_control` int derived by `mark_done` from the snapshot (net or
`WAKER_SCOPE`), instead of reading `task->park_key` itself. The `WAKER_JOIN`
branch is gone. The scope reason (`parent_scope_id != 0 || scope_registered`)
stays with a comment naming Task 9.

## Tests And Gates Changed

- P8 (`TestRuntimeV2LifecycleStaticCompletionResultVisibilityOrder`,
  `runtime_v2_lifecycle_static_test.go`): `t.Skip` deleted. Asserts (i) the
  result writes precede the `TASK_DONE` release store (Task 7's reorder), and
  (ii) `mark_done_needs_control` no longer contains `WAKER_JOIN`. The
  scope/`WAKER_SCOPE`-reason-gone assertion is explicitly deferred to P9
  (`...StaticScopeOwnerLane`, Task 9), noted in the P8 comment — mirroring
  Task 7's approved G6/trace-gate scope adjustments.
- `TestRuntimeV2LifecycleCompletionPinInterleavingTSan`
  (`runtime_v2_lifecycle_behavior_handle_lifetime_test.go`): `t.Skip` deleted.
  Now runs in NO-KEEPALIVE mode (`LIFECYCLE_PIN_STRESS_NO_KEEPALIVE=1`, the
  required-passing config exercising both races together) swept across
  `SURGE_SHARDS=1,2,8`; TSan is the oracle. Added to the
  `runtime-v2-lifecycle-check` `-run` regex (`Makefile`).

## Stale Comment Fixes (Task 7 review, Global Rule 7)

- `ready_take_current_local_tail` (`rt_async_state.c:726`): the "Caller holds
  the control lock." claim was false — its sole caller `rt_task_poll`
  (`rt_async_task.c:197`) is control-free since Task 7. Rewritten to name the
  owner shard lock (taken here, and around the worker's `next_ready` pop and
  steals in `rt_worker_turn.c`) as the local queue's serializer.
- `remove_waiter` (`rt_async_waiter.c:465`): the "Caller holds ex->lock." claim
  was false for the control-free join-poll/completion callers. Rewritten to
  mirror `add_waiter` (`:523`): "control lock OR nothing, never a shard lock";
  it takes the key's store owner shard lock internally, and net keys scan every
  shard's store so those still need the control lane.

## Per-Site HANDLE Sub-Attribution (reviewer Note 3)

`RT_CTRL_SITE_HANDLE` aggregates three distinct causes. Added a
`rt_ctrl_handle_site` enum (`WAKE`/`CANCEL`/`FREE`, `rt_async_internal.h`), a
parallel counter array + `rt_trace_control_lock_handle_site` (`rt_async_trace.c`),
tagged alongside the existing `RT_CTRL_SITE_HANDLE` tag at the three sites:
`rt_task_wake`'s scope-adoption fallback (`rt_async_task.c`), `rt_task_cancel`,
and `task_release_lane_aware`'s last-reference free (`rt_async_state.c`). Dumped
as `ctrl_handle_wake/cancel/free`; the three sum to `ctrl_handle`. Additive
counters, no behavior change. To keep `rt_async_trace.c` within its size band
(Global Rule 4, see LOC), the existing six per-site `ctrl_*` dump appends and the
three new ones were collapsed into two enum-order loops over name tables, which
emits byte-identical output (verified: `trace_append_kv_u64` prepends
` name=value`, and the loops walk the enums in dump order) and net-REDUCES the
file from 671 to 666. Measured on the net bench (sums exactly): `ctrl_handle`
29696 = `ctrl_handle_free` 28672 + `ctrl_handle_wake` 1024 + `ctrl_handle_cancel`
0. The Task 7 28673->29696 rise is the `ctrl_handle_wake` component (`rt_task_wake`
scope-adoption, once per connection = 1024), not a change in the free path — see
Measurement.

## Effective LOC (gate `./check_file_sizes.sh -a`, all pass)

| File | Before | After | Note |
| --- | ---: | ---: | --- |
| `rt_async_state.c` | 1444 | 1447 | legacy ceiling 1580; +3, not grown over |
| `rt_async_task.c` | 312 | 314 | +2 handle sub-site tags |
| `rt_async_trace.c` | 671 | 666 | ACCEPTABLE tier; net -5 (dump loop offsets the sub-counters) |
| `rt_async_internal.h` | 536 | 543 | +7 enum/decl (OK tier) |
| `rt_async_waiter.c` | ~590 | 600 | ACCEPTABLE; comment only |
| `rt_waiter_route.c` | ~105 | 105 | OK; -1 early-out line +comment |

## Baseline Pre-Existence Confirmation (main-requested)

The `park_key` family and the F2 migrate race were both proven pre-existing at
clean `585e3c5c` (the Task 11 landing HEAD this commit sits on), not introduced
by this task. Command (this task's runtime C stashed, the un-skipped no-keepalive
test kept):

```
git stash push -- runtime/native/rt_async_state.c rt_async_waiter.c rt_async_task.c rt_async_trace.c rt_async_internal.h
SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm \
  -run '^TestRuntimeV2LifecycleCompletionPinInterleavingTSan$/shards-8$' -count=1 -parallel=1 -p=1 -v --timeout 250s
```

Result: FAIL, exit 1, `tsan_warnings=1` (~92s), first race reported in
`rt_waiter_migrate_join_waiters` (the F2 sibling); the park_key family reproduces
on other runs. Stash restored after.

## Gates (all pass)

- `git diff --check`: clean.
- `make c-check`, `make cppcheck`: OK.
- `timeout 1500 make runtime-v2-check`: exit 0, 0 FAIL lines — the full
  blast-radius suite (MT liveness, lock-split, accept, channel, waiter,
  fd-registry, heap, lifecycle) green after the wake-primitive change, including
  the newly-wired `CompletionPinInterleavingTSan` at `SURGE_SHARDS=1,2,8`
  (TSan clean).
- `make check`: (recorded at commit).
- `./check_file_sizes.sh -a`: exit 0; per-file deltas above. NOTE: `-a` was
  exiting 1 solely because it scanned the investigator's read-only
  `.claude/worktrees/` nested checkout (6 copies of the legacy over-limit files
  showing BAD because the worktree path does not match the `runtime/native/...`
  allowlist entries) — none in the main tree, none mine, and the default mode
  (`make check`'s gate) already exited 0. Fixed by pruning `*/.claude` from the
  `-a` find, mirroring Task 6's identical `.claude`/`target` exclusion in
  `code_stats.sh`. `bash -n` clean.
- Sentrux (`sentrux check`): root 6174 (Task 7 6174), `runtime` 5289 (5290),
  `runtime/native` 5378 (5379) — flat within noise, all rules pass on all three.
- Net bench per-site before/after: see Measurement.

## Measurement (net `direct/seq`, 8 shards / 8 threads / 1024 conns / 8 req/conn = 8192 requests, `SURGE_TRACE_EXEC=1`)

Anchor = Task 7's row (`control_lock_acquired` ~195600, `ctrl_completion`
28673). Measured (`scripts/bench_native_net.sh`, one 8x1024 row):

| Site | Before (Task 7) | After (Task 8) |
| --- | ---: | ---: |
| `control_lock_acquired` | ~195600 | 192454 |
| `ctrl_create` | 11-12 | 9 |
| `ctrl_join_poll` | 2019-2037 | 2039 |
| `ctrl_completion` | 28673 | 28673 |
| `ctrl_scope` | 106499 | 106499 |
| `ctrl_handle` | 29696 | 29696 |
| total us | ~1.51e6 | 1510801 |

Per-site is essentially bit-stable vs Task 7: the `WAKER_JOIN` reason removal has
~0 effect on this bench because these completions take control via the scope
reason, not the join park-key. `control_lock_acquired` dropped ~1.5% (noise/small
win), no material regression. The `ctrl_completion` clawback is NOT here (it is
the scope reason, Task 9). `ctrl_handle` sub-attribution: free 28672 + wake 1024
+ cancel 0 = 29696 (see the HANDLE section).
