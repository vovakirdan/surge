# Epic 2 Task 2: Field Ownership Map

Status: complete for the documentation pass. This map classifies the current
`rt_executor` fields before any Runtime V2 field movement.

## Scope

This task inspected runtime state only. It did not change runtime code,
compiler code, public ABI, tests, Sentrux rules, benchmarks, or generated
reports.

Source files inspected:

- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_net.c`
- `runtime/native/rt_async_channel.c`
- Additional direct users found by `rg`: `rt_async_task.c`,
  `rt_async_poll.c`, `rt_async_scope.c`, and `rt_async_blocking.c`.

The current executor lock remains the synchronization boundary until a later
task proves a narrower owner. Epic 2 may move storage behind V2-shaped
containers, but it must not change waiter semantics, fd registry semantics,
worker placement semantics, public ABI names, or source-visible async behavior.

## Usage Search Record

Field usage was searched with these patterns:

```bash
rg -n -- '->(inject|local_queues|worker_ctxs|worker_count|running_count|ready_cv|sched_mode|sched_seed)\b' runtime/native
rg -n -- '->(waiters|waiters_len|waiters_cap|net_waiters_len)\b|\b(add_waiter|remove_waiter|pop_waiter|prepare_park|wake_key_all)\b' runtime/native
rg -n -- '->(net_poll_fds|net_poll_fds_cap|net_poll_pfds|net_poll_pfds_cap|net_polling|io_cv)\b|\b(poll_net_waiters|complete_net_waiters|ensure_net_poll_)\b' runtime/native
rg -n -- '->(next_id|next_scope_id|now_ms|tasks|tasks_cap|scopes|scopes_cap)\b|\b(tick_virtual|advance_time_to_next_timer|get_task|get_scope)\b' runtime/native
rg -n -- '->(channel_blocked_workers|compensation_count|compensation_high_water|blocking_.*)\b|\b(rt_blocking_|maybe_start_compensation|rt_wait_current_worker_wakeup)\b' runtime/native
rg -n -- '->(lock|done_cv|workers|initialized|io_started|shutdown)\b|\b(ensure_exec|exec_init_once|rt_start_workers|run_until_done|worker_main|io_thread_main)\b' runtime/native
```

The searches confirmed every `rt_executor` field has an owner category below.

## Owner Categories

| Category | Meaning in Epic 2 |
| --- | --- |
| Runtime lifecycle/control plane | Process-lifetime executor setup, worker startup, global shutdown, and condition variables that preserve the current global lock discipline. |
| `N=1` shard-local hot state | State that represents the single current shard's task registry, ready queues, scheduler counters, virtual timer state, or net poll scratch. It can move behind an `rt_shard` container only with identical behavior. |
| Compatibility/offload state | Existing blocking-pool and sync-channel fallback state. It is preserved as compatibility evidence, not promoted as the target hot path. |
| Trace/debug-facing state | Fields or counters whose values appear in trace output. Moving them requires equivalent trace names or an explicit evidence note. |
| Later-epic state | State whose final owner depends on local waiters, a persistent fd registry, allocator ownership, cross-shard messaging, or multi-shard policy. Epic 2 may wrap this state, but must not implement the later semantics. |

## Full Field Map

| Current field group | Fields | Current users | Owner category | Epic 2 movement status |
| --- | --- | --- | --- | --- |
| Task id allocator and task table | `next_id`, `tasks`, `tasks_cap` | `rt_async_task.c`, `rt_async_blocking.c`, `rt_async_state.c`, `rt_async_poll.c`, `rt_async_channel.c`, `rt_net.c` through `get_task()` and direct task creation | `N=1` shard-local hot state | Can move into the single shard once accessors preserve id allocation, task lookup, handle lifetime, cancellation, and cleanup. Multi-shard id generation waits for a later owner-placement epic. |
| Scope id allocator and scope table | `next_scope_id`, `scopes`, `scopes_cap` | `rt_async_scope.c`, `rt_async_state.c`, `rt_async_task.c`, `rt_async_poll.c` through `get_scope()` and scope creation | `N=1` shard-local hot state | Can move with the task registry if structured join and failfast behavior stay identical. Distributed scope ownership waits for a multi-shard structured-concurrency epic. |
| Virtual timer cursor | `now_ms` | `rt_async_state.c`, `rt_async_poll.c` through sleep, timeout, and timer advancement | `N=1` shard-local hot state with timer later-epic caveat | Can move as a field of the single shard only if sleep, timeout, cancellation, and timer wake behavior remain unchanged. A real timer owner rewrite waits for the timer/local-waiter work. |
| Ready queues and scheduler source state | `inject`, `local_queues`, `worker_ctxs`, `worker_count`, `running_count`, `sched_mode`, `sched_seed` | `rt_async_state.c`, `rt_async_task.c` through worker loops, ready push/pop, stealing, seeded scheduling, snapshots, and run-until-done | `N=1` shard-local hot state plus runtime worker configuration | Can move in scheduler-shape tasks behind `rt_shard`/accessors. Preserve ready queue membership, no double poll, current seeded trace behavior, and the existing `worker_count` runtime setting. Do not turn Tier 1 stealing or global inject into a future multi-shard contract. |
| Global waiter list | `waiters`, `waiters_len`, `waiters_cap` | `rt_async_state.c`, `rt_async_channel.c`, `rt_async_task.c`, `rt_async_poll.c`, `rt_async_scope.c`, `rt_async_blocking.c`, `rt_net.c` through `add_waiter()`, `prepare_park()`, `pop_waiter()`, `remove_waiter()`, and `wake_key_all()` | Later-epic state with legacy wrapper allowed | May be wrapped or nested under the single shard as the same FIFO-by-key list. Splitting by fd, channel, task, timer, or scope waits for the local-waiter epic and its owner-cleanup probes. |
| Net waiter count cache | `net_waiters_len` | `rt_async_state.c`, `rt_net.c` through net waiter accounting and `has_net_waiters()` | Later-epic state tied to net waiters/fd registry | Keep coupled to the legacy waiter list in Epic 2. It may move with net scratch only as a compatibility counter. Persistent readiness registration waits for the fd-registry epic. |
| Net poll scratch buffers | `net_poll_fds`, `net_poll_fds_cap`, `net_poll_pfds`, `net_poll_pfds_cap` | `rt_net.c` through `ensure_net_poll_fds()`, `ensure_net_poll_pfds()`, and `poll_net_waiters()` | `N=1` shard-local hot state | Safe Epic 2 move target for the net-poll-scratch migration. Preserve rebuild-from-waiters semantics; do not introduce persistent fd registry semantics. |
| Net poll ownership flag and I/O wake cv | `net_polling`, `io_cv` | `rt_async_state.c` around `begin_net_poll()`, worker/I/O waits, idle signaling, and shutdown waits | `N=1` shard-local hot state with lifecycle coupling | Can move only with the net scheduler boundary. Preserve one active poll owner, idle-to-I/O signaling, shutdown wakeups, and SIGUSR1/net trace behavior. |
| Executor lock and worker/done condition variables | `lock`, `ready_cv`, `done_cv` | `rt_async_state.c`, `rt_async_task.c`, `rt_async_channel.c`, `rt_net.c`, `rt_async_blocking.c` through lock wrappers and condition waits/signals | Runtime lifecycle/control plane and legacy global-lock compatibility | Keep as the global synchronization boundary until a task proves a narrower owner. Wrapping is allowed; changing lock scope is not part of Task 5. |
| Worker thread lifecycle | `workers`, `worker_ctxs`, `worker_count`, `initialized`, `io_started`, `shutdown` | `rt_async_state.c`, `rt_async_task.c`; `worker_count` is also read by scheduler/compensation paths | Runtime lifecycle/control plane, with `worker_ctxs` also used by scheduler state | Can move into `rt_runtime` plus the single shard during skeleton work if startup, shutdown checks, and `rt_worker_count()` stay equivalent. A real multi-shard thread model waits for a later epic. |
| Sync-channel fallback counters | `channel_blocked_workers`, `compensation_count`, `compensation_high_water` | `rt_async_state.c`, `rt_async_channel.c` through blocking helper waits, compensation startup, snapshots, and trace rows | Compatibility/offload state and trace/debug-facing state | Can move in the channel/blocking compatibility migration. Preserve current sync helper fallback, compensation limits, `TRACE_EXEC_SNAPSHOT channel_blocked`, and compensation trace behavior. |
| Blocking pool lifecycle and queue | `blocking_lock`, `blocking_cv`, `blocking_workers`, `blocking_count`, `blocking_started`, `blocking_shutdown`, `blocking_head`, `blocking_tail` | `rt_async_blocking.c`, `rt_async_state.c` through blocking pool initialization, queue push/pop, worker waits, and shutdown checks | Compatibility/offload state | Can move as a compatibility sub-object after skeleton accessors exist. Preserve blocking task submission, cancellation, completion wakeups, detached worker behavior, and current queue ordering. |
| Blocking pool trace counters | `blocking_running`, `blocking_submitted`, `blocking_completed`, `blocking_cancel_requested` | `rt_async_blocking.c`; `rt_async_state.c` trace dumps read them through `exec_state` | Compatibility/offload state and trace/debug-facing state | Can move only with trace-equivalent accessors or an evidence note that names equivalent trace fields. Do not rename trace output silently. |

## Movable Epic 2 Groups

These groups can move in Epic 2 if each code task preserves the old behavior
boundary and runs the matching probes from `LIVENESS_PROBES.md`:

| Group | Candidate task | Preserved behavior boundary | Required evidence surface |
| --- | --- | --- | --- |
| Runtime lifecycle shell | Task 5, runtime/shard skeleton | `ensure_exec()`, worker startup, `rt_worker_count()`, shutdown checks, lock/cv initialization, and public native ABI stay equivalent. | Static checks plus focused skeleton tests from Tasks 3-4. |
| Task/scope registry as single-shard state | Later accessor cleanup or a separately approved post-skeleton task | Task lookup, task handle lifetime, cancellation, structured join, failfast, and scope cleanup stay equivalent. | Cancellation/join/timeout smoke when touched by code. |
| Scheduler queue shape | Task 7, scheduler migration | Ready queue membership, local/inject/steal behavior, seeded trace rows, and no-double-poll invariant stay equivalent. | Scheduler source trace; parked-with-work invariant status. |
| Net poll scratch | Task 9, net scratch migration | Poll set is still rebuilt from current waiters, dedup behavior stays current, one poll owner remains enforced, and no persistent fd registry is introduced. | Net wakeup/SIGUSR1 trace; native net benchmark trace rows if runtime code moves scratch. |
| Channel/blocking compatibility state | Task 11, channel/blocking compatibility migration | Direct async channels still park tasks; sync helpers still use compatibility fallback; blocking pool completions still wake the owning task. | Direct async channel wakeups; sync fallback and compensation checks; blocking-pool smoke if blocking fields move. |

## Deferred Groups

| Deferred group | Later owner | Why it waits |
| --- | --- | --- |
| Owner-local waiter queues | Local waiter epic | Current `waiters` is one FIFO-by-key list shared by channel, join, timer, scope, blocking, and net waits. Splitting it needs owner cleanup back references and stale-wake tests. |
| Persistent fd registry | Local fd registry epic | Current net polling rebuilds scratch arrays from `waiters` each poll cycle. A registry changes readiness lifetime, dedup, close, and cancellation semantics. |
| Multi-shard owner placement | Multi-shard runtime epic | `worker_count`, `local_queues`, stealing, and current global inject behavior are runtime policy today, not a language contract. `N=1` structure must not decide cross-shard placement. |
| Distributed cancellation and scopes | Multi-shard structured-concurrency epic | `tasks`, `scopes`, and waiter cleanup have no generation tokens today. Stale remote completion/cancel messages need a separate design and probes. |
| Allocator counters and pools | Allocator/pools epic | Heap counters and hot object pools live outside `rt_executor`. This task did not classify allocator storage beyond naming the later owner. |
| IO backend choice | Later IO/backend epic | `poll()` scratch movement can happen now. `epoll` or `io_uring` selection is separate from the field ownership map. |

## File Size Risks

Current line counts:

| File | Lines | Risk |
| --- | --- | --- |
| `runtime/native/rt_async_internal.h` | 404 | Under the 500-line limit, but it is the shared owner contract. Keep additions small. |
| `runtime/native/rt_async_state.c` | 2391 | Over limit. Runtime-code tasks must avoid growth, reduce it, or record a split/follow-up. |
| `runtime/native/rt_net.c` | 1039 | Over limit. Net scratch migration must avoid adding more unrelated logic. |
| `runtime/native/rt_async_channel.c` | 549 | Over limit. Channel/blocking compatibility moves must avoid growth or record why a split is deferred. |

## Checks For This Docs-Only Task

Approved checks:

- `git diff --check`
- the placeholder sanity grep recorded in `02-evidence.md`

No runtime tests, long probes, benchmarks, Sentrux scans, staging, or commit are
part of this task.

## First Code Task Boundary

The first runtime-code task should introduce only the runtime lifecycle shell
needed to create `rt_runtime` and one `rt_shard`. It must not move task/scope
registry ownership yet.

- `lock`
- `ready_cv`
- `io_cv`
- `done_cv`
- `workers`
- `worker_ctxs`
- `worker_count`
- `initialized`
- `io_started`
- `shutdown`
- `sched_mode`
- `sched_seed`

That task may add accessors for the single shard, but it should not move the
legacy waiter list, net waiter count, fd readiness semantics, channel handoff
semantics, blocking pool queue, or task/scope ownership unless its approved task
plan explicitly expands the field group and evidence.

## Map Risks

- The current global waiter list is an implementation artifact. Do not promote
  global FIFO across waiter kinds into a Runtime V2 contract.
- The current I/O path can drain ready work after net readiness. Future code
  must separate net-woken work from unrelated general inject work before
  treating that path as architecture.
- `worker_count` remains a public runtime setting in Epic 2, even though the V2
  target later maps shards more directly to owner threads.
- Trace-facing fields must preserve trace names or record exact equivalence, or
  benchmark/liveness comparisons will become ambiguous.
