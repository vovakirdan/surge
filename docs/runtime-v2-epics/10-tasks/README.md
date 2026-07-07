# Epic 10 Task Index

Epic 10 (`10-runtime-debt-burndown-and-owner-safety.md`) is executed as separate
task documents. Each task restates the runtime state it depends on with exact
`file:line` evidence. The epic document remains authoritative for Boundary
Decisions, Debt Ownership, Proof And Quality Contract, and Performance Contract.

Runtime-only scope: no Surge syntax/parser/semantic/lowering changes for
crossing surfaces, no Phase 4 transport. Stdlib `.sg` changes are allowed only
for the RV2-DEBT-013 owner-safety surface and must not add new public names for
crossing concepts.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-dependency-and-debt-map.md` | Complete | map/evidence | none |
| 2 | `02-debt-003-state-split.md` | Complete; `RV2-DEBT-003` closed | runtime refactor | 1 (scheduler map) |
| 3 | `03-debt-010-net-handle-contract.md` | Complete; `RV2-DEBT-010` closed | runtime code | 1 (net map) |
| 4 | `04-debt-013-http-owner-safety.md` | Complete; `RV2-DEBT-013` closed | stdlib + tests | 1, 3 |
| 5 | `05-epic-closeout.md` | Complete; epic closed | closeout | all |

Execution order ruling: Task 2 (the `rt_async_state.c` split) runs first and
lands verbatim moves only, so the later net-handle work reviews against a
stable, smaller completion/cancel surface. Task 3 decides the copied-handle
contract before Task 4, because the stdlib HTTP decision depends on whether a
non-owner operation is rejected or proven owner-local.

## Owned Debt

- `RV2-DEBT-003`: dependency-aware split of `runtime/native/rt_async_state.c`
  (ready-queue, completion/cancel, handle-lifetime clusters) with Sentrux
  coupling re-check and allowlist removal or reduction.
- `RV2-DEBT-010`: copied/stale net-handle safety: stable handle/generation
  validation before public fd operations.
- `RV2-DEBT-013`: stdlib HTTP owner-shard safety under `SURGE_SHARDS>1`.

## Rules

- Expand only the next task before execution; do not pre-implement later tasks.
- Every implementation slice starts with the written model required by the
  epic's Proof And Quality Contract.
- Gates per runtime-code task: `git diff --check`, `make c-check`,
  `make cppcheck`, focused Go tests named in the task document,
  `make runtime-v2-check`, `make check` (or the recorded narrower gate),
  `./check_file_sizes.sh -a`, and root + `runtime/` + `runtime/native` Sentrux
  scans per `SENTRUX_POLICY.md`.
