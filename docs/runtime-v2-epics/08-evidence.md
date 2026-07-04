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
