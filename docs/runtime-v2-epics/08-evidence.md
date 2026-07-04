# Epic 8 Evidence Ledger

Per-task evidence for Epic 8 (task lifecycle lane and net fairness), per
`08-task-lifecycle-lane-and-net-fairness.md` and `EVIDENCE_TEMPLATE.md`.
Benchmark reports live under `build/benchmarks/` (git-ignored); the rows the
contracts depend on are copied here.

## Task 1: Kickoff Baseline And Sentrux

### Task Identity And Scope

- Task: Epic 8 Task 1 per `08-tasks/01-kickoff-baseline-and-sentrux.md`.
- Epic: 8. Date: 2026-07-04. Author/session: main session.
- Scope: starting facts (checkout, LOC, gates, Sentrux, lifecycle
  control-lock census, fresh benchmark baselines, starvation observation)
  plus one gate blocker found and fixed during baselining.
- Out of scope: dependency map (Task 2), lane model (Task 3), any lifecycle
  migration.
- Proving spike: no.

### Checkout And Environment

- Epic opened at `072bbde0` (docs: epic 8 open). Baselines recorded on the
  tree after the Task 1 gate-blocker fix (see below) — the tree Epic 8
  implementation actually starts from.
- Host: WSL2, 32 hardware threads (`nproc`), same machine as the Epic 7
  closeout rows.
- Untracked, not owned by this epic: `.claude-flow/`, `.claude/`,
  `.mcp.json`, `.swarm/`, `CLAUDE.md`, `ruvector.db` (ruflo tool droppings;
  never committed).

### Task 1 Gate Blocker: ParkUnpark Lost-Wake (found, fixed, committed)

The baseline `runtime-v2-check` failed on `TestMTChannelParkUnpark` (62s
binary timeout), and the focused rerun policy (count>=5) FAILED too (1/5) —
so the documented `RV2-DEBT-002` rerun-to-green route was not honest to
take. Standalone looping reproduced a hard deadlock in ~1/10 runs at
`SURGE_THREADS=2`.

Diagnosis (full event trace, unbuffered `write(2)` log; the
`TRACE_TASK_WAITING` snapshot lines added in this task pinned the state):

- Frozen shape: a ping/pong pair parked on the SAME channel — sender
  committed with `park_seq=N`, receiver parked, and the sender's fresh
  store entry GONE with no pop, no delivery, no wake (`tok=0 resume=0`).
- Killer frame (event log): `REG(sender, seq=N)` immediately followed by
  `RMSTORE removed=1` — a deferred `remove_waiter((key,task))` call that
  had captured its key EARLIER, blocked on the shard lock across several
  rendezvous rounds, then compacted away the FRESH registration.
- Two guilty paths, both running their removal after the owner lock is
  released (a second shard lock is not lane-legal): the deferred stale-key
  removal in `wake_task_with_policy(remove_waiter_flag=1)` (reached from
  `rt_task_wake`/`rt_task_poll` lifecycle wakes — exactly the Epic 8
  steady-path surface), and `park_current`'s post-commit token-abort
  removal.

Fix (`fix(runtime): qualify deferred waiter removals by park generation`):
`remove_waiter_generation` removes only entries whose `seq` matches the
generation captured under the owner lock; channel re-registrations always
carry a newer generation and survive. The dedupe branch of
`channel_park_prepare_locked` re-arms a matched leftover entry with a fresh
generation (previously it could leave a stale-generation entry as the
current registration, which delivery validation then pop-and-dropped —
the second stranding flavor observed). The sync-channel compat lane moved
to `rt_async_compat.c` (139 effective LOC) so `rt_async_state.c` stays
under its ceiling (1452 <= 1580).

Verification: repro loop 120/120 hang-free (pre-fix: hang within 1-10
iterations); focused `TestMTChannelParkUnpark` 10/10; channel/blocking/
lock-split families green; `timeout 1200s make runtime-v2-check` green
TWICE back-to-back with zero failures and no rerun — the first
consecutive-green pair recorded on this host. Note for `RV2-DEBT-002`:
the "load flake" was this bug; the debt entry is updated at closeout.

Triage tooling kept (permanent, `SURGE_TRACE_EXEC=1` only): per-task
`TRACE_TASK_WAITING`/`TRACE_TASK_READY` snapshot lines (park key,
generation, queue flag, wake token, pending resume, owner) and per-shard
`TRACE_STORE` length/capacity. `rt_lane_holds_shard` exported for future
lane assertions.

### Effective LOC (gate `./check_file_sizes.sh -a`, all green)

| File | Effective LOC | Note |
| --- | ---: | --- |
| `rt_async_state.c` | 1452 | allowlist ceiling 1580 (`RV2-DEBT-003`); must not grow this epic without extraction |
| `rt_async_compat.c` | 139 | new (compat lane extraction) |
| `rt_async_task.c` | 282 | lifecycle target file |
| `rt_async_scope.c` | 162 | lifecycle target file |
| `rt_scheduler_placement.c` | 117 | owner assignment / accept re-place |
| `rt_async_internal.h` | 515 | |
| `rt_worker_turn.c` | 239 | |
| `rt_async_waiter.c` | ~590 | grew with `remove_waiter_generation` |
| `rt_lane.c` | 94 | |

### Gates (this tree)

| Command | Result |
| --- | --- |
| `make c-check` | pass |
| `make cppcheck` | pass (52/52) |
| `timeout 1200s make runtime-v2-check` | pass x2 consecutive, zero failures (post-fix) |
| `make check` | pass (pre-commit hook of the fix commit) |
| `./check_file_sizes.sh -a` | 100% good files |
| `git diff --check` | clean |

### Sentrux

`sentrux check` — repo `.`: quality 6174; `runtime`: 5296;
`runtime/native`: 5389; all rules pass on all three scopes. Identical to
the Epic 7 closeout signals (no drift at epic start). MCP tools not
exposed in this session; the CLI is the accepted evidence mechanism for
this epic (same as Epic 7).

### Lifecycle Control-Lock Census (51 sites, by file and class)

