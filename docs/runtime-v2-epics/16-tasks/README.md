# Epic 16 Task Index

Epic 16 (`16-far-copy.md`) executes as separate task documents; the epic
document stays authoritative for the resolved sharing model, the race
matrix, the failure-mode ledger, and the stop conditions.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | In progress | evidence + lease-table design note | none |
| 2 | lease-table runtime + share/release rows | Pending | native runtime, test-first | 1 |
| 3 | self-deadlock detector re-grounding | Pending | native runtime + adversarial rows | 2 |
| 4 | sema share() surface + moved-handle hint | Pending | sema + diagnostics | 1 |
| 5 | lowering + capability + fan-out e2e + FIFO rows | Pending | compiler + gates | 2, 3, 4 |
| 6 | force-close capability (slice-2 review stub) | Pending | design review first | 2 |
| 7 | bench + gates + debt + closeout | Pending | all | all |

Rules: the established task rules apply unchanged (expand only the next
task, test-first rows, per-task gates incl. committed-tree Sentrux
comparisons, `make check` before completion, naming policy, gatecheck
will demand gate wiring for every new tagged test).
