# Epic 10 Task 1: Dependency And Debt Map

**Status:** complete (map recorded before any implementation decision).
**Kind:** map/evidence. Sources: direct code re-derivation (main session) plus
two read-only research subagents (`net-mapper` for RV2-DEBT-010, `http-mapper`
for RV2-DEBT-013), cross-checked against the code by the main session.

## 1. `rt_async_state.c` split map (RV2-DEBT-003)

At the Epic 10 start commit, `runtime/native/rt_async_state.c` was 1426 raw /
1184 effective LOC with the allowlist ceiling at 1184. The file mixed six
owners:

| Cluster | Content | External consumers |
| --- | --- | --- |
| A. Executor bootstrap | `exec_state` + TLS definitions, `panic_msg`, debug printf, env parsing, `exec_init_once`, `ensure_exec`, `rt_start_workers`, worker-ctx debug validation | everything (via `ensure_exec`) |
| B. Task/scope tables | `get_task`, `rt_task_table_snapshot`, `rt_task_slot_store`, `get_scope`, `rt_scope_slot_store`, `ensure_child_cap`, `ensure_scope_child_cap` | 12+ files |
| C. Ready queue | `scheduler_runnable_is_empty`, `rt_sched_idle_sample_locked`, `sched_next_u64`, `current_worker_scheduler`, `current_local_queue`, `pop_task_from_deque`, `ready_push*`, `ready_take_current_local_tail`, `ready_pop`, `worker_next_ready` | `rt_worker_turn.c`, `rt_async_task.c`, `rt_async_poll.c`, `rt_async_blocking.c`, `rt_task_park.c`, `rt_async_compat.c` |
| D. Clock/N=1 runner | `tick_virtual`, `rt_next_sleep_deadline`, `advance_time_to_next_timer`, `next_ready` | `rt_async_poll.c`, `rt_worker_turn.c` |
| E. Handle lifetime | `task_add_ref`, `free_task`, `task_release`, `task_release_lane_aware` | `rt_async_task.c`, `rt_async_select.c`, completion cluster |
| F. Completion/cancel | `clear_select_timers`, `current_task_cancelled`, `cancel_task`, `mark_done_needs_control`, `mark_done`, `apply_poll_outcome` | `rt_async_poll.c`, `rt_async_scope.c`, `rt_async_task.c`, `rt_worker_turn.c`, `rt_async_select.c`, `rt_net*.c`, `rt_channel_sync.c` |

Clusters C, E, F are the DEBT-003 named split candidates and moved in Task 2.
A, B, D stay: A is the file's remaining single owner (bootstrap), B is
needle-pinned to this file by
`TestRuntimeV2LifecycleStaticTaskTableAtomicSnapshot` /
`TestRuntimeV2LifecycleStaticScopeOwnerLane`, and D's `next_ready` is pinned by
`runtime_v2_accept_static_test.go:109`.

Static gates that pin moved functions to file names (repointed in Task 2):
`check_sync_points.sh` (`SP_CANCEL_BEFORE_WAKE`,
`SP_MARKDONE_BEFORE_DONEWAITERS_LOAD`),
`runtime_v2_scheduler_placement_source_test.go`
(`readRuntimeV2SchedulerStateSource` → `pop_task_from_deque`), and the
lifecycle `done_cv` confinement scan. The Makefile globs `runtime/native/*.c`,
so new files need no build wiring.

## 2. Copied net-handle map (RV2-DEBT-010)

Representation: `TcpConn.__opaque` / `TcpListener.__opaque` is an `int`
holding the raw `NetConn*` / `NetListener*` heap pointer
(`net_make_success_ptr`, `rt_net.c:364,415,510`). `NetConn`
(`rt_net_handles.h:30-35`) is `{int fd; bool closed; uint8_t
owner_shard_valid; uint32_t owner_shard_id;}` — no generation, no stable id,
no refcount. Surge-level copies (`copy_conn`, `stdlib/http/http.sg:118`;
`{ __opaque = handle }` reconstructions in `stdlib/net/net.sg:45,71,96,126`
and `stdlib/http/server.sg:437`) alias the same heap struct. `TcpConn` is
`@nosend` (`core/intrinsics.sg:82`), enforced for direct channel sends
(`testdata/golden/sema/invalid/concurrency/channel_send_nosend_tcpconn.sg`)
but laundered by the HTTP server's `int` extraction.

Validation today:

- Data-path entry points `rt_net_read` (`rt_net.c:524`), `rt_net_write`
  (`:547`), `rt_net_read_bytes` (`:570`), `rt_net_write_bytes` (`:601`),
  `rt_net_accept` (`:463`), `rt_net_close_conn` (`:442`),
  `rt_net_close_listener` (`:424`) check ONLY the in-struct `closed` bool
  (plain, unsynchronized) and then issue the raw syscall on `c->fd`. No
  registry, generation, or owner-shard check. There is no `shutdown()` net
  entry point; `close(2)` is the only teardown.
