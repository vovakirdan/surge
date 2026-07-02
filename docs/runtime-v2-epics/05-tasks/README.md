# Epic 5 Task Index

Epic 5 is executed as separate task documents. Each task has its own scope,
files, checks, evidence, and commit boundary. Do not merge task scopes unless
the epic document is updated first.

## Task Order

| Task | File | Status | Kind |
| --- | --- | --- | --- |
| 1 | `01-kickoff-baseline-and-sentrux.md` | Draft | evidence |
| 2 | `02-heap-accounting-dependency-map.md` | Draft | design map |
| 3 | `03-heap-stats-contract-tests.md` | Draft | test writing |
| 4 | `04-heap-accounting-static-shape-tests.md` | Draft | test/static checks |
| 5 | `05-accounting-cell-skeleton.md` | Draft | runtime code |
| 6 | `06-alloc-free-realloc-accounting-migration.md` | Draft | runtime code |
| 7 | `07-heap-stats-aggregation.md` | Draft | runtime code |
| 8 | `08-concurrency-and-performance-evidence.md` | Draft | evidence/benchmark |
| 9 | `09-runtime-v2-heap-ci-gates.md` | Draft | CI |
| 10 | `10-epic-closeout-and-static-gates.md` | Draft | closeout |

## Rules

- Expand only the next task before execution.
- Every runtime-code task must have a preceding or same-epic behavior proof.
- Refactor tasks must prove behavior before and after the move.
- Dead-code deletion requires reference, build, test, and Sentrux evidence.
- Every task updates `05-evidence.md` and `NOTES.md`.
- Every successfully closed task gets its own commit unless two docs-only tasks
  are explicitly merged in `NOTES.md`.
- Any subagent assigned to implement, test, audit, or review a task must first
  return a plan and wait for approval.
