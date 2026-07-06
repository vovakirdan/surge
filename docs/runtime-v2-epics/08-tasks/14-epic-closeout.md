# Epic 8 Task 14: Epic Closeout

Closeout task for Epic 8 (Task Lifecycle Lane And Net Fairness). This document
consolidates the epic's durable results, verifies BOTH contracts clause by
clause with named proofs, reconciles the debt ledger, records the reviewer-note
dispositions, and states the next-epic handoff (including the mandatory syntax /
Phase-4 warning). It is self-contained: it cites `file:line`, test names, and
commit hashes rather than assuming the reader has the whole epic in memory.

Baseline for anchors: epic opened at `072bbde0`; closeout sits on `af0416fc`
(Task 13, amended to fold in the Global-Rule-3 Sentrux acceptance records).

## Task Ledger (all 14 complete)

| Task | Commit(s) | Result |
| --- | --- | --- |
| 1 kickoff/baseline/sentrux | `daeac51e` (+ ParkUnpark fix) | fresh Epic 8 baselines; 26.4 control acq/req on 8x1024; found+fixed a lost-wake gate blocker (`RV2-DEBT-002` root cause). |
| 2 lifecycle dependency map | `daeac51e`-based docs | mapped create/table/join/done/handle/scope/await/cancel/free to locks + target lanes. |
| 3 lifecycle lane proving spike | `c4392d6e` | decided the shard-owned lifecycle model (16 questions), six written rules; TSan model proved publish/pin. |
| 4 behavior contract tests | `583765c4` | 10 `TestRuntimeV2Lifecycle*` behavior tests + native harness; found `RV2-DEBT-019` (two TSan races). |
| 5 static shape + trace tests | `27eeabd7` | G1-G6 static gates + per-site `ctrl_*` counters; escalation verdict (create 3.5/req → segmented table). |
| 6 task create + table publication | `5523094e` (+ `a2d3f87c`, `05d95b60`) | segmented never-moved-slot task table; `__task_create` on owner lane; `ctrl_create` 3.500→~0.001/req. |
| 7 join poll + handle lifetime (+F2) | `d998df20` | join poll/clone/wake off control; F2 placement adoption folded in; `ctrl_join_poll` 3.881→0.249/req. |
| 8 completion epilogue + done path | `ae44d945` | `RV2-DEBT-019` closed (full park_key family); `WAKER_JOIN` reason removed; completion shard-local; `RV2-DEBT-020` raised. |
| 9 scope owner lane | `b9a420c0` | scope table atomic snapshot + scope bookkeeping + `scope_key` store on scope owner shard; `ctrl_scope` 106499→19464 (-82%), total -45%. |
| 10 await/runner/blocking compat | `aa66a0b7` (+ `8c89f358`) | `done_cv`/`compat_cv` external-only + counted separately (`ctrl_await_compat`); `RV2-DEBT-022` raised. |
| 11 net fairness starvation | `585e3c5c` (F2 at `d998df20`) | `RV2-DEBT-015` FIXED: placement-funnel mechanism trace-pinned; F2 fix; stall harness promoted. |
| 12 perf benchmark + CI gate | `18011765` (baseline `8c89f358`) | post-F2 re-baseline; per-commit `TestRuntimeV2PerfControlLaneGate`; `RV2-DEBT-016` CLOSED (12.86/req total, 9.36/req steady << 26.4). |
| 13 large-file + quality tranche | `af0416fc` (amended from `dcd3b6a3`) | `rt_task_park.c` extracted (`rt_async_state.c` 1386→1184 eff LOC); stale invariant comment rewritten to the 3-lane model; Global-Rule-3 −41 code-scope drop accepted with `RV2-DEBT-003` as recovery owner. |
| 14 epic closeout | this commit | contract sweep, DEBT reconciliation, NOTES consolidation, RUNTIME_V2 flowback, handoff; `RV2-DEBT-021` closed (test), `RV2-DEBT-020`/`022`/`023` carried with owners. |

## Proof And Quality Contract — clause-by-clause verification

Every clause is proven; there are NO unmet clauses. "recorded per task" means the
gate is logged in that task's `08-evidence.md` section and the task document.

### Per-task required gates

