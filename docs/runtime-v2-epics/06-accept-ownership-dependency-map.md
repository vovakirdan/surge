# Epic 6 Task 2: Accept Ownership Dependency Map

Status: complete for Epic 6 Task 2 docs-only mapping.

This map pins the current accept/readiness/close/cancellation/shutdown shape
before Epic 6 runtime changes. It does not choose the listener model; Task 3
must reconcile this map with its proving spike before Tasks 4 and 5 finalize
tests.

Baseline: Task 1 recorded start commit `9e1de4a0`
(`docs(runtime): expand epic 6 tasks`) and current baseline evidence in
`06-evidence.md`.

## Classification

| Class | Meaning in Epic 6 |
| --- | --- |
| `net-shard-owned` | This path must resolve through the shard that owns the listener, connection fd, fd-registry row, net waiter, poll scratch, or local wake state. It may still run under `ex->lock` in this epic. |
| `stays-global-compat` | This path remains global under `ex->lock` in Epic 6. Legitimate examples are channels, join/scope wake, cancellation state, blocking completions, timers, `now_ms`, generic ready work, and compatibility scheduler state that is not net-owner-specific yet. |
| `later-epic` | This is lock sharding, Phase 4 inbound messaging/eventfd/credits/PARKED protocol, language syntax, explicit cross-shard calls, distributed cancellation, or Tier 2 migration. |
| `gap` | Required for Epic 6 but not covered by Tasks 6-11 or the epic's Not Included list. No new `gap` item was found in this pass. |

## Boundary Summary

Epic 6 moves net ownership, not the whole runtime. The executor lock remains
global: `rt_executor` owns `lock`, condition variables, `net_polling`,
`shutdown`, task/scope arrays, and blocking-pool state
(`runtime/native/rt_async_internal.h:216-247`). The invariant comment still
states that `ex->lock` owns the single shard waiter store, fd registry rows,
net poll scratch, scheduler queues, timer state, and shutdown flags
(`runtime/native/rt_async_internal.h:249-269`).

Current shard storage is already shaped for ownership but fixed to one shard:
`RT_RUNTIME_SHARD_COUNT` is `1U`
(`runtime/native/rt_async_internal.h:127`), `rt_shard` owns
`scheduler`, `net_poll_scratch`, `fd_registry`, `waiter_store`, and `shard_id`
(`runtime/native/rt_async_internal.h:150-160`), and `rt_runtime` stores
`shard_count` plus `shards[RT_RUNTIME_SHARD_COUNT]`
(`runtime/native/rt_async_internal.h:162-165`).

## Lifecycle Map

