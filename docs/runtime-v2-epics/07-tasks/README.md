# Epic 7 Task Index

Epic 7 is executed as separate task documents. Each task has its own scope,
files, checks, evidence, and commit boundary. Do not merge task scopes unless
`docs/runtime-v2-epics/07-executor-lock-split-and-shard-runtime-state.md` is
updated first.

Every task document is written to stand on its own: it restates the current
runtime state it depends on (with exact `file:line` evidence) instead of
assuming the reader has the whole epic document memorized. The epic document
remains the authoritative source for the Lock Ownership Contract, Proof And
Quality Contract, Performance Contract, and Refactor Safety Contract; task
documents quote the parts they need and point back to it for the rest.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff-baseline-and-sentrux.md` | Complete | evidence | none |
| 2 | `02-executor-lock-dependency-map.md` | Pending | design map | 1 |
| 3 | `03-locking-model-proving-spike.md` | Pending | proving spike | 1, 2 |
| 4 | `04-lock-split-behavior-contract-tests.md` | Pending | test writing | 1, 2, 3 |
| 5 | `05-lock-split-static-shape-tests.md` | Pending | test/static checks | 1, 2, 3 |
| 6 | `06-shard-lock-structure-landing.md` | Pending | runtime code | 3, 5 |
| 7 | `07-scheduler-ready-and-park-wake-migration.md` | Pending | runtime code | 4, 6 |
| 8 | `08-waiter-store-key-ownership-migration.md` | Pending | runtime code | 7 |
| 9 | `09-sleep-timer-store-and-virtual-clock.md` | Pending | runtime code | 7, 8 |
| 10 | `10-channel-owner-shard-migration.md` | Pending | runtime code | 8 |
| 11 | `11-blocking-await-shutdown-lanes.md` | Pending | runtime code | 8, 9, 10 |
| 12 | `12-lock-split-trace-counters-and-benchmarks.md` | Pending | trace/benchmark | 11 |
| 13 | `13-runtime-v2-lock-ci-gate.md` | Pending | CI | 4, 5, 12 |
| 14 | `14-large-file-and-loc-tranche.md` | Pending | refactor | 11, 12, 13 |
| 15 | `15-epic-closeout.md` | Pending | closeout | all |

## Rules

- Expand only the next task before execution; do not pre-implement later
  tasks.
- Every runtime-code task must have a preceding or same-epic behavior proof
  (Task 4) or static proof (Task 5) that describes the property it
  implements.
- Every intermediate commit state must hold the full lock-order contract; the
  only sanctioned transition shape is the nested one (shard lock acquired
  under the global lock, one consistent order) from the epic's Refactor
  Safety Contract.
- Refactor tasks (Task 14) must prove behavior before and after the move.
- Dead-code deletion requires reference, build, test, and Sentrux evidence.
- Every task updates `docs/runtime-v2-epics/07-evidence.md` (created by
  Task 1) and `docs/runtime-v2-epics/NOTES.md`.
- Every successfully closed task gets its own commit unless two docs-only
  tasks are explicitly merged in `NOTES.md`.
- Any subagent assigned to implement, test, audit, or review a task must
  first return a plan and wait for main-agent approval (`RULES.md` Global
  Rule 9). If subagents are unavailable, record that in the task evidence and
  proceed in the main session, as Epic 6 Task 14 did.
- Tasks 2 and 3 may be planned together, but Task 3's spike output rewrites
  Task 2's lane table on conflict; reconcile both before Tasks 4 and 5 start.
  Tasks 4 and 5 may run in parallel with each other. Tasks 6-11 stay strictly
  sequenced. Task 12 onward may overlap the tail of Task 11 only with
  disjoint write sets (counters/benchmark scripts vs. runtime C lanes).
