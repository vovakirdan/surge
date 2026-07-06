# Epic 8: Task Lifecycle Lane And Net Fairness

**Goal:** move task lifecycle, join/done, worker await, and owner-local scope
bookkeeping off the remaining steady-path control lane. Epic 7 removed the
global executor lock from scheduler queues, waiter stores, sleep timers,
channels, and worker wake/park. Epic 8 removes the next measured
serialization point: task create/publish, join polling, completion epilogues,
and request-tree scope bookkeeping.

**Approach:** this epic stays runtime-only. Start with a fresh control-lock
census and a proving spike for shard-owned lifecycle state: task id/table
publication, handle lifetime, join waiter ownership, done result visibility,
scope owner lane, cancellation, and main-thread await compatibility. Add
behavior and static gates before implementation. Then migrate the lifecycle
path in slices: task create/publish, join/poll/release, completion epilogue,
scope bookkeeping, await/runner compatibility, and net fairness. The
8-shard/1024 starvation debt is investigated inside the same epic because it
is scheduler/lifecycle-adjacent and must be understood before Phase 4.

**Status:** complete. Opened against the Epic 7 closeout state on
2026-07-04 and closed after Task 14 with runtime-only scope preserved: no
syntax, parser, lowering, public crossing surface, inbound queues, remote
messages, eventfd credits, remote `select`, or seq-cst `PARKED` protocol work
landed in this epic.

## Closeout Summary

Epic 8 met its lifecycle and fairness contracts. Steady task creation, worker
join polling, normal done completion, and same-owner scope bookkeeping avoid
the control lane in the request steady path. The promoted
`TestRuntimeV2PerfControlLaneGate` is wired through `runtime-v2-perf-check`
and `runtime-v2-check`; the final Task 12 baseline measured about
`12.86` total control acquisitions/request and `9.36` steady-state control
acquisitions/request on the 8-shard/1024 row, materially below the Epic 7
`~26.4/request` baseline. The adopted 8-shard/1024 starvation debt was traced
to the placement funnel and fixed by F2 placement adoption at join consume.

Closed debt: `RV2-DEBT-015`, `RV2-DEBT-016`, `RV2-DEBT-019`, and
`RV2-DEBT-021`. Carried debt for the next planning pass:
`RV2-DEBT-020` (accept-transition join-waiter migration proof),
`RV2-DEBT-022` (external-await `done_cv` ordering), `RV2-DEBT-023`
(cancel vs RUNNING-to-WAITING park ordering), plus the longer-lived
`RV2-DEBT-003` and `RV2-DEBT-017` cleanup/compat items.

**Task documents:** `08-tasks/README.md` (index); expand each task document
before executing it, per the index rules.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/07-executor-lock-split-and-shard-runtime-state.md`
- `docs/runtime-v2-epics/07-executor-lock-dependency-map.md`
- `docs/runtime-v2-epics/07-locking-model-proving-spike.md`
- `docs/runtime-v2-epics/07-evidence.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/NOTES.md`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_async_scope.c`
- `runtime/native/rt_async_blocking.c`
- `runtime/native/rt_async_select.c`
- `runtime/native/rt_scheduler_placement.c`
- `runtime/native/rt_lane.c`
- `runtime/native/rt_channel_sync.c`
- `runtime/native/rt_worker_turn.c`
- `runtime/native/rt_async_waiter.c`
- `runtime/native/rt_async_poll.c`
- `runtime/native/rt_shutdown.c`
- `runtime/native/rt_net_poll_pass.c`
- `runtime/native/rt_net_accept_group.c`
- `internal/vm/runtime_v2_lock_split_*_test.go`
- `internal/vm/runtime_v2_accept_*_test.go`
- `internal/vm/runtime_v2_task_scope_blocking_waiter_contract_test.go`
- `internal/vm/mt_*_test.go`
- `scripts/bench_native_net.sh`
- `scripts/bench_native_channels.sh`
- `.github/workflows/ci.yml`

## Starting State Before Epic 8

Epic 7 split the old executor lock into per-shard lanes plus a reduced control
lane. The worker turn now runs under the owner shard lane, channel operations
have channel owner shards, waiter stores route by owner key, sleep timers use
per-shard stores, and `runtime-v2-lock-check` is part of `runtime-v2-check`.

The remaining measured hot control-lane consumer is task lifecycle:

