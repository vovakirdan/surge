# Epic/Task Reference Cleanup Plan

**Status:** planned, not started. This document is the single owner of the
cleanup; no partial renames outside it.
**Rule that triggered it:** planning artifacts (epic/task documents) are
transient — they will be archived or deleted long before this code stops
mattering. A comment that says "added in Epic 3 Task 2" or an identifier named
`task10_exec_state` tells a future reader nothing. Comments must state the
invariant or behavior itself, self-contained; test fixtures get a
behavior-describing legend, never a task number.

## Naming Policy (effective immediately for new code)

1. **Comments** explain WHAT holds and WHY the mechanism is shaped this way.
   Never which epic/task introduced it. If a comment is only "who added this
   and when" — delete it; git blame answers that.
2. **Identifiers** (functions, types, constants, fixture files, test names)
   describe behavior: what is exercised, what is proven. Harness prefixes come
   from a prepared legend (see below), not from task numbers.
3. **Allowed durable references:**
   - `RV2-DEBT-NNN` — the debt ledger (`DEBT.md`) is a durable, maintained
     document; pointers to it from code are actionable. Write them WITHOUT the
     epic suffix: "(RV2-DEBT-023)" not "(RV2-DEBT-023, Epic 9)".
   - Sync-point names — already behavior-named
     (`SP_IMMEDIATE_ON_BEFORE_PUBLISH`); keep.
   - Spec row IDs (`ON-GATE-N001`, `FAR-OWN-008`, `L02`…) — allowed ONLY
     after the owning matrix is promoted into a durable spec document
     (step C3 below); until then treat as epic references.
   - Epic/task numbers stay in `docs/` (NOTES.md, task docs, DEBT.md history
     columns) and in commit messages — those ARE the planning record.

## Inventory (as of commit `e7c375b2`, 2026-07-10)

| Cluster | Where | Count | Kind |
| --- | --- | --- | --- |
| A1 | `runtime/native/*.{c,h}` comments | ~77 mentions | "Epic N [Task M]" in prose comments, incl. file headers (`rt_task_complete.c`, `rt_immediate_on.c`, `rt_sync_point.h` enum docs) |
| A2 | `internal/**/*.go` non-test comments | ~25 mentions | same, in compiler/buildpipeline (`crossing_transport.go`, `crossing_lowering.go`, `on_crossing_check.go`…) |
| B1 | `internal/vm/testdata/remote_task_behavior*` | ~229 uses | `task9_*` / `task10_*` / `POLL_TASK9_*` / `POLL_TASK10_*` identifiers |
| B2 | test function names wired into Makefile | ~10 funcs | `TestEpic11Block2On`, `TestEpic12IntegrationFixtures`, `TestRuntimeV2HeapAccountingStaticTask5SkeletonShape` (+Task6/Task7), etc. |
| B3 | other `_test.go` comments | ~50 mentions | prose only, not load-bearing |
| C1 | `Makefile` + `check_sync_points.sh` | ~9 mentions | `@echo` gate banners "(Epic 13 Task 9)" and comments |
| C2 | `testdata/golden/crossing/**/*.sg` headers | ~435 files | `// Epic 11 Block 2 negative: … (ON-GATE-N001)` header comments |
| C3 | spec row IDs in fixtures/tests | subset of C2/B3 | `ON-GATE-N001` etc. — currently defined only inside Epic 11 task docs |

## Fixture Legend (prepared up front, use for renames and all new code)

The behavior harness family is `remote_task_behavior_*` — the legend prefix is
`rtb_` (remote task behavior). Poll-function IDs use `POLL_RTB_*`.

| Current | New |
| --- | --- |
| `task9_fail` / `task9_sleep_us` / `task9_wait_u32` / `task9_drain` / `task9_wake` / `task9_await` | `rtb_fail` / `rtb_sleep_us` / `rtb_wait_u32` / `rtb_drain` / `rtb_wake` / `rtb_await` |
| `task9_child_state`, `task9_publish_state`, `task9_lifecycle_state` | `rtb_child_state`, `rtb_publish_state`, `rtb_lifecycle_state` |
| `task10_exec_state` | `rtb_execute_state` |
| `POLL_TASK9_CHILD` / `_PUBLISHER` / `_LIFECYCLE`, `POLL_TASK10_EXEC` | `POLL_RTB_CHILD` / `_PUBLISHER` / `_LIFECYCLE`, `POLL_RTB_EXECUTE` |
| `task9_mode_*` / `task10_mode_*` | `rtb_mode_*` (CLI mode strings are already behavior-named: `already-done`, `immediate-cancel-race`, … — keep) |
| `TestRuntimeV2HeapAccountingStaticTask5SkeletonShape` (+Task6/7) | name by shape being pinned: `…StaticShardCellSkeletonShape`, `…StaticRecordMigrationShape`, `…StaticSnapshotAggregationShape` |
| `TestEpic11Block1Far` … `TestEpic11Block4Contracts` | `TestCrossingFixturesFarHandles`, `TestCrossingFixturesOnPlacement`, `TestCrossingFixturesSpawnOn`, `TestCrossingFixturesContracts` |
| `TestEpic12IntegrationFixtures` | `TestCrossingGuardIntegrationFixtures` |

## Comment Rewrite Rule (with real examples)

Rewrite = state the surviving fact, drop the provenance.

