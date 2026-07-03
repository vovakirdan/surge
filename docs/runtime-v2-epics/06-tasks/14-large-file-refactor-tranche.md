# Task 14: Large File Refactor Tranche

**Status:** Draft
**Kind:** refactor code
**Depends on:** Task 11, Task 12, Task 13

## Context

By this task, behavior is fully proven (Tasks 4-13 all landed and green).
Per the Refactor Safety Contract, refactoring is only allowed once "the
behavior proof exists" — that is why this task sits near the end, not in the
middle of implementation, mirroring Epic 4's own Task 14
(`04-tasks/14-large-file-refactor-tranche.md`), which ran after its net
lifecycle migration was proven, not before.

Files this epic is likely to have touched and pushed further over their
`.loc-legacy-allowlist` ceilings:

- `runtime/native/rt_net.c` — `904` lines at Epic 6 start (`RV2-DEBT-004`),
  touched by Tasks 8, 9, 10, 11. This is the highest-risk file for this
  epic; Task 9 (listener/accept implementation), Task 10 (poller/wake), and
  Task 11 (lifecycle migration) all add code here.
- `runtime/native/rt_async_state.c` — `1727` lines at Epic 6 start
  (`RV2-DEBT-003`), touched by Task 6 (`SURGE_SHARDS` config), Task 7
  (scheduler placement, parked-with-work invariant), Task 10/11 (shutdown
  paths).
- `runtime/native/rt_async_internal.h` — `499` lines at Epic 6 start, one
  line under the Global Rule 4 ceiling already; Task 6 (shard array/config)
  and Task 7 (task ownership metadata) both add fields here and are the
  most likely source of any overage.

The epic's Accepted Baseline Debt section already commits to this tranche
existing: *"`RV2-DEBT-003` and `RV2-DEBT-004` are relevant because this epic
will likely touch `rt_async_state.c` and `rt_net.c`. Every task that touches
an over-limit file must record line counts. A refactor tranche is part of
this epic after the behavior proof exists."*

## Goal

Split cohesive scheduler/net responsibilities out of the over-limit files
this epic touched, reducing or at least not worsening their line counts,
following the same dependency-boundary discipline Epic 4 Task 14 used to
extract `rt_net_trace.c`/`.h` from `rt_net.c`.

## Why This Task Exists

`RULES.md` Global Rule 4 requires every task touching an over-limit file to
record whether it reduces, keeps flat, or schedules a follow-up split — this
task is where that scheduled follow-up actually happens for Epic 6's
cumulative impact, rather than being deferred again to a later epic by
default.

## Scope

- Re-measure every file Tasks 6-11 touched against its Task 1 baseline line
  count; identify the largest net increases.
- For `rt_net.c`: identify a cohesive extraction candidate from what this
  epic added — likely candidates are the listener-group/accept-distribution
  logic (Task 9) or the per-shard poller/wake logic (Task 10), following the
  same pattern Epic 4 Task 14 used to pull `TRACE_NET` into
  `rt_net_trace.c`/`.h`. Do not extract a partial dependency slice that
  leaves both files needing to `#include` each other's internals awkwardly
  — extract along the same ownership boundary this epic itself introduced
  (accept/listener vs. read/write/close is a natural seam if Task 9's
  additions are large enough to justify their own file).
- For `rt_async_state.c`: identify whether Task 7's scheduler-placement/
  no-steal/parked-with-work additions are cohesive enough to extract into
  their own file (e.g. a `rt_scheduler_placement.c` or similar, named for
  what it owns, not a vague `helpers`).
- For `rt_async_internal.h`: if Task 6/7's additions pushed it over 500
  lines, split the shard-configuration and/or task-ownership-metadata
  declarations into a focused new header, not a catch-all.
- Do not create catch-all files such as `common`, `misc`, or vague `helpers`
  (Global Rule 4, Refactor Safety Contract).
- Prove behavior is unchanged before and after each move: run the full
  `make runtime-v2-check` (including the new `runtime-v2-accept-check` from
  Task 13) before and after, and confirm identical pass/fail.
- Delete code only after proving the symbol is unreachable through
  references, build, tests, and Sentrux evidence (Refactor Safety Contract).
- Update `.loc-legacy-allowlist` for any file whose ceiling changes as a
  result (lowering a ceiling that is now met, or explicitly documenting why
  one could not be lowered this epic).

## Out Of Scope

- Any new behavior or bugfix discovered during the refactor — file it as a
  follow-up for the owning task/future epic instead of fixing it inline here
  (Refactor Safety Contract: "keep behavior changes out of refactor
  commits").
- Refactoring files this epic did not touch (e.g. `rt_term.c`, `rt_fs.c` —
  `RV2-DEBT-005` remains someone else's scope).

## Approach / Steps

1. Re-run `wc -l` on every file Tasks 6-11 touched; diff against Task 1's
   baseline.
2. For each file materially larger than baseline, record the dependency
   cluster and owning module before extraction (Refactor Safety Contract:
   "record the dependency cluster and owning module before extraction").
3. Move one responsibility at a time; run the full check suite after each
   move, not only at the end of the whole tranche.
4. Update `.loc-legacy-allowlist` for files whose ceiling is met or lowered.
5. Record rejected extraction paths in `NOTES.md` so they are not
   rediscovered later (Refactor Safety Contract's final bullet).
6. Update `06-evidence.md` and `NOTES.md`.

## Files

Touch (candidates; finalize based on actual Task 6-11 diffs):

- `runtime/native/rt_net.c` and a new extracted sibling file
- `runtime/native/rt_async_state.c` and a new extracted sibling file, if
  warranted
- `runtime/native/rt_async_internal.h` and a new focused header, if
  warranted
- `.loc-legacy-allowlist`

Read:

- `docs/runtime-v2-epics/04-tasks/14-large-file-refactor-tranche.md` (prior
  pattern)
- `docs/runtime-v2-epics/06-evidence.md` (line-count deltas recorded by
  Tasks 6-11)

## Skills & Working Practice

- Full Global Rule 9 plan gate: state the exact extraction boundary and new
  file name(s) before moving code, and get main-agent approval.
- One responsibility per move, one commit per move where practical — do not
  bundle multiple unrelated extractions into one diff, since that makes the
  before/after behavior proof harder to trust.
- If no file actually needs extraction (Tasks 6-11 stayed flat or even
  reduced line counts through careful reuse), say so plainly and close this
  task with that evidence rather than manufacturing a split for its own
  sake — Global Rule 4 requires flat-or-reduced, not "must always split."

## Checks

- `make c-check`
- `make cppcheck`
- `make runtime-v2-check` (before and after each extraction)
- `make check`
- `git diff --check`
- Sentrux root and scoped scans (before and after)
- `wc -l` before/after for every touched file

## Definition Of Done

- [ ] Every file Tasks 6-11 touched has a recorded before/after line count
      against the Task 1 baseline.
- [ ] Each materially-grown file either has a cohesive extraction reducing
      it, or an explicit recorded reason it could not be reduced this epic.
- [ ] No catch-all file was created.
- [ ] Behavior is proven unchanged before and after every move via the full
      check suite.
- [ ] `.loc-legacy-allowlist` reflects the final state accurately.
- [ ] Rejected extraction paths are recorded in `NOTES.md`.

## Evidence To Record

- `06-evidence.md`: Files Touched with full before/after line-count table,
  Commands/Checks (before/after for each move), Sentrux Root/Scoped Signals.
- `DEBT.md`: update `RV2-DEBT-003`/`RV2-DEBT-004` status if either file's
  ceiling changed.
- `NOTES.md`: rejected extraction paths and why.
