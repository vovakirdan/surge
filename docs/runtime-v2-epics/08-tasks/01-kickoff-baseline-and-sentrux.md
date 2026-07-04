# Epic 8 Task 1: Kickoff Baseline And Sentrux

**Kind:** evidence. **Depends on:** none.

**Goal:** freeze the starting facts for the lifecycle-lane migration before
any code moves: checkout state, effective line counts for the files this epic
will rewrite, current gate results, Sentrux baselines, a lifecycle
control-lock census, and fresh benchmark rows produced by a current-checkout
binary on this machine. Later tasks compare against this record, not against
memory or against the Epic 7 closeout numbers alone (same host, different
day: rows must be reproduced).

## Scope

- Create `docs/runtime-v2-epics/08-evidence.md` from `EVIDENCE_TEMPLATE.md`
  with the Task 1 section filled.
- Record `git rev-parse HEAD`, `git status --short`, and untracked files not
  owned by this epic.
- Record effective LOC for `rt_async_state.c` (allowlist ceiling 1580 —
  `RV2-DEBT-003`: it must not grow this epic without an extraction or a
  recorded debt update) and physical/effective LOC for the lifecycle files
  this epic touches: `rt_async_task.c`, `rt_async_scope.c`,
  `rt_scheduler_placement.c`, `rt_async_internal.h`, `rt_worker_turn.c`,
  `rt_async_waiter.c`, `rt_lane.c`.
- Run and record: `make c-check`, `make cppcheck`,
  `timeout 1200s make runtime-v2-check` (now includes
  `runtime-v2-lock-check`), `make check` (or cite the immediately preceding
  green run with its trigger), `./check_file_sizes.sh -a`,
  `git diff --check`.
- Run and record Sentrux `check` for the repository root, `runtime/`, and
  `runtime/native/` (Epic 7 closeout signals to compare against:
  6174 / 5296 / 5389, all rules pass). Record the MCP availability state; if
  only the CLI is available, record that as the epic's accepted evidence
  mechanism.
- Lifecycle control-lock census: enumerate every `rt_control_lock` call site
  (there are ~40-60 in `runtime/native/`) and classify each as
  lifecycle-steady-path (create/join/done/scope/cancel/clone/await),
  compatibility (external await, single-worker runner, sync-channel compat,
  select slow lane), infrastructure (shutdown, trace dump, table growth), or
  non-lifecycle. The census table goes into the evidence ledger and seeds
  Task 2's dependency map.
- Build the current-checkout `surge` binary (`make build`) and produce
  baseline benchmark rows with the Epic 7 Task 12 script settings:
  net `direct/seq`, shards 1 and 8, connections 1/8/32/1024, 8 req/conn,
  with `SURGE_TRACE_EXEC=1` so the Runtime Trace table records
  `control_lock_acquired`, `cross_shard_wakes`, `spurious_wakes_absorbed`,
  `collect_wake_batches`, and `owner_replacements` per row. Compute and
  record control-lock acquisitions per request for the 8-shard/1024 row —
  this number sets the epic's numeric reduction target (Epic 7 closeout
  measured ~26/request).
- Reproduce the `RV2-DEBT-015` starvation probe once (8 shards, 1024
  connections, 100 requests per connection) and record whether >10s tails
  appear on this host today, with the trace snapshot. This is a baseline
  observation only; the investigation itself is Task 11.
- Channel benchmark baseline: `scripts/bench_native_channels.sh` defaults;
  note the `RV2-DEBT-017` sync-probe numbers so Task 10 changes can be
  compared. Use an outer timeout of at least 1500s (default mode's sync
  probe can degrade to the 10ms-slice ceiling; see `RV2-DEBT-017`).
- Confirm the Epic 8 gate plan: which existing gates stay required
  (`runtime-v2-lock-check`, accept gate), which new gates this epic must add
  (lifecycle static/behavior gates from Tasks 4-5, promoted in Task 12), and
  which known debt classes may appear in reruns (`RV2-DEBT-002` load-flake:
  `TestMTChannelParkUnpark` ~62s under `t.Parallel` on a loaded host;
  `RV2-DEBT-018` harness transient).

## Out Of Scope

- No runtime, compiler, test, benchmark-script, or CI changes.
- No dependency-map or lane-model analysis beyond the call-site census
  classification (mapping fields and target lanes is Task 2; deciding the
  model is Task 3).
- No starvation debugging beyond the single baseline reproduction run.

## Checks

Docs-only plus read-only commands and benchmark runs. Required records:
commands above, exit statuses, report paths under `build/benchmarks/`
(git-ignored; copy key rows into the ledger), and the benchmark environment
(machine class, WSL2 note, `nproc`).

## Success Criteria

- `08-evidence.md` exists with a complete Task 1 section, including the
  control-lock census table and the per-request control-acquisition number
  that later tasks must beat.
- Every command above has a recorded pass/fail with evidence.
- Baseline benchmark rows exist for the exact matrix the Performance
  Contract will re-run after the migration, plus the starvation baseline
  observation.
- `NOTES.md` records the Task 1 handoff and the task index status flips to
  Complete.