- Before: `// Task completion and cancellation (Epic 10 Task 2, RV2-DEBT-003
  split): this module owns…`
  After: `// Task completion and cancellation: this module owns the terminal
  task transitions…` (the lane contract prose that follows already carries
  the content; keep it).
- Before: `// Wake-token ordering rule (RV2-DEBT-023, Epic 9). The cancelled
  flag…`
  After: `// Wake-token ordering rule (RV2-DEBT-023). The cancelled flag…`
- Before: `// Immediate 'on placement' execute/reply vertical (Epic 13 Task
  10): one request, one reply…`
  After: `// Immediate 'on placement' execute/reply: one request, one reply,
  one request-scoped cancellation token, no publicly observable far Task
  handle…` (the contract sentence is the comment; the task number added
  nothing).
- A comment that becomes empty after dropping the epic reference was
  provenance-only — delete it outright.

## Execution Plan

Ordering recommendation: run this as ONE standalone cleanup change (single
commit, no behavior changes) AFTER Epic 13 Task 12 closes, BEFORE the
Backend/Test Matrix Cleanup epic starts — Task 11/12 still quote existing
gate names in their docs, and the matrix epic will want the final names.
Steps B2/C1 must land together (test names are wired into Makefile `-run`
patterns).

1. **A1+A2 (production comments)** — manual rewrite per the rule above.
   Mechanical grep list: `grep -rn "Epic [0-9]" --include='*.c' --include='*.h'
   runtime/native/`; same for `internal/` non-test. Where a comment cites
   "(Task N decision M)", inline the decision content itself (one sentence).
2. **B1 (harness identifiers)** — `sed`-able rename per the legend table
   across the six `remote_task_behavior*` files; also rename the two
   header-declared struct typedefs and re-run clang-format.
3. **B2+C1 (gate-wired test names + Makefile)** — rename test functions and
   update the exact `-run '^…$'` patterns in `Makefile`
   (`runtime-v2-crossing-check`, `runtime-v2-heap-check`,
   `runtime-v2-transport-contract-check`) in the same commit. Rewrite the
   `@echo` banners to describe the gate, not the task ("remote task
   acceptance gate", "immediate-on acceptance gate" — drop the parentheses).
4. **B3 (test prose comments)** — same rewrite rule, low priority, can ride
   along with 1.
5. **C2+C3 (golden fixtures + spec IDs)** — two options, decide at execution
   time: (a) promote the Epic 11 fixture matrix (row IDs -> behavior
   description) into a durable `docs/crossing-fixture-matrix.md` and keep the
   IDs in fixture headers as pointers into it; or (b) rewrite each fixture
   header to a self-contained behavior sentence and drop the IDs. Option (a)
   is cheaper (435 files keep their IDs) and keeps golden diffs to comments
   only. Either way `.sg` header comments must drop the "Epic 11 Block N"
   prefix. Golden `.ast`/diag snapshots do not embed the header text (verify
   with one probe fixture before the sweep; if they do, `make golden-update`
   is part of this step and its diff must be comment-only).

## Full Regression (gates for the cleanup commit)

The change must be behavior-neutral; the proof is the full gate set both
epics used, plus the golden suite:

1. `git diff --check`
2. `go build ./...`, `make lint` (`golangci-lint`), `gofmt -l internal/`
3. `make c-check` (includes clang-format check), `make cppcheck`
4. `./check_sync_points.sh` (sync-point allowlist untouched)
5. `./check_file_sizes.sh -a`
6. `make runtime-v2-crossing-check` — twice (updated `-run` patterns must
   still select non-empty test sets: verify each renamed pattern with
   `go test -run … -v | grep -c RUN` > 0; an empty match is a silent gate
   hole)
7. `make runtime-v2-transport-contract-check` (Tasks 4+9+10 sections with the
   renamed banners/tests)
8. `make runtime-v2-check` (full runtime gate family — heap-check contains
   two renamed static tests)
9. Behavior harness direct sweep: all `remote_task_behavior` modes with
   `SURGE_SHARDS=2 SURGE_THREADS=2` (plus the sync-point-armed cancel-race
   mode), via `go test -tags runtime_v2_pending ./internal/vm -run
   TestRuntimeV2RemoteTaskBehavior`
10. `make golden-check`; if step 5 chose the header rewrite: `make
    golden-update` first, then assert the golden diff contains ONLY comment
    lines
11. `make check` (full)
12. Sentrux CLI on the committed tree, all four scopes, compared against the
    committed-tree baselines current at execution time (comment/rename-only
    changes should be quality-neutral; a drop means something more than
    comments moved — stop and inspect)
13. Grep exit criteria (the "done" definition):
    `grep -rn "Epic [0-9]" --include='*.c' --include='*.h' --include='*.go'
    runtime/ internal/` returns 0 matches;
    `grep -rn "task9_\|task10_\|TASK9\|TASK10" internal/ runtime/` returns 0;
    `grep -rn "Task [0-9]" --include='*.go' --include='*.c' --include='*.h'
    runtime/ internal/ | grep -v RV2-DEBT` returns only hits whose context is
    the word "task" in its ordinary meaning (manual review of the residue).

## Non-Goals

- `docs/runtime-v2-epics/**` keeps all epic/task references — it is the
  planning record itself.
- Commit history is immutable; no rewrites.
- `RV2-DEBT-NNN` pointers stay (ledger is durable); only their ", Epic N"
  suffixes go.
