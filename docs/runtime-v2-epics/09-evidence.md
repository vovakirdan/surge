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
| `SP_MIGRATE_GAP` allowlist addition | closed | coder | added in Slice 3 after the old migration order was proven to strand a late join waiter |

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
| Broader DEBT-023 matrix: join-key and sleep-kind variants + TSan | no | future matrix hardening | first slice proves the known never-firing channel key bug deterministically; closeout does not convert broader rows into known debt |
| cancel racing `wake_key_all` mid-drain (`SP_WAKEKEY_MID_DRAIN`) | no | future matrix hardening | hook remains allowlisted but unused; no separate correctness debt was opened |

### Not Run In This Slice

Full `make runtime-v2-check`, full `make check`, and a DEBT-023-specific TSan
proof were not run.

## Slice 3 — RV2-DEBT-020 (accept-migration proof)

**Status:** complete. `RV2-DEBT-020` is closed by a generic join-route protocol
and a deterministic positive/negative `SP_MIGRATE_GAP` proof. Full task write-up:
`09-tasks/03-debt-020-accept-migration-proof.md`.

### Caller Enumeration

| Caller | Re-owned task | Status at call | Join waiter can exist/strand? |
| --- | --- | --- | --- |
| `runtime/native/rt_async_task.c:278` | F2 adoption re-owns `current`, not the DONE child. | RUNNING | Can exist via cloned handles; old migration could strand a gap waiter. Generic fix covers it. |
| `runtime/native/rt_async_waiter.c:381` | accept wake re-owns the popped accept waiter. | WAITING before `wake_net_task` | Can exist via cloned handles; generic fix covers it. |
| `runtime/native/rt_net_accept_group.c:101` | accept ready-now re-owns `rt_current_task()`. | RUNNING | Can exist while mid-accept; generic fix covers it. |
| `runtime/native/rt_net_accept_group.c:111` / `runtime/native/rt_net.c:516` | accept success self-placement re-owns `rt_current_task()`. | RUNNING | Can exist while mid-accept; generic fix covers it. |

### Interleaving Model

The accepted fix is not simple publish-before-migrate. It adds an atomic
`join_owner_shard_id` route on `rt_task` and requires every `WAKER_JOIN`
add/remove/pop and collect-all wake operation to resolve the route, lock that
shard, re-read the route under the same lock, and retry on mismatch. Production owner replacement
publishes the new join route while holding the old route shard lock, then drains
old entries. A stale registrar that selected the old route before publication
either inserted before publication and is drained, or re-reads the changed route
under that same lock and retries on the new store.