| Step | Current evidence | Class | Epic 6 owner/change |
| --- | --- | --- | --- |
| Listener object | `NetListener` stores only `fd` and `closed` (`runtime/native/rt_net.c:45-48`). | `net-shard-owned` | Task 8 must attach listener owner metadata without changing the public ABI. Task 3 decides whether one public listener handle wraps a per-shard listener group. |
| Connection object | `NetConn` stores only `fd` and `closed` (`runtime/native/rt_net.c:50-53`). `rt_task` has no owner/shard metadata (`runtime/native/rt_async_internal.h:167-202`). | `net-shard-owned` | Task 8 must attach connection owner metadata; Task 7 must add placement metadata on task, connection, or both. |
| Wake pipe | `net_poll_wake_read_fd` and `net_poll_wake_write_fd` are process-global statics (`runtime/native/rt_net.c:67-68`). Init/write/drain use those statics (`runtime/native/rt_net.c:93-129`). | `net-shard-owned` | Task 10 must move wake ownership to per-shard local wake state. It must not implement Phase 4 inbound queues, eventfd credits, or seq-cst `PARKED`. |
| Listen syscall | `rt_net_listen` creates one fd (`runtime/native/rt_net.c:413-430`), sets `SO_REUSEADDR` (`runtime/native/rt_net.c:434-439`), binds/listens (`runtime/native/rt_net.c:445-459`), then returns one `NetListener` (`runtime/native/rt_net.c:461-469`). `SO_REUSEPORT` has no source match. | `net-shard-owned` plus Task 3 open point | Task 3 must prove the listener model. If it chooses `SO_REUSEPORT`, the one public handle must own an internal listener group; if it chooses fallback handoff, the handoff must be explicit and measured. |
| Accept syscall | `rt_net_accept` borrows the listener, calls `accept(l->fd)`, prepares the accepted fd, allocates `NetConn`, and returns it (`runtime/native/rt_net.c:566-590`). | `net-shard-owned` plus Task 3 open point | Task 9 must ensure accepted fd and handler task land on the accepting/owner shard. The current code has no internal accept task representation. |
| Outbound connect | `rt_net_connect` allocates a `NetConn` after `connect` and `net_prepare_conn_fd` (`runtime/native/rt_net.c:472-520`). | `net-shard-owned` | Task 8 should give connected outbound sockets an owner, likely the current shard or explicit compatibility owner, so read/write metadata is uniform. |
| Net wait registration | `rt_net_wait_accept/readable/writable` extract an fd and call `net_wait_current_task` (`runtime/native/rt_net.c:780-805`). `net_wait_current_task` builds `net_accept_key`, `net_read_key`, or `net_write_key`, calls `prepare_park`, and verifies the registry row exists (`runtime/native/rt_net.c:730-777`). | `net-shard-owned` for net keys | Net-key registration must use the owner shard's waiter store and fd registry. Non-net waiter keys must keep global compatibility. |
| Net keys | `net_accept_key`, `net_read_key`, and `net_write_key` encode the raw fd; `waker_is_net` recognizes only those net key kinds (`runtime/native/rt_async_waiter.c:42-60`). | `net-shard-owned` | Task 11 must resolve these keys against the fd owner shard. Raw fd alone is not enough once different shards can have registries. |
| Waiter-store bridge | Net waiter add/remove updates `store->net_len` (`runtime/native/rt_async_waiter.c:62-76`), attaches/detaches fd-registry interest through `rt_executor_fd_registry` (`runtime/native/rt_async_waiter.c:87-112`), and uses `rt_executor_waiter_store` for the shared waiter list (`runtime/native/rt_async_waiter.c:181-209`, `242-286`, `330-365`). | mixed | Net-key rows are `net-shard-owned`; channel/join/scope/timer/blocking rows using the same generic helpers remain `stays-global-compat`. Task 11 should split by key/owner, not move every waiter kind. |
| Park wake notification | `park_current` commits a wait, calls `rt_net_wake_poll()` for net keys, then signals `io_cv` (`runtime/native/rt_async_state.c:1002-1035`). | `net-shard-owned` for net wake; `stays-global-compat` for generic park state | Task 10 replaces the wake target for net keys. Generic task state and `io_cv` compatibility remain global in Epic 6 unless Task 10 needs a compatibility broadcast. |
| Poll guard | `has_net_waiters` reads `rt_executor_waiter_store_const(ex)->net_len`; `begin_net_poll` gates the single `ex->net_polling` flag (`runtime/native/rt_async_state.c:1058-1068`). | `net-shard-owned` | Task 10 must make poller ownership per shard. The single `net_polling` flag cannot describe N shard pollers. |
| Poll input | `poll_net_waiters` snapshots `rt_executor_fd_registry_const(ex)`, grows `rt_executor_net_poll_scratch(ex)`, includes the global wake fd, releases `ex->lock` for `poll`, and completes ready rows (`runtime/native/rt_net.c:807-900`). | `net-shard-owned` | Task 10/11 must pass or resolve the owner shard explicitly for registry, scratch, wake fd, and completion. |
| Readiness completion | `rt_fd_registry_complete_ready_net_waiters` calls `rt_executor_fd_registry_const(ex)` and completes read/accept/write keys (`runtime/native/rt_fd_registry.c:288-303`). It wakes waiters through `rt_executor_wake_net_waiters_for_key` (`runtime/native/rt_fd_registry.c:270-285`). | `net-shard-owned` | Completion must use the same shard registry and waiter store as the snapshot. Falling back to shard 0 would be an Epic 6 correctness bug. |
| Close | `rt_net_close_listener` and `rt_net_close_conn` both call `close_net_fd_slot` (`runtime/native/rt_net.c:523-564`). Close marks the fd closed through `rt_executor_fd_registry(ex)`, closes the OS fd, then wakes closed net waiters (`runtime/native/rt_net.c:532-547`). | `net-shard-owned` | Task 8/11 must close the owner shard's row. Listener-group close must close all per-shard listener fds if Task 3 chooses a group model. |
| Cancellation | `cancel_task` marks the task cancelled and wakes a waiting task through `wake_task(..., remove_waiter=1)` (`runtime/native/rt_async_state.c:1297-1318`). `mark_done` clears wait keys, select timers, and park key (`runtime/native/rt_async_state.c:1320-1338`). | `stays-global-compat` for cancellation state; `net-shard-owned` for net waiter cleanup side effects | Epic 6 does not implement distributed cancellation. When a cancelled task owns a net wait, cleanup must remove the owner shard's net waiter/registry interest. Non-net cancellation semantics stay global. |
| Shutdown request/drain | `rt_executor_drain_shutdown_net_waiters` drains `rt_executor_fd_registry(ex)` (`runtime/native/rt_shutdown.c:3-10`). `rt_executor_request_shutdown` sets `ex->shutdown`, drains the same registry, calls `rt_net_wake_poll`, and broadcasts global condvars (`runtime/native/rt_shutdown.c:13-33`). | `net-shard-owned` for net drain/wake; `stays-global-compat` for shutdown flag and global condvars | Task 10/11 must iterate every shard's net registry and wake every shard poller. `ex->shutdown` and blocking shutdown remain global compatibility in this epic. |
| Shutdown registry drain | `rt_fd_registry_drain_shutdown_net_waiters_locked` loops registry rows, completes net keys, wakes the poller, and broadcasts `io_cv` if it completed waiters (`runtime/native/rt_fd_registry.c:320-345`). | `net-shard-owned` | The function must become shard-registry aware or receive the owner registry/shard; it cannot silently call shard-0 helpers for all rows. |
| Worker/I/O loops | Worker and I/O loops observe `ex->shutdown` and use `begin_net_poll`/`poll_net_waiters_owned` (`runtime/native/rt_async_state.c:1535-1558`, `1666-1724`). | mixed | Net polling inside those loops moves per shard in Task 10. Worker lifecycle and shutdown observation stay global compatibility until Epic 7 or later lifecycle work. |

