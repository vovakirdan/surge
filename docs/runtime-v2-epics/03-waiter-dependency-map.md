# Epic 3 Waiter Dependency Map

Status: complete for Epic 3 Task 02 read-only mapping.

Current waiter storage is executor-global:
`rt_executor.waiters`, `waiters_len`, `waiters_cap`, and `net_waiters_len`
in `runtime/native/rt_async_internal.h`.

## Current Storage Contract

`ex->lock` owns `tasks[]`, `scopes[]`, `waiters`, net waiter and poll scratch
state, timer state, and shutdown flags
(`runtime/native/rt_async_internal.h:234`). The waiter list is documented as
FIFO-by-key, and `wake_token` closes wake-before-park races
(`runtime/native/rt_async_internal.h:240`).

## Waker Key Kinds

| Kind | Producer | Consumer | Cleanup path | Behavior protected |
| --- | --- | --- | --- | --- |
| `WAKER_JOIN` | `join_key()` in `runtime/native/rt_async_state.c:542`; task await, timeout, and select in `runtime/native/rt_async_task.c` | `mark_done()` wakes joiners in `runtime/native/rt_async_state.c:2071` | `clear_wait_keys()`, `mark_done()`, and cancellation wake | Await/select wake on task completion. |
| `WAKER_TIMER` | sleep task poll via `timer_key(task->id)` in `runtime/native/rt_async_poll.c` | virtual time advances sleep tasks through `wake_task(..., 1)` | `wake_task()` removes `park_key`; `mark_done()` removes stale park key | Sleep task remains parked until deadline. |
| `WAKER_SCOPE` | scope join and cancellation-pending owner park | last child wakes `scope_key(scope->id)` | child completion removes active child; scope exit frees scope | Owner waits for child drain before scope exit or cancel completion. |
| `WAKER_BLOCKING` | blocking task poll | blocking worker completion calls `wake_key_all(blocking_key(task_id))` | cancellation requests job cancel; task cancellation wakes parked task | Blocking result wakes parked async task once. |
| `WAKER_CHAN_SEND` | blocked send and select-send paths | recv, try-recv, refill, and close pop sender waiters | channel close sets send-closed resume; cancel/done use generic cleanup | FIFO sender handoff and send-on-close result. |
| `WAKER_CHAN_RECV` | blocked recv and select-recv paths | send, try-send, and close pop receiver waiters | channel close sets recv-closed resume; cancel/done use generic cleanup | FIFO receiver handoff and close wakes receivers. |
| `WAKER_NET_ACCEPT` | `rt_net_wait_accept()` through `net_wait_current_task()` | poll readiness completes accept/read keys | `complete_net_waiters()` removes matching waiters | Accept readiness through rebuilt poll set. |
| `WAKER_NET_READ` | `rt_net_wait_readable()` through `net_wait_current_task()` | poll readiness completes read keys | `complete_net_waiters()` removes matching waiters | Read readiness through rebuilt poll set. |
| `WAKER_NET_WRITE` | `rt_net_wait_writable()` through `net_wait_current_task()` | poll readiness completes write keys | `complete_net_waiters()` removes matching waiters | Write readiness through rebuilt poll set. |

## Call Graph

- `add_waiter()` appends to `ex->waiters` and increments `net_waiters_len` for
  net keys (`runtime/native/rt_async_state.c:1262`). Callers are
  `add_wait_key()`, `prepare_park()`, and unprepared `park_current()`.
- `remove_waiter()` compacts matching `(key, task_id)` entries and preserves
  the relative order of the rest (`runtime/native/rt_async_state.c:1243`).
  Callers include `clear_wait_keys()`, `wake_task_with_policy()`,
  wake-before-park rollback, and `mark_done()`.
- `pop_waiter()` scans FIFO by key, drops done/cancelled/stale entries, removes
  the first live waiter, and compacts (`runtime/native/rt_async_state.c:1333`).
  Current callers are channel-only: handoff, try-send, try-recv, status helpers,
  buffer refill, and close draining in `runtime/native/rt_async_channel.c`.
- `wake_task()` delegates to `wake_task_with_policy()`, sets `wake_token`,
  clears `park_key`, optionally removes the waiter, and enqueues ready work
  (`runtime/native/rt_async_state.c:1636`).