Completion-before-migration is closed by current call shapes, not by the route
field alone: F2, accept ready-now, and accept success re-own the executing
`rt_current_task()`, so completion cannot run until `rt_task_replace_owner`
returns to the poll/native call; accept wake re-owns a WAITING task before
`wake_net_task` enqueues it.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_async_internal.h` | add `join_owner_shard_id` and acquire/release helpers | separate join-waiter route from scheduler placement publication | 569 (OK) |
| `runtime/native/rt_scheduler_placement.c` | publish/migrate join route in `rt_task_replace_owner`; negative-control old order | generic fix for all four production call shapes | 125 (OK) |
| `runtime/native/rt_waiter_route.c` | add `rt_waiter_publish_join_owner_and_migrate`; keep old migrator only for negative control | close stale-read/register-after-drain gap | 176 (OK) |
| `runtime/native/rt_waiter_join_route.c` | new join add/remove/pop/collect route-lock helpers | prevent store/lock snapshot mismatch for `WAKER_JOIN` | 141 (OK) |
| `runtime/native/rt_async_waiter.c` | delegate `WAKER_JOIN` add/remove/pop to route-lock helpers | avoid generic split store/lock routing for join keys | 639 (ACCEPTABLE) |
| `runtime/native/rt_task_park.c` | route `wake_key_all_with_policy` join-key collection through the join-route helper | completion wake must not split join store/lock snapshots | 207 (OK) |
| `runtime/native/rt_sync_point.h`, `runtime/native/rt_sync_point.c`, `check_sync_points.sh` | add `SP_MIGRATE_GAP` and allowlist placement | deterministic migration-gap proof | 38 / 180 (OK) |
| `internal/vm/runtime_v2_lifecycle_behavior_*` and `internal/vm/runtime_v2_lifecycle_static_test.go` | add Debt020 harness, static route assertion, dispatch, and negative control | positive/negative proof and static guard | Go tests |
| `Makefile` | include Debt020 proof pair in `runtime-v2-lifecycle-check` | keep the proof in the lifecycle gate | n/a |

### Commands/Checks

| Command | Actual | Exit |
| --- | --- | --- |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LifecycleDebt020MigrateGap(Proof\|NegativeControl)$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | positive proof passed at `SURGE_SHARDS=2,8`; negative-control build failed with `debt020 migrate-gap joiner stranded` and the Go test passed | 0 |
| focused static/proof command including `StaticJoinWaiterRoutesByTargetOwner` and `OwnerLocalWaiterSkeletonStaticShape` | passed | 0 |
| `make runtime-v2-syncpoint-check` | pass; `SP_MIGRATE_GAP` is placed only in `rt_scheduler_placement.c` / `rt_waiter_route.c` | 0 |
| `make c-check` | pass after `clang-format -i runtime/native/rt_waiter_join_route.c runtime/native/rt_async_waiter.c runtime/native/rt_waiter.h` | 0 |
| `make cppcheck` | pass after const-pointer cleanup in the new join-route helpers | 0 |
| `./check_file_sizes.sh -a` | pass after splitting join helpers; `rt_async_waiter.c` 639 ACCEPTABLE, `rt_waiter_join_route.c` 141 OK, `rt_task_park.c` 207 OK, no `>675` files | 0 |
| `git diff --check` | clean | 0 |

### Not Run In This Slice

Full `make runtime-v2-check`, full `make check`, and perf benchmarks were not
run in this slice.

## Slice 4 — RV2-DEBT-022 (done_cv StoreLoad fence)

**Status:** complete. `RV2-DEBT-022` is closed by the seq-cst StoreLoad
handshake in the external-await compatibility path plus a guarded post-`DONE`
`done_cv` broadcast helper.

### Interleaving Model

- Old window: external awaiter did `done_waiters++` then loaded
  `target->status`; completer stored `TASK_DONE` then loaded `done_waiters`.
  Without both store->load edges in one seq-cst order, the awaiter could observe
  not-DONE and park while the completer observed zero waiters and skipped the
  only steady-state `done_cv` broadcast.
- New protocol: `rt_done_waiters_increment_for_external_await`,
  `rt_task_status_load_for_external_await`,
  `rt_task_status_store_done_for_external_awaiters`, and
  `rt_done_waiters_load_after_done` are seq-cst in production. The
  `done_cv` broadcast is delegated to `rt_done_cv_broadcast_after_done`, which
  performs the post-`DONE` waiter load and broadcasts under `ex->lock` with the
  `RT_CTRL_SITE_AWAIT_COMPAT` trace tag.
- Negative control: `RV2_DEBT_022_NEGATIVE_CONTROL` removes the seq-cst
  participation and forces the old missed-waiter decision. The runtime proof
  then strands with `debt022 external awaiter stranded before done_cv wait`,
  showing the condvar/broadcast half would lose the only wake.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `runtime/native/rt_async_internal.h` | add seq-cst external-await helper cluster and `rt_done_cv_broadcast_after_done` prototype | keep the ordering contract named and statically pinnable | 630 (ACCEPTABLE) |
| `runtime/native/rt_async_task.c` | use seq-cst waiter increment/status load helpers; add `SP_AWAIT_BEFORE_DONECV_WAIT` | awaiter half of StoreLoad proof | 317 (OK) |
| `runtime/native/rt_async_state.c` | use seq-cst DONE store helper; delegate `done_cv` broadcast | completer half without adding control-lane fallback to every completion | 1183 (LEGACY OK <= 1184) |
| `runtime/native/rt_done_cv.c` | new focused post-DONE waiter load + single completion broadcast helper | isolate `done_cv` compat and avoid growing `rt_async_state.c` | 20 (OK) |
| `runtime/native/rt_sync_point.h`, `runtime/native/rt_sync_point.c`, `check_sync_points.sh` | add seventh Epic 9 window, `SP_AWAIT_BEFORE_DONECV_WAIT` | deterministic condvar-window proof | header 39 / c 182 (OK) |
| `internal/vm/runtime_v2_lifecycle_behavior_*`, `runtime_v2_lifecycle_static_test.go` | add positive/negative Debt022 proof, external-await matrix, static gates | close debt with deterministic and mechanical evidence | Go tests |
| `Makefile` | add Debt022 proof tests to `runtime-v2-lifecycle-check` | CI gate coverage | n/a |

### Commands/Checks

| Command | Actual | Exit |
| --- | --- | --- |
| `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2LifecycleDebt022(DoneCVStoreLoad(Proof\|NegativeControl)\|ExternalAwaitMatrix)$' -count=1 -parallel=1 -p=1 -v --timeout 120s` | positive proof passed at `SURGE_SHARDS=1,2,8`; negative-control failed for the expected `debt022 external awaiter stranded before done_cv wait`; matrix passed multi-awaiters, already-DONE, parked target, cancelled parked target | 0 |
| focused static + Debt022 proof command including `StaticAwaitCompatCountedSeparately` | passed; static gate pins helper names, single completion `done_cv` broadcast in `rt_done_cv.c`, and no worker completion wait on `done_cv` | 0 |
| `make runtime-v2-syncpoint-check` | pass; allowlist has 7 enumerators and `SP_AWAIT_BEFORE_DONECV_WAIT` is pinned to `rt_async_task.c` | 0 |
| `make c-check` | pass | 0 |
| `make cppcheck` | pass after making the negative-control missed waiter read non-constant to cppcheck | 0 |
| `./check_file_sizes.sh -a` | pass; `rt_async_state.c` 1183, `rt_async_internal.h` 630, `rt_async_task.c` 317, `rt_done_cv.c` 20, `rt_sync_point.c` 182, `rt_sync_point.h` 39 | 0 |
| `make runtime-v2-lifecycle-check` | pass; includes Debt020/022/023 and static lifecycle gates | 0 |
| `make runtime-v2-perf-check` | pass; `control_lock_acquired=11819` (`11.542/req`), `ctrl_await_compat=3458` (`3.377/req`), steady-state-control `8361` (`8.165/req`, ceiling `20.0`), lifecycle-control `6143` (`5.999/req`, ceiling `9.0`), `placement_adoptions=253` | 0 |
| `make runtime-v2-check` | pass after repairing the owner-local net waiter C harness capacity for Task 3's join-route lookup (`stub_tasks[8]` could not contain its own id `55` target) | 0 |
| `make check` | pass; Go tests, golangci-lint, `c-check`, and uncommitted-file LOC gate green | 0 |
| `sentrux check .` | pass; quality `6177`; 10 rules checked | 0 |
| `sentrux check runtime` | pass; quality `5327`; 7 rules checked | 0 |
| `sentrux check runtime/native` | pass; quality `5430`; 7 rules checked | 0 |
| `sentrux gate .` | DEGRADED vs old baseline: quality `6198 -> 6177`, complex functions `533 -> 538` | 1 |
| `sentrux gate runtime` | DEGRADED vs old baseline: quality `5195 -> 5327`, complex functions `21 -> 23` | 1 |
| `sentrux gate runtime/native` | DEGRADED vs old baseline: quality `5159 -> 5430`, coupling `0.00 -> 0.06`, complex functions `21 -> 23` | 1 |

