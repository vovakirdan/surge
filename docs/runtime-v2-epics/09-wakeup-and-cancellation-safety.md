# Epic 9: Wakeup And Cancellation Safety

**Goal:** close the carried Runtime V2 safety debts around external await,
cancellation, and accept-transition join-waiter migration before starting the
final crossing/transport epics. Epic 8 removed the hot task-lifecycle control
lane and fixed the 8x1024 placement funnel. Epic 9 now hardens the ordering
edges that remain: a completion must not miss an external `done_cv` waiter, a
cancelled task must not strand while parking, and owner replacement must not
leave join waiters on a shard that will never wake them.

**Approach:** this epic is proof-first and runtime-only. Start from the
existing debt ledger, re-derive each ordering gap against the current code, add
or select a deterministic proof for the failing interleaving, then implement
the smallest fix that preserves the Epic 8 lane model. Do not restore the hot
control lane to make a race disappear. Do not introduce Phase 4 messaging to
avoid a local proof. Refactor only where it reduces the dependency boundary
needed for the fix.

**Deterministic-proof mechanism is decided once, up front.** All three owned
windows are a few instructions wide, and the quality contract below says TSan
and stress runs are not replacements for deterministic proofs. Reproducing an
instruction-scale window deterministically needs one of: test-only injectable
sync points in the C runtime (env-gated pause hooks), a C-level litmus harness
around the extracted primitives, or scheduler-controlled interleaving in the
existing pin-stress harness. Any of these is new machinery under RULES.md
Global Rule 5, so the epic's first implementation-adjacent task is a proving
spike that selects the mechanism and writes its rules before any fix task
starts: hooks must compile to nothing outside test builds, live on a named
allowlist, and a static gate must prove no hook sits on the worker steady
path. Fix tasks then reuse that one mechanism instead of re-litigating it.

**Status:** in progress. Task documents exist under `09-tasks/`; the first
implementation slices stabilized the sync-point scaffold, closed
`RV2-DEBT-023` for the cancel-vs-park never-firing-key window, and closed
`RV2-DEBT-020` with a generic join-route migration fix. `RV2-DEBT-022` remains
pending.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/NOTES.md`
- `docs/runtime-v2-epics/08-task-lifecycle-lane-and-net-fairness.md`
- `docs/runtime-v2-epics/08-lifecycle-lane-proving-spike.md` (its six written
  lifetime/visibility rules constrain any reorder in `mark_done` or the park
  path)
- `docs/runtime-v2-epics/08-evidence.md` (the Task 12 re-baseline is the
  performance record Epic 9 must not regress)
- `docs/runtime-v2-epics/08-tasks/14-epic-closeout.md`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_task_park.c`
- `runtime/native/rt_async_poll.c`
- `runtime/native/rt_async_waiter.c` (`add_waiter`/`prepare_park`/
  `remove_waiter*` are the registration half of every owned window)
- `runtime/native/rt_async_sleep.c` (timer wake path; the cancel-of-sleeper
  proof row depends on it)
- `runtime/native/rt_lane.c` (the always-on lane-order assertions every fix
  must keep satisfied)
- `runtime/native/rt_waiter_route.c`
- `runtime/native/rt_scheduler_placement.c`
- `runtime/native/rt_net_accept_group.c`
- `runtime/native/rt_async_internal.h`
- `internal/vm/runtime_v2_lifecycle_*_test.go`
- `internal/vm/runtime_v2_lock_split_*_test.go`
- `internal/vm/runtime_v2_perf_gate_test.go`
- `Makefile`

## Starting State Before Epic 9

Epic 8 closed `RV2-DEBT-016`: steady task creation, worker join polling,
normal done completion, and same-owner scope bookkeeping avoid the request
steady-path control lane. `runtime-v2-check` now includes lifecycle and perf
gates, and the 8-shard/1024 net row no longer funnels durable work to shard 0.

The remaining risks are narrow but correctness-critical:

