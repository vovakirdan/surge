# Epic 12 Task Index

Epic 12 (`12-crossing-surface-integration-and-lowering-readiness.md`) is
executed as separate task documents. Each task restates the compiler state it
depends on with exact `file:line` evidence. The epic document remains
authoritative for Boundary Decisions, Debt Ownership, the Lowering Readiness
Contract, and the Test And Proof Contract.

Compile-time/readiness scope only: no Phase 4 transport primitives, no new
syntax or attributes, no hidden local fallback, no public runnable crossing
examples. See the epic's Boundary Decisions.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-dependency-debt-and-representation-map.md` | Complete | map/evidence + decision | none |
| 2 | `02-backend-unavailable-diagnostic-contract.md` | Complete | compiler diagnostics + tests | 1 |
| 3 | `03-lowering-readiness-representation.md` | Pending | compiler metadata + tests | 1, 2 |
| 4 | `04-compile-time-usage-fixtures.md` | Pending | fixtures/probes | 2, 3 |
| 5 | `05-test-harness-hardening.md` | Pending | test harness | 1 (may be promoted) |
| 6 | `06-ci-gates-and-closeout.md` | Pending | CI + closeout | all |

Execution order ruling: Task 1 selected **guard-before-HIR**. No HIR/MIR
crossing-node task is inserted before Task 3. Task 1 did not reproduce
`RV2-DEBT-011`/`RV2-DEBT-018` in focused crossing-adjacent probes, so Task 5 is
not promoted ahead of Tasks 2 and 4 unless those tasks add VM artifact-helper
usage that makes the race relevant.

## Owned Debt

- `RV2-DEBT-001`: broad focused VM/backend command is not a green gate —
  reassigned by Task 1 to the named future **Backend/Test Matrix Cleanup** epic
  because Epic 12 crossing rows stay in `buildpipeline` / `crossinggate`.
- `RV2-DEBT-002`: MT liveness group budget/isolation residue — reassigned by
  Task 1 to **Backend/Test Matrix Cleanup**.
- `RV2-DEBT-011`: VM LLVM build/test artifact races under overlapping tests —
  still in Task 5 / later harness scope, but not promoted before Tasks 2/4.
- `RV2-DEBT-018`: rare empty-output VM harness transient — likely in-scope
  with Task 5 / later harness hardening, but not promoted before Tasks 2/4.
- `RV2-DEBT-024`: higher-order/cross-module crossing-effect propagation —
  decision point in Task 3, criterion in the epic's Debt Ownership section.

## Rules

- Expand only the next task before execution; do not pre-implement later
  tasks.
- Every implementation task starts with tests or a recorded proving-spike plan
  (hypothesis, files, proof command, success/failure criteria, rollback note).
- A task that discovers a valid Epic 11 construct cannot work as designed must
  stop, record the exact failing construct, and return it for design review —
  not patch the surface.
- Gates per compiler-code task: `git diff --check`, focused Go tests named in
  the task document, `go test ./internal/crossinggate/`, `make golden-check`
  when fixtures change, `make check` before task completion,
  `./check_file_sizes.sh -a`, and root + scoped Sentrux scans per
  `SENTRUX_POLICY.md`. `make c-check` / `make cppcheck` only if runtime C is
  touched (not expected in this epic).
