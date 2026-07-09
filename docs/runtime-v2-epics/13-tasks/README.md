# Epic 13 Task Index

Epic 13 (`13-phase4-transport-spine-and-placement-task-lowering.md`) is
executed as separate task documents. Each task restates the runtime/compiler
state it depends on with exact `file:line` evidence. The epic document remains
authoritative for Boundary Decisions, the Runtime Transport Contract, the
Lowering Contract, Debt Ownership, and the Test And Proof Contract.

First Phase 4 execution vertical only: placement task crossing on the
transport-capable backend. No remote channels, no remote `select`, no
distributed scope protocol, no migration, no `pool` execution, no new syntax,
no hidden local fallback. See the epic's Boundary Decisions — in particular
the detached-affine-far-Task severability contract, the plain-data/copyable
payload constraint, the mandatory generation/no-reuse token, and the
task-suspend-vs-shard-park invariant.

## Task Order

| Task | File | Status | Kind | Depends On |
| --- | --- | --- | --- | --- |
| 1 | `01-kickoff-map-and-contracts.md` | Complete | map/evidence + decisions | none |
| 2 | `02-transport-harness-hardening.md` | Pending | test harness | 1 |
| 3 | `03-park-wake-proof.md` | Pending | runtime proof tests | 1, 2 |
| 4 | `04-inbound-transport-spine.md` | Pending | native runtime | 3 |
| 5 | `05-placement-abi.md` | Pending | runtime ABI + prelude | 1 |
| 6 | `06-remote-publication-api.md` | Pending | native runtime API | 4, 5 |
| 7 | `07-lowering-representation.md` | Pending | compiler HIR/MIR + guards | 1 |
| 8 | `08-spawn-on-vertical.md` | Pending | lowering + runtime e2e | 6, 7 |
| 9 | `09-await-cancel-vertical.md` | Pending | lowering + runtime e2e | 8 |
| 10 | `10-immediate-on-vertical.md` | Pending | lowering + runtime e2e | 6, 7, 9 |
| 11 | `11-unsupported-forms-matrix.md` | Pending | backend matrix tests | 8, 9, 10 |
| 12 | `12-benchmark-ci-gate-closeout.md` | Pending | bench + CI + closeout | all |

Execution order rulings:

- Task 2 (harness hardening) runs before any task that adds native
  `SURGE_SHARDS` execution rows. Task 3's negative controls are worthless on a
  harness where a transient empty-output failure is indistinguishable from a
  real regression (`RV2-DEBT-011`/`RV2-DEBT-018`).
- Tasks 8 and 9 gate together: `spawn on` must not become publicly executable
  before the await/cancel discharge path exists. Task 8 may land runtime and
  lowering code, but the crossing guard for `spawn on` flips only in Task 9's
  gate, together with await/cancel.
- Task 10 (`on`) uses the dedicated execute/reply message category decided in
  the epic; it is last of the verticals because it is the most
  semantically loaded form.
- Tasks 5 and 7 depend only on Task 1 and may proceed in parallel with the
  runtime spine work if staffing allows; their outputs join at Task 6/8.

## Owned Debt

- `RV2-DEBT-011` / `RV2-DEBT-018`: promoted, narrow scope — Task 2 gives the
  transport gate's `internal/vm` execution path per-run unique artifact
  directories (or locking) and empty-output capture. The broad matrix rewrite
  stays with the Backend/Test Matrix Cleanup epic.
- `RV2-DEBT-024`: Task 1 records the effect-boundary decision for transport
  lowering (direct crossing-site records vs exported hidden-crossing effects).
- `RV2-DEBT-025`: Task 1 reviews the copyable-far-handles postponement while
  deciding the handle model; affinity is load-bearing for completion/cancel.
- `RV2-DEBT-026`: Task 1 reassigns the stale "Epic 11 follow-up" owner.
- `RV2-DEBT-005`: any touched allowlisted native file records shrank / flat /
  follow-up owner.
- `RV2-DEBT-006`: Task 12's transport benchmark must own per-probe timeouts
  (follow `scripts/stallrepro.py` shape); do not inherit the channel-script
  debt into new tooling.

## Rules

- Expand only the next task before execution; do not pre-implement later
  tasks.
- Every implementation task starts with tests or a recorded proving-spike plan
  (hypothesis, files, proof command, success/failure criteria, rollback note).
- A task that discovers a valid Epic 11 construct cannot be lowered under the
  epic's rules must stop, record the exact failing construct, and return it
  for design review — not invent syntax or a local fallback.
- A task that needs remote channels, remote `select`, distributed scope
  messages, migration, or remote-free routing must stop and record the gap for
  the owning future epic.
- Gates per native-runtime task: `git diff --check`, focused tests named in
  the task document, `make c-check`, `make cppcheck`, `make
  runtime-v2-crossing-check`, `./check_file_sizes.sh -a`, and root + scoped
  Sentrux scans (`runtime/`, `runtime/native/`) per `SENTRUX_POLICY.md`.
- Gates per compiler task: the same minus C gates, plus `make golden-check`
  when fixtures change and `sentrux check internal`.
- `make check` before every task completion.