- `__task_create` still takes `rt_control_lock`, increments `ex->next_id`,
  grows/publishes the task table, initializes task state, links parent/child
  state, assigns the owner shard, pushes ready work, and unlocks
  (`rt_async_task.c:8-43`).
- `rt_task_poll` still takes the control lane around join polling, optional
  child inline polling, result consumption, handle release, and waiter
  registration (`rt_async_task.c:79-148`).
- `rt_task_await` still uses `done_cv` under the control lane for
  multi-worker external awaits (`rt_async_task.c:183-218`). This is a
  compatibility path, but it must not force control-lane traffic onto normal
  worker joins.
- `rt_task_cancel`, `rt_task_clone`, checkpoint creation, and sleep task
  creation still acquire the control lane (`rt_async_task.c:220-303`).
- `mark_done` already has a shard-local fast path, but it still takes the
  control lane for residual wait-key cleanup, scope bookkeeping, scope
  cancellation, `done_cv`, and final free when required
  (`rt_async_state.c:1466-1560`).
- Scope APIs are control-lane operations today: scope enter/register/cancel,
  join, and exit all take `rt_control_lock`
  (`rt_async_scope.c:5-170`).
- `rt_executor` still owns `next_id`, `next_scope_id`, the atomic task table
  pointer, the scope table, `done_cv`, the scope-only control waiter store, and
  `done_waiters` (`rt_async_internal.h:251-290`).
- The invariant comment in `rt_async_internal.h` still describes the old
  executor-wide ownership model. Epic 8 must reconcile it with the actual
  post-Epic-7 lane model before closing.

The Epic 7 closeout matrix improved the 8-shard/1024 row by 23% against the
Epic 6 baseline, but still exposed the next bottleneck:

```text
control_lock_acquired=215781 for 8192 requests
about 26 control-lock acquisitions per request
8-shard/1024 still slower than the 1-shard row
```

`RV2-DEBT-016` records that task lifecycle remains the steady-path control
consumer. `RV2-DEBT-015` records the adopted 8-shard/1024-connection/100-request
starvation: request tails can exceed 10 seconds, then recover when load drops.

## Epic 8 Boundary Decisions

**Runtime-only scope.** Epic 8 does not change Surge syntax, keywords, parser
rules, semantic analysis, lowering, stdlib public examples, or public crossing
surface. `far`, `submit_to`, `crosses`, and shard-movable checking remain
semantic placeholders for the later Phase 4 epic. If a task concludes that
correctness requires inbound queues, remote messages, eventfd credits, remote
`select`, or the seq-cst `PARKED` protocol, stop and re-scope with the user.

**Keep spawn local by default.** Epic 8 preserves the Phase 3/7 rule: normal
spawn inherits the current task's owner shard, and non-worker spawn defaults to
shard 0. No distributed spawn is introduced. One cross-owner lifecycle edge
already exists and must be handled by name: the accept transition
(`rt_task_replace_owner` in `rt_scheduler_placement.c`, driven from
`rt_net_accept_group.c` and the net completion path), which re-places a parked
connection task onto the accepting shard and migrates its join waiters under
the control lane. If the spike finds any other compatibility path that can
produce cross-owner lifecycle edges, it must name that path's fallback too.
Every fallback must be explicit and measured; it must not become a new hidden
hot path.

**Task identity remains stable.** Task ids remain monotonic and task slots do
not get reused for different tasks. The task table may keep the Epic 7
atomic-snapshot structure, but the spike must decide which operations still
need the control lane:

- id allocation;
- task table growth;
- slot publication;
- slot clearing;
- final task memory free;
- handle refcount release;
- lock-free or owner-locked task lookup.

The preferred direction is: table growth stays control-lane, while steady-state
slot publication and lookup avoid the control lane when capacity already
exists and the owner shard lock or atomics provide the needed lifetime proof.

**Join belongs to the target owner.** Join waiters already route by target task
owner. Epic 8 makes worker-side join polling and completion observation use
that owner lane directly. Result fields remain written before the `TASK_DONE`
release store and read after an acquire load. A joiner that consumes the last
handle must not free the target while completion is still touching it.

**Completion is shard-local unless it really owns control work.** The common
completion path for a task with no scope, no external `done_cv` awaiter, no
foreign cleanup, and no final free should remain fully owner-shard-local.
Epic 8 must reduce the reasons `mark_done_needs_control` returns true. If a
remaining reason is not on the request hot path, document it as compatibility,
not as unresolved hot-path debt.

