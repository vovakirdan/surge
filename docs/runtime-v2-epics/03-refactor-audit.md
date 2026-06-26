# Epic 3 Refactor Dependency And Dead-Code Audit

Status: complete for Epic 3 Task 03 read-only audit.

This audit defines refactor boundaries for waiter work. It does not authorize
deleting code or changing behavior.

## File-Size And Responsibility Pressure

| File | Lines | Waiter-related pressure | Audit result |
| --- | ---: | --- | --- |
| `runtime/native/rt_async_state.c` | 2431 | Executor init, trace, scheduler queues, waiter list, wake/park, timer ticking, task cleanup, worker/I/O loops. Core list functions include `ensure_waiter_cap`, `remove_waiter`, `add_waiter`, `clear_wait_keys`, `add_wait_key`, `prepare_park`, and `pop_waiter`. | Primary extraction target. Split by dependency cluster only, not by size. |
| `runtime/native/rt_net.c` | 1040 | Net direct wait registers global waiter keys; poll rebuild scans `ex->waiters`; completion compacts waiters. | Do not extract first. Net depends on the existing rebuild model and `net_waiters_len`. |
| `runtime/native/rt_async_task.c` | 768 | Await, timeout, and select manage multi-key waits and select timers. | Later select/timer tranche after behavior tests. |
| `runtime/native/rt_async_channel.c` | 549 | Send, recv, try-send, try-recv, status helpers, and close all pop global waiters. | Later channel tranche after channel close/cancel race tests. |
| `runtime/native/rt_async_internal.h` | 460 | Shared internal contract: `waker_key`, `waiter`, task wait fields, executor waiter fields, invariants, and prototypes. | Near limit. Avoid adding declarations unless a tranche removes or moves equivalent declarations. |

## Dependency Clusters And Boundaries

| Cluster | Current anchors | Proposed boundary | Do not move yet |
| --- | --- | --- | --- |
| Legacy waiter key/list | key constructors, net-waiter accounting, waiter capacity, add/remove/pop, wait-key registration, `prepare_park` | `runtime/native/rt_async_waiter.c`: keep same executor lock and same FIFO-by-key behavior. | `wake_task_with_policy`, `wake_key_all_with_policy`, queue policy, worker sleep, storage owner move. |
| Wake/park policy | `wake_task_with_policy`, `ready_push_for_waker_key`, `wake_key_all_with_policy`, `park_current` | Keep in `rt_async_state.c` until wake-before-park tests are stronger. | Any change to `wake_token`, ready queue policy, or signal behavior. |
| Channel waiter users | channel send, recv, try, status helper, buffer refill, and close paths | Later channel-owner boundary. Start with compatibility APIs before channel-local queues. | First tranche must not change channel semantics or close behavior. |
| Task/select/timer users | task await, timeout, select, `wait_keys`, `select_timers` | Later `rt_async_select.c` or timer/select module after tests. | `rt_select_poll_tasks` deletion, timer ownership, select timer cleanup. |
| Net waiter users | `net_wait_current_task`, `poll_net_waiters`, `complete_net_waiters` | Later net-waiter boundary preserving poll-set rebuild. | Persistent fd registry, accept ownership, `N>1`. |
| Scope/blocking users | scope join/cancel and blocking task wait/completion | Later task/scope/blocking waiter migration tranche. | Blocking pool redesign or broad offload refactor. |

## First Safe Tranche

Create a legacy waiter helper module only after Task 04 and Task 05 behavior
and static checks exist.

Move exactly:

- `waker_none` and `waker_valid`;
- key constructors;
- `waker_is_net`, `net_waiter_added`, and `net_waiters_removed`;
- `ensure_waiter_cap`;
- `remove_waiter`, `add_waiter`, `clear_wait_keys`, `add_wait_key`,
  `prepare_park`, and `pop_waiter`.

Keep in place:

- `clear_select_timers`;
- `wake_task_with_policy`, `wake_key_all_with_policy`, and `park_current`;
- `tick_virtual`;
- net polling;
- channel handoff;
- task/select ABI;
- all storage fields.

Proof gates for that tranche:

- before/after line counts;
- Sentrux root and native scans with `health` and `check_rules`;
- `git diff --check`;
- `make c-check`;
- `make cppcheck`;
- `make runtime-v2-check`;
- direct channel wakeups from `LIVENESS_PROBES.md`;
- cancellation/join/timeout smoke from `LIVENESS_PROBES.md`;
- MT correctness channel fixture from `LIVENESS_PROBES.md`.

Record missing owner-back-reference tests as blockers until Task 04 adds them.

## Tranche Order

1. Behavior tests/static checks for the current waiter contract.
2. Legacy waiter key/list extraction with no storage movement.
3. Owner-local waiter skeleton behind compatibility APIs, still `N=1`.
4. Channel waiter migration.
5. Task/scope/blocking waiter migration.
6. Timer/select cancellation migration.
7. Net waiter migration, preserving poll rebuild and avoiding fd registry work.
8. Large-file follow-up only after the above proves stable boundaries.

## Dead-Code Suspects

| Symbol/path | Evidence class | Evidence | Decision |
| --- | --- | --- | --- |
| None | proven-dead | No symbol reached deletion-grade proof in this audit. | Delete nothing. |
| `rt_select_poll_tasks` | suspect-only | Native definition in `runtime/native/rt_async_task.c`; public ABI in `runtime/native/rt.h`; LLVM builtin in `internal/backend/llvm/builtins.go`; current emitter calls `rt_select_poll` in `internal/backend/llvm/emit_async.go`. Fresh generated IR was not produced in this task. | Keep. Before deletion: fresh generated-IR search, ABI review, focused select parity tests, `make c-check`, `make cppcheck`, `make runtime-v2-check`, and Sentrux before/after. |
| `rt_runtime_shard_count` | not-dead | Used by `internal/vm/runtime_v2_skeleton_static_test.go`. | Keep. |
| `rt_channel_try_recv_status_locked`, `rt_channel_try_send_status_locked` | not-dead | Used by blocking wrappers and select lowering paths. | Keep. |
| `wake_key_all` | not-dead | Used by scope completion, blocking completion, and join completion policy. | Keep. |
| Net waiter accounting helpers | not-dead | `poll_net_waiters()` depends on `net_waiters_len`; add/remove/pop/wake paths maintain it. | Keep with the waiter module if moved. |

## Risks And Checks

Main risks:

- moving `prepare_park` can break wake-before-park;
- moving `pop_waiter` can break FIFO-by-key and stale waiter cleanup;
- moving net accounting can corrupt `net_waiters_len` and poll decisions;
- moving channel or select code too early can hide behavior changes inside a
  refactor commit.

Required stance:

- do not run the broad accepted-debt command
  `go test ./internal/vm -run 'MT|Async|Net|LLVM'` as a required green gate;
- use the narrow liveness probes from `LIVENESS_PROBES.md`;
- treat missing Sentrux rules as missing compliance, not as a pass.