### Follow-Ups

No new debt was opened for `RV2-DEBT-022`. The broader
`RV2-DEBT-003` completion/cancel split remains open, but this task removed the
`done_cv` broadcast as a blocker by moving it into `rt_done_cv.c` with a static
gate. The Sentrux gate failures are the existing cumulative
coupling/complex-function recovery class carried by `RV2-DEBT-003`; Sentrux
`check` passes at all required scopes and runtime/native quality improved above
the stored baseline.

## Slice 5 — Epic Closeout

**Status:** complete. Full closeout:
`09-tasks/05-epic-closeout.md`.

### Final State

| Item | State |
| --- | --- |
| `RV2-DEBT-023` | CLOSED by Task 2 (`dfbf5897`): unconditional cancel wake token plus positive/negative `SP_PARK_BEFORE_WAITING` proof. |
| `RV2-DEBT-020` | CLOSED by Task 3 (`ff57b8a2`): join-owner route plus positive/negative `SP_MIGRATE_GAP` proof. |
| `RV2-DEBT-022` | CLOSED by Task 4 (`82c633a7`): seq-cst external-await StoreLoad handshake plus guarded `rt_done_cv.c` broadcast helper and positive/negative proof. |
| `RV2-DEBT-003` | OPEN: dependency-aware cleanup remains future work; Task 4 removed the old `done_cv` helper blocker but did not take the completion/cancel split. |
| Broader cancellation matrix | Not converted into new debt. Dedicated rows beyond the three closed windows remain future matrix-hardening candidates if we choose to expand cancellation coverage. |

### Final Gates From Last Code Slice

| Command | Actual | Exit |
| --- | --- | --- |
| `git diff --check` | clean | 0 |
| `make c-check` | pass | 0 |
| `make cppcheck` | pass | 0 |
| `make runtime-v2-syncpoint-check` | pass; seven Epic 9 sync-point enumerators | 0 |
| `make runtime-v2-lifecycle-check` | pass; includes Debt020/022/023 proof pairs plus lifecycle static/TSan gates | 0 |
| `make runtime-v2-perf-check` | pass; `control_lock_acquired=11819`, `ctrl_await_compat=3458`, steady-state-control `8.165/req`, lifecycle-control `5.999/req` | 0 |
| `make runtime-v2-check` | pass | 0 |
| `make check` | pass | 0 |
| `./check_file_sizes.sh -a` | pass; key touched files stayed within current allowlists | 0 |
| `sentrux check .` | pass; quality `6177` | 0 |
| `sentrux check runtime` | pass; quality `5327` | 0 |
| `sentrux check runtime/native` | pass; quality `5430` | 0 |

Sentrux `gate` still reports the existing cumulative `RV2-DEBT-003` recovery
class on complex-function/coupling drift; this is recorded in the debt ledger
and is not a new Epic 9 debt.

### Closeout Docs

Updated in this slice: `09-tasks/05-epic-closeout.md`, this evidence file, the
Epic 9 task index, `09-wakeup-and-cancellation-safety.md`, `NOTES.md`, the
epics README, and `docs/RUNTIME_V2.md`.
