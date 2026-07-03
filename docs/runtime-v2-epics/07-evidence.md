# Epic 7 Evidence Ledger

Evidence for Epic 7 tasks, in task order, following `EVIDENCE_TEMPLATE.md`.
The epic contract lives in
`07-executor-lock-split-and-shard-runtime-state.md`.

## Task 1: Kickoff Baseline And Sentrux

### Task Identity And Scope

- Task: Epic 7 Task 1, kickoff baseline and Sentrux.
- Epic: 7 (`07-executor-lock-split-and-shard-runtime-state.md`).
- Date: 2026-07-03.
- Author/session: main session (Claude Code), no subagents.
- Scope: freeze checkout, LOC, gate, Sentrux, and benchmark baselines; create
  this ledger; no runtime, compiler, test, benchmark-script, or CI changes.
- Out of scope: dependency map (Task 2), locking model (Task 3).
- Proving spike: no.

### Baseline Commit/Status

- Baseline commit: `77475384dbfaf17f0b38a02ed5d7a80beb76a5b9`
  (`docs(runtime): open epic 7 executor lock split`).
- Branch/worktree: `wip/codex/runtime-net-scheduler-refactor` in
  `/home/zov/projects/surge/surge-wip`.
- Status before: only Epic 7 doc files tracked; untracked tool droppings not
  owned by this epic: `.claude-flow/`, `.claude/`, `.mcp.json`, `.swarm/`,
  `CLAUDE.md`, `ruvector.db`. They stay untracked and uncommitted.
- Local environment: WSL2 (`Linux 6.18.33.2-microsoft-standard-WSL2`),
  `ulimit -n` 1048576 soft and hard, clang/cppcheck/go present,
  `sentrux` CLI at `/usr/local/bin/sentrux`.
- Sentrux MCP tools are not exposed in this session; the `sentrux check`
  CLI (which loads `.sentrux/rules.toml` and reports the quality signal and
  rule results) is the recorded evidence mechanism for this epic. This is a
  deviation from the `SENTRUX_POLICY.md` MCP call list, recorded here per the
  policy's own missing-tool rule; the scanned paths and signals below are the
  same data the MCP calls report.

### Files Touched

| Path | Change | Reason | Size/limit note |
| --- | --- | --- | --- |
| `docs/runtime-v2-epics/07-evidence.md` | created | Task 1 ledger | docs |
| `docs/runtime-v2-epics/07-tasks/01-kickoff-baseline-and-sentrux.md` | created | task document | docs |
| `docs/runtime-v2-epics/07-tasks/README.md` | status update | Task 1 complete | docs |
| `docs/runtime-v2-epics/NOTES.md` | handoff appended | Rule 10 | docs |

### Baseline Effective LOC (files this epic will touch)

From `./check_file_sizes.sh -a` at the baseline commit (effective LOC):

| File | Effective LOC | Ceiling | Note |
| --- | ---: | ---: | --- |
| `runtime/native/rt_async_state.c` | 1722 | 1722 | at ceiling; must not grow |
| `runtime/native/rt_async_task.c` | 707 | 731 | under ceiling |
| `runtime/native/rt_net.c` | 818 | 818 | at ceiling; expected untouched |
| `runtime/native/rt_async_waiter.c` | 488 | 500 | 12 lines of headroom |
| `runtime/native/rt_async_internal.h` | 478 | 500 | 22 lines of headroom |
| `runtime/native/rt_scheduler_placement.c` | 91 | 500 | headroom |
| `runtime/native/rt_runtime.c` | n/a (under) | 500 | headroom |

`rt_async_waiter.c` and `rt_async_internal.h` are close to the hard gate;
lane migrations that grow them must extract first.

### Contracts Touched

N/A: docs and read-only evidence only.

### Sentrux Root/Scoped Signals

| Scan | Path | When | quality_signal | Rules result |
| --- | --- | --- | --- | --- |
| Repository | `/home/zov/projects/surge/surge-wip` (`sentrux check .`) | Before | 6182 | 10 rules checked, all pass |
| Scoped | `runtime` | Before | 5340 | 7 rules checked, all pass |
| Scoped | `runtime/native` | Before | 5467 | 7 rules checked, all pass |

