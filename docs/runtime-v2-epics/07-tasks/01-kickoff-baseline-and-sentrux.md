# Epic 7 Task 1: Kickoff Baseline And Sentrux

**Kind:** evidence. **Depends on:** none.

**Goal:** freeze the starting facts for the executor lock split before any
code moves: checkout state, effective line counts for the files this epic will
rewrite, current gate results, Sentrux root and scoped baselines, and fresh
pre-split benchmark rows produced by a current-checkout binary on this
machine. Later tasks compare against this record, not against memory.

## Scope

- Create `docs/runtime-v2-epics/07-evidence.md` from `EVIDENCE_TEMPLATE.md`
  with the Task 1 section filled.
- Record `git rev-parse HEAD`, `git status --short`, and untracked files not
  owned by this epic.
- Record effective LOC for the epic's over-limit files (`rt_async_state.c`,
  `rt_async_task.c`, `rt_net.c`) and physical LOC for the async core files
  this epic touches.
- Run and record: `make c-check`, `make cppcheck`,
  `timeout 600s make runtime-v2-check`, `make check` (or cite the immediately
  preceding green run with its trigger), `./check_file_sizes.sh -a`,
  `git diff --check`.
- Run and record Sentrux `check` for the repository root, `runtime/`, and
  `runtime/native/`, naming the exact scanned paths. Record the MCP
  scan/session availability state; if only the CLI is available in this
  session, record that as the accepted evidence mechanism for the epic.
- Build the current-checkout `surge` binary and produce baseline benchmark
  rows: net `direct/seq` at 1 and 8 shards for 1, 8, 32, and 1024
  connections; channel benchmark defaults. Store reports under
  `build/benchmarks/` (git-ignored) and copy the key rows into the evidence
  ledger.
- Confirm the Epic 7 gate plan: which existing gates stay required, which new
  gates this epic must add (`runtime-v2-lock-check`), and which known debt
  classes (`RV2-DEBT-001`, `RV2-DEBT-002`, `RV2-DEBT-011`) may appear in
  reruns.

## Out Of Scope

- No runtime, compiler, test, benchmark-script, or CI changes.
- No dependency-map or locking-model analysis beyond citing the epic
  document's starting-state anchors (that is Task 2 and Task 3 work).

## Checks

Docs-only plus read-only commands and benchmark runs. Required records:
commands above, exit statuses, report paths, and the benchmark environment
(machine class, WSL2 note, governor if visible).

## Success Criteria

- `07-evidence.md` exists with a complete Task 1 section.
- Every command above has a recorded pass/fail with evidence.
- Baseline benchmark rows exist for the exact matrix the Performance Contract
  will re-run after the split.
- `NOTES.md` records the Task 1 handoff and the task index status flips to
  Complete.
