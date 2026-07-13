# Epic 17 Task Index

Epic 17 (`17-remote-select.md`) executes as separate task documents; the
epic document stays authoritative for the resolved model (C, owner-side
proxy selector), the 16-row acceptance race matrix, the recorded B-vs-A
tail divergence, and the lift path.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | Complete (commit 4453833b) | evidence re-pin + contract clause + sema surface design + code-space reservations | none |
| 2 | `02-proxy-selector-vertical.md` | Complete (commits 0cfe9c09, 82e09ea1, 9b19600d) | native runtime, test-first | 1 |
| 3 | detector chain-collapse (rows 15-16; suspect scan covers select bodies, panic names the select shape) | Complete | native runtime + adversarial rows | 2 |
| 4 | `04-sema-lowering-e2e.md` | In progress (design locked) | compiler + gates | 1, 2 |
| 5 | stabilization (selector lifecycle seam) + bench + closeout | Pending | all | all |

Rules: the established task rules apply unchanged (expand only the next
task, test-first rows, per-task gates incl. committed-tree Sentrux
comparisons, `make check` before completion, naming policy, gatecheck
will demand gate wiring for every new tagged test).