These equal the Epic 6 closeout signals (6182/5340/5467), so Epic 7 starts
from the recorded Epic 6 quality baseline.

### Commands/Checks

| Command or tool | Expected | Actual | Exit | Note |
| --- | --- | --- | --- | --- |
| `git diff --check` | no output | no output | 0 | whitespace gate |
| `make check` | pass | pass | 0 | ran as pre-commit hook of the epic-open commit `77475384` (test, lint, c-check, file sizes) |
| `make c-check` | pass | pass | 0 | inside the same pre-commit run |
| `make cppcheck` | pass | pass (43/43 files) | 0 | run directly |
| `timeout 600s make runtime-v2-check` | pass | pass on first run (liveness, heap, waiter, fd-registry, accept gates all PASS) | 0 | no `RV2-DEBT-002` rerun needed this time |
| `./check_file_sizes.sh -a` | pass | pass (707 files, 0 over) | 0 | LOC table above |
| `sentrux check .` / `runtime` / `runtime/native` | all rules pass | all pass | 0 | signals above |

### Benchmarks And Generated Reports

All rows use the current-checkout binary (`make build`, commit `77475384`,
verified by the script's embedded-commit check). Reports live under
git-ignored `build/benchmarks/`.

Epic 6-comparable matrix (`REQUESTS=8`, `RUN_TIMEOUT=60s`, direct/seq),
report `runtime-v2-epic7-task1-baseline-epic6-matrix.md`:

| shards | connections | total requests | total us | avg us/op | p50 us | p95 us |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 8 | 3669 | 193.96 | 175.07 | 215.07 |
| 1 | 8 | 64 | 14598 | 948.11 | 973.96 | 1709.08 |
| 1 | 32 | 256 | 54550 | 2644.84 | 2362.09 | 5526.40 |
| 1 | 1024 | 8192 | 1537005 | 22617.57 | 20261.97 | 36085.56 |
| 8 | 1 | 8 | 4123 | 231.75 | 178.75 | 297.75 |
| 8 | 8 | 64 | 17026 | 1012.79 | 380.45 | 1826.83 |
| 8 | 32 | 256 | 63725 | 3669.72 | 411.97 | 30694.39 |
| 8 | 1024 | 8192 | 2516745 | 32679.68 | 1182.68 | 64826.68 |

Steady-state matrix (`REQUESTS=2000`, `RUN_TIMEOUT=120s`, direct/seq),
report `runtime-v2-epic7-task1-baseline-steady-small.md`:

| shards | connections | total requests | total us | avg us/op | p50 us | p95 us |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 | 2000 | 200608 | 98.33 | 84.08 | 168.39 |
| 1 | 8 | 16000 | 2625660 | 1305.24 | 1241.84 | 2128.67 |
| 1 | 32 | 64000 | 10343970 | 5132.66 | 5028.47 | 8288.46 |
| 8 | 1 | 2000 | 309135 | 152.59 | 99.44 | 476.35 |
| 8 | 8 | 16000 | 2920313 | 1072.16 | 700.62 | 2130.48 |
| 8 | 32 | 64000 | 28495039 | 8441.11 | 1531.64 | 42322.13 |

Steady-state 1024-connection row (`REQUESTS=100`, `RUN_TIMEOUT=120s`),
report `runtime-v2-epic7-task1-baseline-steady-1024.md`:

| shards | connections | total requests | total us | avg us/op | p50 us | p95 us |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1024 | 102400 | 16771952 | 20706.51 | 20256.86 | 35386.74 |
| 8 | 1024 | 102400 | fails | fails | fails | fails |

Channel benchmark (`scripts/bench_native_channels.sh` defaults), report
`native-channel-request-reply.md`, key rows:

| mode (threads) | probe | ns/op |
| --- | --- | ---: |
| 1 | channel_ping_pong | 4438 |
| 1 | channel_reused_reply | 3457 |
| 1 | channel_sync_new_reply | 9264 |
| 2 | channel_ping_pong | 17663 |
| 2 | channel_reused_reply | 13117 |
| 2 | channel_sync_new_reply | 93638 |
| 4 | channel_ping_pong | 17659 |

Baseline interpretation, which is the Epic 7 performance case:

- 8-shard rows lose to 1-shard everywhere the load is steady: 2.75x slower at
  32 connections x 2000 requests (28.50s vs 10.34s) with a 5x worse p95
  (42.3ms vs 8.3ms), and 1.64x slower on the Epic 6 matrix at 1024
  connections. Epic 6 already proved owner placement is correct
  (`global_path_fallbacks=0`, `sched steal=0`), so the preserved global
  executor lock and its broadcast wakeups are the recorded suspect.
- Channel request/reply degrades ~4x from 1 worker to 2+ workers
  (4.4us -> 17.7us ping-pong) and the sync-channel path degrades ~10x
  (9.3us -> 93.6us), matching the global-lock hypothesis for channels.

### Trace Counters/Liveness Proof

| Probe | Expected | Actual | Pass/blocker |
| --- | --- | --- | --- |
| 8-shard/1024-conn/100-req steady row | completes | client `TimeoutError: timed out` in `recv_exact` (>10s stall on a connection); reproduced 3/3 runs; 965/1024 connections completed (`served=100`) before the failing wait; server side exits cleanly with `accept_owner_total=1024`, `global_path_fallbacks=0`, `sched steal=0`, `parked_with_work=0` | recorded baseline deficiency, not a new regression |
| 1-shard/1024-conn/100-req steady row | completes | completes (16.77s total) | pass |

The reproducible 8-shard starvation (>10s per-connection stall under load
while other shards make progress) is adopted as an Epic 7 target probe: after
the lock split this row must pass, or its failure becomes a closeout blocker.

### Known Regressions

- None introduced by this task. The 8-shard/1024x100 client timeout exists at
  the baseline commit before any Epic 7 code.

### Dead Ends / Paths Not To Retry

- Running the 1024-connection row with the default `REQUESTS=2000` and
  `RUN_TIMEOUT=30s` kills the server mid-row (2M requests cannot finish in
  30s) and reports `ConnectionResetError`; use the Epic 6 matrix
  (`REQUESTS=8`) or `REQUESTS=100` with `RUN_TIMEOUT=120s`.

### Rollback/Recovery Notes

- Generated artifacts: reports under `build/benchmarks/` (git-ignored) and
  scratch logs; safe to delete.
- No runtime processes left running; benchmark script cleans up its children.

### Follow-Ups And Blockers

| Item | Blocks completion? | Owner | Reason |
| --- | --- | --- | --- |
| Task 12 must rerun the exact three net matrices and the channel defaults above | yes (epic) | Task 12 | Performance Contract comparison |
| 8-shard/1024x100 steady row must pass post-split | yes (epic) | Task 12 / closeout | adopted liveness target |
| Sentrux MCP unavailability recorded; CLI is the epic's mechanism | no | this ledger | policy deviation note |

### Notes Consolidation Checklist

- [x] `NOTES.md` has the start context and intended proof.
- [x] `NOTES.md` has what changed and what was checked.
- [x] Durable decisions are in the epic document; baselines live here.
- [x] Skipped checks: none skipped.
- [x] Dead ends recorded (benchmark row parameters).
- [x] Follow-ups have owners.

## Task 2: Executor Lock Dependency Map

- Task: Epic 7 Task 2, dependency map. Docs-only; proving spike: no.
- Date: 2026-07-03. Author/session: main session.
- Scope: created `07-executor-lock-dependency-map.md` (lock-site inventory,
  field lane tables for `rt_executor`/`rt_shard`/`rt_task`/`rt_scope`,
  waiter-key ownership table, path-by-path target lanes, hazard list,
  already-safe list) and the Task 2 task document. No code changed.
- Baseline: anchors cite commit `ae752f44` (code identical to `77475384`).
- Key facts the map pins: 187 `rt_lock`/`rt_unlock` sites across 12 files;
  the `poll()`
  syscall already runs unlocked (`rt_net.c:817-823`); non-net waiter kinds
  all live in shard 0's store today (`rt_async_waiter.c:438-459`,
  `rt_runtime.c:245-251`); sleep timers scan the whole task table per yield
  (`rt_async_state.c:1162-1226`); `mark_done` broadcasts the global
  `done_cv` on every completion (`rt_async_state.c:1470`).