- `wake_key_all()` removes all waiters for one key and wakes each task
  (`runtime/native/rt_async_state.c:1691`). Current producers are blocking
  completion, scope child drain, and task completion joiners.
- `prepare_park()` pre-registers a waiter before the task stores
  `TASK_WAITING` (`runtime/native/rt_async_state.c:1319`). `rt_async_yield()`
  turns `pending_key` into `POLL_PARKED`; `park_current()` commits or rolls
  back the park through `wake_token`.

## Cleanup Paths

| Path | Current behavior |
| --- | --- |
| Done | `mark_done()` clears multi-key waiters, clears select timers, removes `park_key`, stores `TASK_DONE`, wakes joiners, and broadcasts `done_cv`. |
| Cancellation | `cancel_task()` marks cancelled, requests blocking cancel, wakes if waiting, and recurses to children. `pop_waiter()` and net completion drop cancelled waiters. |
| Timeout | `rt_timeout_poll()` joins both target and sleep-task join keys. Expired timeout cancels the target. `clear_select_timers()` cancels timer tasks and releases handles. |
| Channel close | `rt_channel_close()` sets `closed`, drains recv waiters as `RESUME_CHAN_RECV_CLOSED`, then drains send waiters as `RESUME_CHAN_SEND_CLOSED`. |
| Scope cancellation | `rt_scope_cancel_all()` calls `scope_cancel_children_locked()`. Failfast child cancellation cancels siblings and wakes the owner. |
| Blocking completion | The blocking worker marks the job done and calls `wake_key_all(blocking_key(task_id))`. |
| Net readiness | `poll_net_waiters()` rebuilds the poll set by scanning `ex->waiters`, dedupes fds, polls, then `complete_net_waiters()` removes and wakes matching live tasks. |
| Shutdown-adjacent | Worker, I/O, and blocking loops observe shutdown flags. No dedicated shutdown waiter drain contract exists yet. |

## Decisions

Contract now:

- `ex->lock` owns waiter mutation.
- Wake-before-park is guarded by `wake_token`.
- Done cleanup removes `wait_keys`, `select_timers`, and `park_key` before join
  wake.
- Channel close wakes both recv and send waiters.

Implementation detail:

- One global waiter array with O(n) scans.
- `net_waiters_len` is a polling hint, not ownership.
- `pending_key` is thread-local handoff state.
- Timeout waits on a sleep task's `WAKER_JOIN`; `WAKER_TIMER` parks only the
  sleep task itself.

Open decision:

- Whether FIFO-by-key remains a Runtime V2 contract or becomes an
  implementation detail.
- Exact owner-local home for each key kind.
- Net fd close semantics: current close paths close fds but do not wake
  existing net waiters.
- Shutdown waiter cleanup: current loops observe shutdown flags, but no scoped
  cleanup contract exists.
- Whether channel no-signal handoff policy is contract or scheduler detail.

## Later Tests And Probes

Existing probes to run before and after relevant migrations:

- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMT(NonYieldingTrySendHandoffWakesReceiver|RecvAckHandoffCompletesSenderAfterNonYieldingReceiver|BufferedRecvRefillCompletesSenderAfterNonYieldingReceiver|BufferedBlockingRecvRefillWakesSender|ChannelParkUnpark)' -v --timeout 120s`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMT(WakeupsAndCancellation|CorrectnessWakeups|StructuredConcurrency|BlockingPool)' -v --timeout 120s`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMTCorrectnessChannels' -v --timeout 90s`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestMTNetWaiterWakeupLatency' -v --timeout 90s`
- `SURGE_SKIP_TIMEOUT_TESTS=0 go test ./internal/vm -run 'TestNativeNetSingleThreadBlockingChannelInAsyncServer' -v --timeout 90s`

Missing probes to add before migration:

- owner-local cleanup by kind: cancel one waiter, wake the same key, and prove
  the next live waiter receives it;
- channel close/cancel matrix for recv, send, close, and cancel;
- timer/select cleanup probe proving losing timer tasks cannot leave stale
  joiners;
- net close/cancel/readiness probe for fd waiters without adding a persistent
  fd registry;
- shutdown liveness probe proving worker, I/O, blocking, net, timer, and
  channel waiters cannot strand.
