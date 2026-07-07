# Runtime V2 Epics

This directory tracks the incremental migration from the current native Surge
runtime to the Runtime V2 target architecture described in
`docs/RUNTIME_V2.md`.

The working mode is real-time and evidence-driven:

1. write the next epic as a standalone document;
2. agree on its brief task list and acceptance gates;
3. update `NOTES.md` with current context before starting the next task;
4. expand only the next task into implementation detail;
5. implement, verify, and update `NOTES.md` with the evidence;
6. consolidate durable notes into the epic and linked docs before closing it;
7. use the new evidence to shape the next epic.

Keep a short roadmap for the whole migration, but do not fully task every later
epic before the earlier runtime facts are known.

Global working rules live in `RULES.md`. An epic may add local rules, but it
must not weaken the global rules.

## Current Status

Epic 1 is complete. Epic 2 is complete for its `N=1`
runtime/shard-structure scope in `02-n1-runtime-shard-structure.md`, with
task evidence in `02-evidence.md`. Epic 3 is complete for owner-local waiters
and dependency-aware runtime refactoring under the same `N=1` boundary. Epic 4
is complete for persistent fd registry and net lifecycle proof, with accepted
debt recorded in `DEBT.md`. Epic 5 is complete for per-shard heap accounting
and stable heap-accounting CI gates. Epic 6 is complete for structural `N>1`
native TCP accept ownership under the preserved global executor lock and the
Tier 1 no-steal scheduler boundary. It added the stable
`runtime-v2-accept-check` gate, final benchmark evidence, and an explicit Epic
7 lock-splitting handoff. Remaining copied/raw net-handle owner guards and
legacy large-file cleanup stay recorded in `DEBT.md`. Epic 7 is complete per
`07-executor-lock-split-and-shard-runtime-state.md`: it split the preserved
global executor lock into per-shard locks plus a reduced global control lane,
moved scheduler queues, waiter stores, sleep timers, and channel ownership to
shard-owned state, and promoted the lock gate (`runtime-v2-lock-check`) into
`runtime-v2-check`. Epic 8 is complete per
`08-task-lifecycle-lane-and-net-fairness.md`: it moved steady task lifecycle
work and same-owner scope bookkeeping off the hot control lane, fixed the
adopted 8x1024 starvation debt, promoted lifecycle/perf gates into
`runtime-v2-check`, and recorded the remaining wakeup/cancel/accept-transition
debts in `DEBT.md`. The explicit crossing surface and Phase 4 transport move
to a later epic and still require a dedicated syntax review first. Epic 9 is
complete per `09-wakeup-and-cancellation-safety.md`: it added the test-only
sync-point proof scaffold and closed the three carried local safety debts
without adding Phase 4 transport or language syntax: `RV2-DEBT-023`
(cancel-vs-park), `RV2-DEBT-020` (owner-replacement join-waiter migration), and
`RV2-DEBT-022` (external-await `done_cv` StoreLoad ordering). Broader
cancellation matrix rows remain future test-matrix hardening candidates, not
new known correctness debt.

## Current Runtime V2 Artifacts

- `RULES.md`: global Runtime V2 development rules.
- `SENTRUX_POLICY.md`: repository and scoped Sentrux policy, current baseline
  signals, and rule-check requirements.
- `DEBT.md`: durable Runtime V2 debt ledger with owners and close conditions.
- `EVIDENCE_TEMPLATE.md`: required evidence format for later tasks and epics.
- `01-baseline-evidence.md`: current checkout checks, benchmark reports,
  counters, and blockers before Epic 2.
- `LIVENESS_PROBES.md`: required liveness probes by changed runtime surface.
- `OPEN_DECISIONS_BEFORE_EPIC_2.md`: explicit blockers and deferrals before
  structural `N=1` work.
- `NOTES.md`: live handoff log; durable decisions must move into the owning
  document before an epic closes.
- `02-evidence.md`: Epic 2 task evidence ledger.
- `03-owner-local-waiters-and-runtime-refactor.md`: Epic 3 scope, closeout,
  remaining debt, and Epic 4 handoff.
- `03-evidence.md`: Epic 3 task evidence ledger.
- `04-persistent-fd-registry-and-net-lifecycle.md`: Epic 4 scope, task list,
  acceptance gates, closeout, and Epic 5 handoff.
- `04-evidence.md`: Epic 4 task evidence ledger.
- `05-per-shard-heap-accounting.md`: Epic 5 scope, acceptance matrix, closeout,
  and Epic 6 handoff for shard-owned heap accounting.
- `05-evidence.md`: Epic 5 task evidence ledger.
- `06-n2-accept-ownership-and-tier1-scheduler.md`: Epic 6 scope, acceptance
  contract, closeout, and Epic 7 handoff.
- `06-evidence.md`: Epic 6 task evidence, benchmark rows, final gates, and
  closeout accounting.
- `06-tasks/`: Epic 6's 15 expanded task documents and task index.
- `07-executor-lock-split-and-shard-runtime-state.md`: Epic 7 scope, boundary
  decisions, lock ownership contract, closeout, and Epic 8 runtime handoff.
- `07-evidence.md`: Epic 7 task evidence ledger and final certification
  record.
- `07-executor-lock-dependency-map.md`: Epic 7 field/path/lock-lane map.
- `07-locking-model-proving-spike.md`: Epic 7 locking model decisions
  D1-D16 and the park/wake protocol proof.
- `07-tasks/`: Epic 7 task index and expanded task documents.
- `08-task-lifecycle-lane-and-net-fairness.md`: Epic 8 scope, boundary
  decisions, contracts, task list, closeout, and next-runtime handoff.
