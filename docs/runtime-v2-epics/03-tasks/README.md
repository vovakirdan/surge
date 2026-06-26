# Epic 3 Task Index

Epic 3 is executed as separate task documents. Each task has its own scope,
files, checks, evidence, and commit boundary. Do not merge task scopes unless
the epic document is updated first.

## Task Order

| Task | File | Status | Kind |
| --- | --- | --- | --- |
| 1 | `01-kickoff-baseline-and-sentrux.md` | Complete | evidence |
| 2 | `02-waiter-dependency-map.md` | Complete | design map |
| 3 | `03-refactor-dependency-and-dead-code-audit.md` | Complete | refactor audit |
| 4 | `04-waiter-behavior-contract-tests.md` | Complete | test writing |
| 5 | `05-waiter-module-extraction-tests.md` | Complete | test/static checks |
| 6 | `06-extract-waiter-key-list-helpers.md` | Complete | refactor code |
| 7 | `07-owner-local-waiter-skeleton-tests.md` | Draft | test writing |
| 8 | `08-owner-local-waiter-skeleton.md` | Draft | runtime code |
| 9 | `09-channel-waiter-tests.md` | Draft | test writing |
| 10 | `10-channel-waiter-migration.md` | Draft | runtime code |
| 11 | `11-task-scope-blocking-waiter-tests.md` | Draft | test writing |
| 12 | `12-task-scope-blocking-waiter-migration.md` | Draft | runtime code |
| 13 | `13-timer-select-cancellation-tests.md` | Draft | test writing |
| 14 | `14-timer-select-cancellation-migration.md` | Draft | runtime code |
| 15 | `15-net-waiter-tests-and-trace-contract.md` | Draft | test/trace writing |
| 16 | `16-net-waiter-migration.md` | Draft | runtime code |
| 17 | `17-large-file-refactor-tranche.md` | Draft | refactor code |
| 18 | `18-runtime-v2-waiter-ci-gates.md` | Draft | CI |
| 19 | `19-epic-closeout-and-static-gates.md` | Draft | closeout |

## Rules

- Expand only the next task before execution.
- Every runtime-code task must have a preceding or same-epic behavior proof.
- Refactor tasks must prove behavior before and after the move.
- Dead-code deletion requires reference, build, test, and Sentrux evidence.
- Every task updates `03-evidence.md` and `NOTES.md`.
- Every successfully closed task gets its own commit unless two docs-only tasks
  are explicitly merged in `NOTES.md`.
- Any subagent assigned to implement, test, audit, or review a task must first
  return a plan and wait for approval.