**Scopes get an owner-lane model, not distributed scopes.** Normal request
trees are shard-local by default, so scope bookkeeping can move to the scope
owner lane without Phase 4. A scope's owner is the owner shard of the task that
entered it. Scope child registration, child completion, `join_all`, and
failfast cancellation should avoid the control lane for same-owner request
trees. Cross-owner or externally initiated scope operations may keep a control
fallback in this epic. That fallback must preserve existing behavior and must
not pretend to be Phase 4 distributed scopes.

**External await stays a compatibility path.** `rt_task_await` from a
non-worker thread may keep `done_cv` and the control lane. Worker-side await
and join must not route through that path. If the main-thread runner or
single-worker compatibility path needs control for correctness, keep it narrow
and count it separately from request steady state.

**Cancellation stays bounded and legible.** Cancellation may still use current
task ids and existing wake helpers, but cleanup must remain proportional to the
registrations made by the cancelled task. No task may scan every shard or the
whole task table in the normal cancellation path. Scope failfast cancellation
must preserve current observable behavior before and after the scope lane move.

**Net fairness is investigated, not guessed.** `RV2-DEBT-015` may be caused by
poll behavior, ready/inject pacing, accept ownership, lifecycle control-lane
traffic, WSL2 host behavior, or a combination. Epic 8 must first reproduce and
instrument the stall, then choose a fix or a documented mitigation. A fairness
patch without a reproducer and before/after trace is not acceptable.

**The select slow lane stays control-serialized.** `rt_async_select.c` keeps
its control-lane scan and the control-era channel try wrappers this epic.
Select's timeout timers and join keys are lifecycle-adjacent, but migrating
select is not required to close `RV2-DEBT-016` and would widen the write set
of the join/completion tasks; it stays a named non-goal here and a candidate
for the epic that owns it. Lifecycle tasks must not regress select behavior
and must keep its wake-only waiter entries (`seq == 0`) working.

**No new hidden magic.** A new lifecycle helper must name its owner lane in its
API or in the invariant comment. Helpers that may take the control lane must
be visibly named or documented. New recoverable failures in V2 C helpers use
explicit status codes; `panic_msg` stays for invariant violations and legacy
adapters.

## Accepted Baseline Debt

Epic 8 owns or touches:

- `RV2-DEBT-015`: 8-shard/1024-connection/100-request starvation.
- `RV2-DEBT-016`: task lifecycle control-lane traffic.
- `RV2-DEBT-003`: `rt_async_state.c` remains a legacy over-limit file and must
  not grow without an extraction or a recorded debt update.
- `RV2-DEBT-007`: Sentrux thresholds are still calibrated to legacy code; do
  not weaken them.
- `RV2-DEBT-017`: sync-channel compatibility latency is relevant only if this
  epic changes completion, join, or compat wait paths.
- `RV2-DEBT-018`: rare VM harness transient may appear in full gates; do not
  attribute a new failure to it without focused rerun evidence.

Epic 8 does not own:

- `RV2-DEBT-001`, `RV2-DEBT-002`, `RV2-DEBT-011`, `RV2-DEBT-012`: broad
  VM/native/LLVM test matrix and harness work remains Epic 12 unless this epic
  directly changes the failing path.
- `RV2-DEBT-010`, `RV2-DEBT-013`: copied/raw net-handle ABI and stdlib HTTP
  owner-local redesign remain future net-handle work.
- `RV2-DEBT-005`: unrelated non-async native runtime large files.
- `RV2-DEBT-006`: channel benchmark timeout ownership, unless Epic 8 modifies
  the benchmark script for lifecycle/fairness evidence.

Any newly found debt must be added to `DEBT.md` with an owner before the task
that found it closes.

## Implementation Principles

- Keep changes owner-sized. A task should move one lifecycle surface, not
  rewrite the executor.
- Preserve `SURGE_SHARDS=1` behavior throughout.
- Keep lock order: control lane before at most one shard lock; never shard lock
  then control lock; never two shard locks.
- Prefer owner-lane APIs over boolean flags that hide lock ownership.
- Add tests before dropping the control lane from a path.
- Keep task lifetime rules written down before relying on atomics or unlocked
  reads.
