# Epic 19 Task Index

Epic 19 (`19-drop-emission.md`) executes as separate task documents;
the epic document stays authoritative for the resolved semantics
(scope-exit drops), the partial-move boundary, the reassignment order,
the @raii disposition, and the loop/shadowing row list.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff.md` | Complete | semantics record + free-helper design + census design + ATTRIBUTES.md changes + baselines | none |
| 2 | runtime + backend floor: per-type frees, explicit-@drop leaf emission, census harness | Pending | runtime + backend, test-first | 1 |
| 3 | scope-exit synthesis on leaf types (+ partial-move rejection, reassignment order, loop/shadowing rows) | Pending | sema + HIR, test-first | 2 |
| 4 | recursive glue + census e2e at SHARDS=1/2/8 | Pending | compiler + gates | 3 |
| 5 | bench + debt + closeout (vertical 2 scoped) | Pending | all | all |

Rules: the established task rules apply unchanged (expand only the next
task, test-first rows, per-task gates incl. committed-tree Sentrux
comparisons, `make check` before completion, naming policy, gatecheck
will demand gate wiring for every new tagged test).
