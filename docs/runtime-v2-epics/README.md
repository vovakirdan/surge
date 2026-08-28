# Runtime V2 Epics

This directory tracks the incremental migration from the current native Surge
runtime to the Runtime V2 target architecture described in
`docs/RUNTIME_V2.md`.

The working mode is real-time and evidence-driven:

1. write the next epic as a standalone document;
2. agree on its design, bounded workstreams, and acceptance gates;
3. update `NOTES.md` with current context before starting a workstream;
4. implement in isolated worktrees, review intended commits, and integrate only
   non-overlapping or dependency-ordered changes;
5. verify the integrated result and run independent non-author review;
6. update `NOTES.md` with evidence and record nonblocking findings in `DEBT.md`;
7. consolidate durable decisions into the epic and linked docs before closing;
8. use the new evidence to shape the next epic.

A task-document hierarchy is optional. Use one only when it reduces execution
risk; a complete epic may be run directly through its workstreams and gates.

Keep a short roadmap for the whole migration, but do not fully task every later
epic before the earlier runtime facts are known.

Global working rules live in `RULES.md`. An epic may add local rules, but it
must not weaken the global rules.

## Where we are and what is next

Two documents answer this, and they answer different questions.

`PLAN.md` is the technical plan: the measured state of every epic, the numbers
that close Epic 23b's remaining waves, the ordered work with sizes, and what is
deliberately NOT picked up. It carries no session history.

The **BOARD** at the top of `NOTES.md` is the near-term dispatch: what is in
flight right now, what is taken next and in what order, and which files are
claimed by which lane. Read the board before starting anything; read the plan to
know why.

This section and the roadmap table below are the HISTORY.

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
debts in `DEBT.md`. Epic 9 is complete per
`09-wakeup-and-cancellation-safety.md`: it added the test-only sync-point proof
scaffold and closed the three carried local safety debts without adding Phase 4
transport or language syntax: `RV2-DEBT-023` (cancel-vs-park),
`RV2-DEBT-020` (owner-replacement join-waiter migration), and `RV2-DEBT-022`
(external-await `done_cv` StoreLoad ordering). Epic 10 is complete per
`10-runtime-debt-burndown-and-owner-safety.md`: it closed the dependency-aware
runtime split, stable copied net-handle safety, and owner-local stdlib HTTP
server path. Epic 11 is complete per
`11-explicit-crossing-language-surface.md`: it accepted the Draft 9 crossing
surface at lexer/parser/sema/golden level (`far`, `on`, `spawn on`, inferred
effects, `@shard_movable`, `@shard_pinned`) without Phase 4 transport. Epic 12
is complete per `12-crossing-surface-integration-and-lowering-readiness.md`:
it preserved crossing meaning at sema/readiness level, froze deterministic
backend-unavailable behavior, added `runtime-v2-crossing-check`, reconciled
backend/test debt, and wrote the Phase 4 transport/lowering handoff. It did not
add HIR/MIR crossing nodes, runtime transport, public runnable examples, or new
syntax. Epic 13 is complete per
`13-phase4-transport-spine-and-placement-task-lowering.md` (Closeout,
2026-07-10): the first executable Phase 4 vertical — inbound transport
spine, placement ABI, and the placement task crossings (`spawn on`,
`far Task.await/cancel`, immediate `on` on its dedicated execute/reply
category) run in production on LLVM/native across `SURGE_SHARDS=1,2,8`
with generation tokens, owner-routed cancellation, teardown release, and
trace-counter proof. The negative space is matrix-owned
(`13-tasks/11-unsupported-forms-matrix.md`), the stable gate is
`runtime-v2-transport-check` (wired into `runtime-v2-check`), and
`scripts/bench_crossing.py` records the crossing baseline. Later epics shipped
remote channels, remote `select`, far-handle sharing, migration, crossing drop
activation, and the owner-routed reclamation core. The current resumption head
is Epic 23b: inline value storage plus the end-to-end typed-carrier ABI. The
detailed current chain is maintained in the roadmap and Detour Chain below.

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
- `23-storage-model-and-typed-carrier-abi.md`: accepted normative storage,
  place, ordinary-call, carrier, `Task<T>.clone()`, and byte-credit design.
- `23b-inline-storage-and-typed-carriers.md`: executable end-to-end Epic 23
  Phase 2 workstreams and acceptance gates; no separate task hierarchy.
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
- `10-runtime-debt-burndown-and-owner-safety.md`: Epic 10 scope and closeout
  for dependency-aware cleanup, stable copied net-handle safety, and stdlib
  HTTP owner-shard hardening before Phase 4.
- `11-explicit-crossing-language-surface.md`: Epic 11 accepted language
  surface, golden-test contract, compile-time implementation scope, and Draft 9
  closeout for explicit crossing.
- `11-tasks/`: Epic 11 block documents for `far`, `on`, `spawn on`, and
  crossing contracts.