- `08-tasks/`: Epic 8 task index and expanded task documents.
- `08-evidence.md`: Epic 8 task evidence ledger, final benchmark rows, and
  closeout accounting.
- `09-wakeup-and-cancellation-safety.md`: Epic 9 scope, boundary decisions,
  proof contract, closeout status, and acceptance criteria for
  wakeup/cancel/accept transition safety before final crossing work.
- `09-evidence.md`: Epic 9 evidence ledger, final proof/gate evidence, and
  closeout accounting.
- `09-tasks/`: Epic 9 task index and expanded task documents.
- `10-runtime-debt-burndown-and-owner-safety.md`: draft Epic 10 scope for
  dependency-aware cleanup, copied net-handle safety, and stdlib HTTP
  owner-shard hardening before Phase 4.

Known backend-test debt remains accepted for now: the focused
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` baseline failure is outside
the Runtime V2 green gates. Epic 12 owns the VM/native/LLVM test-matrix
rewrite. Until then, runtime tasks must keep that debt named in `DEBT.md` and
must not attribute new regressions to it without evidence.

## Standing Migration Goals

Every epic should move the runtime toward these goals:

- raise scoped `runtime/` quality without lowering repository quality;
- replace executor-wide state with owner-oriented runtime primitives;
- use explicit status codes in new V2 C APIs instead of `panic_msg` for
  recoverable failures;
- keep hot-path ownership, wakeup, cancellation, and cleanup paths legible;
- keep `NOTES.md` current enough for fast task switching;
- reduce or contain legacy files that exceed the 500-line Runtime V2 limit.

## Epic Roadmap

| Epic | Document | Purpose |
| --- | --- | --- |
| 1 | `01-contract-rules-harness.md` (`01-contract-rules-harness-tasks.md`) | Complete. Defines the contracts, strict development rules, baseline probes, and quality gates that every later epic must satisfy. |
| 2 | `02-n1-runtime-shard-structure.md` | Complete. Introduced V2-shaped `rt_runtime` / `rt_shard` structures with `N=1`, migrated selected owner surfaces, and added the stable Runtime V2 liveness gate without changing behavior. |
| 3 | `03-owner-local-waiters-and-runtime-refactor.md` | Complete. Moved included waiter surfaces to owner-local structures under `N=1`, added stable waiter liveness gates, and landed dependency-aware runtime refactoring. |
| 4 | `04-persistent-fd-registry-and-net-lifecycle.md` | Complete. Replaced poll-set rebuilds with a shard-local persistent fd registry and proved net lifecycle ownership with accepted debt. |
| 5 | `05-per-shard-heap-accounting.md` | Complete. Moved heap accounting from global hot counters to runtime/shard-owned cells, preserved public allocation behavior, and added stable heap-accounting gates to Runtime V2 CI coverage. |
| 6 | `06-n2-accept-ownership-and-tier1-scheduler.md` | Complete. Enabled structural multi-shard native TCP accept ownership under the preserved global lock, added per-shard net poller/wake ownership, removed non-owner stealing for Tier 1 connection tasks, and promoted the stable accept gate. |
| 7 | `07-executor-lock-split-and-shard-runtime-state.md` | Complete. Split the preserved global executor lock into per-shard locks plus a reduced control lane, moved scheduler queues, per-key waiter-store ownership, sleep stores, channel owner shards, and re-laned blocking/await/shutdown paths under `N>1`. |
| 8 | `08-task-lifecycle-lane-and-net-fairness.md` | Complete. Moved task lifecycle, join/done, await accounting, and same-owner scope bookkeeping off the remaining steady-path control lane; fixed the adopted 8x1024 starvation debt; added lifecycle/perf gates. No syntax or Phase 4 transport work. |
| 9 | `09-wakeup-and-cancellation-safety.md` | Complete. Runtime-only safety pass before final crossing work. Added deterministic sync-point proofs and closed `RV2-DEBT-023`, `RV2-DEBT-020`, and `RV2-DEBT-022`. No syntax or Phase 4 transport work. |
| 10 | `10-runtime-debt-burndown-and-owner-safety.md` | Draft. Burn down owner-safety and cleanup debt before Phase 4: `RV2-DEBT-003`, `RV2-DEBT-010`, and `RV2-DEBT-013`; no syntax or transport work. |
| 11 | TBD | Add the `Io` boundary and optional backend work such as `io_uring` after ownership is stable. |
| 12 | TBD | Rewrite the VM/native/LLVM test matrix around the stable Runtime V2 contracts and remove accepted backend-test debt. |

## Language Syntax Gate

Before any epic or task changes Surge syntax, keywords, parser rules, semantic
checks for new syntax, or public examples, stop and review the language surface
with the user. The `RUNTIME_V2.md` names for crossing, far handles, and shard
movability describe semantics only. They are not final keyword choices.

## Standing Gates

Every epic must say which of these gates apply and record the exact evidence:

- `make check`
- `make c-check`
- `make cppcheck` for native C changes
- exact focused Go tests chosen from `LIVENESS_PROBES.md` or the epic's CI
  contract
- native benchmarks, usually `./scripts/bench_native_net.sh` and
  `./scripts/bench_native_channels.sh`
- live runtime trace or timeout-based liveness proof for scheduler, wakeup, or
  cancellation changes
- CI coverage for stable Runtime V2 liveness tests before an epic closes
- a doc update that states which Runtime V2 contract was preserved, changed, or
  deliberately deferred

Sentrux rule files exist for the repository root, `runtime/`, and
`runtime/native/`. Runtime tasks must record root and scoped `check_rules`
results as pass/fail evidence; a missing-rules result is now a blocker, not an
accepted Runtime V2 state.

Do not use the broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required green gate until
the later test/backend matrix epic fixes or replaces that accepted debt.