| Clause | Proof |
| --- | --- |
| `git diff --check` | recorded green in every runtime-code task section (Tasks 1,6,7,8,9,10,12,13) of `08-evidence.md`. |
| `make c-check` | recorded green per task; re-run green this closeout (rt_waiter_route.c + cancel comment). |
| `make cppcheck` | recorded green per task; re-run green this closeout. |
| `make runtime-v2-check` | recorded green per task; final closeout run in the Task 14 evidence section. |
| `make check` | recorded green per task. |
| `./check_file_sizes.sh -a` | recorded per task; `rt_async_state.c` 1452→1184 eff LOC across the epic, never grown over its (lowered) ceiling. |
| root/`runtime`/`runtime/native` Sentrux scans + rules | recorded per task; final sequence in the Task 14 evidence section. |
| effective LOC for touched over-limit + new files | recorded per task (Task 1 LOC table; each task's LOC table). |

### Lifecycle focused probes

| Probe clause | Proof (test / commit) |
| --- | --- |
| owner-local task create + ready publication | `TestRuntimeV2LifecycleOwnerLocalCreateAndReadyPublication` + `...StaticCreateReadyPushOwnerShard` (Task 6, `5523094e`). |
| join polling + result observation at `SURGE_SHARDS=1,2,8` | `TestRuntimeV2LifecycleJoinPollResultObservation` (create/join tests sweep shards; Task 7, `d998df20`). |
| join waiter cleanup before/during/after registration | `TestRuntimeV2LifecycleJoinWaiterCleanupRegisterThenVerify` (Task 4/7). |
| handle clone/release + last-reference free | `TestRuntimeV2LifecycleCloneReleaseLastReferenceFree` (Task 4/7). |
| scope enter/register/join/exit incl. failfast cancellation | `TestRuntimeV2LifecycleScopeEnterRegisterJoinExit`, `...ScopeFailfastCancellation`, `...ScopeCancelledPollTeardown` (+ `...AcrossShards` at 1/2/8) (Task 4/9). |
| worker-side await vs external `rt_task_await` compat | `TestRuntimeV2LifecycleWorkerAwaitVsExternalAwait` + `...StaticAwaitCompatCountedSeparately` + `...TraceAwaitCompatCountedSeparately` (Task 10, `aa66a0b7`). |
| shutdown with tasks parked in join/scope/timer/channel/blocking/net | `TestRuntimeV2LifecycleShutdownWithParkedTasks` (5 kinds) + `TestRuntimeV2NetPollerShutdownWakesEveryShard` (net, `runtime-v2-accept-check`). |
| no parked-with-work after lifecycle changes | `TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant`/`...SourceGate` + the shutdown test's per-shard `rt_debug_assert_no_parked_with_work` sweep. |
| no shard-0 fallback for owner-known lifecycle paths | `...StaticCreateReadyPushOwnerShard` (P6), `...StaticJoinPollOwnerLane` (P7), `TestRuntimeV2LifecycleJoinConsumePlacementAdoption` (F2). |
| cross-owner scope completion (added at closeout) | `TestRuntimeV2LifecycleScopeCrossOwnerChildDone` (Task 14, `RV2-DEBT-021`; SHARDS=2,8). |

### Static gates

| Static clause | Proof |
| --- | --- |
| migrated worker lifecycle paths do not call `rt_control_lock` | `...StaticCreateReadyPushOwnerShard` (P6), `...StaticJoinPollOwnerLane` (P7), `...StaticScopeOwnerLane` (P9). |
| task table reads use the approved snapshot/slot protocol | `...StaticTaskTableAtomicSnapshot` (G3). |
| join waiters route through the target owner store | `...StaticJoinWaiterRoutesByTargetOwner` (G2). |
| scope waiters route through the scope owner or named control fallback | `...StaticScopeOwnerLane` (P9) + the cross-owner control fallback in `rt_async_scope.c:340-377`, now covered by `...ScopeCrossOwnerChildDone`. |
| `done_cv` used only by external/main-thread await compat | `...StaticAwaitCompatCountedSeparately` (P10, 5 assertions incl. the `done_waiters`-guarded broadcast). |
| the invariant comment in `rt_async_internal.h` names the current owner lanes | MET by Task 13's rewrite (`af0416fc`) to the three-lane (control / shard / atomic) model naming the cross-owner and external-await control residuals. This clause is met by the doc rewrite verified in review, not a mechanical static gate — recorded honestly. |

### Performance/fairness gates

| Clause | Proof |
| --- | --- |
| fresh Epic 8 baseline rows before implementation | `08-evidence.md` Task 1 (net matrix, starvation probe, channels). |
| Epic 7 closeout comparison rows for 1/8 shards at 1/8/32/1024 conns | Task 1 baseline table (Epic 7 rows in parentheses) + Task 12 post-F2 re-baseline. |
| 8x1024x100 starvation reproducer with live trace | Task 11 (`scripts/stallrepro.py` + trace) + Task 12 acceptance (90s run). |
| `SURGE_TRACE_EXEC=1` rows (control_lock_acquired, cross_shard_wakes, spurious_wakes_absorbed, collect_wake_batches, parked_with_work, owner_replacements) | Task 1 (8x1024 counters) + Task 12 counter table. |
| explicit control-lock acq/request before and after | Task 1: 26.4/req → Task 12: 12.86/req total, 9.36/req steady-state. |

## Performance Contract — clause-by-clause verification

Epic 8 is complete only if `RV2-DEBT-016` has a measured resolution:

| Clause | Verdict | Proof |
| --- | --- | --- |
| create / join poll / normal done / same-owner scope avoid the control lane in steady state | MET | Tasks 6-9 static gates + per-site counters; `TestRuntimeV2PerfControlLaneGate` (lifecycle-control ~6.0/req, ceiling 9.0). |
| 8x1024 ≥ fresh 1-shard row, or a named smaller point with evidence + owner | MET | Task 12: 8x1024 total ~1.48M us ~4% FASTER than 1-shard ~1.54M us across x5; two named residuals (`ctrl_await_compat`, cross-owner `ctrl_scope`) reassigned to `RV2-DEBT-022` and net-handle/placement work. |
| `control_lock_acquired`/req drops materially from ~26 | MET | 26.4 → 12.86/req total, 9.36/req steady-state (`TestRuntimeV2PerfControlLaneGate` guards it per commit). |
| `RV2-DEBT-015` fixed or constrained | FIXED | Task 11/12: 90s sustained 8x1024 run, 0 tails ≥5s/≥10s (was 8.4% of requests ≥1s); `cpu_validate.sh` per-worker CPU balanced. |

All four Performance Contract clauses MET. No unmet clause.

## Epic Acceptance checklist (epic doc "Epic Acceptance")

All satisfied: spike recorded and followed (Task 3); `SURGE_SHARDS=1` preserves
all stable gates + `make check` (per-task `make check`, N=1 runner untouched);
worker create/join/done/same-owner-scope avoid `rt_control_lock` in steady state
(static gates + counters); lifetime rules written (spike rules 1-6) with focused
race tests (`...CompletionPinInterleavingTSan` TSan-clean at 1/2/8,
`...CancelSpawnChildrenRace`); scope failfast/cancel/join-all/shutdown liveness
pass multi-shard with explicit timeouts; external/main-thread await works and is
counted separately (`ctrl_await_compat`); `RV2-DEBT-015` fixed with evidence;
Performance Contract satisfied; the lifecycle gate (`runtime-v2-lifecycle-check`)
and per-commit perf gate (`runtime-v2-perf-check`) are wired into
`runtime-v2-check`; all required commands pass; Sentrux recorded with the −41
code-scope drop accepted under Global Rule 3 (`RV2-DEBT-003` recovery owner);
`rt_async_state.c` did not grow (1452→1184 eff LOC); `DEBT.md`, `NOTES.md`, the
epic doc, `README.md`, and `docs/RUNTIME_V2.md` updated with the final state.

## Debt reconciliation (final states)

| ID | Final state at closeout |
| --- | --- |
| `RV2-DEBT-015` | CLOSED (Task 11 investigation + Task 7 F2 fix, `d998df20`). |
| `RV2-DEBT-016` | CLOSED (Task 12 re-baseline, `8c89f358`; three-step Task 8→9→10 attribution chain preserved verbatim in `DEBT.md`). |
| `RV2-DEBT-019` | CLOSED (Task 8 full park_key race family + Task 7 result reorder). |
| `RV2-DEBT-020` | CARRY. The stale `rt_waiter_route.c` "caller holds the control lock, so no same-key registration can interleave" comment was FALSE post-Task-7 and is now corrected in place with the re-derivation: F2 adoption is provably benign (DONE-target + register-then-verify self-consume), the accept-transition self-replace of a still-RUNNING `rt_current_task()` is the unproven residual. Owner reassigned from "Epic 8 closeout" to the future net-handle/accept epic. (Subsumes reviewer note T8-N2.) |
| `RV2-DEBT-021` | CLOSED. `TestRuntimeV2LifecycleScopeCrossOwnerChildDone` implemented (real F2 machinery: a scope-child adopts a shard-1 connection-placed grandchild and completes cross-owner, driving `scope_on_child_done`'s counted control fallback + cross-shard wake), wired into `runtime-v2-lifecycle-check`, SHARDS=2,8. |
| `RV2-DEBT-022` | CARRY (external-await `done_cv` StoreLoad window). Owner: a focused external-await/compat fix or the next runtime epic. |
| `RV2-DEBT-023` | NEW, CARRY. Narrow latent lost-cancellation window in `cancel_task` found during this closeout's mid-park re-derivation (see Reviewer Notes below). Owner: a focused cancellation/wakeup-ordering fix or the next scheduler/wakeup epic. |
| `RV2-DEBT-003` | OPEN (recovery owner for the Task 13 −41 code-scope Sentrux drop; remaining ready-queue / completion-cancel / handle-lifetime split candidates must REDUCE the visible wake/park coupling, re-checked at that time). |
| `RV2-DEBT-017` | OPEN (sync-compat B2 latency; retire with the sync-channel compat lane). |
| `RV2-DEBT-006` | OPEN (channel bench per-probe timeout ownership; the promoted `stallrepro.py` harness is the reference shape). |
| `RV2-DEBT-002`, `-018` | OPEN, Epic 12 test/harness. `-002` root-caused+fixed by Task 1; residual is the shared MT budget. `-018` transient policy honored (focused count≥5 before any rerun-to-green). |
| `RV2-DEBT-001`, `-005`, `-007`, `-010`, `-011`, `-012`, `-013` | OPEN, pre-existing owners unchanged (not touched by Epic 8's steady path). |

## Reviewer-note dispositions (all Task 8/9 notes parked to closeout)

- **T8-N1** (park_key reader audit overclaim): FIXED. `08-tasks/08-completion-epilogue-and-done-path.md`'s "Post-change park_key reader audit" no longer claims "no cross-thread unlocked reader of a task's park_key remains" — it now names `channel_deliver_foreign`'s pre-lock `channel_candidate_valid` peek (`rt_channel_lane.h:87-90`) as the surviving, documented-benign candidate/validate cross-lock read (a mismatch drops the candidate), distinct from `channel_deliver_same_shard_locked`'s owner-shard-locked read.
- **T8-N2** (inline DEBT-020 pointer at `rt_waiter_route.c:89-90`): SUBSUMED by the `RV2-DEBT-020` comment correction — the migrate comment now states the accurate post-Task-7 lock model, the F2 benign proof, and the accept-case carried residual with the `RV2-DEBT-020` pointer inline.
- **T8-supplemental** (cancel landing exactly mid-park unwind): ANALYZED → NEW DEBT. `cancel_task` (`rt_async_state.c:1145`) stores the cancelled flag unconditionally but wakes only on a `TASK_WAITING` read. READY targets are safe (already queued); RUNNING targets that YIELD/complete observe the flag on their next poll. The ONE unresolved case: a RUNNING target whose poll already passed its `current_task_cancelled` check and returned `POLL_PARKED` then commits to `TASK_WAITING` in `park_current` (`rt_task_park.c:270`), which re-checks only the wake TOKEN (`:271`), not the cancelled flag — a cancel reading RUNNING in that window skips the wake and the target strands with `cancelled=1` until its park_key fires by other means. For a never-firing key the cancellation is lost. This is a real (narrow) latent gap → `RV2-DEBT-023` with a candidate fix (unconditional wake token in `cancel_task`, so `park_current`'s token re-check aborts the racing park). The in-code comment at the cancel site states this truthfully rather than claiming safety.
- **T9-N1** (cross-owner test): IMPLEMENTED as `RV2-DEBT-021` above.
- **Task 12 optional hardening** (`drivePerfGateClients` errgroup fail-fast, `internal/vm/runtime_v2_perf_gate_test.go:367`): SKIPPED with reason. `golang.org/x/sync/errgroup` is not a current module dependency; adding one for a test-only cosmetic fail-fast is not worth it, and the existing `sync.WaitGroup` + buffered `errCh` already returns the first error deterministically. No gate-stability benefit.

## Next-epic handoff

### MANDATORY syntax / Phase-4 warning

Epic 8 did NO syntax, parser, semantic-analysis, lowering, or Phase-4 work. The
crossing surface — `far`, `submit_to`, `crosses`, shard-movable checking,
inbound queues, remote messages, eventfd credits, remote `select`, and the
seq-cst `PARKED` protocol — remains entirely undesigned and unimplemented. Per
the epic doc's "Next Runtime Handoff And Syntax Gate" and Global working rules,
the next epic that touches any crossing surface MUST begin with a dedicated
language-syntax review with the user. No task may introduce or bless the names
`far`, `submit_to`, `crosses`, or `shard-movable` without that review.

### Carried debts inherited by the next runtime epic

- `RV2-DEBT-003`: `rt_async_state.c` split candidates (ready-queue,
  completion/cancel, handle-lifetime) must REDUCE the visible wake/park coupling
  (Task 13 recovery owner), Sentrux coupling re-checked when they land.
- `RV2-DEBT-020`: the join-waiter-migration accept-transition micro-window
  (net-handle/accept epic; needs the acceptor's join-structure analysis).
- `RV2-DEBT-022`: the external-await `done_cv` StoreLoad window (focused
  external-await/compat fix).
- `RV2-DEBT-023`: the `cancel_task` RUNNING→WAITING lost-cancellation window
  (focused cancellation/wakeup-ordering fix).
- `RV2-DEBT-017`: sync-channel compat B2 latency (retire with the compat lane).

### Named residuals reassigned to existing owners (NOT Epic 8 lifecycle debt)

- external-await `ctrl_await_compat` (28674, ~3.5/req on 8x1024): a
  harness-structural artifact (every multi-worker Surge program parks a root
  external awaiter for its lifetime); ordering owned by `RV2-DEBT-022`.
- cross-owner `ctrl_scope` (`scope_on_child_done` fallback, 19464, ~2.38/req):
  future net-handle/placement work (re-pin a scope to the adopting shard, or
  fold `active_children` under the adoption's control barrier). Now has the
  deterministic regression test (`RV2-DEBT-021` closed).
- net-handle/accept removal residual: `ctrl_handle_free` 3.63/req (net-wrapper
  child last-ref free) is net-handle ABI work (`RV2-DEBT-010`/`-013`).

### Promoted harness + runbook

- `scripts/stallrepro.py`, `scripts/run_stallrepro.sh`, `scripts/cpu_validate.sh`
  own their per-probe timeouts (reference shape for `RV2-DEBT-006`).
- The nightly acceptance runbook lives in `08-tasks/12-performance-benchmark-and-ci-gate.md`.
- Per-commit gate: `TestRuntimeV2PerfControlLaneGate` via
  `runtime-v2-perf-check` → `runtime-v2-check` guards the control-lane and F2
  fairness contract against regression.

## Sentrux quality-vs-epic-start comparison

Epic start (Task 1, `daeac51e`): root 6174, `runtime` 5296, `runtime/native`
5389, all rules pass. Closeout final sequence is recorded in the Task 14 section
of `08-evidence.md`. The only intra-epic code-scope quality change is Task 13's
−41 `runtime/native` (5382→5341) / −40 `runtime` park/wake extraction drop,
accepted under Global Rule 3 with `RV2-DEBT-003` as the recorded recovery owner
(the tool's own `sentrux gate` shows the Quality dimension +182 ABOVE the
committed Jul-2 baseline; the drop is previously-invisible intra-file coupling
becoming visible inter-module coupling, not a real regression).