- `RV2-DEBT-022`: external `rt_task_await` waits on `done_cv` under `ex->lock`
  (the control mutex), while completion reads `done_waiters` without a lock to
  decide whether to broadcast. A running target can complete after the external
  awaiter starts parking but before the completion observes `done_waiters`,
  skipping the only `done_cv` wake source.
- `RV2-DEBT-023`: `cancel_task` stores the cancelled flag but wakes only if its
  lock-free status read sees `TASK_WAITING`. A running task can pass its
  cancellation check, return `POLL_PARKED`, then commit to `TASK_WAITING` after
  the cancel path skipped the wake. If the park key never fires, the cancelled
  task strands.
- `RV2-DEBT-020`: CLOSED by Epic 9 Task 3. `rt_task_replace_owner` now publishes
  an atomic join-owner route before migration, and `WAKER_JOIN`
  add/remove/pop/collect-all wake revalidate that route under the selected shard
  lock. The deterministic
  `SP_MIGRATE_GAP` negative-control build proves the old order stranded a late
  join waiter.

`RV2-DEBT-003` remains open because `rt_async_state.c` is still over the
Runtime V2 line target. The relevant remaining split candidate is the
completion/cancel cluster (`cancel_task`, `mark_done*`, `apply_poll_outcome`).
Epic 9 may reduce that file only when the split follows the safety fix's real
dependency boundary. Two riders from the ledger apply verbatim: the
`RV2-DEBT-003` RECOVERY NOTE requires any further split to REDUCE the
now-visible wake/park inter-module coupling (not merely relocate lines), with
the Sentrux coupling dimension re-checked when it lands; and the completion
cluster was deliberately deferred in Epic 8 Task 13 because a lifecycle static
gate pins `done_cv` behavior to `rt_async_state.c` by filename — moving the
cluster requires the same mechanical gate pin-split Task 13 performed for
`park_current`, recorded in the task document.

## Epic 9 Boundary Decisions

**Runtime-only scope.** Epic 9 does not change Surge syntax, keywords, parser
rules, semantic analysis, lowering, stdlib public examples, or public crossing
surface. It does not implement inbound queues, remote messages, eventfd
credits, remote `select`, remote-free queues, shard-movable checking, or the
seq-cst Phase 4 `PARKED` protocol.

**Safety before new capacity.** This epic closes correctness windows before
adding new runtime capabilities. Performance is still measured, but a
performance improvement is not an acceptance substitute for a proof that no
awaiter, parked task, or join waiter can strand.

**No hidden control-lane rollback.** A fix must not make migrated Epic 8 hot
paths control-lane-owned again. If a compatibility path still needs the
control lane, name it, count it separately, and keep it outside worker steady
state.

**External await stays compatibility.** `done_cv` remains external/main-thread
await compatibility, not the worker join mechanism. Closing `RV2-DEBT-022`
requires an ordering protocol: either the seq-cst StoreLoad handshake described
in `DEBT.md` or an equivalent proof that a completion cannot skip the broadcast
for a parked external awaiter.

**Cancellation must wake the transition.** Closing `RV2-DEBT-023` requires the
cancel path to force a cancelled task to re-run when cancellation races the
task's `RUNNING -> WAITING` park transition. The likely shape is an
unconditional wake token in `cancel_task`, but the task must prove the chosen
mechanism does not enqueue DONE tasks, resurrect released tasks, or scan global
state. Two more obligations are named up front: a token set on a target that
later parks on a legitimate key produces one spurious abort-and-requeue — that
is benign but must be stated and, if observable in trace counters, counted;
and the token store's lock context must be derived explicitly (the token is a
per-task atomic, so no shard lock is required, but `cancel_task` holds the
control lane during its tree walk — the write must be shown compatible with
the lane order in both the control-held and control-free call shapes).

**Accept-transition proof is fixed generically.** Epic 9 Task 3 did not rely on
a net-handle/stdlib blocker. It enumerated all four `rt_task_replace_owner` call
shapes and closed the stale-read/register-after-drain gap with a generic
join-route protocol. Future owner-replacement call sites must still re-derive
the completion-before-migration property or use a stronger old+new wake/marker
scheme.