- `12-crossing-surface-integration-and-lowering-readiness.md`: Epic 12 closeout
  for backend-unavailable diagnostics, sema lowering-readiness records,
  matrix/debt reconciliation, `runtime-v2-crossing-check`, and Phase 4
  transport handoff.
- `13-phase4-transport-spine-and-placement-task-lowering.md`: completed first
  executable Phase 4 transport/lowering vertical.

Known backend-test debt remains accepted for now: the focused
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` baseline failure is outside
the Runtime V2 green gates. Epic 12 reconciled the crossing-readiness part of
that debt; remaining VM/native/LLVM matrix and artifact-lifecycle work is owned
by **Backend/Test Matrix Cleanup** in `DEBT.md`.

## Standing Migration Goals

Every epic should move the runtime toward these goals:

- raise scoped `runtime/` quality without lowering repository quality;
- replace executor-wide state with owner-oriented runtime primitives;
- use explicit status codes in new V2 C APIs instead of `panic_msg` for
  recoverable failures;
- keep hot-path ownership, wakeup, cancellation, and cleanup paths legible;
- emit precise diagnostics, notes, and only provably safe fixes when the
  compiler can decide an invalid program;
- keep compiler goldens reviewed and deterministic even when AST/MIR trees
  legitimately rebuild;
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
| 10 | `10-runtime-debt-burndown-and-owner-safety.md` | Complete. Closed `RV2-DEBT-003`, `RV2-DEBT-010`, and `RV2-DEBT-013`: dependency-aware runtime split, stable net handle ids, and owner-local stdlib HTTP server path. No syntax or Phase 4 transport work. |
| 11 | `11-explicit-crossing-language-surface.md` | Complete. Accepted and implemented the Draft 9 compile-time crossing surface: `far`, contextual `on`, `spawn on`, inferred crossing effects, and shard movement attributes. No Phase 4 transport. |
| 12 | `12-crossing-surface-integration-and-lowering-readiness.md` | Complete. Preserved crossing meaning through sema readiness metadata, enforced deterministic backend-unavailable behavior, promoted `runtime-v2-crossing-check`, reconciled backend-test debt, and prepared the Phase 4 transport/lowering handoff. |
| 13 | `13-phase4-transport-spine-and-placement-task-lowering.md` | Complete. Shipped the first real Phase 4 execution vertical: inbound transport, placement task lowering, `spawn on shard/distributed`, `far Task.await/cancel`, and immediate `on shard/distributed`. Later carrier surfaces are owned by their later epics. |
| 14 | `14-phase4-remote-channels.md` | Complete. Remote channels: `channel_on`, cross-shard send/recv, per-payload drop functions. |
| 15 | `15-structural-cleanup.md` | Complete. Structural cleanup and gate integrity; `gatecheck` folded into `make check`. |
| 16 | `16-far-copy.md` (`16-candidates.md`) | Complete. `share()` sibling leases for far channels. |
| 17 | `17-remote-select.md` | Complete. Remote `select` via the proxy-selector model. |
| 18 | `18-migration.md` | Complete. Owned `@shard_movable` capture migration (capture lift). |
| 19 | `19-drop-emission.md` (`19-candidates.md`) | Complete. Drop emission for non-Copy owned values. |
| 20 | `20-crossing-drop-activation.md` | Complete. Crossing drop activation. |
| 21 | `21-owner-routed-frees.md` | **Core vertical complete; acceptance closeout open.** The owner-routed reclamation path shipped; RV2-DEBT-125 owns only the unperformed Task 9 bench/matrix/seam/debt closeout. Separate remaining tails stay under RV2-DEBT-054/056/061/062. |
| 22 | `22-numeric-reclamation.md` | **PARTIAL — parked.** Phases 0a/0b/1 shipped: the ownership axes were split out of `IsCopy`, and `float` became a reference-counted scalar with a strict-zero valgrind gate. The six `float`/composite crossing deep-copy barriers are now absorbed by Epic 23b's generic cross operations; after 23b, Epic 22 resumes with Phase 2 `int`/`uint` reclamation. |
| 23 | `23-value-composites.md` | **Phase 1 COMPLETE.** A struct/tuple/union/fixed-array is a value: copy duplicates, the transitional box is reclaimed, and every crossing route carries one. Its former Phase 2 placeholder is superseded by the accepted storage design and executable Epic 23b. Phase 1 history and semantic tests remain authoritative. |
| 23b | `23-storage-model-and-typed-carrier-abi.md`; `23b-inline-storage-and-typed-carriers.md` | **READY FOR IMPLEMENTATION.** Complete Phase 2 end to end: inline value storage, destination-oriented calls, typed arrays/maps/tasks/channels/select/blocking/far transport, generic `float`/composite cross-clone barriers, result-entitlement cloning, physical byte credits, diagnostics, and deletion of the old erased ABI. Independent ABI, diagnostics, and execution re-reviews approved the integrated design. |
| 24 | `24-partial-moves.md` | **Complete 2026-07-29** (steps 0-9), residuals closed 2026-07-31. Full partial-move tracking: the moved-set is keyed by PLACE, use-after-move answers per place, a partially-moved value drops only what it still holds, a reinitialized place comes back, and a capture takes a projection. `RV2-DEBT-077` closed, with nine defects found and closed on the way. Step 8 covers STRUCT FIELDS only (array elements and union payloads stay rejected, settled with the owner) — that residual stands. Step 0's tail — converting the compiler-generated field reads to the explicit transfer mode — is CLOSED: it was one real site (the crossing capture unpack, not three), asserted at the MIR level in `internal/crossinggate`; the single-suspend async lowering the other two sites were attributed to had no caller and was deleted. |
| 25 | `25-ownership-verifier-and-tooling.md` | **Complete 2026-08-02.** MIR ownership verification is live behind `build --dev`, with a fast `make check` sample, the exact 1046-fixture corpus gate, opt-in annotated MIR, and a typed four-axis VM+LLVM/Valgrind harness. Steps 0-5 shipped; the optional highest-risk/lowest-priority heap-site census is intentionally deferred as RV2-DEBT-120. This epic remains outside and does not block the 22→23→24 detour chain. |


## Detour Chain — what is parked, and why

Epics 22-24 were each interrupted by the next, for the same reason every time:
the work uncovered a defect underneath it that made finishing pointless until
the defect was fixed. This section exists because that chain was not written
down anywhere, and a reader arriving at Epic 24 could not tell what was waiting
behind it.

```
22 numeric reclamation  ──parked──▶  23 value composites  ──parked──▶  24 partial moves
   float shipped;                       Phase 1 shipped;                 COMPLETE 2026-07-29
   barriers move to 23b;                 inline is Epic 23b      ◀──prerequisite closed──┘
   int/uint not started
