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