**Refactor follows ownership, not file size alone.** If Epic 9 extracts the
completion/cancel cluster from `rt_async_state.c`, the split must reduce real
coupling and keep the load-bearing comments with the code they explain. A move
that only lowers LOC while making wake/park ownership harder to follow fails
the epic's quality bar.

**Existing gates remain load-bearing.** `runtime-v2-lifecycle-check`,
`runtime-v2-lock-check`, and `runtime-v2-perf-check` must keep passing. Epic 9
may add narrower gates, but it must not weaken the Epic 8 control-lane or
placement-adoption gates.

## Debt Ownership

Epic 9 owns:

- `RV2-DEBT-022`: external-await `done_cv` ordering.
- `RV2-DEBT-023`: cancellation vs `RUNNING -> WAITING` park ordering.
- `RV2-DEBT-020`: closed by Task 3's join-route migration fix and
  `SP_MIGRATE_GAP` proof.

Epic 9 may touch:

- `RV2-DEBT-003`: only for a dependency-aware completion/cancel split tied to
  the safety fixes.
- `RV2-DEBT-017`: only if a fix changes the sync-channel compatibility wait or
  its wake/broadcast behavior.

Epic 9 does not own:

- `RV2-DEBT-001`, `RV2-DEBT-002`, `RV2-DEBT-011`, `RV2-DEBT-018`: broad
  VM/native/LLVM matrix and harness work remains the test-matrix epic unless a
  new Epic 9 change causes a focused regression.
- `RV2-DEBT-005`, `RV2-DEBT-006`, `RV2-DEBT-007`, `RV2-DEBT-010`,
  `RV2-DEBT-012`, `RV2-DEBT-013`: keep these in the ledger unless the accepted
  task scope explicitly touches their owning surface.

## Proof And Quality Contract

Every implementation slice must start with a written interleaving model:

- owners and locks involved;
- the exact state transition being protected;
- the old failure window;
- the new ordering guarantee;
- the test or trace that would fail if the guarantee regressed.

Required proof coverage:

- external await against an already-running target, a parked target, and an
  already-DONE target (the trivial case belongs in the matrix, not in prose);
- multiple concurrent external awaiters on the same and on different targets
  (`done_waiters > 1`; the broadcast-vs-signal choice must be argued);
- cancellation racing a task that is about to park on a never-firing key —
  including a sleeping task explicitly (cancelled sleepers have their own
  deadline-index removal path in `mark_done`);
- cancellation racing the wake side: a cancel arriving while
  `wake_key_all` is mid-drain on the target's key (token vs batch-compaction
  ordering);
- the `RV2-DEBT-022` x `RV2-DEBT-023` intersection: cancellation of a task
  whose completion is concurrently racing a parked external awaiter — both
  fixes touch `mark_done`/`cancel_task`, so the combined window must be named
  and proven, not assumed independent;
- cancellation of READY, WAITING, RUNNING, and DONE targets with no task
  resurrection;
- accept-transition owner replacement with any join-waiter migration outcome
  named and proven;
- shutdown while tasks are parked in join, channel, timer, blocking, and net
  waits after the wake/cancel changes;
- no parked-with-work and no stranded waiter after the changed paths;
- `SURGE_SHARDS=1`, `2`, and `8` where the path is shard-sensitive.

Static or trace gates should prove:

- worker join paths still avoid `done_cv`;
- normal worker completion does not take the control lane only to satisfy
  external await compatibility;
- `done_cv` broadcasts remain external-await-only and counted separately;
- `cancel_task` has a named wake-token ordering rule;
- `rt_waiter_migrate_join_waiters` no longer carries an unproven
  accept-transition assumption.

Every runtime-code task must run and record:

- `git diff --check`;
- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- `make check`, unless the task document records a narrower approved gate and
  the final epic closeout runs the full gate;
- `./check_file_sizes.sh -a` when C/H files change;
- root, `runtime/`, and `runtime/native` Sentrux scans, plus rule results per
  `SENTRUX_POLICY.md`;
