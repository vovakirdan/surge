# Epic 15 Task Index

Epic 15 (`15-structural-cleanup.md`) executes as separate task documents;
the epic document stays authoritative for the boundary decisions, the
review records, and the stop conditions.

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff-attribution.md` | Complete | evidence + decision table + move inventory | none |
| 1b | `01b-liveness-precondition.md` | Complete | diagnostics + TSan stress + overlap review | 1 |
| 2 | `02-gate-integrity.md` | Complete | tooling + make check step | 1b |
| 3 | structural pass (all enforced scopes) | In progress | refactor, per-row dispositions | 1, 1b |
| 3r | remeasure noise bands post-cleanup | Pending | measurement | 3 |
| 4 | threshold re-baseline + SENTRUX_POLICY | Pending | policy + debt closure | 3r |
| 5 | naming remainder (allowlist -> C3 -> C2) | Pending | docs + fixtures | none (after 2 preferred) |
| 6 | closeout (clean clone, symbol census) | Pending | release invariants | all |

Rules: Epic 13/14 task rules apply unchanged (expand only the next task,
behavior-neutral proof per task — full gate set green before and after,
goldens comment-only, committed-tree Sentrux comparisons, `make check`
before completion, naming policy in force).
