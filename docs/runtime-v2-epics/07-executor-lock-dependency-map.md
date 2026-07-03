# Epic 7 Executor Lock Dependency Map

Task 2 output. This map records what `rt_executor.lock` protects today, who
reads and writes each piece of state, and the target lane after the split.
Lane names: **control** (the reduced global lock), **shard** (the owning
shard's new lock), **atomic** (designated atomic with documented ordering),
**immutable** (written once before threads exist), **tls** (thread-local),
**blocking** (the existing `blocking_lock`). Target lanes marked *(spike)*
are Task 3 decisions; everything else is fixed by the epic document.

Baseline commit for all anchors: `ae752f44` (Task 1 evidence commit; code
identical to `77475384`).

## 1. Lock Acquisition Inventory

`rt_lock`/`rt_unlock` call sites by file (excluding the definitions):

| File | Sites | Character |
| --- | ---: | --- |
| `rt_async_task.c` | 55 | spawn, wake, join/poll, cancel, clone, timeout, select |
| `rt_async_channel.c` | 36 | send/recv/try/close, sync-channel blocking helpers |
| `rt_async_scope.c` | 20 | scope enter/register/cancel/join/exit |
| `rt_async_poll.c` | 17 | main-thread runner (`run_ready_one`, `run_until_done`), cancel-pending scope branch |
| `rt_net_accept_group.c` | 16 | listener group create/accept/close |
| `rt_async_state.c` | 15 | worker loop, io thread, sync-compat wait, `rt_task_wake` internals |
| `rt_net.c` | 10 | net wait registration, poll build/complete (the `poll()` syscall itself runs unlocked, `rt_net.c:817-823`) |
| `rt_async_blocking.c` | 6 | submit, completion wake |
| `rt_async_trace.c` | 4 | debug snapshot dumps (SIGUSR1/exit) |
| `rt_shutdown.c` | 4 | shutdown request, net waiter drain |
| `rt_net_lifecycle.c` | 2 | close/cancel cleanup |
| `rt_fd_registry.c` | 2 | registry mutation helpers |

Condition variables today: one `ready_cv` (workers on all shards sleep at
`rt_async_state.c:1675`; broadcast for any multi-shard wake at `737-743`),
one `io_cv` (io thread and N=1 runner; signaled on every net park at
`rt_async_state.c:1159` and after every poll pass at `rt_net_poller.c:168`),
one `done_cv` (main-thread await at `rt_async_task.c:185`; broadcast on every
`mark_done` at `rt_async_state.c:1470`). The blocking pool has its own
`blocking_cv`.

## 2. `rt_executor` Fields

| Field (`rt_async_internal.h:227-257`) | Written by | Read by | Target lane |
| --- | --- | --- | --- |
| `next_id` | `__task_create` (`rt_async_task.c:26`), sleep/checkpoint spawns (`680`, `705`), blocking submit (`rt_async_blocking.c:238`) | same | control |
| `next_scope_id` | `rt_scope_enter` (`rt_async_scope.c:16`) | same | control |
| `now_ms` | `tick_virtual` (`rt_async_state.c:1166`), `advance_time_to_next_timer` (`1214`) | sleep poll (`rt_async_poll.c:35,41`), timers (`1240`, `1819`) | global clock cell; read/advance protocol *(spike)* |
| `runtime` | `exec_init_once` | everywhere | immutable |
| `tasks`, `tasks_cap` | grow/publish (`ensure_task_cap`, spawn sites), free (`free_task`, `rt_async_state.c:1357-1383`) | `get_task` from every path | control for grow/publish/free; reads per lifetime rule *(spike: slot atomicity + owner-locked deref)* |
| `scopes`, `scopes_cap` | `ensure_scope_cap`, `scope_exit_locked` | `get_scope` | control |
| `lock` | — | — | becomes the control mutex |
| `ready_cv` | signal/broadcast sites above | worker sleep, sync-compat wait | replaced by per-shard worker cv; compat waiters *(spike)* |
| `io_cv` | net park, poll pass, registry, shutdown | io thread, N=1 runner waits (`1250`, `1268`, `1805-1830`) | shard-local poller wake on hot paths; N=1 io-thread coordination lane *(spike)* |
| `done_cv` | `mark_done` broadcast (`1470`), shutdown | main-thread await (`rt_async_task.c:185`) | control; broadcast gated on a control-waiter count *(spike)* |
| `workers`, `initialized`, `io_started` | init/start | init/start | immutable after start |
| `shutdown` | `rt_executor_request_shutdown` (`rt_shutdown.c:23`) | worker loop, io thread, runner loops | atomic read + control-lane write |
| `blocking_lock/cv/head/tail/started/shutdown/count/worker_ctxs/workers` | blocking pool (`rt_async_blocking.c`) | same | blocking (unchanged) |
| `blocking_running/submitted/completed/cancel_requested` | atomics already | trace | atomic (unchanged) |

## 3. `rt_shard` Fields

| Field (`rt_async_internal.h:153-165`) | Current protection | Target lane |
| --- | --- | --- |
| `scheduler.inject`, `scheduler.local_queues` | `ex->lock` | shard |
| `scheduler.running_count` | `ex->lock` | shard |
| `scheduler.worker_ctxs`, `worker_count`, `sched_mode`, `sched_seed` | written before workers start | immutable |
| `heap_accounting` | Epic 5 cells | unchanged |
| `net_poll_scratch` | `ex->lock` during poll build | shard |
| `net_poll_wake` fds | init once; pipe writes are syscalls | immutable fds; pipe I/O lock-free |
| `fd_registry` | `ex->lock` | shard |
| `channel_blocking_compat` | `ex->lock`, shard-0 only via compat accessor (`rt_runtime.c:226-235`) | control (compat path) |
| `waiter_store` | `ex->lock` | shard, with per-key ownership (section 5) |
| `shard_id` | init | immutable |
| `net_polling` | `ex->lock` (`rt_net_poller.c:153-170`) | shard |

## 4. `rt_task` And `rt_scope` Fields

`rt_task` (`rt_async_internal.h:172-213`):

| Field group | Current protection | Target lane |
| --- | --- | --- |
| `status`, `enqueued`, `cancelled`, `wake_token`, `polling`, `handle_refs` | atomics (transitions under `ex->lock`) | atomic; queue/park transitions under owner shard lock |
| `id`, `poll_fn_id` | set at spawn | immutable |
| `owner_shard_id`, `owner_shard_valid`, `placement_class` | `ex->lock` (spawn inherit `rt_scheduler_placement.c:50-67`; accept re-place `rt_async_waiter.c:345-358`) | owner shard lock; re-placement transition protocol *(spike)* |
| `park_key`, `park_prepared`, `wait_keys[]`, `net_ready_accept_*` | `ex->lock` | owner shard lock (the list belongs to the task; each registration belongs to the key owner's store) |
| `resume_kind`, `resume_bits` | `ex->lock` (written by channel/blocking wakers and by the task itself) | owner shard lock; wakers write them in the collect-then-wake step under the owner lock |
| `state`, `checkpoint_polled`, `sleep_armed`, `sleep_delay`, `sleep_deadline` | `ex->lock` + single-poller guarantee | owner shard lock; `sleep_deadline` also read by the clock lane *(spike)* |
| `scope_id`, `parent_scope_id`, `scope_registered`, `cancel_pending`, `children[]` | `ex->lock` | control (scope tree stays global this epic) |
| `timeout_task_id`, `select_timers[]` | `ex->lock`, only touched by the owning task's own poll | owner shard lock |
| `result_kind`, `result_bits` | written once in `mark_done` | owner shard lock at write; readers observe after `TASK_DONE` acquire load |

`rt_scope` (`rt_async_internal.h:215-225`): every field stays **control** in
Epic 7. Scope wake (`scope_key` waiters) ownership is a *(spike)* decision:
control-lane store or owner-task shard store.

## 5. Waiter Key Ownership

Current: net keys already live in the fd owner shard's store; every other
kind lands in shard 0's store via `rt_executor_waiter_store(ex)`
(`rt_async_waiter.c:438-459`, `rt_runtime.c:245-251`).

| Key kind | Producer/consumer today | Target store owner |
| --- | --- | --- |
| `WAKER_NET_ACCEPT/READ/WRITE` (fd) | net wait/poller (owner shard) | fd owner shard (unchanged) |
| `WAKER_JOIN` (task id) | `rt_task_poll`, select, timeout | target task's owner shard (wake happens in `mark_done` on that shard) |
| `WAKER_TIMER` (task id) | sleep task park | sleeping task's owner shard, via the new sleep store |
| `WAKER_CHAN_SEND/RECV` (channel ptr) | channel ops | channel owner shard |
| `WAKER_BLOCKING` (task id) | blocking poll park; completion wake from pool thread (`rt_async_blocking.c:119-122`) | task's owner shard |
| `WAKER_SCOPE` (scope id) | `rt_scope_join_all` park; `scope_child_done_locked` wake | *(spike)*: control store or scope-owner-task shard store |

Multi-key parks exist: `rt_timeout_poll` (join + timer join), `rt_select_poll`
(joins + channel keys + timer joins) register one task under several key
owners (`rt_async_task.c:317-329`, `570-673`). Registration and cleanup
(`clear_wait_keys`) must take each key owner's lock in sequence — never two at
once — and the wake side must tolerate a waiter entry whose task was already
woken by a sibling key (`wake_token` + status check absorb this today at
`rt_async_state.c:1044-1074`).

## 6. Path-By-Path Target Lanes

| Path | Today | Target |
| --- | --- | --- |
| Worker scheduler turn (`rt_worker_main`, `rt_async_state.c:1638-1727`) | global lock around pop/run bookkeeping; non-user polls under lock | shard lock only; non-user polls move off-lock or stay short under shard lock *(spike)* |
| Worker sleep (`1675`) | global `ready_cv` | per-shard cv; wake targets only the owner shard |
| `ready_push`/`wake_task` (`800-863`, `1044-1090`) | global | owner shard lock; cross-shard producers use collect-then-wake |
| `park_current` (`1126-1160`) | global | owner shard lock + key owner store lock in order |
| Net wait registration (`rt_net.c:686-739`) | global | fd owner shard lock |
| Net poll build/complete (`rt_net.c:691-823`, `rt_net_poller.c`) | global (syscall unlocked) | owner shard lock (syscall stays unlocked) |
| Accept readiness completion + re-place (`rt_async_waiter.c:322-370`) | global | fd owner shard store lock, then task owner transition *(spike)* |
| Channel send/recv/try/close (`rt_async_channel.c`) | global | channel owner shard lock; peer wakes collect-then-wake |
| Sync-channel compat (`rt_wait_current_worker_wakeup`, `1598-1636`; compensation `1519-1574`) | global + `ready_cv` | control lane compat; must not intrude on shard hot paths |
| Spawn (`__task_create`, sleep/checkpoint/blocking submits) | global | control (id+publish+scope) then owner shard (ready push), order control→shard |
| Join poll (`rt_task_poll`) | global | target owner shard (join store) + control only for scope adoption writes (`rt_async_task.c:77-84` analogue in `rt_task_wake`) |
| Main-thread await (`rt_task_await:179-196`) | global + `done_cv` | control lane |
| N=1 runner (`next_ready` `1228-1275`, `run_ready_one`, `run_until_done`) | global | control + shard 0, preserving N=1 semantics |
| io thread (`rt_io_main`, `1779-1850`) | global + `io_cv` | N=1 coordination lane *(spike)*; multi-shard net polling already shard-owned |
| Cancellation (`cancel_task`, `1406-1427`, recursive over children) | global | control (children tree) + owner shard per task wake, order control→shard |
| `mark_done` (`1429-1474`) | global | owner shard (task state, join wakes) + control (scope bookkeeping, done_cv, free slot), order fixed by the lifetime rule *(spike)* |
| Scope ops (`rt_async_scope.c`) | global | control |
| Blocking completion wake (`rt_async_blocking.c:113-123`) | global | task owner shard via collect-then-wake |
| Timers (`tick_virtual` `1162-1180`, `next_sleep_deadline` `1182-1204`, `advance` `1206-1226`) | global, O(total tasks) scans | explicit sleep store; lane and clock protocol *(spike)*; scans die |
| Shutdown (`rt_shutdown.c:18-43`) | global broadcast set | control write + per-shard wake sweep (cv + pipe) + blocking |
| Trace dumps (`rt_async_trace.c:309`, `511`) | global scan | control + sequential shard locks (debug-only) |
| Listener group ops (`rt_net_accept_group.c`) | global | per-member owner shard, sequential, never two shard locks |

## 7. Hazards The Split Must Not Recreate

- **Wake-vs-park race:** closed today by `wake_token` exchange inside one
  lock (`rt_async_state.c:1126-1160`). After the split the same token must
  close the collect-then-wake window; a wake that fires between store-pop and
  owner-lock acquisition may only produce one bounded spurious poll.
- **Wake with stale `park_key`:** `wake_task(remove_waiter_flag=1)` reads the
  task's current `park_key` (`1060-1063`). Under split locks that read is only
  legal under the task owner's lock, and the removal must go to the key
  owner's store (which may be a different shard) — so removal becomes part of
  the collect-then-wake chain, not a same-lock convenience.
- **Duplicate ready enqueue:** guarded by `enqueued` (`800-863`). Both
  enqueue points (owner worker local push, cross-shard inject) must keep the
  guard under the owner shard lock.
- **Accept winner cleanup:** `clear_accept_winner_wait_keys` scans the whole
  task table today (`rt_async_waiter.c:304-320`). Post-split it must not:
  either the pending-accept set becomes owner-shard state, or the scan takes
  the control lane explicitly (cold path) — *(spike)*.
- **Channel value loss on cancelled peers:** `pop_waiter` drops
  done/cancelled waiters while holding the one lock (`rt_async_waiter.c:503-546`).
  Collect-then-wake must re-validate under the peer's owner lock and must not
  lose the handed-off value: an undeliverable peer means retry with the next
  waiter or requeue to the buffer, matching the contracts in
  `runtime-v2-waiter-check`.
- **`poll_ready_child_inline`** (`rt_async_task.c:143-168`) unlocks and
  relocks around an inline child poll; the split version must state which
  locks are held at each step and keep the child's scheduler accounting on
  the child's owner shard.
- **Compensation workers** share the spawning shard's scheduler
  (`1519-1574`); they must keep working when that scheduler is shard-locked.
- **`exec_init_once`** starts workers before `initialized=1`; per-shard locks
  and cvs must be initialized before any worker can observe them.

## 8. What Is Already Safe

- Trace counters in `rt_async_trace.c` / `rt_net_trace.c` are atomics or
  debug-only aggregations; no lane change except the two debug snapshot scans.
- `tls_current_task`, `tls_worker_ctx`, `poll_env`, `poll_result`,
  `pending_key` are thread-local.
- `channel_wake_force_inject`, env-derived config, and
  `rt_runtime_start_config` are set before workers start.
- Blocking pool internals already run under `blocking_lock`.
- Heap accounting cells are per-owner since Epic 5.
