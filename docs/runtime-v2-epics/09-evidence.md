# Epic 9 Evidence — Wakeup And Cancellation Safety

Follows `EVIDENCE_TEMPLATE.md`. One section per slice. This file also carries the
coder implementation log for the epic (the swarm memory write path is malformed
this session, so durable decisions live here and in `DEBT.md`, not in memory).

## Task Identity And Scope

- Epic: `09-wakeup-and-cancellation-safety`
- Repo/branch: `/home/zov/projects/surge/surge` @ `codex/runtime-net-scheduler-refactor`
- Scope: runtime-only close/narrow of `RV2-DEBT-022`, `RV2-DEBT-023`,
  `RV2-DEBT-020`, plus the test-only proving-spike mechanism.
- Out of scope: Surge syntax/parser/semantic/lowering/stdlib-public; Phase 4
  transport; control-lane rollback of Epic 8 paths; the `RV2-DEBT-003`
  completion/cancel split (architect ruled OUT of Epic 9).
- Execution order: Slice 1 (spike) → Slice 2 (023) → Slice 3 (020 proof) →
  Slice 4 (022) → Slice 5 (closeout).

## Baseline Commit/Status

- Baseline commit: `d80ef41c` (docs(runtime): close epic 8 debt ledger).
- Perf floor to hold (architect / researcher `epic9-baseline-gates`):
  `control_lock_acquired` ~11906 (11.627/req), `ctrl_await_compat` ~3458
  (3.377/req), steady-state-control ~8448 (8.250/req, ceiling 20),
  lifecycle-control ~6139 (5.995/req, ceiling 9), `placement_adoptions` ~249.
- `rt_async_state.c` LOC ceiling: 1184 eff (`.loc-legacy-allowlist`); must not
  grow.

---

## Slice 1 — Proving Spike (test-only sync points)

**Status:** complete for scaffold + first consumer proof. See
`09-tasks/01-proving-spike-sync-points.md` for the Global Rule 1 record and the
mechanism rationale.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_sync_point.h` | new | `RT_SYNC_POINT` / `RT_SYNC_POINT_IF` macros + allowlist enum + DEBT-023 negative-control toggle | 37 (`check_file_sizes.sh -a`, OK) |
| `runtime/native/rt_sync_point.c` | new | armed rendezvous impl; empty TU in release | 178 (`check_file_sizes.sh -a`, OK) |
| `check_sync_points.sh` | new | static gate (nm negative-symbol + allowlist + placement + no-default-arming) | shell, 109 lines |
| `Makefile` | edit | add `runtime-v2-syncpoint-check`, call it from `runtime-v2-check`, add to `.PHONY` | n/a |
| `internal/vm/runtime_v2_lifecycle_behavior_harness_test.go` | edit | add `buildRuntimeV2LifecycleHarnessSyncPoints` (`-DRT_TEST_SYNC_POINTS`, optional negative control) | Go test harness |

### Commands/Checks

| Command | Expected | Actual | Exit | Note |
| --- | --- | --- | --- | --- |
| `git diff --check` | no output | no output | 0 | whitespace gate |
| `make c-check` | pass | pass (after `clang-format -i`) | 0 | format + strict-warning compile |
| `make cppcheck` | pass | pass | 0 | checks default + `RT_TEST_SYNC_POINTS` configs |
| `make runtime-v2-syncpoint-check` | pass | pass (4/4 checks OK) | 0 | new static gate |
| `clang ... -c rt_sync_point.c` (tag off) | no `rt_sync_point_*` symbols | none | 0 | nm negative-symbol |
| `clang ... -DRT_TEST_SYNC_POINTS -c` | armed symbols present | `rt_sync_point_reach`, `rt_sync_point_reached_count` | 0 | armed build links |
| focused sync-point harness build | compile with `-DRT_TEST_SYNC_POINTS` and negative-control variant | compiled through targeted Go proof | 0 | no default Make target arms it |
| `./check_file_sizes.sh -a` (new files) | OK | `rt_sync_point.c` 178, `rt_sync_point.h` 37 | 0 | both OK |

### Trace/Liveness

N/A for the mechanism itself (test scaffolding). The windows it reproduces are
proven in Slices 2/4.

### Follow-Ups

| Item | Blocks? | Owner | Reason |
| --- | --- | --- | --- |
| `SP_MIGRATE_GAP` allowlist addition | no | coder | only if the DEBT-020 proof (Slice 3) becomes a code fix |

---

## Slice 2 — RV2-DEBT-023 (cancel wake token)

**Status:** fix landed and deterministic cancel-vs-park proof green.
Full write-up: `09-tasks/02-debt-023-cancel-wake-token.md`.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_async_state.c` | `cancel_task`: unconditional wake + `SP_CANCEL_BEFORE_WAKE`; rewrote the wake-token ordering comment | close RV2-DEBT-023 | 1184 eff (at ceiling; guard removal −1 offset the include +1) |
| `runtime/native/rt_async_poll.c` | `run_ready_one` user path: `RT_SYNC_POINT_IF(..., SP_PARK_BEFORE_WAITING)` after `POLL_PARKED`, before control reacquire | external-cancel proof window for the main-thread scheduler path; release build does not evaluate the predicate | 313 (OK) |
| `runtime/native/rt_worker_turn.c` | shard-worker user paths: same conditional `SP_PARK_BEFORE_WAITING` window | lifecycle harness and `SURGE_SHARDS=2,8` use real shard workers, not `run_ready_one`; release build does not evaluate the predicate | 243 (OK) |
| `runtime/native/rt_task_park.c` | removed obsolete syncpoint include/call; park commit consumes token as before | hook moved before WAITING commit path | 203 (OK) |
| `check_sync_points.sh` | allow `SP_PARK_BEFORE_WAITING` only in `rt_async_poll.c` and `rt_worker_turn.c`; parse `RT_SYNC_POINT` and `RT_SYNC_POINT_IF` | keep placement gate load-bearing after worker-path proof | shell |
| `internal/vm/runtime_v2_lifecycle_behavior_syncpoint_test.go` | new | external main-thread cancel proof, positive `SURGE_SHARDS=1,2,8`, expected-failing negative-control run | 111 lines |
| `internal/vm/runtime_v2_lifecycle_behavior_await_shutdown_test.go` | edit | dispatch proof poll id/mode only under `RT_TEST_SYNC_POINTS` | harness-only |