## Shard-0 Accessor Inventory

Current shard-0 accessors are not all bugs. Epic 6 forbids shard-0 fallback only
for net ownership paths that move in this epic.

| Symbol/callers | Current evidence | Class | Required treatment |
| --- | --- | --- | --- |
| `rt_runtime_shard0` | Definition returns `shards[0]` only if `shard_count == RT_RUNTIME_SHARD_COUNT` (`runtime/native/rt_runtime.c:50-55`). Used by heap, scheduler, poll scratch, channel compat, waiter store, and fd registry accessors (`runtime/native/rt_runtime.c:61-163`), and by `exec_init_once` scheduler init (`runtime/native/rt_async_state.c:216-220`). | mixed | Task 6 should introduce bounded runtime shard lookup. Net callers need owner-shard lookup; legitimate global compatibility callers may keep explicit shard-0/global accessors with names that say so. |
| Scheduler accessors | `rt_executor_scheduler`/const resolve shard 0 (`runtime/native/rt_runtime.c:79-90`). Callers include worker count, runnable checks, ready push/pop, steal, compensation, worker/I/O drain, and task await (`runtime/native/rt_async_state.c:240-265`, `644-652`, `788-916`, `1410-1518`, `1603-1630`; `runtime/native/rt_async_task.c:142-179`). | mixed | Task 7 must add owner placement/no-steal for connection tasks. Generic ready work and `SURGE_SHARDS=1` stealing remain compatibility behavior. |
| Net poll scratch accessor | `rt_executor_net_poll_scratch` resolves shard 0 (`runtime/native/rt_runtime.c:92-99`). Sole runtime consumer is `poll_net_waiters` (`runtime/native/rt_net.c:815-831`). | `net-shard-owned` | Task 10 must resolve scratch from the poller/owner shard. |
| FD registry accessor | `rt_executor_fd_registry`/const resolve shard 0 (`runtime/native/rt_runtime.c:144-163`). Runtime callers are net waiter bridge, close, poll input, readiness completion, and shutdown drain (`runtime/native/rt_async_waiter.c:87-112`, `runtime/native/rt_net.c:532-546`, `807-900`, `runtime/native/rt_fd_registry.c:288-303`, `runtime/native/rt_shutdown.c:3-20`). | `net-shard-owned` | Task 11 must pass owner shard/registry explicitly or add owner-aware executor APIs. Static gates should ban shard-0 fd-registry fallback on net paths. |
| Waiter-store accessor | `rt_executor_waiter_store`/const resolve shard 0 (`runtime/native/rt_runtime.c:123-142`). Callers include net completion, generic add/remove/pop, wake-all, trace snapshot, and net wait length (`runtime/native/rt_async_waiter.c:181-365`, `runtime/native/rt_async_state.c:973-999`, `1058-1068`, `runtime/native/rt_async_trace.c:262-263`). | mixed | Net-key operations move to owner shard. Channel/join/scope/timer/blocking compatibility stays global under `ex->lock`. |
| Channel blocking compat accessor | Resolves shard 0 (`runtime/native/rt_runtime.c:102-120`). Callers are channel/sync-helper trace and compensation (`runtime/native/rt_async_state.c:1410-1518`, `runtime/native/rt_async_trace.c:334`). | `stays-global-compat` | Do not migrate in Epic 6 unless a task explicitly touches sync-helper compatibility. Static gates must not flag this as a net shard-0 shortcut. |