- effective LOC for every touched over-limit file and every new native runtime
  file.

TSan or stress tests are not replacements for deterministic proofs, but they
are required when a task changes lock-free ordering, task status transitions,
or waiter migration.

## Performance Contract

Epic 9 must preserve the Epic 8 control-lane result. The per-commit perf gate
must continue to pass, and closeout must record the final
`TestRuntimeV2PerfControlLaneGate` counters. Any material increase in
`control_lock_acquired`, `ctrl_await_compat`, lifecycle-control/request, or
steady-state-control/request must be explained and either fixed or assigned to
a named debt with evidence.

The `RV2-DEBT-022` fix is the one place this epic touches the completion hot
path: a seq-cst store on `TASK_DONE` (or whatever ordering the chosen protocol
needs) is paid by EVERY completion, not only await-compat ones. The task that
lands it must measure the steady-path cost of the chosen ordering with a
before/after `bench_native_net.sh` run against the Task 12 re-baseline row,
and cheaper shapes (for example, a fence taken only when `done_waiters` is
observed nonzero) remain admissible under the "equivalent proof" clause.

This epic is not expected to improve throughput. Its expected value is
stronger correctness and a smaller set of unknown ordering assumptions before
Phase 4.

## Subagent Plan

Use subagents after the document is accepted and task slices exist. Each
implementation subagent must first return a plan and wait for approval. A
separate review/testing subagent must inspect the same scope before the task
closes. Do not run two implementation subagents against the same C files at
the same time unless the write sets are explicitly split.

Suggested task order (final slicing happens in the task documents):

1. Kickoff: baseline capture + Sentrux session start (Epic 8 Task 1 shape).
2. Proving spike: select and rule the deterministic interleaving mechanism
   (see Approach); written interleaving models for all three windows.
3. `RV2-DEBT-023` fix (smallest surface; the candidate one-line token fix and
   its proof obligations are already recorded).
4. `RV2-DEBT-020` proof-or-fix (completed early as Task 3 with a generic
   join-route migration fix).
5. `RV2-DEBT-022` fix (protocol change on the completion/await handshake).
6. Optional dependency-aware completion/cancel split (`RV2-DEBT-003` riders
   apply).
7. Closeout (contract sweep, DEBT/NOTES/RUNTIME_V2 reconciliation, Sentrux
   session end).

Likely independent review surfaces:

- external-await ordering (`done_cv`, completion, `rt_task_await`);
- cancellation wake-token ordering (`cancel_task`, `park_current`,
  `current_task_cancelled`);
- accept-transition join migration (`rt_waiter_route.c`,
  `rt_scheduler_placement.c`, `rt_net_accept_group.c`);
- dependency-aware completion/cancel refactor, only after the safety model is
  stable.

## Epic Acceptance

Epic 9 is complete only when:

- `RV2-DEBT-022` is closed with a deterministic proof and an ordering argument;
- `RV2-DEBT-023` is closed with a deterministic proof and an ordering argument;
- `RV2-DEBT-020` is closed by the Task 3 deterministic migration-gap proof;
- every changed wake/cancel/await path has a written ownership and cleanup
  invariant;
- `runtime-v2-check`, `make check`, `make c-check`, `make cppcheck`,
  `git diff --check`, and applicable Sentrux/LOC checks pass or have recorded
  blockers unrelated to Epic 9;
- `DEBT.md`, `NOTES.md`, this document, `README.md`, and `docs/RUNTIME_V2.md`
  are updated with the final state.

## Next Runtime Handoff And Syntax Gate

If Epic 9 closes the local wakeup/cancel/accept-transition safety debts, the
next planning pass can choose between owner-replacement safety work and the
explicit Phase 4 crossing surface. Any task that changes Surge syntax,
keywords, parser rules, semantic checks, lowering, public examples, or public
crossing APIs must stop first for a dedicated language-syntax review with the
user. Names such as `far`, `submit_to`, `crosses`, and `shard-movable` remain
semantic placeholders until that review.