- Open decisions delegated to Task 3, marked *(spike)* in the map: virtual
  clock read/advance protocol; task lifetime/lookup rule (slot atomicity +
  owner-locked deref); scope-key store owner; accept re-placement transition;
  fate of `ready_cv`/`io_cv`/`done_cv`; whether non-user task polls stay
  under the shard lock; accept-winner cleanup shape.
- Commands: `git diff --check` clean; docs-only, no other gates required.
- Regressions: none. Dead ends: none new.
- Follow-ups: Task 3 must answer every *(spike)* mark or record why it moved
  to a later task.

## Task 3: Locking-Model Proving Spike

- Task: Epic 7 Task 3, locking-model proving spike. Proving spike: yes; the
  Global Rule 1 record lives in `07-locking-model-proving-spike.md`.
- Date: 2026-07-03. Author/session: main session.
- Scope: fixed the locking model as decisions D1-D16 (per-shard lock +
  `worker_cv` + `poller_cv`; lock order control -> at most one shard lock
  with a TLS debug assertion; task deref/free/owner-stability rules;
  control-lane accept re-placement; owner-hinted waiter entries with
  same-shard fast wake and control-arbitrated collect-then-wake; atomic
  virtual clock + per-shard sleep stores + last-idle-worker advance;
  control-lane scope keys, blocking completion, select/timeout multi-key
  registration; `mark_done` shard-phase/control-epilogue split; gated
  `done_cv`; sync-compat on shard `worker_cv`; io thread N=1 on shard 0
  `poller_cv`, N>1 coarse backstop with a must-stay-zero counter).
