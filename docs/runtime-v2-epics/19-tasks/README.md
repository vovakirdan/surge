# Epic 19 Task Index

Epic 19 (`19-drop-emission.md`) executes as separate task documents;
the epic document stays authoritative for the resolved semantics
(scope-exit drops), the partial-move boundary, the reassignment order,
the @raii disposition, and the loop/shadowing row list.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | Complete | semantics record + free-helper design + census design + ATTRIBUTES.md changes + baselines | none |
| 2 | `02-leaf-free-floor.md` | Complete | runtime + backend floor: per-type frees, explicit-@drop leaf emission, drop-consumes sema, free-count harness | 1 |
| 3 | `03-scope-exit-synthesis.md` | Complete | scope-exit synthesis + statement-end temporaries; partial-move and loop back-edge rejections; reassignment order; first balance windows | 2 |
| 4 | `04-recursive-glue.md` | Complete | compiler + gates: recursive composite glue (A+B+C shipped; crossing census moved to Epic 20) | 3 |
| 5 | bench + debt + closeout (vertical 2 scoped) | Reassigned | executes as Epic 20 Task 1 (`20-crossing-drop-activation.md`) | all |

Rules: the established task rules apply unchanged (expand only the next
task, test-first rows, per-task gates incl. committed-tree Sentrux
comparisons, `make check` before completion, naming policy, gatecheck
will demand gate wiring for every new tagged test).
