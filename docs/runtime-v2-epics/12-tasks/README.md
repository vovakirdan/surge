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
| 1 | `01-dependency-debt-and-representation-map.md` | Pending | map/evidence + decision | none |
| 2 | `02-backend-unavailable-diagnostic-contract.md` | Pending | compiler diagnostics + tests | 1 |
| 3 | `03-lowering-readiness-representation.md` | Pending | compiler metadata + tests | 1, 2 |
| 4 | `04-compile-time-usage-fixtures.md` | Pending | fixtures/probes | 2, 3 |
| 5 | `05-test-harness-hardening.md` | Pending | test harness | 1 (may be promoted) |
| 6 | `06-ci-gates-and-closeout.md` | Pending | CI + closeout | all |

Execution order ruling: Task 1 runs first and makes two decisions that bind
everything after it — the representation decision (guard-before-HIR vs
lower-into-HIR-then-guard) and the debt disposition. If Task 1 finds that
`RV2-DEBT-011`/`RV2-DEBT-018` make backend matrix rows untrustworthy, Task 5
is promoted to run before Tasks 2 and 4; otherwise Task 5 runs in index order.
If Task 1 selects representation option (b), a new task document for HIR/MIR
node introduction must be written and inserted before Task 3, and this index
updated.

## Owned Debt

- `RV2-DEBT-001`: broad focused VM/backend command is not a green gate —
  in-scope only as far as the crossing-readiness matrix needs it, else
  reassign with a named owner.
- `RV2-DEBT-002`: MT liveness group budget/isolation residue — same treatment
  as DEBT-001.
- `RV2-DEBT-011`: VM LLVM build/test artifact races under overlapping tests —
  likely in-scope (Task 5).
- `RV2-DEBT-018`: rare empty-output VM harness transient — likely in-scope
  (Task 5, probably closed by the DEBT-011 fix).
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
