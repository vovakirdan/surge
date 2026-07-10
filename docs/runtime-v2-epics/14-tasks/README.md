# Epic 14 Task Index

Epic 14 (`14-phase4-remote-channels.md`) executes as separate task documents;
the epic document stays authoritative for the boundary decisions, the
race/failure matrix, the diagnostics contract, and the stop conditions.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | Complete | map/evidence + decisions | none |
| 1.5 | `02-handle-genesis.md` | Complete | typing + runtime handle registry | 1 |
| 2 | behavior rows (race/failure matrix) | Pending | test harness rows | 1, 1.5 |
| 3 | runtime: anchor resolve + close/teardown wake obligations | Pending | native runtime | 2 |
| 4 | lowering + capability flip + guard matrix | Pending | compiler + gates | 3 |
| 5 | negative matrix + payload negatives + hidden-fallback audit | Pending | tests + audit | 4 |
| 5b | diagnostics precision pass (crossing family) | Pending | sema diagnostics | 4 |
| 6 | QUEUE_FULL stress, bench row, gate wiring, debt, closeout | Pending | bench + CI + closeout | all |

Rules: Epic 13's task rules apply unchanged (expand only the next task,
test-first or recorded spike plan, stop conditions route findings to owners,
per-task gates incl. committed-tree Sentrux comparisons, `make check` before
completion, and the naming policy — behavior-named identifiers, no epic/task
references in code).
