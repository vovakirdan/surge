# Epic 17 Task Index

Epic 17 (`17-remote-select.md`) executes as separate task documents; the
epic document stays authoritative for the resolved model (C, owner-side
proxy selector), the 16-row acceptance race matrix, the recorded B-vs-A
tail divergence, and the lift path.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | In progress | evidence re-pin + contract clause + sema surface design + code-space reservations | none |
| 2 | proxy-selector runtime vertical (matrix rows 1-14) | Pending | native runtime, test-first | 1 |
| 3 | detector chain-collapse (matrix rows 15-16) | Pending | native runtime + adversarial rows | 2 |
| 4 | sema + lowering + capability + e2e | Pending | compiler + gates | 1, 2 |
| 5 | stabilization (selector lifecycle seam) + bench + closeout | Pending | all | all |

Rules: the established task rules apply unchanged (expand only the next
task, test-first rows, per-task gates incl. committed-tree Sentrux
comparisons, `make check` before completion, naming policy, gatecheck
will demand gate wiring for every new tagged test).