- The per-shard fd registry already assigns a monotonic, never-reset
  `generation` per row (`fd_registry_create_row`, `rt_fd_registry.c:90-103`)
  and already rejects stale completions by generation + `close_state` +
  interest (`rt_fd_registry_completion_state`, `rt_fd_registry.c:286-309`).
  Generations protect poll snapshots and waiter completion only.
- The wait path re-verifies against the owner registry:
  `net_wait_current_task` (`rt_net.c:630`) →
  `rt_net_owner_shard_probe_locked` (`rt_async_waiter.c:85-117`) treats the
  task's shard as a hint and scans every shard's registry for the fd.
- Error paths for invalid/closed handles are status-code based
  (`NET_ERR_NOT_CONNECTED`, `rt_net.c:445,526`), not `panic_msg`.

Hazards: (a) an aliased handle can race an unsynchronized `closed=true` write
and issue a syscall on an fd number the OS has already reused; (b) nothing
ties a handle to a registry row, so any future free of `NetConn` would let
reconstructed copies dangle. No test drives a copied/stale public handle on a
reused fd (`TestRuntimeV2FDRegistryClosedFDFailsFast` covers the registry,
not the public handle syscall path).

Design levers already present: per-fd registry generation; owner-shard
metadata on handles; a locked owner probe; a status-code reject path.

## 3. Stdlib HTTP owner map (RV2-DEBT-013)

Flow (`stdlib/http/server.sg`): `serve` (`:448`) listens, spawns
`worker_count` (default 1) long-lived `serve_worker` tasks up front (`:460`),
then the accept loop extracts `conn.__opaque` and sends the bare int through
`Channel<int>` (`:472-473,492-493`); each worker reconstructs
`TcpConn { __opaque = handle }` (`:437`) and runs `serve_conn` (`:438`),
which does all reads/writes/close and spawns per-request handler tasks.

Placement mechanics under `SURGE_SHARDS>1` (one worker thread per shard):

- `accept` migrates the ACCEPTING task to the accepted connection's owner
  shard (`rt_net_place_current_task_on_owner`, `rt_net.c:516`,
  `rt_net_accept_group.c:104-114`) with `TASK_PLACEMENT_CONNECTION`.
- F2 placement adoption (`rt_task_poll_adopt_placement`,
  `rt_async_task.c:261`) fires only when a task awaits a DONE
  connection-placed child.
- The HTTP workers get neither: they are spawned at startup on the acceptor's
  shard with generic placement (`rt_task_assign_spawn_owner`,
  `rt_scheduler_placement.c:56`) and receive an int from a channel, never
  awaiting the placed accept task. Under `SURGE_SHARDS>1` a worker on shard i
  routinely services a connection owned by shard j.

Why it does not corrupt today: read/write correctness does not depend on the
caller's shard. The wait path probes for the true owner registry per wait
(`rt_net_owner_shard_probe_locked`) and registers interest under the owner's
lock; raw `read()/write()` carry no shard affinity. The real costs are (a)
the owner-local placement fast path is silently defeated for all stdlib HTTP
traffic — an O(shards) probe + cross-shard interest registration per wait —
and (b) the multi-shard path is completely untested:
`TestMTCorrectnessHTTPServer` sets only `SURGE_THREADS` (1 shard), and the
`SURGE_SHARDS=8` perf gate drives the `@local`-pinned benchmark server, not
`http.serve`.

Raw-handle transfer inventory (every `__opaque` crossing a task boundary):
`server.sg:472/492→437` (the debt surface);
`mt_correctness_test.go:1056` single-thread echo test (same laundering
pattern, single-shard only); `stdlib/net/net.sg` `*_owned` reconstructions
(same-task, wrapper-internal); `testdata/llvm_parity/http_connect.sg:78`
(whole `TcpListener` into `spawn`, single-shard). No `examples/` use net/http.

## 4. Decisions this map fed (final Tasks 3-4 result)

- Task 3 (RV2-DEBT-010): the mid-task ABI investigation replaced the initial
  generation-in-handle plan with stable runtime handle ids. Public
  `TcpConn.__opaque` / `TcpListener.__opaque` values now resolve through a
  handle table before any fd/owner/closed/generation field is read; live
  canonical conn operations still use the owner shard's fd-registry generation
  as the lifetime proof and reject failures with `NET_ERR_NOT_CONNECTED`.
- Task 4 (RV2-DEBT-013): the HTTP `Channel<int>` worker handoff was removed.
  `http.serve` now uses fixed local accept workers with copied listener handles
  and handles each accepted `TcpConn` directly on the owner-local task path.
  `runtime-v2-http-owner-check` covers `SURGE_SHARDS=1,2,8`.