## Scheduler And `SURGE_SHARDS`/`SURGE_THREADS`

`SURGE_THREADS` is read only by `rt_env_worker_count`
(`runtime/native/rt_async_state.c:109-123`). `exec_init_once` falls back to
`rt_runtime_default_worker_count`, prepares heap cells for `threads`, initializes
the single shard scheduler, starts workers if `threads > 1`, and initializes
blocking workers (`runtime/native/rt_async_state.c:187-232`). `rt_start_workers`
reads the single scheduler, starts one I/O thread plus `worker_count` worker
threads, and stores one `worker_ctxs` array on that scheduler
(`runtime/native/rt_async_state.c:278-319`).

Task 6 owns configuration parsing and the structural shard array. It should add
`SURGE_SHARDS` next to `rt_env_worker_count`, define the conflict rule, and
initialize per-shard containers. Task 7 or the worker-placement task owns actual
shard-aware worker startup. Per the Epic 6 boundary, when `SURGE_SHARDS>1`, the
target Tier 1 shape is one worker per shard; `SURGE_THREADS` must be unset or
equal to `SURGE_SHARDS`. With `SURGE_SHARDS=1`, existing intra-shard worker
stealing stays valid.

Current steal branches live in `worker_next_ready`: seeded mode can steal from
another worker local queue (`runtime/native/rt_async_state.c:845-863`), and
parallel mode steals from other local queues (`runtime/native/rt_async_state.c:893-912`).
`SCHED_TRACE` increments steal counters in `rt_async_trace.c:396` and prints
`steal=` at `runtime/native/rt_async_trace.c:479-488`. Task 7 must make the
steal check connection-owner-aware without breaking `SURGE_SHARDS=1` tests that
expect current stealing.

## Native Handle And VM ABI Flow

Native Runtime V2 net handles are opaque pointers returned through `rt.h`:
listen, close, accept, read/write, and wait functions are declared at
`runtime/native/rt.h:77-88`. The native structs currently carry only `fd` and
`closed` (`runtime/native/rt_net.c:45-53`), and borrowed/value helpers unwrap
the public value shape without owner metadata (`runtime/native/rt_net.c:385-410`).

The Go VM interpreter has its own mirror for non-native execution:
`vmNetListener` and `vmNetConn` also store only `fd` and `closed`
(`internal/vm/intrinsic_net_helpers.go:13-21`). Intrinsic dispatch maps
`rt_net_listen`, `rt_net_accept`, `rt_net_read`, and friends to VM handlers
(`internal/vm/intrinsic.go:237-254`). VM listen stores `vm.netListeners[handle]`
with one fd (`internal/vm/intrinsic_net.go:95-106`), VM accept creates a new
`vmNetConn` handle from the accepted fd (`internal/vm/intrinsic_net.go:321-356`),
and VM read uses the stored fd directly (`internal/vm/intrinsic_net.go:397-428`).