### No-resurrection proof

All 6 `cancel_task` callers verified control-held (table in the task doc):
recursion `:1206` and `rt_task_cancel:375` structurally; `scope_cancel_children_
controlled` (state.c:1373, scope.c:178/201/368) via the `need_control` guard;
both select sites; `clear_select_timers` (control from `mark_done` only when
`select_timers_len>0` forces `need_control`). No caller is control-free.

### Design change vs brief

`rt_trace_cancel_wake_forced` DROPPED — it grew `rt_async_trace.c` 666→676 (BAD,
Global Rule 4 violation) and tipped `rt_async_state.c` to 1185. The
`SP_CANCEL_BEFORE_WAKE` reached-count gives the same assertion. Flagged to
architect.

### Commands/Checks

| Command | Actual | Exit |
| --- | --- | --- |
| `git diff --check` | clean | 0 |
| `make c-check` | pass | 0 |
| `make cppcheck` | pass (57/57) | 0 |
| `make runtime-v2-syncpoint-check` | pass | 0 |
| `./check_file_sizes.sh -a` (filtered touched lines) | state.c 1184, trace.c 666, poll.c 313, worker_turn.c 243, park.c 203, sync_point.c 178, sync_point.h 37 | 0 |
| targeted proof: `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LifecycleDebt023CancelParkWakeToken(Proof\|NegativeControl)$' -count=1 -parallel=1 -p=1 -v --timeout 60s` | positive proof pass at shards 1/2/8; negative-control harness fails after both syncpoint counts and Go test passes | 0 |
| `make runtime-v2-lifecycle-check` | pass, includes `Debt023CancelParkWakeTokenProof` and `Debt023CancelParkWakeTokenNegativeControl` | 0 |
| armed full build (`-DRT_TEST_SYNC_POINTS`) | all `runtime/native/*.c` compile via targeted proof | 0 |
| release nm | zero `rt_sync_point_reach` symbols | 0 |

### Follow-Ups

| Item | Blocks? | Owner | Reason |
| --- | --- | --- | --- |
| Broader DEBT-023 matrix: join-key and sleep-kind variants + TSan | no for first-slice close; yes for full epic matrix | coder + tester | first slice proves the never-firing channel key path deterministically |
| cancel racing `wake_key_all` mid-drain (`SP_WAKEKEY_MID_DRAIN`) | no for first-slice close; yes for full epic matrix | coder | hook remains allowlisted but unused |

### Not Run In This Slice

Full `make runtime-v2-check`, full `make check`, and a DEBT-023-specific TSan
proof were not run.

## Slice 3 — RV2-DEBT-020 (accept-migration proof)

_Pending; proof reset. Task 3 must re-enumerate all `rt_task_replace_owner`
callers and prove whether any `join_key` waiter can strand during owner
replacement. Earlier local narrowing to a net-handle/stdlib-ABI blocker is not
accepted as evidence. Do not update `rt_waiter_route.c` comments until this proof
or fix is complete._

## Slice 4 — RV2-DEBT-022 (done_cv StoreLoad fence)

_Pending. seq-cst fence on both sides of the external-await handshake; DONE
store stays plain release; LOC offset by removing `mark_done_needs_control`
:1245-1247; update the `rt_async_internal.h` lane-invariant comment; negative
control MUST fail; before/after `bench_native_net.sh`; hold the perf floor._

## Slice 5 — Closeout

_Pending. DEBT.md / NOTES.md / README.md / RUNTIME_V2.md / epic-doc status;
final perf counters; contract sweep._
