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

Epic 1 is complete. Epic 2 is drafted in
`02-n1-runtime-shard-structure.md`, with separate task files under
`02-tasks/`.

## Current Runtime V2 Artifacts

- `RULES.md`: global Runtime V2 development rules.
- `SENTRUX_POLICY.md`: repository and scoped Sentrux policy, current baseline
  signals, and missing-rules blocker.
- `EVIDENCE_TEMPLATE.md`: required evidence format for later tasks and epics.
- `01-baseline-evidence.md`: current checkout checks, benchmark reports,
  counters, and blockers before Epic 2.
- `LIVENESS_PROBES.md`: required liveness probes by changed runtime surface.
- `OPEN_DECISIONS_BEFORE_EPIC_2.md`: explicit blockers and deferrals before
  structural `N=1` work.
- `NOTES.md`: live handoff log; durable decisions must move into the owning
  document before an epic closes.
- `02-evidence.md`: pending Epic 2 task evidence ledger.

Known backend-test debt is accepted for now: the focused
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` baseline failure is not an
Epic 2 start blocker. A later test/backend epic will rewrite the VM/native/LLVM
test matrix around the Runtime V2 contracts. Until then, runtime tasks must keep
that debt named and must not attribute new regressions to it without evidence.

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
| 2 | `02-n1-runtime-shard-structure.md` | Draft. Introduce V2-shaped `rt_runtime` / `rt_shard` structures with `N=1` and unchanged behavior. |
| 3 | TBD | Move waiters to owner-local structures while keeping `N=1`. |
| 4 | TBD | Replace poll-set rebuilds with a shard-local persistent fd registry. |
| 5 | TBD | Move hot heap counters to per-shard accounting before real `N>1` benchmarking. |
| 6 | TBD | Enable multi-shard accept ownership and remove hot-path stealing for connection tasks. |
| 7 | TBD | Add the explicit crossing language surface: `far`, `submit_to`, and shard-movable checks. |
| 8 | TBD | Add cross-shard runtime transport, remote operations, Tier 2 destinations, and generation-checked distributed flows. |
| 9 | TBD | Add remote-free routing and shard-local hot pools. |
| 10 | TBD | Add the `Io` boundary and optional backend work such as `io_uring` after ownership is stable. |
| 11 | TBD | Rewrite the VM/native/LLVM test matrix around the stable Runtime V2 contracts and remove accepted backend-test debt. |

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

Until Sentrux rule files exist, tasks may record scan and health evidence but
must not claim rule compliance from a missing-rules `check_rules` result.

Do not use the broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required green gate until
the later test/backend matrix epic fixes or replaces that accepted debt.
