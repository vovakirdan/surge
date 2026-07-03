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