Epic 6 must preserve public Surge syntax and standard-library signatures.
Owner metadata therefore belongs inside native `NetListener`/`NetConn` and any
internal listener-group object, not in user-visible syntax. VM mirror structs
are evidence of the public ABI shape, but the N>1 native ownership work should
not broaden into interpreter semantics unless a test requires parity metadata.

## Task 3 Reconciliation Points

Task 3 must answer the listener model questions before Task 4/5 contracts
freeze:

- Where do internal accept loops/tasks live if the public program has one
  `TcpListener` and one user accept loop?
- If `SO_REUSEPORT` is selected, does the public handle own N listener fds, and
  does close close the whole group?
- How does an accepted `NetConn` and its handler task enter the owner shard
  without language syntax or `submit_to`?
- If a single acceptor plus handoff fallback is selected, which handoff counter
  and trace row mark that this is compatibility, not the target hot path?
- How does the selected model handle low-connection `SO_REUSEPORT` skew without
  treating it as a correctness failure?

## First Safe Implementation Boundary

The first safe code boundary is Task 6: replace fixed
`RT_RUNTIME_SHARD_COUNT` storage with `RT_RUNTIME_MAX_SHARDS` plus runtime
`shard_count`, add `SURGE_SHARDS` parsing/conflict validation, and initialize
each shard's existing containers under the preserved global lock. This can land
before accept distribution because it does not need handler placement,
per-shard wake pipes, or no-steal enforcement.

Task 6 should not make `rt_start_workers` fully shard-aware unless its approved
task scope is expanded; Task 7/10 own actual placement and poller ownership.

## Gaps

No new unowned Epic 6 gap was found. Every mapped dependency lands in Tasks
6-11, Task 3 reconciliation, or the epic's later-work boundary:

- lock sharding remains Epic 7;
- Phase 4 messaging/eventfd/credits/PARKED remains later;
- language syntax and keywords remain blocked on user review;
- non-net waiters and primitives remain global compatibility under `ex->lock`;
- `rt_fd_registry_free` still has no normal lifecycle caller, but shutdown
  registry ownership and per-shard drain/free wiring belong to Tasks 10/11 and
  closeout evidence.

## Search Commands Used

```bash
rg -n 'rt_runtime_shard0|rt_executor_(scheduler|net_poll_scratch|channel_blocking_compat|waiter_store|fd_registry)' runtime/native internal/vm
rg -n 'rt_fd_registry_(attach_net_interest|detach_net_interest|net_interest_present|snapshot_poll_interest|complete_ready_net_waiters|drain_shutdown_net_waiters_locked|wake_closed_net_waiters|mark_closed)' runtime/native internal/vm
rg -n 'net_poll_wake_(init|drain)|rt_net_wake_poll|net_poll_wake_read_fd|net_poll_wake_write_fd|poll_net_waiters|poll_net_waiters_owned|begin_net_poll|has_net_waiters' runtime/native
rg -n 'rt_net_(listen|accept|close_listener|close_conn|read|write|read_bytes|write_bytes|wait_accept|wait_readable|wait_writable)|NetListener|NetConn|net_listener_from|net_conn_from' runtime/native internal/vm
rg -n 'net_(accept|read|write)_key|waker_is_net|prepare_park|park_current|wake_task|wake_task_with_policy|mark_done|cancel_task|pop_waiter|add_waiter|net_waiter' runtime/native
rg -n 'SURGE_THREADS|rt_env_worker_count|exec_init_once|rt_shard_scheduler_init|worker_ctx|worker_next_ready|steal|SCHED_TRACE|trace_sched_steal' runtime/native internal/vm docs/runtime-v2-epics/LIVENESS_PROBES.md
rg -n 'shutdown|blocking_shutdown|rt_executor_request_shutdown|rt_executor_drain_shutdown_net_waiters' runtime/native internal/vm docs/runtime-v2-epics
rg -n 'RT_RUNTIME_SHARD_COUNT|RT_RUNTIME_MAX_SHARDS|shard_count|#error' runtime/native internal/vm/runtime_v2_*_test.go
rg -n 'SO_REUSEPORT|SO_REUSEADDR' runtime/native/rt_net.c
```