- Proof: standalone C model (source inlined in the spike doc), 4 shards, 32
  tasks x 20000 cycles, 3 cross-shard wake threads. Runs: 4x
  `clang -O1 -g -fsanitize=thread` PASS with zero TSan reports (~13-15s
  each), 2x `clang -O2 -DNDEBUG` PASS. `total_wakes=639968` equals the exact
  number of parks performed, so every registered waiter was woken exactly
  once (no lost wakeups, no double consumption); `slow_wakes=480281`
  (foreign-hint collect-then-wake path exercised heavily);
  spurious parks 273-538 per run (0.04-0.08% of wakes), bounded.
- Rejected alternatives recorded in the spike doc: single cv per shard;
  free under owner lock alone; owner chasing under shard locks;
  foreign-entry deref under store locks; per-shard virtual clocks.
- Commands: prototype builds/runs above; `git diff --check` clean. No
  repository code changed, so no C gates required.
- Regressions: none. Dead ends: the rejected-alternatives list.
- Follow-ups: Tasks 4-5 encode D1-D16 as behavior and static contracts;
  Tasks 6-11 implement them; any deviation updates the spike doc first.

## Task 4: Lock-Split Behavior Contract Tests

- Task: Epic 7 Task 4. Proving spike: no. Date: 2026-07-04. Main session.
- Scope: added the lock-split behavior harness and Go driver
  (`internal/vm/runtime_v2_lock_split_harness_test.go`, 605 physical lines;
  `internal/vm/runtime_v2_lock_split_behavior_test.go`, 98 lines), nine
  modes x two shard configs per `04-lock-split-behavior-contract-tests.md`.
  The harness file exceeds the 500-line test-file SHOULD because the
  embedded C program is one cohesive harness; splitting the string constant
  would not improve reviewability. Recorded as the task's accepted
  deviation.
- **Discovered defect (pre-existing, multi-shard):** the cross-channel FIFO
  mode hung at `SURGE_SHARDS=3` (8/9 modes passed; `shards-1` all passed).
  Root cause: `wake_channel_task_no_signal` pushes the woken task into its
  owner scheduler's inject queue without signaling; when the owner is
  another shard whose only worker is asleep, nobody drains the queue — the
  no-signal handoff contract only holds for the current worker's own
  scheduler. Any cross-shard bounded-channel producer/consumer could
  deadlock under `SURGE_SHARDS>1`. The parked-with-work assertion cannot
  catch it because the queue becomes non-empty after the worker commits to
  sleep.
