# Epic 4 Task Index

Epic 4 is executed as separate task documents. Each task has its own scope,
files, checks, evidence, and commit boundary. Do not merge task scopes unless
the epic document is updated first.

## Task Order

| Task | File | Status | Kind |
| --- | --- | --- | --- |
| 1 | `01-kickoff-baseline-and-sentrux.md` | Draft | evidence |
| 2 | `02-fd-registry-dependency-map.md` | Draft | design map |
| 3 | `03-fd-lifecycle-contract-tests.md` | Draft | test writing |
| 4 | `04-registry-static-shape-tests.md` | Draft | test/static checks |
| 5 | `05-registry-container-skeleton.md` | Draft | runtime code |
| 6 | `06-net-wait-registration.md` | Draft | runtime code |
| 7 | `07-poll-from-registry.md` | Complete | runtime code |
| 8 | `08-close-cancel-and-reregister-tests.md` | Complete | test writing |
| 9 | `09-close-cancel-and-reregister-migration.md` | Complete | runtime code |
| 10 | `10-wake-fd-and-shutdown-tests.md` | Complete | test writing |
| 11 | `11-wake-fd-and-shutdown-migration.md` | Draft | runtime code |
| 12 | `12-trace-counters-and-benchmark-contract.md` | Draft | trace/benchmark |
| 13 | `13-runtime-v2-fd-registry-ci-gates.md` | Draft | CI |
| 14 | `14-large-file-refactor-tranche.md` | Draft | refactor code |
| 15 | `15-epic-closeout-and-static-gates.md` | Draft | closeout |

## Rules

- Expand only the next task before execution.
- Every runtime-code task must have a preceding or same-epic behavior proof.
- Refactor tasks must prove behavior before and after the move.
- Dead-code deletion requires reference, build, test, and Sentrux evidence.
- Every task updates `04-evidence.md` and `NOTES.md`.
- Every successfully closed task gets its own commit unless two docs-only tasks
  are explicitly merged in `NOTES.md`.
- Any subagent assigned to implement, test, audit, or review a task must first
  return a plan and wait for approval.
