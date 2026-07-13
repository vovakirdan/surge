# Epic 18 Task Index

Epic 18 (`18-migration.md`) executes as separate task documents; the
epic document stays authoritative for the resolved surface (Model A,
capture lift), the 21-row exactly-once-drop matrix, its live-remote-
owner organizing axis, and the recorded tails (owned results, far data
handles, fallible move).

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | Complete (commit 87db8776) | evidence re-pin + drop-metadata design + sema surface design | none |
| 2 | `02-drop-plumbing.md` | Complete | native runtime + compiler drop dispatch, test-first | 1 |
| 3 | runtime vertical B: remote-owner rows (10-18) + cross-cutting (19-21) | Pending | native runtime, test-first | 2 |
| 4 | sema + guard flip + e2e | Pending | compiler + gates | 1, 2, 3 |
| 5 | bench + debt + closeout | Pending | all | all |

Rules: the established task rules apply unchanged (expand only the next
task, test-first rows, per-task gates incl. committed-tree Sentrux
comparisons, `make check` before completion, naming policy, gatecheck
will demand gate wiring for every new tagged test).
