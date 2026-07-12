# Epic 14 Task Index

Epic 14 (`14-phase4-remote-channels.md`) executes as separate task documents;
the epic document stays authoritative for the boundary decisions, the
race/failure matrix, the diagnostics contract, and the stop conditions.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | Complete | map/evidence + decisions | none |
| 1.5 | `02-handle-genesis.md` | Complete | typing + runtime handle registry | 1 |
| 2 | `03-anchored-ops-runtime.md` (joint with 3) | Complete | test harness rows | 1, 1.5 |
| 3 | `03-anchored-ops-runtime.md` (joint with 2) | Complete | native runtime | 2 |
| 4 | `04-on-ch-lowering.md` | Complete | compiler + gates | 3 |
| 5 | `05-negative-matrix.md` | Complete | tests + audit | 4 |
| 5b | `05b-diagnostics-precision.md` | Complete | guard diagnostics | 4 |
| 6 | QUEUE_FULL stress, bench row, gate wiring, debt, closeout | Pending (next) | bench + CI + closeout | all |

Rules: Epic 13's task rules apply unchanged (expand only the next task,
test-first or recorded spike plan, stop conditions route findings to owners,
per-task gates incl. committed-tree Sentrux comparisons, `make check` before
completion, and the naming policy — behavior-named identifiers, no epic/task
references in code).
