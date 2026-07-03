# Epic 6: N>1 Accept Ownership And Tier-1 Scheduler Boundary

**Goal:** enable the first real multi-shard Runtime V2 slice for native TCP
accept/read/write paths: accepted connections stay on the accepting shard,
their fd registry and waiters stay shard-owned, and connection tasks stop using
hot-path stealing as a balancing mechanism.

**Approach:** this epic changes shard ownership and scheduler placement while
the global executor lock remains in place. Start with a proving spike for the
listener model, because the current public `TcpListener` is one opaque handle
while the target prefers per-shard `SO_REUSEPORT` accept sockets on Linux. Then
add tests, static shape gates, runtime configuration, owner metadata, accept
distribution, no-steal connection scheduling, per-shard net poller ownership,
trace counters, benchmarks, and CI gates. Keep `SURGE_SHARDS=1` behavior
compatible throughout the epic.

**Status:** draft. Task files have not been expanded yet.

**Task documents:** to be created under `06-tasks/` after this epic document is
approved.

## Inputs

- `docs/RUNTIME_V2.md`
- `docs/runtime-v2-epics/README.md`
- `docs/runtime-v2-epics/RULES.md`
- `docs/runtime-v2-epics/SENTRUX_POLICY.md`
- `docs/runtime-v2-epics/EVIDENCE_TEMPLATE.md`
- `docs/runtime-v2-epics/LIVENESS_PROBES.md`
- `docs/runtime-v2-epics/04-persistent-fd-registry-and-net-lifecycle.md`
- `docs/runtime-v2-epics/04-evidence.md`
- `docs/runtime-v2-epics/05-per-shard-heap-accounting.md`
- `docs/runtime-v2-epics/05-evidence.md`
- `docs/runtime-v2-epics/DEBT.md`
- `docs/runtime-v2-epics/NOTES.md`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_fd_registry.c`
- `runtime/native/rt_heap_accounting.c`
- `runtime/native/rt.h`
- `internal/vm/mt_*_test.go`
- `internal/vm/runtime_v2_*_test.go`
- `internal/vm/runtime_v2_fd_registry_static_test.go`
- `internal/vm/runtime_v2_skeleton_static_test.go`
- `scripts/bench_native_net.sh`
- `.github/workflows/*`

## Starting State Before Epic 6

Runtime structure is still `N=1`:

- `RT_RUNTIME_SHARD_COUNT` is `1U`.
- `rt_runtime` owns a fixed `shards[RT_RUNTIME_SHARD_COUNT]` array.
- `rt_runtime_init_global_n1()` initializes only `shards[0]`.
- many compatibility accessors call `rt_runtime_shard0()` and return shard 0
  state.
- `rt_executor.lock` still owns tasks, scopes, scheduler queues, waiters, fd
  registry rows, net poll scratch, channel compatibility counters, timer state,
  and shutdown flags.

Scheduler structure is still worker-oriented, not shard-oriented:

- one shard contains many worker-local queues;
- workers pop local work, injected work, and stolen work from other worker
  queues;
- `SCHED_TRACE steal` currently proves the existing scheduler can steal work,
  but Runtime V2 Tier 1 connection tasks must not rely on that path.

Net structure is ready for `N=1` owner-local polling but not for `N>1` accept
ownership:

- `rt_net_listen()` creates one nonblocking listener fd and sets
  `SO_REUSEADDR`, not `SO_REUSEPORT`;
- `rt_net_accept()` accepts on that one fd and returns a connection struct with
  the raw fd view;
- `poll_net_waiters()` builds poll input from the shard-owned fd registry, but
  all current net accessors still route through the executor's shard-0
  compatibility helpers;
- wake-pipe state, the I/O poll thread, and `net_polling` are process-global or
  executor-global compatibility state;
- copied `TcpConn` and `TcpListener` handle generation remains open debt
  (`RV2-DEBT-010`).

Heap accounting no longer adds a global hot counter to every allocation/free
event. That means Epic 6 can measure scheduler sharding without the old heap
counter source of truth masking the result.

Some current Runtime V2 static gates deliberately pin `N=1`. Task 6 must update
those contracts instead of discovering the break mid-implementation:

- `runtime_v2_skeleton_static_test.go` asserts `RT_RUNTIME_SHARD_COUNT == 1`;
- `runtime_v2_fd_registry_static_test.go` has an `N=1` static boundary;
- Epic 5 heap-accounting static gates may need equivalent owner-index updates if
  shard storage changes.

## Epic 6 Boundary Decisions

Epic 6 makes structural `N>1` visible under the existing global lock.
`rt_executor.lock` continues to own tasks, scopes, scheduler queues, waiter
stores, fd registry mutation, net poll scratch, channel compatibility counters,
timer state, and shutdown flags. Lock sharding is the next runtime ownership
epic, not part of Epic 6.

Performance expectations must match that boundary. Under one executor lock,
Epic 6 should not promise line-rate throughput scaling. The expected wins are
clear ownership, better locality, and removal of connection-task steal churn.
Benchmarks must explain throughput improvement or lack of improvement in that
light.

Thread ownership follows the target shard model. With `SURGE_SHARDS>1`, Tier 1
uses one worker per shard. `SURGE_THREADS` remains the compatibility worker
count for `SURGE_SHARDS=1`; in multi-shard mode, `SURGE_THREADS` must be unset
or equal to `SURGE_SHARDS`. A conflicting value is an explicit configuration
error, not a silent override.

No-steal is shard-relative. With `SURGE_SHARDS=1`, there is no non-owner shard,
so the no-steal rule is vacuous and current intra-shard worker stealing remains
a compatibility path. Existing MT gates such as `TestMTWorkStealing` and
`TestMTSeededScheduler` must keep passing for the `SURGE_SHARDS=1` path.

Epic 6 owns per-shard net polling. Each shard that owns net fds must have a
poller owner and a wake mechanism for registry changes, close, cancellation,
and shutdown. The implementation must choose either a poller thread per shard
or shard-worker-owned polling before net lifecycle migration starts.

Per-shard wake in Epic 6 is not the Phase 4 cross-shard transport. A pipe is an
acceptable first wake mechanism. Eventfd, inbound message queues, target
credits, and the seq-cst `PARKED` protocol remain Phase 4 work and must not be
implemented or pre-decided here.

Everything except net accept/readiness ownership keeps global compatibility
semantics in Epic 6. Channels, task join, scope wake, cancellation, blocking
completions, timers, `now_ms`, sleep scans, and generic ready work still operate
under `ex->lock` without cross-shard messaging. Static gates must forbid
shard-0 fallback only for the net ownership path that Epic 6 moves; they must
not fail legitimate global compatibility paths.

The preferred runtime storage shape is `RT_RUNTIME_MAX_SHARDS` plus runtime
`shard_count`, not an unbounded dynamically allocated shard array, unless Task
6 proves a different shape is simpler and equally testable.

## Accepted Baseline Debt

The broad focused VM command
`go test ./internal/vm -run 'MT|Async|Net|LLVM'` remains accepted backend-test
debt. Do not add it as a required green gate in this epic.

`RV2-DEBT-001`, `RV2-DEBT-002`, and `RV2-DEBT-011` remain owned by Epic 11
unless a task explicitly changes the related test-harness path.

`RV2-DEBT-003` and `RV2-DEBT-004` are relevant because this epic will likely
touch `rt_async_state.c` and `rt_net.c`. Every task that touches an over-limit
file must record line counts. A refactor tranche is part of this epic after the
behavior proof exists.

`RV2-DEBT-010` is in scope only if a task changes listener or connection handle
representation. If the epic leaves copied handle generation open, the closeout
must say why and keep the debt owner explicit.

`RV2-DEBT-012` is not an Epic 6 close condition unless a task changes the heap
benchmark script or allocation-heavy benchmark surface.

Any new accept-ownership, no-steal, shard lifecycle, or benchmark debt
discovered during this epic must be closed before closeout or added to
`DEBT.md` with an owner and close condition.

## Scope

Included:

- map current runtime/shard, scheduler, net listener, fd registry, close,
  cancellation, wake-fd, shutdown, and benchmark paths before changing code;
- run a proving spike for the listener model: per-shard `SO_REUSEPORT` sockets
  where available, or a documented explicit accept handoff fallback if the
  current public handle shape blocks the ideal first implementation;
- introduce an internal shard-count configuration, likely `SURGE_SHARDS`, with
  `1` as the compatibility default until CI promotes a multi-shard gate;
- initialize `N` shards with their own scheduler state, waiter store, fd
  registry, net poll scratch, heap accounting, and trace counters;
- keep all shard-owned state under the existing global executor lock for this
  epic;
- define and enforce the `SURGE_SHARDS` and `SURGE_THREADS` interaction;
- preserve public Surge syntax, public standard-library signatures, and native
  net ABI while changing internal ownership;
- attach owner-shard metadata to listener and connection runtime objects;
- keep every accepted connection fd registered on exactly one owning shard;
- keep accepted connection tasks on the owning shard: local spawn from a
  shard-owned request task stays on that shard, and a task that uses a
  shard-owned connection must either be on the owner shard or fail a debug/static
  proof added by this epic;
- prevent Tier 1 connection tasks from being stolen by a non-owner shard;
- make any compatibility handoff explicit in code, trace counters, and
  evidence;
- prove `SURGE_SHARDS=1` behavior remains compatible with Epic 4 and Epic 5
  gates;
- add focused multi-shard accept, fd-readiness, close, cancellation, and
  shutdown tests;
- add focused parked-with-local-work invariants for the no-steal scheduler path;
- update the current `N=1` static gates to the new runtime-shard contract;
- add static checks that prevent shard-0-only net ownership shortcuts from
  returning while preserving legitimate global compatibility paths;
- add per-shard net poller and wake ownership for registry changes, close,
  cancellation, and shutdown;
- add trace counters for shard count, accepted connections by shard,
  connection-task owner placement, denied or avoided Tier 1 steals, global-path
  fallbacks, fd readiness batches, and shard imbalance;
- add stable local and CI gates for the green multi-shard subset;
- extend or add native net benchmark evidence that compares single-shard and
  multi-shard rows with a current-checkout binary;
- keep `NOTES.md` and the Epic 6 evidence ledger current after every task.

Not included:

- no Surge syntax changes;
- no final keyword choices for `far`, `submit_to`, `crosses`, or
  shard-movable markers;
- no parser, semantic-analysis, async-lowering, or public example changes for
  crossing syntax;
- no cross-shard user messages, inbound transport credits, remote bounded
  channel protocol, remote select, or distributed scope protocol;
- no Phase 4 inbound queues, eventfd commitment, credit-return protocol, or
  seq-cst `PARKED` cross-shard wake protocol;
- no sharding of `rt_executor.lock`, task storage, scope storage, timer scans,
  blocking completions, or global channel semantics;
- no remote-free queues, owner-shard allocator metadata, slab allocator, or
  bump allocator;
- no Tier 2 CPU-pool syntax or scheduler split beyond preserving the existing
  blocking-worker compatibility path;
- no migration control plane for moving an existing connection to another
  shard;
- no `epoll`, `kqueue`, or `io_uring` backend migration;
- no broad VM/native/LLVM test-matrix rewrite;
- no unrelated channel, timer, filesystem, bignum, string, or terminal cleanup.

## Accept Ownership Contract

Epic 6 must make these properties true and testable:

- `SURGE_SHARDS=1` preserves the current observable native net behavior.
- `SURGE_SHARDS=N`, where `N > 1`, initializes exactly `N` runtime shards or
  fails with an explicit status and diagnostic.
- `SURGE_SHARDS>1` creates one Tier 1 worker per shard. `SURGE_THREADS` is a
  compatibility control for `SURGE_SHARDS=1`; in multi-shard mode, a conflicting
  `SURGE_THREADS` value fails explicitly.
- `rt_executor.lock` remains the single state lock in Epic 6. The code may split
  structures by owner, but it must not claim lock-level scalability.
- Each shard owns its scheduler state, waiter store, fd registry, net poll
  scratch, heap accounting cells, and trace counters.
- A listener object knows whether it is a single-fd listener, a per-shard
  listener group, or an explicit fallback handoff listener.
- A per-shard listener group is closed as one logical listener handle. Closing
  it closes every fd in the group and wakes or cancels waiters on every owning
  shard. Linux may drop connections sitting in a closed `SO_REUSEPORT` socket's
  accept queue; Epic 6 must record this as expected OS behavior rather than
  promising those queued connections survive close.
- Each accepted connection has one owner shard at creation time.
- The accepted connection fd is registered in the owning shard's fd registry,
  not a process-wide or shard-0 registry.
- Read, write, close, cancellation, and shutdown for that connection use the
  owner shard's registry and waiter state.
- A local spawn from a request task inherits the current shard. A task that acts
  on a `TcpConn` or `TcpListener` must run on that object's owner shard unless a
  future explicit migration path moves it. This epic does not implement
  migration.
- A non-owner task must not silently operate on a shard-owned connection through
  shard 0, a global lock fallback, or an implicit handoff. The task must either
  be rejected by an Epic 6 guard or recorded as explicit future debt before
  closeout.
- Tier 1 connection tasks are not stolen by non-owner shards. If the current
  scheduler cannot prove task class or ownership yet, the task must add that
  metadata before disabling steals.
- CPU-bound non-connection work may keep using the current compatibility
  scheduler while Tier 2 is still future work, but it must not justify stealing
  connection tasks.
- The one-user-accept-loop API conflict must be resolved before implementation:
  Task 3 must decide where internal accept tasks live and how a handler task
  lands on the accepted connection's owner shard without new Surge syntax.
- Any accept handoff fallback is visible in trace counters and benchmark
  evidence. It must not be described as the ideal `SO_REUSEPORT` hot path.
- Each shard that owns net fds has a poller owner and wake mechanism. This is
  local net readiness plumbing only; it is not a cross-shard message transport.
- Closing a listener or connection wakes or cancels only waiters owned by the
  correct shard and does not complete stale waiters on another shard.
- Runtime shutdown wakes every shard poller and worker without leaving live
  connection waiters or benchmark child processes behind.

New V2 C primitives must use owner-first arguments and explicit status codes for
recoverable failures. Recoverable multi-shard configuration, listener-group
creation, shard initialization, fd registration, and scheduler-placement errors
must not call `panic_msg` unless the task documents an unrecoverable invariant
violation.

## Proof And Quality Contract

Every runtime-code task must run:

- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- `make check`, unless the task document records a narrower approved gate;
- `git diff --check`;
- root, `runtime/`, and `runtime/native/` Sentrux scans plus rule checks;
- line counts for every touched over-limit file and every new or heavily
  rewritten native runtime file.

Accept-ownership tasks must also select or add focused probes that prove:

- the `SURGE_SHARDS=1` compatibility path remains green;
- `SURGE_SHARDS=2` or higher initializes multiple shards and rejects invalid
  values deterministically;
- `SURGE_SHARDS>1` runs one Tier 1 worker per shard and rejects conflicting
  `SURGE_THREADS` values;
- accepted connections are distributed across the intended shard owners or the
  chosen fallback records explicit handoff rows;
- each accepted connection's read/write waiters and fd registry entry live on
  the owner shard;
- close, cancellation, fd readiness, and shutdown do not cross into shard 0 by
  accident;
- connection tasks are not reported through `SCHED_TRACE steal`;
- a shard worker or poller does not sleep while its own ready queue, local
  fd-ready batch, or required wake work is non-empty;
- stale close or copied-handle cases either remain protected or keep
  `RV2-DEBT-010` open with exact evidence;
- the current broad VM/backend debt did not mask a new Epic 6 regression.

The epic must add a stable CI gate before closeout. The gate should run the
smallest deterministic multi-shard accept subset with `SURGE_BACKEND=llvm`,
`SURGE_SKIP_TIMEOUT_TESTS=0`, `-parallel=1`, and `-p=1`.

## Performance Contract

Epic 6 is performance-sensitive. It must not close with only functional tests.

Required evidence:

- build and use the current checkout `surge` binary for every benchmark row;
- compare single-shard and multi-shard native TCP rows;
- include small-load rows for 1, 8, and 32 connections;
- include at least one higher-load row near 1k connections, and 10k if the
  current benchmark harness can run it safely;
- record trace counters for accepted connections by shard, global-path
  fallbacks, connection-task steals, fd readiness batches, and shard imbalance;
- explain any small-load latency regression and any many-connection throughput
  improvement or lack of improvement.

The existing 32-connection native net probe is useful as a regression check, but
it is not enough by itself to prove shared-nothing scaling. `SO_REUSEPORT` skew
at 1, 8, and 32 connections is expected and is not a failure by itself. Shard
distribution is judged from the higher-load row and trace counters.

Because Epic 6 keeps the global executor lock, a flat or disappointing
throughput result is acceptable if the evidence proves correct owner placement,
reduced or eliminated connection-task steals, and no regression in the stable
small-load rows.

## Refactor Safety Contract

Refactoring in this epic is allowed only when it satisfies this contract:

- write or select the behavior proof before moving code;
- record the dependency cluster and owning module before extraction;
- keep behavior changes out of refactor commits;
- move one responsibility at a time;
- do not create catch-all files such as `common`, `misc`, or vague `helpers`;
- keep new or heavily rewritten runtime files at or below 500 lines;
- reduce or keep flat every touched over-limit file unless the task records a
  specific proving-spike exception;
- use owner-first APIs and explicit status codes for new V2 primitives;
- delete code only after proving the symbol is unreachable or obsolete through
  references, build, tests, and Sentrux evidence;
- record rejected paths in `NOTES.md` so they are not rediscovered later.

## Parallelization Model

After Task 1, dependency mapping, listener-model research, behavior tests, and
static shape tests can be planned in parallel if their write sets stay separate.

Implementation tasks should stay sequenced until the listener model and
connection-task ownership metadata are chosen. Review subagents may run after
each implementation task. Every subagent must start with a plan-only pass and
wait for main-agent approval before edits, test-writing, or review work starts.

## Brief Task List

| Task | Document | Purpose |
| --- | --- | --- |
| 1 | `06-tasks/01-kickoff-baseline-and-sentrux.md` | Record checkout, line counts, accepted debt, Sentrux state, current Runtime V2 gates, net probes, and final Epic 6 gate plan. |
| 2 | `06-tasks/02-accept-ownership-dependency-map.md` | Map runtime/shard, scheduler, listener, accept, fd registry, wake, close, shutdown, handle, and benchmark dependencies. |
| 3 | `06-tasks/03-listener-model-proving-spike.md` | Decide the first implementable listener model and answer the one-user-accept-loop conflict: where internal accept tasks live and how handler tasks land on owner shards without new syntax. |
| 4 | `06-tasks/04-multishard-accept-contract-tests.md` | Add focused behavior tests for shard-count config, accept distribution, owner-local fd readiness, close, cancellation, and shutdown. |
| 5 | `06-tasks/05-multishard-static-shape-tests.md` | Add static checks for dynamic shard count, net-only shard-0 shortcut bans, owner metadata, and no Tier 1 connection steals without failing global compatibility paths. |
| 6 | `06-tasks/06-runtime-shard-array-and-config.md` | Replace the fixed `N=1` runtime skeleton with an internal `N>=1` shard configuration, update `N=1` static pins, and prefer `RT_RUNTIME_MAX_SHARDS` plus runtime `shard_count`. |
| 7 | `06-tasks/07-per-shard-scheduler-placement.md` | Add owner-shard placement metadata, route connection tasks to owner queues without stealing by non-owner shards, and add the parked-with-local-work invariant. |
| 8 | `06-tasks/08-listener-and-connection-owner-metadata.md` | Attach shard owner and lifecycle metadata to listener/connection runtime objects, including listener-group close semantics, and keep public ABI stable. |
| 9 | `06-tasks/09-accept-distribution-implementation.md` | Implement the selected listener model and prove accepted fds enter the owning shard's registry. |
| 10 | `06-tasks/10-per-shard-poller-and-wake-ownership.md` | Add per-shard net poller ownership and a local wake mechanism for registry changes, close, cancellation, and shutdown without implementing Phase 4 inbound messaging. |
| 11 | `06-tasks/11-multishard-net-lifecycle-migration.md` | Migrate read/write waiters, close, cancellation, and shutdown paths to the selected per-shard owner model while non-net primitives stay globally compatible. |
| 12 | `06-tasks/12-trace-counters-and-benchmark-evidence.md` | Add accept-owner and no-steal trace counters, extend net benchmark evidence, and record single-shard versus multi-shard rows calibrated for the global-lock boundary. |
| 13 | `06-tasks/13-runtime-v2-accept-ci-gates.md` | Add a stable `runtime-v2-accept-check` target and wire it into local Runtime V2 gates and CI. |
| 14 | `06-tasks/14-large-file-refactor-tranche.md` | Split cohesive scheduler/net responsibilities after behavior is proven and reduce touched over-limit files. |
| 15 | `06-tasks/15-epic-closeout-and-static-gates.md` | Consolidate evidence, update durable docs, close or record epic-owned debt, and state the next runtime handoff plus the language syntax gate. |

## Epic Acceptance

Epic 6 is complete only when:

- `SURGE_SHARDS=1` preserves the current stable Runtime V2 behavior;
- `SURGE_SHARDS>1` initializes a bounded number of shards with explicit failure
  behavior for invalid configuration;
- multi-shard mode uses one Tier 1 worker per shard and handles `SURGE_THREADS`
  conflicts explicitly;
- Epic 6 states and preserves the global-lock boundary; it does not claim
  lock-level scalability;
- listener ownership, accepted connection ownership, fd registry ownership, and
  connection-task placement are visible in code and trace evidence;
- per-shard net poller and wake ownership exists for net readiness, close,
  cancellation, and shutdown without implementing Phase 4 cross-shard transport;
- accepted connection fds are registered on the owning shard;
- Tier 1 connection tasks are not stolen by non-owner shards;
- the parked-with-local-work invariant is implemented or an equivalent focused
  no-sleep-with-ready-work proof is recorded;
- close, cancellation, readiness, and shutdown tests cover the multi-shard path;
- stable multi-shard accept tests run in `make runtime-v2-check` and CI;
- benchmark evidence compares single-shard and multi-shard native TCP rows with
  a current-checkout binary and explains results under the preserved global
  executor lock;
- `make c-check`, `make cppcheck`, `make runtime-v2-check`, `make check`, and
  `git diff --check` pass or have recorded blockers unrelated to Epic 6;
- root, `runtime/`, and `runtime/native/` Sentrux scans and rule checks are
  recorded as pass/fail evidence;
- touched over-limit files have recorded line-count outcomes and the refactor
  tranche either reduces them or records an explicit reason it could not;
- every Epic 6 debt is either closed or recorded in `DEBT.md` with an owner and
  close condition;
- `06-evidence.md`, `NOTES.md`, this document, `README.md`, and any changed
  Runtime V2 docs are updated with the final state.

## Next Runtime Handoff And Syntax Gate

Epic 7 should split the global executor lock and move the remaining global
compatibility primitives toward shard-owned state. That work is separate from
the language syntax surface and from Phase 4 cross-shard messaging.

Any later syntax or crossing epic should start only after a deliberate
language-surface discussion with the user. Current names such as `far`,
`submit_to`, `crosses`, and `shard-movable` are semantic placeholders, not
accepted syntax.

Epic 6 must not silently begin that work. If an Epic 6 task discovers that
accept ownership needs language syntax, stop and discuss the syntax boundary
before changing parser, semantic-analysis, lowering, standard-library, or public
example files.