- Fix (test-first, in this task's commit series):
  - `711d41f3 refactor(runtime): extract ready-queue deque helpers from
    rt_async_state` — pure move of the six deque helpers into
    `rt_async_deque.c` (141 effective LOC) because `rt_async_state.c` sat
    exactly at its 1722 ceiling; state.c dropped to 1580, header 482.
    Full `runtime-v2-check` green after the move.
  - `d78c8d1f fix(runtime): signal cross-scheduler channel handoff wakes` —
    `wake_channel_task_no_signal` now signals when the woken task's
    scheduler is not the current worker's scheduler (state.c 1582/1722).
- Checks: `make c-check` pass; `make cppcheck` pass (after a
  const-qualification style fix it flagged); focused lock-split suite
  9/9 x 2 configs pass post-fix (`go test -tags runtime_v2_pending
  ./internal/vm -run '^TestRuntimeV2LockSplit' ...` 17.7s);
  `timeout 600s make runtime-v2-check` pass twice (58 PASS lines, exit 0);
  `make check` green in both pre-commit runs; `git diff --check` clean.
- Sentrux (CLI): root 6181 (was 6182), `runtime` 5326 (was 5340),
  `runtime/native` 5450 (was 5467); all rules pass on all three paths. The
  small signal drop tracks the file split (one file became two) and the new
  test files; floors hold. Recovery owner: Task 14 re-evaluates after the
  full split and must restore or explain the delta at closeout.
- Contracts: cross-shard join/cancel/channel FIFO+close/blocking/sleep/
  select/timeout/shutdown liveness pinned at `SURGE_SHARDS=1` and `=3`;
  channel FIFO and close-after-drain semantics preserved by the fix (close
  only completes receivers after handoff/buffer drain, per mode
  `cross-channel`).
- Regressions: none; the fixed defect predates the epic.
- Dead ends: none new.
- Follow-ups: Task 12 should count cross-scheduler channel signal wakes;
  the Task 10 channel migration keeps these nine modes green.

## Task 5: Lock-Split Static Shape Tests

- Task: Epic 7 Task 5. Proving spike: no. Date: 2026-07-04. Main session.
- Scope: added `internal/vm/runtime_v2_lock_split_static_test.go` (292
  lines, tagged `runtime_v2_pending`) with eight gates pinning D1-D16
  structural shape per `05-lock-split-static-shape-tests.md`, plus a
  definition-aware C function-body finder (the shared `cFunctionBody`
  matches forward declarations, e.g. `rt_worker_main`'s prototype).
- Expected-red proof: all eight gates fail at this commit with actionable
  messages (missing `rt_shard.lock`/cvs/`owner_hint`; missing lane API;
  non-atomic `now_ms`/missing sleep store; 187 ambiguous `rt_lock`/
  `rt_unlock` call-site hits; `rt_worker_main` on the global lock;
  `tick_virtual`/`advance_time_to_next_timer` still scanning `tasks_cap`;
  channel without owner; `ready_cv`/`io_cv` still referenced). No Makefile
  gate runs them until Task 13 wires the green set.
- Checks: `gofmt -l` clean; `go vet -tags runtime_v2_pending` clean;
  `git diff --check` clean. Docs-plus-tests only; no C changed in this
  slice.
- Flip plan recorded in the task doc: Task 6 greens the shape/lane gates,
  Task 7 the worker-loop gate, Task 9 the clock/sleep gates, Task 10 the
  channel gate, Task 11 the ambiguous-lock and condvar-retirement gates.

## Task 6: Shard Lock Structure Landing

- Task: Epic 7 Task 6. Proving spike: no. Date: 2026-07-04. Main session.
- Scope per `06-shard-lock-structure-landing.md`: new
  `runtime/native/rt_lane.c` (86 effective LOC: lane API, always-on TLS
  order tracking with panics per D2, `rt_lane_debug_enabled`,
  `rt_shard_sync_init/destroy` with status codes and partial-init unwind);
  `rt_shard` gained `lock`/`worker_cv`/`poller_cv`; `waiter` gained
  `owner_hint` (populated in `add_waiter` from the task's owner shard, 0
  until universal assignment); `rt_shard_init` inits shard sync first and
  unwinds on later failures; `rt_lock`/`rt_unlock` are now one-line
  delegates to `rt_control_lock`/`rt_control_unlock` so lane tracking is
  consistent from this commit (they die in Task 11).
- Behavior identical: no path takes a shard lock yet.
- Checks: `make c-check` pass (after clang-format), `make cppcheck` pass,
  Task 4 behavior suite 9/9 x 2 configs pass (18.2s),
  `timeout 600s make runtime-v2-check` exit 0, `git diff --check` clean,
  `make check` green in pre-commit. Static gates: `ShardSyncShape` and
  `LaneAPIShape` flipped green (the lane/clock snippets needed non-static
  functions to survive `-Werror` unused-function); `ClockAndSleepStoreShape`
  and the rest stay red by design.
- LOC: `rt_lane.c` 86; `rt_async_internal.h` 493/500 and
  `rt_async_waiter.c` 490/500 are now tight — Tasks 8-9 must extract before
  growing either; `rt_async_state.c` 1576/1722; `rt_runtime.c` 292.
- Sentrux (CLI): 6181/5334/5458, all rules pass (runtime/native slightly up
  from Task 4's 5326/5450).
- Regressions: none. Follow-ups: header and waiter files near the hard
  gate.

## Task 7: Scheduler Ready And Park/Wake Migration

- Task: Epic 7 Task 7. Proving spike: no. Date: 2026-07-04. Main session.
- Scope per `07-scheduler-ready-and-park-wake-migration.md`:
  - Universal owner assignment (D3): `rt_task_assign_spawn_owner` in
    `rt_scheduler_placement.c`, called by `__task_create`,
    checkpoint/sleep spawns, and `rt_blocking_submit`; non-worker spawns get
    shard 0. `rt_task_owner_shard` added; `rt_task_scheduler` now routes
    through it; `ready_scheduler_for_task`'s current-worker fallback deleted
    (ownerless tasks are a pre-Task-7 compatibility case pinned to shard 0).
  - New `rt_sched_wake.c` (47 effective LOC): `rt_sched_wake_signal_shard_n`
    (bump `wake_pending` under the shard lock, then signal/broadcast),
    `rt_sched_wake_broadcast_all` (shutdown/compat sweep with one token per
    potential sleeper), `rt_sched_worker_sleep` (release control, wait on
    `worker_cv` consuming `wake_pending`, reacquire control).
  - `ready_cv` deleted. Worker sleep uses the shard `worker_cv`;
    `ready_push` signals only the owner shard; local-to-inject moves signal
    the own shard with one token per moved task; the sync-channel compat
    wait and its fallback broadcast moved to a new control-lane `compat_cv`
    (compat tasks are RUNNING during the wait, so the fallback broadcast is
    their only wake path — recorded rationale in code);
    `rt_net_wake_poll_for_task_wait_keys` signals the owner shards instead
    of broadcasting to every worker; shutdown sweeps all shard cvs +
    `compat_cv`. `ex->shutdown` became `_Atomic uint8_t` for shard-side
    predicates.
- Contract-test updates forced by the shape change (Epic 6 pattern):
  `runtime_v2_net_poller_static_test.go` stubs
  `rt_sched_wake_signal_shard_n` and the shutdown source gate now requires
  `rt_sched_wake_broadcast_all` + `compat_cv` instead of `ready_cv`;
  `runtime_v2_fd_registry_shutdown_static_test.go` stubs and asserts one
  `rt_sched_wake_broadcast_all` call per shutdown request.
- Checks: `make c-check` and `make cppcheck` pass; Task 4 behavior suite
  9/9 x 2 configs pass; `timeout 600s make runtime-v2-check` pass twice;
  `make check` green in pre-commit; `git diff --check` clean.
- LOC: `rt_sched_wake.c` 47; `rt_async_state.c` 1563/1722 (down);
  `rt_async_internal.h` 499/500 (Task 8 must extract before adding decls);
  `rt_scheduler_placement.c` 103; `rt_net_poller.c` 166; `rt_shutdown.c` 41.
- Sentrux: root 6177, `runtime/native` 5428, all rules pass (drift within
  the epic-recorded band; Task 14 owns reconciliation).
- Regressions: none observed across two full gate runs.

## Task 8: Waiter-Store Key Ownership Migration

- Task: Epic 7 Task 8. Proving spike: no. Date: 2026-07-04. Main session.
- Commits: `f02deef1 refactor(runtime): extract waiter types into
  rt_waiter.h` (pure move; internal.h 499 -> 434 effective) and the routing
  commit (this one).
- Scope per `08-waiter-store-key-ownership-migration.md`: per-key store
  resolution (`rt_waiter_route.c`, 47 effective LOC): join/timer/blocking ->
  task owner store, scope -> new `ex->control_waiters`, channel -> shard-0
  compat until Task 10, net unchanged; `add/remove/pop_waiter` and
  `wake_key_all_with_policy` route through it; waiter trace aggregation
  scans the control store; `rt_task_replace_owner` migrates `join_key`
  entries at all three accept-transition sites so completion wakes never
  scan a stale shard.
- Stub-harness update: owner-local waiter harness gained
  `rt_task_owner_shard`/`rt_task_replace_owner` stubs and includes
  `rt_waiter_route.c`. `ensure_waiter_cap` kept (pinned by
  `runtime_v2_waiter_static_test.go`).
- Checks: c-check/cppcheck pass; Task 4 suite 9/9 x 2; `runtime-v2-check`
  pass twice; `make check` pre-commit; `git diff --check` clean.
- LOC: `rt_async_waiter.c` 491/500 (back under after the route extraction),
  `rt_waiter.h` 80, `rt_waiter_route.c` 47, internal.h 439,
  `rt_scheduler_placement.c` 116, `rt_net_accept_group.c` 247.
- Sentrux: `runtime/native` 5419, rules pass.
- Regressions: none. Follow-up: Task 10 moves channel keys to the channel
  owner store.

## Task 9: Sleep/Timer Store And Virtual Clock

- Task: Epic 7 Task 9. Proving spike: no. Date: 2026-07-04. Main session.
- Scope per `09-sleep-timer-store-and-virtual-clock.md`: `rt_async_sleep.c`
  (127 effective LOC) with per-shard `(deadline, task_id)`-sorted stores,
  atomic `min_deadline` mirrors, atomic `now_ms` (relaxed `fetch_add`
  ticks + monotonic CAS advance); `poll_sleep_task` arms into the owner
  store; `mark_done` cleans cancelled sleepers; `tick_virtual` fires own
  due sleepers inline and hands foreign shards wake tokens; worker loop
  pops own due sleepers at scan top; `next_sleep_deadline` = min over
  mirrors; `advance_time_to_next_timer` = CAS + all-shard fire sweep. The
  whole-table scans are gone.
- Defect caught during the task: zeroed `min_deadline` mirrors read as a
  phantom deadline at 0 and would spin the idle paths;
  `rt_sleep_store_init` in `rt_shard_init` fixes empty stores to
  `UINT64_MAX` (recorded in code).
- Rule 5 note: the sleep store coexists with timer-key waiter entries by
  design — park registration vs ordered deadline index; the reason is in
  the file header and task doc.
- Checks: c-check/cppcheck pass; full lock-split suite: 9 behavior modes
  green, `ClockAndSleepStoreShape` + `NoWholeTableSleepScan` flipped green,
  the four Task-10/11 gates red by design; `runtime-v2-check` pass twice
  (includes `TestMTBlockingChannelHelpersAllowTimersToAdvance` and seeded
  scheduler); `make check` pre-commit; `git diff --check` clean.
- LOC: `rt_async_sleep.c` 127; state.c 1565/1722; internal.h 460;
  poll.c 308.
- Regressions: none.