- Do not use performance as a substitute for liveness proof.
- Do not delete compatibility paths until behavior tests prove they are unused
  or replaced.
- Keep `NOTES.md` current during every task and consolidate durable decisions
  into this document, `DEBT.md`, or `docs/RUNTIME_V2.md` before closeout.

## Proof And Quality Contract

Every runtime-code task must run and record:

- `git diff --check`;
- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- `make check`, unless the task document records a narrower approved gate;
- `./check_file_sizes.sh -a` when C/H files change;
- root, `runtime/`, and `runtime/native` Sentrux scans, plus rule results per
  `SENTRUX_POLICY.md`;
- effective LOC for every touched over-limit file and every new native runtime
  file.

Lifecycle tasks must also add or select focused probes for:

- owner-local task creation and ready publication;
- join polling and result observation across `SURGE_SHARDS=1`, `2`, and `8`;
- join waiter cleanup when the target completes before, during, and after
  registration;
- handle clone/release and last-reference free;
- scope enter/register/join/exit, including failfast cancellation;
- worker-side await vs external `rt_task_await` compatibility;
- shutdown with tasks parked in join, scope, timer, channel, blocking, and net
  wait states;
- no parked-with-work after lifecycle changes;
- no shard-0 fallback for owner-known task lifecycle paths.

Static gates should prove:

- migrated worker lifecycle paths do not call `rt_control_lock`;
- task table reads use the approved snapshot/slot protocol;
- join waiters route through the target owner store;
- scope waiters route through the scope owner or named control fallback;
- `done_cv` is used only by external/main-thread await compatibility;
- the invariant comment in `rt_async_internal.h` names the current owner lanes.

Performance/fairness gates must include:

- fresh Epic 8 baseline rows before implementation;
- Epic 7 closeout comparison rows for `1` and `8` shards at `1`, `8`, `32`,
  and `1024` connections;
- the 8-shard/1024-connection/100-request starvation reproducer with live
  trace evidence;
- `SURGE_TRACE_EXEC=1` rows showing `control_lock_acquired`,
  `cross_shard_wakes`, `spurious_wakes_absorbed`, `collect_wake_batches`,
  `parked_with_work`, and `owner_replacements` (exact `TRACE_EXEC` field
  names from Epic 7 Task 12);
- an explicit calculation of control-lock acquisitions per request before and
  after the lifecycle migration.

## Performance Contract

Epic 8 is complete only if `RV2-DEBT-016` has a measured resolution:

- task create, worker join polling, normal done completion, and same-owner
  request-tree scope bookkeeping avoid the control lane in steady state;
- the 8-shard/1024 row at least matches the fresh 1-shard row, or the closeout
  names a smaller remaining serialization point with evidence and a new owner;
- `control_lock_acquired` per request drops materially from the Epic 7
  closeout value of about 26/request. The Task 1 baseline sets the exact
  numeric target after reproducing current rows on the same host.

Epic 8 is also complete only if `RV2-DEBT-015` has a resolved state:

- fixed: the 8-shard/1024-connection/100-request probe completes without
  >10s request tails on the reference host; or
- constrained: the mechanism is pinned to platform behavior or a non-Epic-8
  subsystem with trace evidence, a stable mitigation, and an updated debt owner.

Small-load `1`, `8`, and `32` connection rows may move within noise, but any
material regression must be explained. Low-connection `SO_REUSEPORT` skew is
not a failure by itself; the high-connection row carries the scaling judgment.

## Subagent Plan

Use subagents for work that can be reviewed independently. Before a subagent
implements anything, ask it for a plan and approve or correct that plan. After
the implementation subagent finishes, launch a separate review/testing
subagent for the same scope. Do not close a task until the implementation
evidence and independent review evidence are both recorded.

Likely parallelizable scopes after Task 3:

- behavior tests and static shape tests, if they touch disjoint files;
- trace/benchmark script work and lifecycle implementation, if counters are
  designed first;
- scope-lane migration and net-fairness investigation only after the proving
  spike confirms their boundary.

