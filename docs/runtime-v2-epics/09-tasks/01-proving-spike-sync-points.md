# Epic 9 Task 1: Proving Spike — Deterministic Interleaving Sync Points

**Status:** complete for the scaffold and first consumer proof.
**Kind:** proving spike (RULES.md Global Rule 1 / Global Rule 5).

## Why a spike is needed

All three Epic 9 windows are instruction-scale:

- `RV2-DEBT-023`: cancel's status observation vs `park_current`'s `TASK_WAITING`
  store (`rt_task_park.c:270`), a few instructions wide.
- `RV2-DEBT-022`: the awaiter's `done_waiters` increment / status load vs the
  completer's `TASK_DONE` store / `done_waiters` load — a StoreLoad window.
- `RV2-DEBT-020`: the migrate source-unlock..dest-lock gap.

The epic's Proof And Quality Contract says TSan and stress runs are corroboration,
not the deterministic proof. Reproducing an instruction-scale window on demand
needs injected rendezvous inside the real runtime.

## Mechanism decision (architect ruling, verbatim rationale)

Chosen mechanism: **(a) test-only env-gated injectable sync points ("pause
hooks") in the C runtime**, over (b) a C-level litmus harness and (c)
scheduler-controlled interleaving in the pin-stress harness.

Architect's decisive reasoning (recorded verbatim, memory write path malformed):

- (c) is OUT — too coarse for a few-instruction window; `SURGE_SCHED=seeded`
  controls scheduler CHOICES only, not OS-thread/I/O interleavings
  (`rt_async_state.c:99`), and existing gates are coarse busy-wait + `sleep_us`.
- (b) C-litmus fits DEBT-022's pure StoreLoad, but for DEBT-023/020 it must
  RE-IMPLEMENT `park_current`/`cancel_task`/`wake_task_on_shard_locked`, proving
  a model rather than the real interlock the 023 fix depends on; the epic
  classifies (b) as new machinery too, and it still needs a separate TU.
- (a) is the ONE mechanism that proves the REAL shipping functions for all three
  windows, and it REUSES the existing subprocess `runLifecycleHarness` for
  delivery (env-armed subprocess, `poll_fn_id` behaviors, `spawn_pinned` +
  `TASK_PLACEMENT_CONNECTION` for F2, `runLifecycleModeAcrossShards` for the
  `SURGE_SHARDS=1/2/8` sweep). The only NEW surface is the `RT_SYNC_POINT`
  rendezvous macro + named allowlist header + the static gate — minimal and
  bounded new machinery under Global Rule 5. It also generalizes the existing
  `channel_wake_force_inject` env/test-gated-static idiom (`SURGE_CHANNEL_WAKE_
  INJECT`, accessor `rt_async_state.c:26`, env-init `:196`).

## Global Rule 1 proving-spike record

- **Hypothesis:** a compile-time-gated macro that injects a named rendezvous
  into real runtime code can reproduce each instruction-scale window
  deterministically, while compiling to nothing (no code, no symbol) in the
  shipping build so it can never sit on the worker steady path.
- **Files/surfaces allowed to change:** new `runtime/native/rt_sync_point.h`,
  `runtime/native/rt_sync_point.c`, `check_sync_points.sh`, the
  `runtime-v2-syncpoint-check` Makefile target; later, `RT_SYNC_POINT(...)` or
  `RT_SYNC_POINT_IF(...)` call sites inside the five allowlisted windows only.
- **Behavior explicitly NOT final design:** the arming ENV format
  (`SURGE_SYNC_POINT`) and the rendezvous actions are test scaffolding, not a
  runtime contract; only the compile-to-nothing property and the allowlist are
  load-bearing.
- **Proof/gate:** `check_sync_points.sh` (nm negative-symbol on the tag-off
  build + allowlist + placement + no-default-arming); the fix tasks' deterministic
  tests each ship a negative control that MUST fail without the fix.
- **Success criteria:** release build links zero `rt_sync_point_*` rendezvous
  symbols; armed build reproduces each window on demand; static gate green in
  `runtime-v2-check`.
- **Failure criteria:** any rendezvous symbol in the tag-off build; a hook
  outside the five windows; a default build path arming the hooks.
- **Rollback note:** the mechanism is isolated to the two new files + the gate;
  reverting them and the `RT_SYNC_POINT(...)` / `RT_SYNC_POINT_IF(...)` call
  sites removes it entirely with no shipping-path residue (the macros are
  already no-ops there).

## Implementation

### `RT_SYNC_POINT` macros + allowlist — `runtime/native/rt_sync_point.h`

- `RT_SYNC_POINT(name)` expands to `rt_sync_point_reach(RT_SYNC_POINT_##name)`
  only when `RT_TEST_SYNC_POINTS` is defined; otherwise to
  `((void)RT_SYNC_POINT_##name)` — zero code, zero symbol, but the enumerator
  reference keeps the allowlist load-bearing (an unknown `name` fails to compile
  in BOTH builds).
- `RT_SYNC_POINT_IF(cond, name)` is the conditional form for hot paths: armed
  builds evaluate `cond` and reach the point only when it is true; release builds
  do not evaluate `cond` at all and keep only the enumerator cast. This preserves
  the "no branch / no symbol / no condition evaluation" shipping-path property
  while still making the allowlist load-bearing.
- The allowlist is the `rt_sync_point_id` enum. Exactly five windows are
  permitted this epic:
  - `SP_CANCEL_BEFORE_WAKE` (cancel_task, `rt_async_state.c`)
  - `SP_PARK_BEFORE_WAITING` (user-task PARKED outcome before WAITING commit,
    `rt_async_poll.c` and the equivalent shard-worker path in
    `rt_worker_turn.c`)
  - `SP_MARKDONE_BEFORE_DONEWAITERS_LOAD` (mark_done tail, `rt_async_state.c`)
  - `SP_AWAIT_AFTER_INCREMENT` (rt_task_await, `rt_async_task.c`)
  - `SP_WAKEKEY_MID_DRAIN` (wake_key_all_with_policy, `rt_task_park.c`)
  - `SP_MIGRATE_GAP` is reserved for an RV2-DEBT-020 code fix and is NOT added
    unless that fix is approved.

### Armed implementation — `runtime/native/rt_sync_point.c`

The whole translation unit is empty (one non-emitting typedef) unless
`RT_TEST_SYNC_POINTS` is defined. Armed, it parses `SURGE_SYNC_POINT=
"NAME:ACTION[,NAME:ACTION...]"` once and offers three rendezvous actions:

- `barrier` — reaching threads wait at a shared, generation-reusable barrier
  whose width is the number of `barrier` points armed; all are released
  together (simultaneous entry into the racy region, for the StoreLoad litmus).
  A hand-rolled generation barrier is used instead of `pthread_barrier_t`, which
  glibc hides under `-std=c11`.
- `block` — reaching thread waits for one permit from a shared counting
  semaphore (hold a thread at this window for an ordered interleaving).
- `open` — reaching thread grants one permit, then continues (release a thread
  blocked at its `block` window).

Both primitives are re-armable across the many iterations the DEBT-022
store-buffer litmus needs, and all waits are bounded (10 s) so a mis-armed test
fails loud instead of hanging the harness. `rt_sync_point_reached_count(id)`
lets a proof assert a window was actually exercised (never a silent skip).

### Static gate — `check_sync_points.sh` / `make runtime-v2-syncpoint-check`

1. **Negative-symbol:** compile every `runtime/native/*.c` (rt_entry.c excluded,
   mirroring the harness) WITHOUT `RT_TEST_SYNC_POINTS`, and assert no object
   references `rt_sync_point_reach`. Proves the release build links no hook.
2. **Allowlist:** every `RT_SYNC_POINT(<name>)` or
   `RT_SYNC_POINT_IF(<cond>, <name>)` uses an allowlisted enumerator,
   cross-checked against the header so the gate cannot drift from it.
3. **Placement:** each name appears only in its designated window file(s).
4. **No-default-arming:** no Makefile path defines `RT_TEST_SYNC_POINTS` or
   passes `-tags surge_syncpoints` outside the syncpoint gate itself.

### Build/test wiring

The subprocess `runLifecycleHarness` compiles all `runtime/native/*.c` with
`clang -std=c11 -Wall -Wextra -Werror -pthread`
(`internal/vm/runtime_v2_lifecycle_behavior_harness_test.go:35`); `rt_sync_point.c`
is picked up automatically. The first consumer uses
`buildRuntimeV2LifecycleHarnessSyncPoints`, which passes `-DRT_TEST_SYNC_POINTS`
only for the focused `runtime_v2_pending` proof test. Its negative-control build
adds `-DRV2_DEBT_023_NEGATIVE_CONTROL`. Default Make targets do not define either
macro; `make runtime-v2-syncpoint-check` verifies this and the tag-off
negative-symbol property.

## Evidence

Per-slice gate results are in `../09-evidence.md` (Slice 1 section):
`git diff --check` clean; `make c-check` and `make cppcheck` pass;
`make runtime-v2-syncpoint-check` green; focused positive/negative
`RV2-DEBT-023` proof green; `make runtime-v2-lifecycle-check` green with the
proof wired in; `./check_file_sizes.sh -a` reports `rt_sync_point.c` = 178 and
`rt_sync_point.h` = 37 (both OK).
