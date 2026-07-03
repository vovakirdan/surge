# Epic 6 Task Index

Epic 6 is executed as separate task documents. Each task has its own scope,
files, checks, evidence, and commit boundary. Do not merge task scopes unless
`docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md` is
updated first.

Every task document is written to stand on its own: it restates the current
runtime state it depends on (with exact `file:line` evidence) instead of
assuming the reader has the whole epic document memorized. The epic document
remains the authoritative source for the full Accept Ownership Contract,
Performance Contract, and Refactor Safety Contract; task documents quote the
parts they need and point back to it for the rest.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff-baseline-and-sentrux.md` | Complete | evidence | none |
| 2 | `02-accept-ownership-dependency-map.md` | Complete | design map | 1 |
| 3 | `03-listener-model-proving-spike.md` | Complete | proving spike | 1, 2 |
| 4 | `04-multishard-accept-contract-tests.md` | Complete | test writing | 1, 2, 3 |
| 5 | `05-multishard-static-shape-tests.md` | Complete | test/static checks | 1, 2, 3 |
| 6 | `06-runtime-shard-array-and-config.md` | Complete | runtime code | 1, 2, 3, 5 |
| 7 | `07-per-shard-scheduler-placement.md` | Complete | runtime code | 6 |
| 8 | `08-listener-and-connection-owner-metadata.md` | Complete | runtime code | 6 |
| 9 | `09-accept-distribution-implementation.md` | Complete | runtime code | 3, 6, 7, 8 |
| 10 | `10-per-shard-poller-and-wake-ownership.md` | Complete | runtime code | 6, 7 |
| 11 | `11-multishard-net-lifecycle-migration.md` | Complete | runtime code | 4, 8, 9, 10 |
| 12 | `12-trace-counters-and-benchmark-evidence.md` | Complete | trace/benchmark | 9, 10, 11 |
| 13 | `13-runtime-v2-accept-ci-gates.md` | Draft | CI | 4, 5, 12 |
| 14 | `14-large-file-refactor-tranche.md` | Draft | refactor code | 11, 12, 13 |
| 15 | `15-epic-closeout-and-static-gates.md` | Draft | closeout | all |

## Rules

- Expand only the next task before execution; do not pre-implement later tasks.
- Every runtime-code task must have a preceding or same-epic behavior proof
  (Task 4) or static proof (Task 5) that describes the property it implements.
- Refactor tasks (Task 14) must prove behavior before and after the move.
- Dead-code deletion requires reference, build, test, and Sentrux evidence.
- Every task updates `docs/runtime-v2-epics/06-evidence.md` (created by Task 1)
  and `docs/runtime-v2-epics/NOTES.md`.
- Every successfully closed task gets its own commit unless two docs-only tasks
  are explicitly merged in `NOTES.md`.
- Any subagent assigned to implement, test, audit, or review a task must first
  return a plan and wait for main-agent approval (`RULES.md` Global Rule 9).
- Tasks 2 and 3 may be planned in parallel once Task 1 lands, but Task 3's
  spike result can change Task 2's dependency map; reconcile both before
  Task 4/5 start. Tasks 4 and 5 may be planned in parallel with each other and
  with Task 3, but must not assert behavior the spike later contradicts.
  Tasks 6-11 are implementation tasks and stay sequenced: each changes shared
  runtime structures the next one depends on. Task 12 onward may overlap with
  the tail of Task 11 only if their write sets stay disjoint (trace counters
  and benchmark scripts vs. net lifecycle C code).