Steady-path lifecycle (the epic's migration target, 16 sites):

| Site | Function | Class |
| --- | --- | --- |
| `rt_async_task.c:15` | `__task_create` | create/publish (every spawn) |
| `rt_async_task.c:62` | `rt_task_wake` | handle wake |
| `rt_async_task.c:88` | `rt_task_poll` | worker join poll (every await poll) |
| `rt_async_task.c:173` | `poll_ready_child_inline` | join inline child poll |
| `rt_async_task.c:229` | `rt_task_cancel` | cancel |
| `rt_async_task.c:243` | `rt_task_clone` | handle ref |
| `rt_async_task.c:289` | `checkpoint` | checkpoint spawn |
| `rt_async_task.c:300` | `rt_sleep` | sleep-task spawn |
| `rt_async_state.c` (`task_release_lane_aware`) | handle release/final free |
| `rt_async_state.c` (`mark_done`) | completion epilogue when `mark_done_needs_control` |
| `rt_async_state.c` (`apply_poll_outcome`) | cancelled-branch control |
| `rt_async_scope.c:10/45/84/100/134` | scope enter/register/cancel-all/join-all/exit | scope bookkeeping |

Named compatibility (stays, counted separately): `rt_task_await` x2
(external await + single-worker), `run_until_done` x2 + `run_ready_one` x2
+ `poll_task` (N=1/legacy runner bracket), `rt_wait_current_worker_wakeup`
(compat lane, now in `rt_async_compat.c`), `rt_channel_send/recv_blocking`
(non-task blocking wrappers), `rt_channel_recv` foreign-deliver arm,
select x3 (named non-goal), `wake_task_with_policy` x2
(compensation/compat_cv fallbacks), blocking pool x2.

Net/accept contract sites (Epic 6/7 design, not this epic's target):
`rt_net_accept_group.c` x5, `rt_net_lifecycle.c:40`, `rt_fd_registry.c`,
`rt_async_waiter.c` (net accept completion), `rt_net_poll_pass.c`.

Infrastructure: shutdown x2, trace dumps x2, `rt_io_wait_slice`/`rt_io_main`
x3, `rt_run_ready_one_nowait_locked` (io drain).

### Benchmark Baselines (post-fix tree; reports under `build/benchmarks/`)

See `rv2-e8-task1-baseline-epic6-matrix.md` (net matrix, shards 1/8 x
conns 1/8/32/1024, direct/seq, 8 req/conn, trace on),
`rv2-e8-task1-starvation-probe.md` (8 shards x 1024 conns x 100 req —
the `RV2-DEBT-015` observation), and
`rv2-e8-task1-baseline-channels.md`. Key rows and the per-request
control-lock number are appended below when the runs complete.

### Gate Plan

- Required and green: `runtime-v2-check` (liveness, heap, waiter,
  fd-registry, accept, lock-split stages).
- This epic adds: lifecycle behavior probes (Task 4), lifecycle static
  gates + counters (Task 5), promoted via Task 12.
- Debt classes that may appear in reruns: `RV2-DEBT-018` (harness
  transient; rerun-to-green only with focused count>=5 proof).
  `RV2-DEBT-002`'s ParkUnpark flake is expected NOT to reappear after the
  Task 1 fix; any recurrence is a new finding, not a rerun candidate.

### Benchmark Baseline Rows (post-fix tree, recorded)

Net matrix `rv2-e8-task1-baseline-epic6-matrix.md` (total us; Epic 7
closeout row in parentheses for continuity):

| shards | conns | total us | note |
| ---: | ---: | ---: | --- |
| 1 | 1 | 23897 | p50 168us; total skewed by one slow connect — small-row noise |
| 1 | 8 | 16354 | (14896) |
| 1 | 32 | 54671 | (51968) |
| 1 | 1024 | 1548717 | (1530555) |
| 8 | 1 | 3550 | (3199) |
| 8 | 8 | 16239 | (16354) |
| 8 | 32 | 61873 | (61081) |
| 8 | 1024 | 2010821 | (1939032); 1.30x the 1-shard row |

8-shard/1024 trace counters: `control_lock_acquired=216463` for 8192
requests = **26.4 control acquisitions per request** — the epic's numeric
reduction target. `cross_shard_wakes=1904`, `spurious_wakes_absorbed=7`,
`collect_wake_batches=5337`, `owner_replacements=1029`, steal counters
zero.

`RV2-DEBT-015` observation (`rv2-e8-task1-starvation-probe.md`): the
8-shard/1024-conn/100-req probe COMPLETED CLEANLY in one run — 19.0s
total, avg 22818us/op driven by connection count, **p50 168us / p95
307us, no >10s tails**. This is a single-run observation on a lightly
loaded host: it does not close the debt (the stall was intermittent and
load-coupled), but it means the Task 11 investigation starts from "not
currently reproducing" and must first re-establish a reproducer —
possibly the Epic 7 fairness ticks plus this task's lost-wake fix
already changed the landscape. Recorded as the baseline state.

Channels baseline `rv2-e8-task1-baseline-channels.md` (ns/op) — the
`RV2-DEBT-017` reference points for Task 10:

| mode | probe | ns/op |
| --- | --- | ---: |
| 1 | ping_pong | 4442 |
| 1 | reused_reply | 3568 |
| 1 | new_reply | 3944 |
| 1 | sync_new_reply | 4255 |
| 2 | ping_pong | 15498 |
| 2 | reused_reply | 12115 |
| 2 | new_reply | 13406 |
| 2 | sync_new_reply | 45277 |
| default(32) | ping_pong | 16127 |
| default(32) | reused_reply | 12487 |
| default(32) | new_reply | 14144 |
| default(32) | sync_new_reply | 456739 |

## Task 2: Lifecycle Dependency Map

### Task Identity And Scope

- Task: Epic 8 Task 2 per `08-tasks/02-lifecycle-dependency-map.md`.
- Epic: 8. Date: 2026-07-04. Author/session: `mapper` architect subagent
  (plan approved by `main` before any edit, per RULES.md Global Rule 9).
- Scope: produce `08-lifecycle-dependency-map.md` (the lifecycle analogue of
  the Epic 7 executor-lock map) and its self-contained task document; update
  the task index, this ledger, and `NOTES.md`.
- Out of scope: any C/test/benchmark/CI change; lane-model **decisions**
  (Task 3); select slow lane migration (named non-goal); Phase 4 surfaces.
- Proving spike: no.

### Baseline Commit/Status

- Baseline commit: `daeac51e` (Task 1 kickoff-baseline record).
- Branch/worktree: `codex/runtime-net-scheduler-refactor`.
- Status before: Task 1 complete; no `02-*` task doc or lifecycle map existed.
- Status after: map and task doc created; index row flipped to Complete.
- Dirty or untracked not touched: `.claude-flow/`, `.claude/`, `CLAUDE.md`,
  `.mcp.json`, `.swarm/`, `ruvector.db` (tool droppings, never committed).
- Local environment blockers: none.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/08-lifecycle-dependency-map.md` | created | the dependency map | docs; no code line limit |
| `docs/runtime-v2-epics/08-tasks/02-lifecycle-dependency-map.md` | created | self-contained task doc | docs |
| `docs/runtime-v2-epics/08-tasks/README.md` | edited | Task 2 status → Complete | docs |
| `docs/runtime-v2-epics/08-evidence.md` | edited | this Task 2 section | docs |
| `docs/runtime-v2-epics/NOTES.md` | edited | Task 2 working notes | docs |

### Contracts Touched

| Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
| --- | --- | --- | --- |
| any runtime behavior | runtime C | N/A — docs-only, no code changed | no C/test files in the diff |
| line numbers vs Task 1 census | `08-evidence.md` census | preserved (re-verified) | 16 steady-path sites match at baseline `daeac51e` |

### Sentrux Root/Scoped Signals

- N/A for this docs-only task; no scanned path changed. Epic-level signals
  recorded in the Task 1 section (repo 6174, `runtime` 5296,
  `runtime/native` 5389, all rules pass) remain current.

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
| --- | --- | --- | --- | --- |
| `git diff --check` | no output | no output | `0` | tracked-file whitespace gate |
| build | N/A | N/A | N/A | docs-only; nothing compiled |

### Benchmarks And Generated Reports

- N/A; no runtime path changed.

### Trace Counters/Liveness Proof

- N/A; no runtime path changed.

### Known Regressions

- None; documentation only.

### Dead Ends / Paths Not To Retry

- None.

### Rollback/Recovery Notes

- Revert the five docs files listed above; no runtime or generated artifacts.

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Answer the 16 Task 3 open questions (S5-Q1..Q14, S6-Q1, S7-Q1, S9-Q7) | No (this task) / Yes (Task 3) | `08-tasks/03-lifecycle-lane-proving-spike.md` | map records current+target+open-questions; spike decides and rewrites the lane table on conflict |
| Reconcile the stale invariant comment `rt_async_internal.h:292-304` | No | Task 13/14 closeout | still describes pre-Epic-7 executor-wide ownership |

## Task 3: Lifecycle Lane Proving Spike

### Task Identity And Scope

- Task: Epic 8 Task 3 per `08-tasks/03-lifecycle-lane-proving-spike.md`.
- Epic: 8. Date: 2026-07-04. Author/session: `spiker` subagent (plan approved
  by `main` before any edit or long-running command, per RULES.md Global Rule 9
  and Rule 1).
- Scope: decide the shard-owned lifecycle model — answer all 16 `(spike)`
  questions with evidence; produce the spike record and the six written rules;
  reconcile the map's lane table; update the index, this ledger, and `NOTES.md`.
- Out of scope: any committed C/test/benchmark/CI change (the throwaway TSan
  model stays scratchpad-only); implementation (Tasks 4-10); select migration
  (non-goal); Phase 4 surfaces.
- Proving spike: **yes** (RULES.md Global Rule 1). Temporary `MUST` deviation:
  none in the tree — the model lives outside the repository, so no runtime
  `MUST` was violated in the committed state.

### Baseline Commit/Status

- Baseline commit: `daeac51e` (Task 1 kickoff-baseline). All 16 census sites
  re-verified in place.
- Branch/worktree: `codex/runtime-net-scheduler-refactor`.
- Status before: Task 2 complete; the 16 `(spike)` cells were open.
- Status after: all 16 decided; map lane table reconciled; index row Complete.
- Dirty/untracked not touched: `.claude-flow/`, `.claude/`, `CLAUDE.md`,
  `.mcp.json`, `.swarm/`, `ruvector.db`; scratchpad probe not staged.
- Local environment blockers: none.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/08-lifecycle-lane-proving-spike.md` | created | the spike record + six written rules | docs; no code line limit |
| `docs/runtime-v2-epics/08-tasks/03-lifecycle-lane-proving-spike.md` | created | self-contained task doc | docs |
| `docs/runtime-v2-epics/08-lifecycle-dependency-map.md` | edited | reconcile lane table to the decided lanes (index rule) | docs |
| `docs/runtime-v2-epics/08-tasks/README.md` | edited | Task 3 status → Complete | docs |
| `docs/runtime-v2-epics/08-evidence.md` | edited | this Task 3 section | docs |
| `docs/runtime-v2-epics/NOTES.md` | edited | Task 3 working notes | docs |

Not committed: `lifecycle_publish_refcount_spike.c` (scratchpad throwaway
proof model).

### Contracts Touched

| Contract or behavior | Source | Preserved, changed, or N/A | Evidence |
| --- | --- | --- | --- |
| any runtime behavior | runtime C | N/A — no committed C change; tree C state pristine | `git status` shows no `runtime/` file staged/modified |
| register-then-verify join race | `rt_async_task.c:129-145` | preserved (argument extends it) | `TestRuntimeV2CancelledJoinWaiterDoesNotConsumeTaskCompletionWake` PASS |
| completion pin covers `mark_done` body | `rt_async_state.c:1515,1574` | preserved; recorded as rule 1 | TSan model asserts the interleaving; `-DUNSAFE_NOPIN` aborts |
| result written before `TASK_DONE` release store | `rt_async_state.c:1540-1543` | **change required in Task 8** (currently written after) | rule 1 records the reorder; model validates the correct order |

### Sentrux Root/Scoped Signals

- N/A for this docs-only commit; no scanned path changed. Epic-level signals in
  the Task 1 section (repo 6174, `runtime` 5296, `runtime/native` 5389, all
  rules pass) remain current.

### Commands/Checks

| Command or tool | Expected result | Actual result | Exit/status | Evidence path or note |
| --- | --- | --- | --- | --- |
| `git diff --check` | no output | no output | `0` | tracked-file whitespace gate |
| `git status` C-pristine | no `runtime/` change | clean | n/a | docs-only commit; model in scratchpad |
| `clang -O1 -g -fsanitize=thread lifecycle_publish_refcount_spike.c` then run x2 | PASS, zero TSan reports | `published=160000 lost_publishes=0 uaf_detected=0` PASS x2 | `0` | Ubuntu clang 18.1.3 |
| `clang -O2 -DNDEBUG ...` then run x2 | PASS | PASS x2 | `0` | optimized build |
| `clang ... -DUNSAFE_PUBLISH ...` (negative control) | FAIL (lost publish) | `lost_publishes=1` | `1` | shard-lane publish vs growth loses a slot |
| `clang ... -DUNSAFE_NOPIN ...` (negative control) | FAIL (UAF) | poisoned-payload assert abort | `134` | pin removed → joiner frees mid-body |
| `SURGE_BACKEND=llvm go test -tags runtime_v2_pending ./internal/vm -run '^TestRuntimeV2(CancelledJoinWaiter...|FailfastScope...|BlockingCompletion...|CancelledBlockingWaiter...)$'` | PASS | 4/4 PASS (11.6s) | `0` | S5-Q3/Q8 corroboration |
| owner-shard-id audit (`grep rt_task_set_placement / rt_task_replace_owner`) | single post-spawn writer | only `rt_task_replace_owner` (accept) | n/a | S7-Q1 |

### Benchmarks And Generated Reports

- N/A; no runtime path changed. The spike defines the Task 5 create-site
  `control_lock_acquired` counter and the S5-Q1 escalation threshold
  (>= 2.0 acq/request on the 8x1024 row) as the future measurement.

### Trace Counters/Liveness Proof

- Liveness proof is the throwaway TSan model (no lost publishes/UAF; completion
  pin holds the struct across the joiner-frees-mid-body interleaving) plus the
  existing waiter-contract tests at baseline. No committed runtime change.

### Verdict Summary (16 questions)

- S5-Q1 YES (ready-push owner shard; publish stays control-serialized with
  growth, realization A; segmented-table B on the >=2.0 acq/request trigger).
- S5-Q2 YES; S5-Q3 YES; S5-Q4 YES; S5-Q5 control tree walk + owner wake;
  S5-Q6 YES; S5-Q7 adopt atomic-snapshot for `ex->scopes`; S5-Q8 YES; S5-Q9 YES;
  S5-Q10 move `scope_key` to scope owner store (revises D8); S5-Q11 YES;
  S5-Q12 YES; S5-Q14 YES; S6-Q1 YES (net-key + `done_waiters` survive);
  S7-Q1 YES; S9-Q7 `seq == 0` unqualified.

### Known Regressions

- None; documentation only, tree C state pristine.

### Dead Ends / Paths Not To Retry

- Shard-lane slot publication concurrent with control-lane copy-on-grow table
  growth: unsafe (no lane-order-legal happens-before; `-DUNSAFE_PUBLISH`
  demonstrates the lost slot). Do not retry without the segmented-table (B)
  change.

### Rollback/Recovery Notes

- Revert the six docs files; no runtime or generated artifacts. The scratchpad
  model is disposable.

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Reorder `mark_done` result writes before the `TASK_DONE` release store | No (this task) / Yes (Task 8) | `08-tasks/08-completion-epilogue-and-done-path.md` | rule 1: lock-free join read needs result visible before DONE |
| Add create-site `control_lock_acquired` counter + evaluate S5-Q1 escalation | No (this task) / Yes (Tasks 5/6) | `05-...`, `06-...` | decides realization A vs segmented table B |
| Reconcile the stale invariant comment `rt_async_internal.h:292-304` | No | Task 13/14 closeout | still describes pre-Epic-7 executor-wide ownership |

## Task 4: Lifecycle Behavior Contract Tests

### Task Identity And Scope

- Task: Epic 8 Task 4 per `08-tasks/04-lifecycle-behavior-contract-tests.md`.
- Epic: 8. Date: 2026-07-04. Author/session: subagent `tester-behavior`.
- Scope: five new Go test files (`internal/vm/runtime_v2_lifecycle_behavior_*_test.go`,
  build tag `runtime_v2_pending`), a native C harness compiled at test time
  (no repository C/H file touched), covering the epic's focused-probe list
  for create/join/handle-lifetime/scope/await/shutdown.
- Out of scope: static-shape/trace-gate tests (Task 5, landed); any lifecycle
  migration (Tasks 6-10); net-fairness investigation (Task 11).
- Proving spike: no.

### Files Added

- `internal/vm/runtime_v2_lifecycle_behavior_harness_test.go` (478 lines):
  build helpers (plain + TSan variants), env/run helpers,
  `lifecycleHarnessCommon` (includes, shared globals, spawn/wait helpers,
  owner-probe/join/clone/pin poll functions).
- `internal/vm/runtime_v2_lifecycle_behavior_create_join_test.go` (213
  lines): 3 tests + `lifecycleHarnessCreateJoinModes`.
- `internal/vm/runtime_v2_lifecycle_behavior_handle_lifetime_test.go` (185
  lines): 2 tests (one TSan-gated, `t.Skip`'d pending Task 8) +
  `lifecycleHarnessHandleLifetimeModes`.
- `internal/vm/runtime_v2_lifecycle_behavior_scope_test.go` (353 lines): 3
  tests + `lifecycleHarnessScopeAndShutdown`.
- `internal/vm/runtime_v2_lifecycle_behavior_await_shutdown_test.go` (374
  lines): 2 tests + `lifecycleHarnessMain` (dispatcher, remaining mode
  drivers, `main()`).
- All five at or under the 500-line cap (Global Rule 4); the harness source
  is deliberately split across all five files' Go constants purely for that
  cap and concatenated at build time (see the task document for the exact
  build-function assembly).

### Scenario -> Probe-List Mapping

See `08-tasks/04-lifecycle-behavior-contract-tests.md` "Scenario ->
Probe-List Mapping" for the full table. Summary: 10 new
`TestRuntimeV2Lifecycle*` tests, one native harness, covering owner-local
create/ready-push, same/cross-shard join result observation, three join
register-then-verify timing cases, clone/release stress, a TSan completion-
pin stress (`t.Skip`'d pending Task 8, see below), scope enter/register/
join/exit, scope failfast cancellation, cancelled-poll scope teardown,
worker-vs-external await, and shutdown with 5 concurrently parked wait
kinds (join/scope/timer/channel/blocking).

One probe-list item is satisfied by **selecting** an existing test in
addition to adding a new one, per the epic rules' "add or select" language:
net-parked shutdown (`TestRuntimeV2NetPollerShutdownWakesEveryShard`,
`runtime-v2-accept-check`) is not duplicated with a synthetic socket in this
harness. Scope failfast cancellation was initially scoped this way too
(selecting the existing `TestRuntimeV2FailfastScopeCancellationWakesOwner`)
but the main session asked for a dedicated raw-C-level probe as well, since
failfast is an explicitly required item in the epic's contract;
`TestRuntimeV2LifecycleScopeFailfastCancellation` was added to satisfy that
directly, driving `rt_scope_register_child`'s late-registration failfast
branch (`rt_async_scope.c:67-75`) with an explicit registration order the
raw C harness controls, complementing rather than duplicating the existing
Surge-source-level test. The "no parked-with-work" probe is satisfied by the
existing `TestRuntimeV2SchedulerPlacementParkedWithWorkInvariant` /
`...SourceGate` pair; see the task document for why re-deriving it via an
external call to `rt_debug_assert_no_parked_with_work` against actively
busy-yielding tasks does not work (the helper only checks queue emptiness,
not worker sleep state, so it fires unconditionally against any
continuously-re-enqueuing task — not a race, just the wrong tool for that
external-check shape).

### Discovered Runtime Behavior (recorded here and in the task document)

Virtual-clock idle fast-forward: `tick_virtual`/`advance_time_to_next_timer`
(`rt_async_state.c:1199-1257`) jumps the virtual clock straight to the next
timer deadline once workers go idle, so a long `rt_sleep` fires almost
immediately (~200ms observed) rather than staying parked — not a stable
"parked forever" primitive in this runtime. Not previously written down
anywhere this task's research found; relevant to future timer/shutdown
liveness work.

### Finding: Two TSan-Confirmed Data Races, Recorded As RV2-DEBT-019 (Resolved For This Task)

`TestRuntimeV2LifecycleCompletionPinInterleavingTSan` found two real,
reproducible races in the current baseline. Full detail and file:line
citations in `08-tasks/04-lifecycle-behavior-contract-tests.md` "Open
Finding" section and `DEBT.md` RV2-DEBT-019. Summary:

1. **Result-visibility ordering** (`mark_done` writes `result_kind`/
   `result_bits` after the `TASK_DONE` release store when
   `mark_done_needs_control` is false, `rt_async_state.c:1486-1545`) is
   Rule 1's already-documented "Required change" for Task 8, now confirmed
   by TSan against the real tree rather than only by written argument.
   **Mitigated** in the test by holding one external awaiter alive for the
   whole stress window (forces `mark_done_needs_control` true universally,
   matching the precondition the "sound today" claim depends on).
2. **`park_key` race** (`wake_task_on_shard_locked`'s write at
   `rt_async_state.c:965` vs `mark_done_needs_control`'s unlocked read at
   `:1494`, both touching the same task's `park_key`) reproduces
   consistently even after mitigating (1). Not found documented anywhere in
   this epic's records prior to this task. Sits in the exact surfaces Tasks
   7 and 8 are about to migrate.

**Resolution (main session decision):** the test is committed with its full
body intact, gated by `t.Skip("pending Task 8: baseline races RV2-DEBT-019
...")`, matching the epic's established pending-gate convention (Task 5's
P6-P10 static gates use the same pattern). The in-test mitigation for race
(1) is now toggleable via `LIFECYCLE_PIN_STRESS_NO_KEEPALIVE=1` (unset by
default), so Task 8 can reproduce either race in isolation or both together
while implementing its fix. RV2-DEBT-019 is recorded in `DEBT.md` with owner
Task 8 (interacting with Task 7's helper-wake call site) and an explicit
close condition (reorder the result writes, make the `park_key` read/write
pair race-free, delete the `t.Skip`, add the test to
`runtime-v2-lifecycle-check`). Because Task 5 already wired
`make runtime-v2-lifecycle-check` (broad `-run '^TestRuntimeV2Lifecycle'`,
`Makefile:132-134`) into `make runtime-v2-check`, the `t.Skip` is what keeps
this from failing that gate today; confirmed the full `^TestRuntimeV2Lifecycle`
regex (this task's 10 tests plus Task 5's static/trace tests) runs green
with exactly one `SKIP` (this test) and no `FAIL`.

### Gates (this tree)

- `gofmt -l` on all five new files: clean.
- `go vet -tags runtime_v2_pending ./internal/vm/...`: clean.
- Standalone C harness compile (`clang -std=c11 -Wall -Wextra -Werror
  -pthread`) against every `runtime/native/*.c` except `rt_entry.c`: clean,
  zero warnings, both the plain and `-fsanitize=thread -g -O1` variants.
- `SURGE_BACKEND=llvm SURGE_SKIP_TIMEOUT_TESTS=0 go test -tags
  runtime_v2_pending ./internal/vm -run '^TestRuntimeV2Lifecycle' -count=1
  -parallel=1 -p=1 -v --timeout 360s`: **all 10 of this task's tests PASS
  except the TSan test which SKIPs as designed**; Task 5's static/trace
  tests in the same regex also PASS/SKIP as expected; zero FAIL. ~28s total.
- Focused `SURGE_SHARDS=1,2,8` matrix for the create/join tests: PASS at all
  three shard counts (both standalone and via `go test`).
- Full gate suite (`git diff --check`, `make c-check`, `make cppcheck`,
  `make runtime-v2-check`, `make check`, `./check_file_sizes.sh -a`):
  recorded below once run under the granted commit barrier.

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Fix RV2-DEBT-019 (both races), delete the `t.Skip`, add to `runtime-v2-lifecycle-check` | No (this task) / Yes (Task 8) | `08-completion-epilogue-and-done-path.md`, `DEBT.md` | Recorded debt with owner and close condition |

## Task 5: Lifecycle Static Shape And Trace Tests

### Task Identity And Scope

Per-site `control_lock_acquired` attribution (additive C counters, no behavior
change), static gate machinery mirroring Epic 7's lock-split static tests, a
trace-contract gate, and the 8x1024 per-site baseline that decides the Task 6
escalation. Full write-up: `08-tasks/05-lifecycle-static-shape-and-trace-tests.md`.

### Files Touched

- C (additive): `rt_async_internal.h` (`rt_ctrl_site` enum + decl),
  `rt_async_trace.c` (per-site array, `rt_trace_control_lock_site`, 6 dump
  fields, buf 1152->1280), `rt_async_task.c` / `rt_async_scope.c` /
  `rt_async_state.c` (census-site tags; `state.c` +3 lines).
- `scripts/bench_native_net.sh` (6 per-site trace columns).
- Tests (NEW, `//go:build runtime_v2_pending`):
  `internal/vm/runtime_v2_lifecycle_static_test.go` (G1-G6 active + P6-P10
  pending), `internal/vm/runtime_v2_lifecycle_trace_test.go`.
- `Makefile` (`runtime-v2-lifecycle-check` stage, `-run` regex enumerating each
  green test by name — the 6 active gates + trace gate + Task 4's behavior
  contracts; wired into `runtime-v2-check`).
- `rt_lane.c`: NOT touched (generic `control_lock_acquired` unchanged).

### Per-Site Baseline (net `direct/seq`, 8 shards / 8 threads / 1024 conns / 8 req/conn = 8192 requests, `SURGE_TRACE_EXEC=1`)

| Site | Total | Per request |
| --- | ---: | ---: |
| `control_lock_acquired` (all) | 215842 | 26.348 |
| `ctrl_create` | 28673 | **3.500** |
| `ctrl_join_poll` | 31822 | 3.885 |
| `ctrl_scope` | 106499 | 13.000 |
| `ctrl_completion` | 4169 | 0.509 |
| `ctrl_await_compat` | 1 | 0.000 |
| `ctrl_handle` | 1 | 0.000 |
| sum(sites) | 171165 | 20.894 |
| residual (`OTHER`) | 44677 | 5.454 |

Total 26.348/request re-verifies the Task 1 baseline of 26.4/request (counters
are non-perturbing).

### Escalation Verdict (S5-Q1)

**Create = 3.500 control acq/request >= 2.0 → ESCALATE. Task 6 adopts
realization (B), the segmented never-moved-slot task table.** The
per-connection amortization hypothesis for (A) is disproven: request trees spawn
multiple tasks per request (3.5 creates/request), so create is a material
per-request control consumer. Secondary: `ctrl_scope = 13.000/request` is the
single largest attributable consumer (Task 9 has the biggest payoff);
`ctrl_join_poll = 3.885` is Task 7's target; `ctrl_completion = 0.509` is Task
8's; handle/await-compat ~0 on the net bench (no public wake/clone/cancel,
worker-side joins).

### Gates (this tree)

- `git diff --check`: clean.
- `make c-check`, `make cppcheck`: OK.
- Active static gates G1-G6 verified against real sources; G1 `clang
  -fsyntax-only` snippet compiles.
- `./check_file_sizes.sh -a`: `rt_async_state.c` 1455 (<=1580 legacy),
  `rt_async_trace.c` 648 (ACCEPTABLE), all others green.
- `make runtime-v2-check` (incl. `runtime-v2-lifecycle-check`) + `make check` +
  Sentrux scans: run under the commit barrier after Task 4 lands (recorded at
  commit time).

### Known Regressions

- None; C changes are additive counters (no behavior change), tests are new.

### Static Gate Inventory

- Active (green, wired): G1 enum/API shape, G2 join route-by-target-owner, G3
  task-table atomic snapshot, G4 join/scope unqualified seq, G5 create counter
  wired, G6 all census sites tagged.
- Pending (`t.Skip` with activation criteria; delete `Skip` in the peel commit):
  P6 create ready-push owner-shard (Task 6), P7 join-poll owner lane (Task 7),
  P8 completion result-visibility order (Task 8), P9 scope owner lane (Task 9),
  P10 await-compat counted separately (Task 10).

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner or next document | Reason |
| --- | --- | --- | --- |
| Escalate Task 6 to segmented table (B) | No (this task) / Yes (Task 6) | `06-task-create-and-table-publication.md` | create = 3.500/request >= 2.0 (measured) |
| Activate P6-P10 static gates on their peel commits | No | Tasks 6-10 | delete the `t.Skip` line naming each task |

## Task 6: Task Create And Table Publication

### Task Identity And Scope

Realization (B) (mandatory per Task 5's escalation verdict, `ctrl_create=
3.500/req >= 2.0`): the segmented never-moved-slot task table, plus the
`__task_create` restructure onto the owner shard lane. Full write-up:
`08-tasks/06-task-create-and-table-publication.md`.

### Files Touched

- C: `rt_async_internal.h` (`rt_task_table`/new `rt_task_segment`, `next_id`
  atomic, new decls), new `rt_task_table.c` (41 lines, segment allocation),
  `rt_async_state.c` (`get_task`/`rt_task_slot_store`/`rt_task_table_snapshot`
  reimplemented for segments, old `ensure_task_cap` moved out,
  `cancel_task` fixed for the task_add_child hazard — net -11 lines),
  `rt_async_task.c` (`__task_create` restructured), `rt_async_waiter.c` +
  `rt_async_trace.c` (the two full-table scanners updated to
  `get_task`+`rt_task_table_snapshot` bound).
- Tests: `internal/vm/runtime_v2_lifecycle_static_test.go` (P6 `t.Skip`
  deleted), new `internal/vm/runtime_v2_lifecycle_task6_cancel_spawn_race_test.go`
  (self-contained cancel-vs-spawn race probe, plain + TSan variants),
  `internal/vm/runtime_v2_owner_local_waiter_static_test.go` (pre-existing
  stub signature updated for the `rt_task_table_snapshot` return-type
  change — not a lifecycle behavior-test file).
- `Makefile`: `runtime-v2-lifecycle-check` regex gains
  `StaticCreateReadyPushOwnerShard` and `CancelSpawnChildrenRace`.

### Hazard Found And Fixed

Moving `task_add_child` off control (required to hit the escalation target)
opened a genuine data race against `cancel_task`'s control-held children[]
walk (a running parent being cancelled from another thread while it spawns).
Fixed by nesting `task_add_child` in the parent/child's shared owner-shard
lock and having `cancel_task` snapshot children ids under that same lock
before recursing. Full argument, including the owner-replacement edge case
main flagged, is in the task document's "Hazard Found And Fixed" section.
Proven by a new TSan-backed race test
(`TestRuntimeV2LifecycleCancelSpawnChildrenRace`, enumerated;
`...RaceTSan`, best-effort, passed clean — zero TSan reports).

### Gates

- `git diff --check`: clean.
- `make c-check`, `make cppcheck`: OK.
- `timeout 1200s make runtime-v2-check`: exit 0 (all lifecycle behavior +
  static + trace gates green, including the newly-activated P6 and the new
  race gate).
- `make check`: exit 0 (all Go packages, lint 0 issues).
- `./check_file_sizes.sh -a`: `rt_async_state.c` **1444** (down from 1455;
  ceiling 1580, not grown), `rt_task_table.c` 41 (new, well under 500),
  `rt_async_task.c` 307, `rt_async_internal.h` 535 — all OK.
- Sentrux: root 6175 (baseline 6174), `runtime` 5294 (baseline 5296),
  `runtime/native` 5385 (baseline 5387) — all "All rules pass", no drop.

### Before/After Measurement (8x1024 row, `SURGE_TRACE_EXEC=1`)

| Site | Before /req | After /req |
| --- | ---: | ---: |
| `control_lock_acquired` | 26.348 | 22.780 |
| `ctrl_create` | **3.500** | **0.001** |
| `ctrl_join_poll` | 3.885 | 3.881 |
| `ctrl_completion` | 0.509 | 0.506 |
| `ctrl_scope` | 13.000 | 13.000 |
| `ctrl_handle` | ~0 | 3.500 |
| residual `OTHER` | 5.454 | 1.890 |

`ctrl_create` is essentially eliminated (residual = rare segment-growth
events, ~7-8 total across 8192 requests). `ctrl_create`'s old cost reappears
almost exactly in `ctrl_handle` (via `poll_ready_child_inline`'s pre-existing,
Task-6-untouched control bracket, S5-Q4, Task 7 territory — full explanation
in the task document). The genuine net win (`control_lock_acquired` dropping
3.570/request) comes from the `OTHER` residual, not fully attributed within
this task's scope; flagged for Task 7 to pin down further when it touches
`rt_task_poll`/`poll_ready_child_inline` directly.

### Task 7 Handoff

- `get_task`'s external contract is unchanged (same signature, same
  lock-free acquire semantics); no caller elsewhere in the tree needed
  changes.
- `rt_task_table_snapshot` now returns `uint64_t` (a `next_id` bound), not a
  struct pointer — use `get_task(ex, i)` + this bound for any future
  full-table scan, never direct struct access.
- `ctrl_handle` now dominated by `poll_ready_child_inline` on the net bench;
  expect a large drop when Task 7 migrates its control bracket (S5-Q4).
- `cancel_task`'s children[] read is now owner-shard-lock-protected; keep
  paired with `task_add_child`'s lane in any future change.

### Commit Boundary

One commit: segmented task table + `__task_create` restructure + the
`cancel_task` hazard fix + P6 activation + the new race-probe test + doc
updates. No other lifecycle surface changes in this commit.

### Review Response Addendum

Independent review verdict: APPROVE-WITH-NOTES, no blockers. Two minor
findings, both fixed in a follow-up commit before Task 7 spawned:

1. `TestRuntimeV2LifecycleCancelSpawnChildrenRace`/`...RaceTSan` hardcoded
   `SURGE_SHARDS=4`, missing the epic's required `SURGE_SHARDS=1,2,8`
   sweep — the hazard-closing test must also exercise the `SHARDS=1`
   degenerate case. Fixed: both tests now sweep `1,2,8` via subtests
   (`shards-1`/`shards-2`/`shards-8`), mirroring
   `runLifecycleModeAcrossShards`'s shard/thread convention. Re-ran:
   `TestRuntimeV2LifecycleCancelSpawnChildrenRace` (plain) green at all
   three; `...RaceTSan` green at all three (zero TSan reports).
2. `STATS.md` (committed alongside the Task 6 commit by the pre-commit
   hook) was bogus: `scripts/code_stats_md.sh`'s `get_dir_stats "."` scan
   used an unscoped `find "."`, which recursed into
   `.claude/worktrees/<agent>/` (a full nested repo checkout from the
   Task 11 investigator's separate worktree) and roughly doubled every
   "main code" count (Files 720->1374, LOC 163977->307176). The
   `cmd`/`internal`/`runtime/native`-scoped scans were unaffected (their
   `find` start paths don't nest under `.claude/`). Fixed: added
   `-not -path "./.claude/*" -not -path "./target/*"` to both
   `scripts/code_stats.sh` and `scripts/code_stats_md.sh` (identical
   duplicated logic in both, fixed identically to avoid future
   divergence), regenerated `STATS.md` (now Files 721/LOC 164167 main
   code — matches the reviewer's cited correct baseline within a few
   files/lines, the small remaining delta being this task's own new test
   file).

Checks run for this addendum (narrower scope than a full runtime-code
task per main's direction - test-sweep + script-only, not a lifecycle
surface change): `bash -n` on both scripts; focused race test at
`SURGE_SHARDS=1,2,8` (plain + TSan, both green); `make check` (exit 0,
all Go packages, lint 0 issues, `c-check` OK, file-size gate reports no
applicable uncommitted C/Go files needing action). Full
`runtime-v2-check` not run (not required for this scope, per main).
Follow-up commit: `fix(runtime): sweep cancel-spawn race test shards and
exclude tool dirs from code stats`.

## Task 7: Join Poll And Handle Lifetime

### Task Identity And Scope

Migrates `rt_task_poll` (join register + result read), `poll_ready_child_inline`,
`rt_task_clone`, and `rt_task_wake` off the control lane per the spike (rule
2, S5-Q2/Q3/Q4/Q6); `rt_task_cancel`/`cancel_task` stay control per S5-Q5
(unchanged). Folds in F2, the Epic 8 Task 11 net-fairness fix
(`RV2-DEBT-015`): a joiner consuming a DONE child carrying
`TASK_PLACEMENT_CONNECTION` adopts the child's placement, so
`serve_many`/`serve_conn`'s durable pipeline follows the accepting shard
instead of staying pinned to shard 0. Full write-up:
`08-tasks/07-join-poll-and-handle-lifetime.md`.

### Sequencing Hazard Resolution

Chose arm (i) from the Task 6 handoff: pulled the `mark_done` result-write
reorder (write `result_kind`/`result_bits` before the `TASK_DONE` release
store) into this task as a 2-line enabling change, rather than deferring to
Task 8. This closes RACE 1 of `RV2-DEBT-019` (Task 4's TSan finding); RACE 2
(the unlocked `park_key` read in `mark_done_needs_control`) and un-skipping
`TestRuntimeV2LifecycleCompletionPinInterleavingTSan` remain Task 8's.

### Files Touched

- C: `rt_async_state.c` (2-line `mark_done` reorder only), `rt_async_task.c`
  (`rt_task_poll`, `poll_ready_child_inline`, `rt_task_clone`, `rt_task_wake`;
  new static helper `rt_task_poll_adopt_placement` for F2),
  `rt_async_trace.c` + `rt_async_internal.h` (new `placement_adoptions`
  counter).
- Tests: `internal/vm/runtime_v2_lifecycle_static_test.go` (P7 `t.Skip`
  deleted; G6's table repointed/pruned — see "Review-Visible Changes"
  below), `internal/vm/runtime_v2_lifecycle_trace_test.go` (trace-contract
  `ctrl_join_poll` must-be-nonzero assertion removed, `ctrl_create` kept),
  new `internal/vm/runtime_v2_lifecycle_behavior_placement_adoption_test.go`
  (F2 positive/negative), small wiring additions in
  `runtime_v2_lifecycle_behavior_harness_test.go` (2 enum values, one
  concatenation entry) and `runtime_v2_lifecycle_behavior_await_shutdown_test.go`
  (2 `main()` dispatch lines, 2 switch cases).
- `Makefile`: `runtime-v2-lifecycle-check` regex gains
  `StaticJoinPollOwnerLane` and `JoinConsumePlacementAdoption`.

### Review-Visible Changes To Task 5's Shipped Gates

Both approved by main before implementation (plan-gate response).

1. `TestRuntimeV2LifecycleStaticCensusSitesTagged` (G6): `rt_task_clone`'s
   case deleted (S5-Q6 drops control unconditionally, nothing left to tag);
   `rt_task_poll`'s `RT_CTRL_SITE_JOIN_POLL` entry repointed to
   `rt_task_poll_adopt_placement` (the tag now lives in that separate
   helper, structurally required by P7's own "no `rt_control_lock(` in
   `rt_task_poll`'s own body" bar).
2. `TestRuntimeV2LifecycleTraceControlSiteContract`: `ctrl_join_poll` removed
   from the must-be-nonzero list (genuinely 0 in that synthetic
   no-connection-placement program after this task); `ctrl_create` kept
   (segment growth still fires at least once per process).

### F2 Hard-Constraint Arm Chosen

Arm (1): explicit control fallback in `rt_task_poll_adopt_placement`, gated
`!rt_lane_holds_control()`, tagged `RT_CTRL_SITE_JOIN_POLL`, plus a dedicated
`placement_adoptions` trace counter. Reuses the accept-transition's own
`rt_task_replace_owner` primitive and safety argument (self-replace on a
RUNNING task, on that task's own thread) rather than re-deriving Task 6's
children[]-append happens-before chain, per the spec's explicit permission
(adoption is O(connections), never per-request steady state).

### F2 Correctness Test

New `TestRuntimeV2LifecycleJoinConsumePlacementAdoption` (positive/negative
subtests, per main's explicit review requirement R2): a joiner on shard 0
consumes a DONE child pinned to shard 1. Positive (child
`TASK_PLACEMENT_CONNECTION`-placed): joiner's own `owner_shard_id`/
`placement_class` become shard-1/`CONNECTION`. Negative (child
`TASK_PLACEMENT_GENERIC`-placed): joiner's placement is unchanged — the
guard is as load-bearing as the adoption. Both green.

### Measurement: Net Bench 8x1024, `SURGE_TRACE_EXEC=1`, 3 Runs

Direct mode, 8 shards / 8 threads / 1024 connections / 8 req/conn = 8192
requests, run directly against `benchmarks/native/net_request_reply`
(bit-exact reproducible across all 3 runs for every per-site counter).
"Before" is Task 6's committed baseline (`5523094e`/`a2d3f87c`/`05d95b60`).

| Site | Before /req (total) | After /req (total) |
| --- | ---: | ---: |
| `control_lock_acquired` | 22.780 (186593) | **23.90 (195751-195430)** |
| `ctrl_create` | 0.001 (8) | 0.001 (11-12) |
| `ctrl_join_poll` | 3.881 (31792) | **0.249 (2019-2037)** |
| `ctrl_completion` | 0.506 (4141) | **3.500 (28673)** |
| `ctrl_scope` | 13.000 (106499) | 13.000 (106499, exact match) |
| `ctrl_handle` | 3.500 (28673) | 3.626 (29696) |
| `placement_adoptions` (new) | n/a | 0.247 (2019-2037) |

**F2 works exactly as designed** (the epic's primary goal for this task):
`placement_adoptions` fires ~2019-2037 times (once per accept-ish event,
matching the O(connections) frequency bound, not O(requests) — 1024
connections, adoption fires roughly twice per connection: once for
`serve_many` adopting per accept, once for the first `serve_conn` spawn
inheriting it). A mid-load `SIGUSR1` dump (60 req/conn, sampled ~1s in)
shows the owner histogram genuinely spread across all 8 shards for the
first time (`338`-`440` tasks per shard, vs the pre-F2 baseline's "3073/3073
owner=0" from `epic8-task11-placement-funnel`); `TRACE_STORE` waiter counts
are similarly distributed (338-444 per shard vs all-in-shard-0 before);
steady-state `inject_len=0` at both the mid-load sample and exit (vs ~1023
before). `ctrl_join_poll` drops to near-zero as designed (S5-Q3): the
residual ~0.25/req is entirely the F2 fallback firing, not steady-state join
traffic.

**Honest accounting of a real, reproducible increase (main's "report
honestly" precedent, Task 6):** `control_lock_acquired`'s total went *up*
(186593 -> ~195600), not down, driven by `ctrl_completion` jumping from
4141 to a bit-exact 28673 every run. Root cause: before this task,
`poll_ready_child_inline` held control across its entire body (including the
nested `apply_poll_outcome`/`mark_done` call), so `mark_done`'s own
`need_control` check short-circuited false (`rt_lane_holds_control()` was
already true) and its control-lane work for these completions ran "for
free" under the caller's ambient lock — untagged, since the tag call only
fires inside `mark_done`'s own take-lock branch. Migrating
`poll_ready_child_inline` off control (S5-Q4, required by this task) removes
that ambient hold, so `mark_done` now correctly evaluates its own need for
these same completions and — since Task 6 already established this exact
benchmark drives the `write_owned(...).await()`/`net.read_some(...).await()`
inline-child-poll pattern on almost every request (`ctrl_handle`=28673 at
Task 6 landing) — takes its own separate, now-honestly-tagged
`RT_CTRL_SITE_COMPLETION` lock for nearly all of them (28673, matching that
same population almost exactly). This is not a bug and not something to
revert (S5-Q4's "no control acquire" is the spike's explicit verdict for
`poll_ready_child_inline` itself, and it correctly has zero control
acquisitions of its own now); it is a previously-hidden cost becoming
visible, and it is **exactly the surface Task 8/9 are positioned to remove**:
`mark_done_needs_control`'s scope/join-key reasons (driving most of this
`ctrl_completion` rise, since these net-wrapper children are scope-registered)
are Task 8's S6-Q1 reduction target, and scope ownership moving off control
entirely is Task 9's. Flagged explicitly in the Task 8 handoff below.
`ctrl_handle` also rose slightly (28673 -> 29696) despite
`poll_ready_child_inline`'s own bracket being removed entirely; not fully
attributed within this task (candidate: F2's redistribution changing the
timing of some existing `rt_task_cancel`/timeout-race code path in
`stdlib/net`), reported honestly rather than claimed as understood.

### Gates

- `git diff --check`: clean.
- `make c-check` (cfmt + strict warnings): OK (one cppcheck-driven fix:
  `rt_task_poll_adopt_placement`'s `target` parameter declared
  `const rt_task*`).
- `make cppcheck`: OK, 0 findings after the const-parameter fix.
- `timeout 1200s make runtime-v2-check`: exit 0, all lifecycle
  behavior/static/trace gates green, including the newly-activated P7
  (`StaticJoinPollOwnerLane`) and the new F2 test
  (`JoinConsumePlacementAdoption`, both subtests).
- `make check`: exit 0 (all Go packages, `golangci-lint` 0 issues, `c-check`
  OK, file sizes OK).
- `./check_file_sizes.sh -a`: `rt_async_task.c` 312 (up from 307, OK),
  `rt_async_internal.h` 536 (OK), `rt_async_state.c` 1444 (unchanged
  effective count, legacy ceiling 1580 not approached), `rt_async_trace.c`
  671 (acceptable tier, was already there pre-task; +11 lines from the new
  counter).
- Sentrux: root 6174 (baseline 6175), `runtime` 5290 (baseline 5294),
  `runtime/native` 5379 (baseline 5385) — all "All rules pass", normal
  noise, no quality drop.
- No `RV2-DEBT-018` transient encountered; every gate run passed on the
  first attempt.

### Task 8 Handoff

- The `mark_done` result-write reorder is done (before the `TASK_DONE`
  store); RACE 2 (`park_key` unlocked read) and the TSan pin test un-skip
  are still yours.
- `mark_done_needs_control`'s scope/join-key reasons are the direct cause of
  this task's `ctrl_completion` rise (4141 -> 28673) — reducing them per
  S6-Q1 should claw most of that back, and is now measurable against this
  task's row rather than Task 6's (which never actually exercised
  `mark_done` control-free for these completions, per the root-cause
  explanation above).
- `rt_task_poll_adopt_placement` (F2) calls `rt_task_replace_owner` on
  `current` — if Task 8 changes anything about how/when `mark_done` runs
  relative to `rt_task_poll`'s DONE branches, re-check that F2's "no shard
  lock held" invariant (R1) still holds at both call sites.
- Per `epic8-task11-starvation`'s `RV2-DEBT-016` reinterpretation: this row
  is the first one where 8-shard execution is genuinely distributed: from
  here on, per-request control cost is paid by 8 truly-contending workers,
  not amortized on a single busy one — Task 8/9's own before/after
  measurements should use this task's row as their baseline, not Task 6's.
exclude tool dirs from code stats`.
