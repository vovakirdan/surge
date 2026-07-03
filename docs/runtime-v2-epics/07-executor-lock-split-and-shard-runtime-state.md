# Epic 7: Executor Lock Split And Shard-Owned Runtime State

**Goal:** remove `rt_executor.lock` from the Tier 1 hot path. After this epic,
steady-state scheduling, parking, waking, net readiness, sleep timers, and
same-shard channel operations run under per-shard locks, while a reduced global
control lane owns only task/scope identity, lifecycle bookkeeping, and
shutdown. Epic 6 proved owner placement under the preserved global lock; Epic 7
makes that ownership real at the synchronization level.

**Approach:** this epic changes lock ownership, not public behavior. Start
with a dependency map of every field the current single lock protects, then a
proving spike that fixes the two-lane locking model: lock ordering, the task
lifetime rule for lock-free-or-owner-locked task lookup, and the park/wake
protocol under split locks. Land behavior and static contract tests before
implementation. Implement in ownership-sized steps: shard lock structure
first with behavior identical, then scheduler queues, then waiter stores,
then sleep timers, then channels, then blocking/await/shutdown lanes. Keep
`SURGE_SHARDS=1` observable behavior compatible throughout, and keep the
Epic 6 accept-ownership result intact.

**Status:** in progress. Task documents are expanded one at a time per
`README.md` working mode.

