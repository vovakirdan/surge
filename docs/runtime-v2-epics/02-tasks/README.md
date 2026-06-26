# Epic 2 Task Index

Epic 2 is executed as separate task documents. Each task has its own scope,
files, steps, checks, and commit boundary. Do not merge task scopes unless the
epic document is updated first.

## Task Order

| Task | File | Status | Kind |
| --- | --- | --- | --- |
| 1 | `01-kickoff-evidence.md` | Draft | evidence |
| 2 | `02-field-ownership-map.md` | Draft | design map |
| 3 | `03-runtime-v2-ci-test-contract.md` | Draft | test/CI design |
| 4 | `04-runtime-shard-skeleton-tests.md` | Draft | test writing |
| 5 | `05-runtime-shard-skeleton.md` | Draft | runtime code |
| 6 | `06-scheduler-shape-tests.md` | Draft | test writing |
| 7 | `07-scheduler-shape-migration.md` | Draft | runtime code |
| 8 | `08-net-poll-scratch-tests.md` | Draft | test writing |
| 9 | `09-net-poll-scratch-migration.md` | Draft | runtime code |
| 10 | `10-channel-blocking-compat-tests.md` | Draft | test writing |
| 11 | `11-channel-blocking-compat-migration.md` | Draft | runtime code |
| 12 | `12-ci-runtime-v2-gates.md` | Draft | CI |
| 13 | `13-accessor-cleanup-and-static-gates.md` | Draft | cleanup/static gates |
| 14 | `14-epic-closeout.md` | Draft | closeout |

## Rules

- Every runtime-code task must have a preceding or same-epic test-writing task.
- Tests added in this epic must be stable enough to run locally with explicit
  timeout wrappers before they are added to CI.
- CI must not add the broad accepted-debt command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required green gate.
- CI may add only named, proven Runtime V2 liveness subsets or a new dedicated
  target whose local command is recorded in task evidence.
- Every successfully closed task gets its own commit unless two docs-only tasks
  are explicitly merged in `NOTES.md`.
