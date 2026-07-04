# Epic 8 Task Index

Epic 8 is executed as separate task documents. Each task has its own scope,
files, checks, evidence, and commit boundary. Do not merge task scopes unless
`docs/runtime-v2-epics/08-task-lifecycle-lane-and-net-fairness.md` is updated
first.

Every task document is written to stand on its own: it restates the current
runtime state it depends on (with exact `file:line` evidence) instead of
assuming the reader has the whole epic document memorized. The epic document
remains the authoritative source for the Boundary Decisions, Proof And
Quality Contract, and Performance Contract; task documents quote the parts
they need and point back to it for the rest.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff-baseline-and-sentrux.md` | Complete | evidence | none |
| 2 | `02-lifecycle-dependency-map.md` | Complete | design map | 1 |
| 3 | `03-lifecycle-lane-proving-spike.md` | Complete | proving spike | 1, 2 |
| 4 | `04-lifecycle-behavior-contract-tests.md` | Pending | test writing | 1, 2, 3 |
| 5 | `05-lifecycle-static-shape-and-trace-tests.md` | Pending | test/static checks | 1, 2, 3 |
| 6 | `06-task-create-and-table-publication.md` | Pending | runtime code | 3, 4, 5 |
| 7 | `07-join-poll-and-handle-lifetime.md` | Pending | runtime code | 6 |
| 8 | `08-completion-epilogue-and-done-path.md` | Pending | runtime code | 7 |
| 9 | `09-scope-owner-lane.md` | Pending | runtime code | 7, 8 |
| 10 | `10-await-runner-blocking-compat.md` | Pending | runtime code | 8, 9 |
| 11 | `11-net-fairness-starvation-investigation.md` | Pending | investigation/runtime code | 1, 3 (may start after Task 3; must not share C write sets with Tasks 6-10 in flight) |
| 12 | `12-performance-benchmark-and-ci-gate.md` | Pending | trace/benchmark/CI | 10, 11 |
| 13 | `13-large-file-and-quality-tranche.md` | Pending | refactor | 10, 11, 12 |
| 14 | `14-epic-closeout.md` | Pending | closeout | all |

## Rules

- Expand only the next task before execution; do not pre-implement later
  tasks.
- Every runtime-code task must have a preceding or same-epic behavior proof
  (Task 4) or static proof (Task 5) that describes the property it
  implements.
- Every intermediate commit state must hold the Epic 7 lane contract:
  control lane before at most one shard lock; never shard lock then control
  lock; never two shard locks. The lane assertions in `rt_lane.c` stay always
  on. A lifecycle path may drop the control lane only in the same commit that
  proves its new owner-lane guardian (tests plus static gate), mirroring the
  Epic 7 additive-then-peel shape.
- Task lifetime rules (lookup, result visibility, handle release, final free)
  must be written in the spike or the task document before any commit relies
  on atomics or unlocked reads for them.
- Refactor tasks (Task 13) must prove behavior before and after the move.
- Dead-code deletion requires reference, build, test, and Sentrux evidence.
- Every task updates `docs/runtime-v2-epics/08-evidence.md` (created by
  Task 1) and `docs/runtime-v2-epics/NOTES.md`.
- Every successfully closed task gets its own commit unless two docs-only
  tasks are explicitly merged in `NOTES.md`.
- Any subagent assigned to implement, test, audit, or review a task must
  first return a plan and wait for main-agent approval (`RULES.md` Global
  Rule 9). If subagents are unavailable, record that in the task evidence and
  proceed in the main session.
- Tasks 2 and 3 may be planned together, but Task 3's spike output rewrites
  Task 2's lane table on conflict; reconcile both before Tasks 4 and 5 start.
  Tasks 4 and 5 may run in parallel with each other. Tasks 6-10 stay strictly
  sequenced. Task 11 is independent after Task 3 and may interleave, but its
  C write set must stay disjoint from the lifecycle task in flight; if the
  investigation implicates lifecycle control traffic itself, fold the finding
  into the owning lifecycle task instead of patching from Task 11.
- `RV2-DEBT-018` (rare ~2.3ms exit=1 empty-output harness transient) may
  appear in full gates: rerun-to-green is acceptable only with a focused
  rerun (count>=5) proving the failure is not reproducible, recorded in the
  task evidence.