**Task documents:** `07-tasks/01-kickoff-baseline-and-sentrux.md` through
`07-tasks/15-epic-closeout.md`, indexed in `07-tasks/README.md`.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/06-n2-accept-ownership-and-tier1-scheduler.md`
- `docs/runtime-v2-epics/06-evidence.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/NOTES.md`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_async_channel.c`
- `runtime/native/rt_async_scope.c`
- `runtime/native/rt_async_blocking.c`
- `runtime/native/rt_async_waiter.c`
- `runtime/native/rt_async_poll.c`
- `runtime/native/rt_scheduler_placement.c`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_net_poller.c`
- `runtime/native/rt_shutdown.c`
- `internal/vm/mt_*_test.go`
- `internal/vm/runtime_v2_*_test.go`
- `scripts/bench_native_net.sh`
- `scripts/bench_native_channels.sh`
- `.github/workflows/*`

## Starting State Before Epic 7

One mutex owns nearly all async runtime state. The invariant comment at
`runtime/native/rt_async_internal.h:259-271` states it directly: `ex->lock`
owns tasks/scopes, shard stores, scheduler queues and counters,
channel/blocking compatibility counters, net polling, timers, and shutdown.

Scheduler evidence:

- Every worker scheduler turn takes the global lock: pop ready, mark running,
  apply outcome (`rt_async_state.c:1656`, worker loop `1638-1727`). User task
  polls drop the lock; checkpoint/sleep/blocking task polls run under it
  (`rt_async_state.c:1693-1706`).
- All workers on all shards sleep on one global `ready_cv`
  (`rt_async_state.c:1675`), and multi-shard wakeups broadcast to every worker
  in the process (`signal_ready_workers`, `rt_async_state.c:737-743`).
- `park_current` and `wake_task_with_policy` mutate task park state, waiter
  stores, and ready queues under the global lock
  (`rt_async_state.c:1126-1160`, `1044-1074`).
- The parked-with-work invariant is asserted per shard but still under the
  global lock (`rt_async_state.c:1673`, `rt_scheduler_placement.c:90-99`).

Waiter evidence:

- Net waiter stores are owner-shard-local since Epic 6, but every non-net
  waiter kind — join, timer, scope, channel, blocking — is appended to shard
  0's store through the `rt_executor_waiter_store(ex)` compatibility accessor
  (`rt_async_waiter.c:438-459`, `rt_runtime.c:245-251`).
- `pop_waiter` and `remove_waiter` compact whole stores by scan under the
  global lock (`rt_async_waiter.c:407-436`, `503-546`).
- Accept readiness re-places a parked task onto the ready fd's owner shard at
  wake time (`rt_async_waiter.c:345-358`), so task owner shard is mutable at a
  controlled wake point, not only at spawn.

Timer evidence:

- Time is a virtual clock: `ex->now_ms` advances by one on every yielded poll
  (`tick_virtual`, `rt_async_state.c:1162-1180`) and jumps to the next
  deadline when idle (`advance_time_to_next_timer`,
  `rt_async_state.c:1206-1226`).
- Sleep deadlines are discovered by scanning the entire task table on every
  tick and every idle check (`rt_async_state.c:1170-1179`, `1182-1204`). The
  scan cost is O(total tasks) per yield, under the global lock.

Channel evidence:

- Direct channel send/recv/try/close paths take the global lock and use the
  shard-0 waiter store (`rt_async_channel.c:130-203`, `213-287`, `509-549`).
- The sync-channel compatibility path parks OS workers on the global
  `ready_cv` and may start compensation workers, all accounted under the
  global lock (`rt_async_state.c:1598-1636`, `1519-1574`).

Lifecycle evidence:

- Spawn allocates ids and publishes into `ex->tasks[]` under the global lock
  (`rt_async_task.c:18-53`, `676-725`; `rt_async_blocking.c:232-294`). Task
  ids are monotonically increasing and slots are keyed by id.
- `mark_done` clears wait keys, updates the parent scope, wakes join waiters,
  broadcasts the global `done_cv`, and may free the task, all under the global
  lock (`rt_async_state.c:1429-1474`).
- The blocking pool already has its own `blocking_lock`/`blocking_cv`
  (`rt_async_blocking.c:28-58`), but completion re-enters the global lock to
  wake the awaiting task (`rt_async_blocking.c:119-122`).
- Scopes are global objects mutated only under the global lock
  (`rt_async_scope.c`).
- Shutdown sets `ex->shutdown`, drains net waiters per shard, wakes all shard
  pollers, and broadcasts all three global condition variables
  (`rt_shutdown.c:18-43`).

Per-shard state that already exists and stays:

- shard scheduler queues, waiter store, fd registry, net poll scratch, heap
  accounting cells, net poll wake pipe, `net_polling` flag
  (`rt_async_internal.h:153-165`), and the Tier 1 no-steal placement gate
  (`rt_scheduler_placement.c:69-88`).

Benchmark starting point: the Epic 6 closeout report
(`build/benchmarks/runtime-v2-epic6-closeout-native-net.md`, regenerable)
showed the 8-shard/1024-connection row using all owner shards with zero
global-path fallbacks and zero steals, yet slower than the 1-shard row. The
preserved global executor lock is the recorded explanation. Epic 7 exists to
remove that explanation.

## Epic 7 Boundary Decisions

**Two-lane model.** Epic 7 targets exactly two lock lanes:

- a per-shard lock (working name `shard->lock`) with a per-shard worker
  condition variable, owning that shard's scheduler queues, running count,
  waiter store, sleep/timer store membership, net poll state, and the task
  state of tasks the shard owns;
- a reduced global control lane (the surviving `ex->lock` role), owning task
  and scope table storage, id allocation, task free, the scope tree,
  channel-blocking compatibility counters, shutdown flags, and the main-thread
  await path.

No third lane is allowed without a proving-spike record. The blocking pool's
existing `blocking_lock` remains as-is; it predates this epic and is already
isolated.

**Every task has an owner shard.** Today `owner_shard_valid == 0` falls back
to shard 0 (`rt_scheduler_placement.c:36-48`). Epic 7 makes owner-shard
assignment universal at task creation (spawn inherits the parent, non-worker
spawns get shard 0), because under split locks "which lock owns this task's
state" must never be ambiguous. Accept-readiness re-placement
(`rt_async_waiter.c:345-358`) remains legal but becomes an explicit owner
transition with a protocol chosen by the Task 3 spike.

**Lock ordering is a contract.** The epic fixes the order: a thread holding a
shard lock MUST NOT acquire the control lock, and MUST NOT acquire a second
shard lock. The control lock MAY be held while acquiring at most one shard
lock. Cross-shard wakes therefore use collect-then-wake: pop the waiter under
the key owner's lock, release it, then act under the task owner's lock.
Spurious wakes produced by that window are an accepted mechanism of the
existing `wake_token` protocol, must lead to a bounded re-park rather than a
spin, and must be visible in a trace counter.

**Task identity is stable.** Task ids never decrease and id slots are never
reused for a different task. Task memory may be freed only when status is
`TASK_DONE`, `handle_refs` is zero, and the freeing thread holds the locks the
spike's lifetime rule names (candidate: owner shard lock plus control lock for
the slot). Any hot-path task lookup outside those locks must be justified by
the spike's published rule, not by "it worked in tests".

**Virtual time semantics are preserved, its scan is not.** `now_ms` stays one
process-wide virtual clock in this epic; per-shard clocks would change
observable sleep semantics and belong to a later decision. The whole-table
sleep scans (`tick_virtual`, `next_sleep_deadline`,
`advance_time_to_next_timer`) are implementation artifacts, not contracts, and
are replaced by an explicit sleep store. Wake order for sleepers with equal
deadlines preserves the current ascending-task-id order unless a focused test
proves nothing observable depends on it.

**Channels get an owning shard.** A channel is owned by the shard of the task
that created it (shard 0 when created outside a task). Channel buffer state
and channel waiter keys live under the owner shard's lock. Same-shard
send/recv must not touch the control lane or any other shard's lock in steady
state. Cross-shard channel operations acquire the channel owner's lock and use
collect-then-wake for peers on other shards; that cost is acceptable and
counted. This is lock-based ownership only: Phase 4 cross-shard messaging,
credits, and the seq-cst `PARKED` wake protocol remain out of scope.

**Global compatibility paths shrink but survive.** The sync-channel
compatibility path (`rt_wait_current_worker_wakeup`, compensation workers),
main-thread `rt_task_await` on `done_cv`, and the seeded scheduler mode keep
working. They may keep using the control lane; they must not force the control
lane onto the per-poll hot path of unrelated tasks. Completion may touch the
control lane only when a control-lane waiter can exist or for lifecycle steps
the lifetime rule assigns there.

**Determinism boundary.** `SCHED_SEEDED` determinism is preserved for
`SURGE_SHARDS=1` (`TestMTSeededScheduler` stays a gate). Multi-shard seeded
determinism is not promised, matching Epic 6.

**Performance honesty.** Epic 7 removes the global lock from the hot path but
does not add cross-shard messaging, io backends, or allocator pools. Expected
wins: multi-shard TCP stops paying one process-wide mutex and one broadcast
condvar; same-shard channel pairs stop serializing against unrelated shards.
The 8-shard rows must be compared against both the Epic 6 closeout rows and
fresh 1-shard rows; any row that fails to improve gets an analysis naming what
still serializes, with a recorded owner for the follow-up.

## Accepted Baseline Debt

The broad focused VM command `go test ./internal/vm -run 'MT|Async|Net|LLVM'`
remains accepted backend-test debt (`RV2-DEBT-001`); do not add it as a green
gate. `RV2-DEBT-002` timeout-class flakes may appear in `runtime-v2-check`
runs; rerun-to-green is the accepted handling and new failures must be
separated from that class with evidence.

`RV2-DEBT-003` (`rt_async_state.c`, ceiling 1722 effective LOC) is directly in
scope: this epic rewrites its scheduler core and MUST NOT grow the file. New
lock, sleep-store, and wake-path code lands in new focused files at or under
500 lines. `RV2-DEBT-005`'s `rt_async_task.c` ceiling (731) applies the same
way. `RV2-DEBT-004` (`rt_net.c`, 818) is expected to stay mostly untouched.

`RV2-DEBT-010` and `RV2-DEBT-013` (raw/copied net-handle guards, stdlib HTTP
owner-locality) stay owned by a future net-handle ABI/stdlib task. Epic 7 must
not silently change copied-handle semantics while touching wake paths.

Any new lock-ordering, liveness, or benchmark debt discovered during this epic
must be closed before closeout or added to `DEBT.md` with an owner and close
condition.

## Scope

Included:

- map every field currently protected by `rt_executor.lock` to readers,
  writers, and a target lane (shard lock, control lane, atomic, or
  thread-local) with `file:line` evidence, before changing code;
- run a proving spike that fixes the locking model: lock ordering, the task
  lifetime/lookup rule, the park/wake and owner re-placement protocol under
  split locks, and the condvar layout (per-shard worker cv; fate of `io_cv`
  and `done_cv`);
- add focused behavior tests for cross-shard join, cancellation, channel
  send/recv/close, blocking completion, sleep/timeout, select across owners,
  and shutdown under `SURGE_SHARDS>1`, plus `SURGE_SHARDS=1` compatibility
  probes, before the implementation tasks that need them;
- add static shape tests for the new lock fields, the lock-order debug
  assertions, and bans on reintroducing global-lock acquisition into the
  migrated hot paths;
- introduce `shard->lock` and the per-shard worker condition variable with
  init/destroy lifecycle and debug lock-order/lock-owner assertions, landing
  structurally before any path stops taking the global lock;
- migrate scheduler ready queues, running counts, worker sleep/wake, yield
  requeue, and the parked-with-work invariant to the shard lock;
- migrate waiter stores to per-key owner locks: net keys stay with the fd
  owner shard; join, timer, and blocking keys move to the parked task's owner
  shard; channel keys move to the channel owner shard; scope keys follow the
  spike's decision for scope wake;
- make task owner-shard assignment universal at creation and make accept-time
  re-placement an explicit, asserted transition;
- replace whole-table sleep scans with an explicit sleep store while
  preserving one global virtual clock and `SURGE_SHARDS=1` observable timing
  semantics;
- give channels an owner shard and shard-local fast paths, preserving FIFO
  per key, close semantics, and the cancelled-waiter contracts already gated
  by `runtime-v2-waiter-check`;
- keep blocking submit/completion correct across lanes: completion wakes the
  awaiting task through its owner shard without re-entering a global choke
  point;
- keep main-thread `rt_task_await`, `run_until_done`, and the sync-channel
  compatibility path live and deadlock-free under the new ordering;
- make runtime shutdown wake every shard worker cv, every shard poller, the
  blocking pool, and any control-lane waiter, with no lost-wakeup window;
- add trace counters for control-lane acquisitions by class, same-shard vs
  cross-shard wakes, collect-then-wake batches, spurious wakes, and owner
  re-placements;
- extend net and channel benchmark evidence comparing single-shard and
  multi-shard rows against the Epic 6 closeout baseline with a
  current-checkout binary;
- add a stable `runtime-v2-lock-check` CI gate and wire it into
  `runtime-v2-check`;
- record LOC outcomes for every touched over-limit file and keep new files at
  or under 500 lines;
- keep `NOTES.md` and the Epic 7 evidence ledger current after every task.

Not included:

- no Surge syntax changes and no `far`/`submit_to`/`crosses` surface work;
- no Phase 4 cross-shard message transport: no inbound queues, no eventfd
  commitment, no credits, no seq-cst `PARKED` protocol (per-shard wake pipes
  and condvars remain the wake mechanism);
- no remote bounded-channel request/ack protocol and no remote `select`
  coordinator; cross-shard channel cost stays lock-based and visible only in
  counters;
- no migration control plane for moving live connections between shards;
- no Tier 2 CPU pool split beyond the existing blocking pool;
- no per-shard virtual clocks and no wall-clock timer rework;
- no remote-free queues, allocator ownership metadata, or slab/bump pools;
- no `epoll`/`kqueue`/`io_uring` backend work;
- no scheduler policy redesign: steal behavior inside a shard, LIFO slots, and
  injection policy keep their current shape except where lock ownership
  forces a mechanical change;
- no stdlib/HTTP server redesign (`RV2-DEBT-013` stays open);
- no broad VM/native/LLVM test-matrix rewrite (`RV2-DEBT-001`).

## Lock Ownership Contract

Epic 7 must make these properties true and testable:

- `SURGE_SHARDS=1` preserves current observable behavior: every stable
  Runtime V2 gate (`runtime-v2-check` including heap, waiter, fd-registry, and
  accept gates) and `make check` pass unchanged.
- `rt_executor.lock` no longer serializes steady-state Tier 1 execution. A
  connection task's readiness-wake-poll-park cycle and a same-shard channel
  handoff acquire only their owner shard's lock. A trace counter proves the
  control lane stays flat during the steady-state phase of the net benchmark.
- Each lock has a written owner list. Every field of `rt_executor`,
  `rt_shard`, `rt_task`, and `rt_scope` is assigned to exactly one lane
  (shard lock, control lane, designated atomic, or thread-local), recorded in
  the dependency map and enforced by the final invariant comment in
  `rt_async_internal.h`.
- Lock order holds everywhere: control lock before at most one shard lock;
  never shard lock then control lock; never two shard locks. A debug build
  assertion (env-gated like `rt_debug_assert_no_parked_with_work`) checks the
  order on every acquisition.
- Task ids are never reused; task memory is freed only under the lifetime
  rule's locks; every task lookup path is classified as owner-locked,
  control-locked, or spike-justified.
- No lost wakeups: for every waiter kind, wake-side and park-side serialize on
  the key owner's lock, and the `wake_token` exchange closes the
  collect-then-wake window. Spurious wakes are bounded (a woken task either
  observes its condition or re-parks once) and counted.
- No parked-with-work: a shard worker does not sleep on its shard cv while its
  shard has queued ready work, due sleepers, or required net wake work. The
  existing invariant assertion moves under the shard lock and stays enforced.
- Waiter cleanup stays proportional: cancellation and `mark_done` remove a
  task's registrations through its `wait_keys` back references, touching only
  the key owners involved, not every store.
- Channel semantics are preserved: FIFO per key, close wakes all waiters with
  closed status, cancelled waiters do not consume values or wakes
  (`runtime-v2-waiter-check` contracts), and the buffered-channel single-block
  allocation contract stays intact.
- Sleep semantics are preserved for `SURGE_SHARDS=1`: the virtual clock
  advances on yields and idle jumps exactly as before, and equal-deadline wake
  order is unchanged unless a recorded test proves the order unobservable.
- Blocking completion, failfast scope cancellation, timeout/select, and
  accept-time owner re-placement keep their existing observable contracts
  under `SURGE_SHARDS>1` stress probes.
- Shutdown wakes every sleeper on every lane exactly like today's broadcast
  set (`rt_shutdown.c:18-43` equivalent) and leaves no thread parked forever;
  a timeout-based liveness probe proves it for multi-shard runs.
- New V2 C primitives use owner-first arguments and explicit status codes for
  recoverable failures; `panic_msg` stays reserved for invariant violations
  and named legacy adapters.

## Proof And Quality Contract

Every runtime-code task must run:

- `git diff --check`;
- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- `make check`, unless the task document records a narrower approved gate;
- `./check_file_sizes.sh -a` when any C/H file changed;
- root, `runtime/`, and `runtime/native/` Sentrux scans plus rule checks,
  recorded per `SENTRUX_POLICY.md`;
- line counts for every touched over-limit file and every new or heavily
  rewritten native runtime file.

Lock-split tasks must also select or add focused probes that prove:

- `SURGE_SHARDS=1` compatibility for the migrated surface (scheduler, waiter,
  timer, channel, blocking, shutdown probes from `LIVENESS_PROBES.md` and the
  existing runtime-v2 gates);
- multi-shard liveness for the migrated surface: cross-shard join,
  cancellation, channel close, blocking completion, and shutdown complete
  without hangs under `SURGE_SHARDS>=2`, with explicit Go timeouts;
- the lock-order assertion and the parked-with-work assertion stay silent in
  debug-enabled stress runs;
- no `SCHED_TRACE steal` rows for connection tasks (Epic 6 contract stays);
- the control-lane trace counter stays flat during steady-state net load;
- the current broad VM/backend debt did not mask a new Epic 7 regression.

Scheduler, wakeup, cancellation, channel, timer, and shutdown changes need a
liveness proof, not only unit assertions: either a focused timeout-bounded Go
test or a live trace probe per `LIVENESS_PROBES.md`.

The epic must add a stable CI gate (`runtime-v2-lock-check`) before closeout,
covering the smallest deterministic lock-split subset with
`SURGE_BACKEND=llvm`, `SURGE_SKIP_TIMEOUT_TESTS=0`, `-count=1`, `-parallel=1`,
and `-p=1`.

## Performance Contract

Epic 7 is the epic that claimed the global lock was the bottleneck; it must
measure that claim.

Required evidence:

- build and use the current-checkout `surge` binary for every row;
- net rows: 1-shard and 8-shard at 1, 8, 32, and 1024 connections, same
  matrix as the Epic 6 closeout report, compared against that report's
  numbers;
- channel rows: `scripts/bench_native_channels.sh` baseline vs post-split,
  single-shard, plus a multi-shard row if the script supports it without
  redesign (its timeout debt is `RV2-DEBT-006`);
- trace counters: control-lane acquisitions, cross-shard wakes, spurious
  wakes, collect-then-wake batches, owner re-placements, plus the Epic 6
  accept/steal counters to prove no regression;
- an explanation for every row: small-load rows (1, 8, 32 connections,
  1-shard) must not regress beyond noise; the 8-shard/1024 row must improve
  against the Epic 6 closeout baseline or the closeout must name the next
  serialization point with evidence and an owner.

A flat multi-shard result no longer gets the Epic 6 "global lock preserved"
excuse. If multi-shard throughput still loses to single-shard after the split,
the epic cannot close on benchmarks alone: it needs a recorded analysis
(counters, profile, or trace) of what serializes, and a follow-up owner.

## Refactor Safety Contract

Epic 6's contract applies unchanged, with the lock-specific additions:

- write or select the behavior proof before moving code;
- record the dependency cluster and owning lane before extraction;
- keep behavior changes out of refactor commits and lock-lane changes out of
  file-move commits;
- one lane migration at a time: a single task must not move scheduler queues
  and waiter stores and timers in one step;
- every intermediate commit state holds the full lock-order contract — a
  nested transition (shard lock inside the global lock, one consistent order)
  is the only sanctioned intermediate;
- no catch-all files; new files own one concept (a lock lane, a sleep store,
  a wake path) and stay at or under 500 effective lines;
- touched over-limit files stay flat or shrink (`rt_async_state.c` 1722,
  `rt_async_task.c` 731, `rt_net.c` 818 effective LOC ceilings);
- delete code only with reference, build, test, and Sentrux evidence;
- record rejected paths in `NOTES.md`.

## Parallelization Model

Task 1 runs first. Tasks 2 and 3 may be planned together but Task 3's spike
result can rewrite Task 2's lane assignments; reconcile both before Tasks 4
and 5 start. Tasks 4 and 5 may run in parallel with each other once the spike
has fixed the model. Tasks 6 through 11 are strictly sequenced: each migrates
state the next one locks against. Tasks 12 and 13 may overlap the tail of Task
11 only with disjoint write sets. Any subagent follows `RULES.md` Global Rule
9: plan first, approval before edits; if subagents are unavailable, the main
session records that and proceeds locally, as in Epic 6 Task 14.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `07-tasks/01-kickoff-baseline-and-sentrux.md` | Record checkout, line counts, gate results, Sentrux baseline, benchmark baseline rows, and the final Epic 7 gate plan. |
| 2 | `07-tasks/02-executor-lock-dependency-map.md` | Map every global-lock-protected field to readers, writers, and target lane with `file:line` evidence. |
| 3 | `07-tasks/03-locking-model-proving-spike.md` | Fix the two-lane model: lock ordering, task lifetime/lookup rule, park/wake and owner re-placement protocol, condvar layout. |
| 4 | `07-tasks/04-lock-split-behavior-contract-tests.md` | Add focused multi-shard behavior tests for join, cancel, channel, blocking, sleep/select, and shutdown liveness. |
| 5 | `07-tasks/05-lock-split-static-shape-tests.md` | Add static gates for lock fields, order assertions, and bans on global-lock reintroduction in migrated paths. |
| 6 | `07-tasks/06-shard-lock-structure-landing.md` | Introduce `shard->lock` + per-shard worker cv with lifecycle and debug assertions, behavior identical (nested under the global lock). |
| 7 | `07-tasks/07-scheduler-ready-and-park-wake-migration.md` | Move ready queues, running counts, worker sleep/wake, and parked-with-work under the shard lock; peel the global lock off the worker loop. |
| 8 | `07-tasks/08-waiter-store-key-ownership-migration.md` | Move waiter stores to per-key owner locks with collect-then-wake and universal task owner assignment. |
| 9 | `07-tasks/09-sleep-timer-store-and-virtual-clock.md` | Replace whole-table sleep scans with an explicit sleep store; keep one virtual clock and N=1 timing semantics. |
| 10 | `07-tasks/10-channel-owner-shard-migration.md` | Give channels an owner shard; shard-local fast path; cross-owner ops via collect-then-wake; preserve channel contracts. |
| 11 | `07-tasks/11-blocking-await-shutdown-lanes.md` | Re-lane blocking completion, main-thread await/done_cv, sync-channel compat, and shutdown wake-all. |
| 12 | `07-tasks/12-lock-split-trace-counters-and-benchmarks.md` | Add lock-split trace counters and produce the net + channel benchmark evidence against the Epic 6 baseline. |
| 13 | `07-tasks/13-runtime-v2-lock-ci-gate.md` | Add a stable `runtime-v2-lock-check` target and wire it into `runtime-v2-check` and CI. |
| 14 | `07-tasks/14-large-file-and-loc-tranche.md` | Verify extraction boundaries, reduce or pin touched over-limit files, tighten `.loc-legacy-allowlist`. |
| 15 | `07-tasks/15-epic-closeout.md` | Consolidate evidence, update durable docs, close or record debt, and state the Epic 8 handoff plus the syntax gate. |

## Epic Acceptance

Epic 7 is complete only when:

- the Lock Ownership Contract properties are implemented and tested;
- `SURGE_SHARDS=1` preserves all stable Runtime V2 gates and `make check`;
- the worker scheduler loop, park/wake, waiter stores, sleep timers, and
  channel operations no longer acquire `rt_executor.lock` on their steady
  paths, proven by code shape gates and the control-lane trace counter;
- the lock-order and parked-with-work debug assertions exist, run in the
  focused stress probes, and stay silent;
- multi-shard liveness probes for join, cancellation, channel close, blocking
  completion, and shutdown pass with explicit timeouts;
- the Epic 6 accept gate (`runtime-v2-accept-check`) still passes and
  connection tasks still show zero non-owner steals;
- benchmark evidence satisfies the Performance Contract, including the
  explained comparison against the Epic 6 closeout rows;
- `runtime-v2-lock-check` runs in `make runtime-v2-check` and CI;
- `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check` pass or have recorded blockers unrelated to Epic 7;
- root, `runtime/`, and `runtime/native/` Sentrux scans and rule checks are
  recorded as pass/fail evidence with no unexplained quality drop;
- touched over-limit files have recorded line-count outcomes; new files are at
  or under 500 effective lines;
- every Epic 7 debt is closed or recorded in `DEBT.md` with an owner and close
  condition;
- `07-evidence.md`, `NOTES.md`, this document, `README.md`, and
  `docs/RUNTIME_V2.md` phase notes are updated with the final state.

## Next Runtime Handoff And Syntax Gate

Epic 8 owns the explicit crossing surface and Phase 4 transport. It must start
with a dedicated language-syntax review with the user; `far`, `submit_to`,
`crosses`, and `shard-movable` remain semantic placeholders until then. Epic 7
must not pre-implement Phase 4 mechanics: if a task concludes that correctness
requires inbound message queues or the seq-cst `PARKED` protocol, stop and
re-scope with the user instead of building transport under a lock-split label.