```

The chain has unwound one link: Epic 24 landed on 2026-07-29, so Epic 23 Phase 2
is the head of the queue again rather than something waiting behind a defect.
Its physical model is now accepted in
`23-storage-model-and-typed-carrier-abi.md`, and execution is owned by
`23b-inline-storage-and-typed-carriers.md`.

- **22 → 23.** Epic 22 made `float` a counted scalar and needed to install deep
  copies at the six crossing barriers. Doing that for a COMPOSITE carrying a
  float required knowing what copying a composite means — and it turned out
  copying one did not duplicate it at all: `let mut q = p; q.a = 99` mutated
  `p`, on both backends. That is Epic 23.
- **23 → 24.** Epic 23 Phase 1 fixed the `@copy` case. The move-only case is a
  different question — reading a field out of a live struct is a PARTIAL MOVE,
  which sema does not track, so it silently aliases (RV2-DEBT-077). Epic 23
  Phase 2 (inline storage) makes that worse rather than better: the alias
  becomes a bitwise duplicate with two owners. That is Epic 24.

**Resumption order, now live:** Epic 23b (inline storage, all typed carriers,
and the current `float`/composite cross-clone barriers), then Epic 22's
`int`/`uint` reclamation. Epic 22 is last on purpose — its remaining work is
simplified by one type-directed carrier model instead of having to support both
composite boxes and inline values.

The two prerequisites previously recorded for Phase 2 are now both resolved:

1. ~~Epic 24's step-0 tail.~~ **DONE 2026-07-31.** It was one real site, not
   three: the crossing capture unpack now declares whether it takes its value,
   and both it and the async state-envelope protocol are asserted at the MIR
   level in `internal/crossinggate`. The single-suspend async lowering those
   sites were attributed to had no caller and is deleted.
2. ~~The storage-model document.~~ **DONE 2026-08-04.** The accepted
   places/references, frame-slot, ordinary-call, typed-carrier, task-clone, and
   byte-credit model is `23-storage-model-and-typed-carrier-abi.md`; Epic 23b
   implements it without a compatibility layer.

Debt disposition: Epic 23b absorbs the crossing-barrier portion of
RV2-DEBT-038; RV2-DEBT-035/068 keep the later `int`/`uint` reclamation. The
historical 036/037 rows and Epic 24's RV2-DEBT-077/078 are closed.

## Language Syntax Gate

Before any future epic or task changes Surge syntax, keywords, parser rules,
semantic checks for new syntax, or public examples, stop and review the
language surface with the user. Epic 11 selected the current crossing surface;
later epics must not add shorthand, new keywords, or public examples without an
explicit design review.

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
- `runtime-v2-crossing-check` for crossing compiler/backend readiness
- for compiler-output work, the reviewed two-pass golden protocol in
  `RULES.md`, including two serialized `make golden-check` closeout runs
- a doc update that states which Runtime V2 contract was preserved, changed, or
  deliberately deferred

Sentrux rule files exist for the repository root, `runtime/`, and
`runtime/native/`. Runtime tasks must record root and scoped `check_rules`
results as pass/fail evidence; a missing-rules result is now a blocker, not an
accepted Runtime V2 state.

Do not use the broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required green gate until
the later test/backend matrix epic fixes or replaces that accepted debt.