Do not run two subagents against the same C files at the same time unless the
main session has split the write set precisely.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `08-tasks/01-kickoff-baseline-and-sentrux.md` | Record checkout, LOC, Sentrux, Runtime V2 gates, lifecycle control-lock census, and fresh net/fairness baselines. |
| 2 | `08-tasks/02-lifecycle-dependency-map.md` | Map task create, task table, join, done, handle refs, scope bookkeeping, await, cancel, and free to current locks and target lanes. |
| 3 | `08-tasks/03-lifecycle-lane-proving-spike.md` | Decide the shard-owned lifecycle model: id/table publication, lookup/free, join result visibility, scope owner lane, cancellation, and external await fallback. |
| 4 | `08-tasks/04-lifecycle-behavior-contract-tests.md` | Add focused behavior/liveness tests for create/join/done/await/scope/cancel/shutdown before migration. |
| 5 | `08-tasks/05-lifecycle-static-shape-and-trace-tests.md` | Add static gates and trace counters that prove migrated lifecycle paths stop using the control lane. |
| 6 | `08-tasks/06-task-create-and-table-publication.md` | Move steady-state task creation and ready publication to owner-lane ownership while keeping table growth safe. |
| 7 | `08-tasks/07-join-poll-and-handle-lifetime.md` | Move worker join polling, result reads, waiter registration races, clone/release, and final free protocol to the approved lifecycle model. |
| 8 | `08-tasks/08-completion-epilogue-and-done-path.md` | Reduce `mark_done_needs_control`, move common completion fully shard-local, and keep `done_cv` as external-await compatibility only. |
| 9 | `08-tasks/09-scope-owner-lane.md` | Move same-owner scope enter/register/join/exit/failfast bookkeeping to the scope owner lane with a named compatibility fallback for cross-owner cases. |
| 10 | `08-tasks/10-await-runner-blocking-compat.md` | Narrow `rt_task_await`, single-worker runner, blocking completion, and sync-compat interactions so they do not reintroduce hot control-lane traffic. |
| 11 | `08-tasks/11-net-fairness-starvation-investigation.md` | Reproduce, instrument, and fix or constrain `RV2-DEBT-015` with before/after trace evidence. |
| 12 | `08-tasks/12-performance-benchmark-and-ci-gate.md` | Produce final net/channel/lifecycle benchmark evidence and promote any stable lifecycle gate into `runtime-v2-check`. |
| 13 | `08-tasks/13-large-file-and-quality-tranche.md` | Reduce or pin touched over-limit files, update stale lane comments, and record Sentrux/LOC outcomes. |
| 14 | `08-tasks/14-epic-closeout.md` | Consolidate evidence, update durable docs, close or reassign debt, and state the next epic handoff plus syntax warning. |

## Epic Acceptance

Epic 8 is complete only when:

- the lifecycle proving spike is recorded and every implementation task follows
  it or records an approved deviation;
- `SURGE_SHARDS=1` preserves all stable Runtime V2 gates and `make check`;
- worker-side task create, join poll, normal done completion, and same-owner
  scope bookkeeping avoid `rt_control_lock` in steady state, proven by static
  gates and trace counters;
- task lookup, result visibility, handle release, and final free have a written
  lifetime rule and focused race tests;
- scope failfast, cancellation, join-all, and shutdown liveness tests pass
  under multi-shard configurations with explicit timeouts;
- external/main-thread await compatibility still works and is counted
  separately from worker steady state;
- the 8-shard/1024 starvation debt is fixed or constrained with evidence;
- performance evidence satisfies the Performance Contract above;
- any new lifecycle gate is wired into `runtime-v2-check` and CI before
  closeout;
- `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check` pass or have recorded blockers unrelated to Epic 8;
- root, `runtime/`, and `runtime/native` Sentrux scans and rule results are
  recorded with no unexplained quality drop;
- touched over-limit files have recorded line-count outcomes, and
  `rt_async_state.c` does not grow without an extraction or explicit debt
  update;
- `DEBT.md`, `NOTES.md`, this document, `README.md`, and `docs/RUNTIME_V2.md`
  are updated with the final state.

## Next Runtime Handoff And Syntax Gate

The lifecycle and fairness evidence is now known. The next runtime planning
pass should decide whether to close the carried wakeup/cancel/accept-transition
safety debts before starting the final crossing work. If the next epic changes
Surge syntax, keywords, parser rules, semantic checks, lowering, public
examples, or the explicit crossing surface, it must start with a dedicated
language-syntax review with the user. No task may introduce or bless names such
as `far`, `submit_to`, `crosses`, or `shard-movable` without that review.
