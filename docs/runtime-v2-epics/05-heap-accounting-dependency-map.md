# Epic 5 Task 2: Heap Accounting Dependency Map

Status: complete for design handoff.

This map records the current heap-accounting paths before Task 5 introduces an
accounting-cell skeleton. It is docs-only evidence. It does not change runtime
code, tests, CI, or task documents.

## Source Audit

Primary sources:

- `runtime/native/rt_alloc.c`
- `runtime/native/rt_runtime.c`
- `runtime/native/rt_async_state.c`
- `runtime/native/rt_async_internal.h`
- `runtime/native/rt_async_blocking.c`
- `runtime/native/rt_async_task.c`
- `runtime/native/rt_async_poll.c`
- `internal/vm/llvm_native_heap_stats_test.go`
- `internal/vm/intrinsic.go`
- `internal/vm/intrinsic_debug.go`
- `internal/backend/llvm/builtins.go`
- `internal/backend/llvm/emit_intrinsics_runtime.go`
- `docs/RUNTIME.md`
- `docs/RUNTIME.ru.md`
- `docs/RUNTIME_V2.md`
- `docs/LANGUAGE.md`

During the audit, two untracked parallel test surfaces were present:
`internal/vm/runtime_v2_heap_accounting_contract_test.go` and
`internal/vm/runtime_v2_heap_accounting_static_test.go`. This map does not
depend on either file being committed, but the consumer table names them because
they were present in the workspace and already encode Task 3 and Task 4
expectations.

## Current Producer Paths

`runtime/native/rt_alloc.c` owns all native heap-accounting writes today:

| Path | Current behavior | Contract to preserve |
| --- | --- | --- |
| `alloc_size(size)` | Normalizes `0` to `1`. | Zero-size allocation counts as one live byte. |
| `record_alloc(size)` | Relaxed atomic increments: `heap_alloc_count += 1`, `heap_live_blocks += 1`, `heap_live_bytes += alloc_size(size)`. | Successful allocation records exactly one allocation event and one live block. |
| `record_free(size)` | Relaxed atomic increments `heap_free_count += 1`; subtracts one live block and `alloc_size(size)` live bytes. | `rt_free(NULL, ...)` records nothing; non-null free records exactly one free event. |
| `record_realloc(old_size, new_size)` | Relaxed atomic increments both alloc and free counts; adjusts live bytes by the size delta; leaves live blocks unchanged. | Successful in-place realloc is one alloc event plus one free event with no live-block delta. |
| `rt_alloc(size, align)` | Uses `malloc` when `align <= sizeof(void*)`, otherwise `posix_memalign`; records only after success. | Failed allocation records nothing; public ABI stays `void* rt_alloc(uint64_t, uint64_t)`. |
| `rt_free(ptr, size, align)` | Ignores `align`; if `ptr != NULL`, calls `rt_array_forget_allocation(ptr)` then `record_free(size)`; always calls `free(ptr)`. | Keep the array-forget side effect on the same non-null free path. |
| `rt_realloc(ptr, old_size, new_size, align)` | `new_size == 0` delegates to `rt_free`; ordinary alignment uses `realloc`; aligned growth allocates, copies, then frees the old block when `ptr != NULL && old_size > 0`. | Failed realloc records nothing and leaves the old allocation alive; aligned realloc keeps the existing copy/free shape. |
| `rt_heap_stats()` | Loads the four counters first, then allocates `SurgeHeapStats`, then converts each snapshot value to `BigUint`; native RC fields are always zero. | Snapshot values must not include allocations made to build the result object. |

The current source of truth is one process-global hot counter set:

- `heap_alloc_count`
- `heap_free_count`
- `heap_live_blocks`
- `heap_live_bytes`

All four are file-scope `_Atomic uint64_t` values in `rt_alloc.c`. The current
implementation uses `memory_order_relaxed` for all increments, decrements, and
loads.

## Heap-Accounted Native Allocation Callers

Every current native heap-stat producer reaches the paths above through
`rt_alloc`, `rt_free`, or `rt_realloc`. Not every native libc allocation is
heap-accounted today: direct `malloc`, `realloc`, and `free` calls in files such
as `rt_bignum_format.c`, `rt_io.c`, `rt_fs.c`, and `rt_net.c` are temporary
native buffers outside the current `rt_heap_stats()` contract. Epic 5 preserves
that boundary; it moves the existing heap-accounted producer state and does not
make all libc temporaries visible to `rt_heap_stats()`.

The direct accounted callers fall into these runtime contexts:

| Context | Representative callers | Notes |
| --- | --- | --- |
| Cold or main thread before runtime workers | `rt_runtime.c` initializes shard scheduler queues; `rt_async_state.c` allocates worker arrays and worker contexts; `rt_async_blocking.c` allocates blocking thread arrays; simple non-async programs can call native intrinsics without starting workers. | `rt_alloc.c` must not call `ensure_exec()` to find accounting state, because executor initialization itself allocates. |
| Runtime-active main or synchronous runner thread | In single-worker mode, `rt_task_await` calls `run_until_done`, which repeatedly calls `run_ready_one` and can poll user tasks on the caller thread without `tls_worker_id`. | Task 5 must either register a main/synchronous-runner lane or explicitly route this active runtime path to the cold/external cell with that tradeoff recorded. |
| Executor worker thread | `rt_worker_main` sets `tls_worker_id` and polls user and runtime tasks. Calls can come from compiler-emitted `rt_alloc`/`rt_free`/`rt_realloc`, async task creation, channels, arrays, maps, strings, bignums, fs, net, ranges, tags, and waiter/task/scope helpers. | Hot request-path writes must avoid one shared global counter block. |
| I/O thread | `rt_io_main` owns net polling and can drain ready work through `run_ready_one_nowait_locked`; `tls_worker_id` remains unset. | The I/O thread needs its own accounting lane, not the cold fallback and not a worker-local queue lookup. |
| Blocking worker thread | `rt_blocking_worker_main` executes `__surge_blocking_call` for `blocking { ... }`; `blocking_job_release` can free the captured state and the job object. | Allocations inside blocking code and frees of state allocated by a submitting worker prove that alloc/free can land on different lanes. |
| Runtime-internal helper under executor lock | Waiter store growth, fd registry growth, deque growth, task/scope child arrays, net poll scratch, channel allocation, and task release/free. | These calls still hit `rt_alloc`, so the accounting API must be callable under runtime locks without taking scheduler-internal locks. |

The compiler also emits direct native calls:

- `internal/backend/llvm/emit_intrinsics_runtime.go` lowers explicit
  `rt_alloc`, `rt_free`, and `rt_realloc` intrinsics.
- Other LLVM emitters allocate native backing storage for literals, arrays,
  default values, tags, errors, iterators, numeric values, time values, boxes,
  and union casts.
- `internal/backend/llvm/builtins.go` declares `rt_heap_stats` as a no-arg
  native builtin returning `ptr`.

## Current Heap-Stats Consumers

| Consumer | What it observes |
| --- | --- |
| `runtime/native/rt.h` | Public native ABI for `rt_alloc`, `rt_free`, `rt_realloc`, and `rt_heap_stats`. |
| `internal/backend/llvm/builtins.go` | Native `rt_heap_stats` declaration: return `ptr`, no params. |
| `internal/backend/llvm/emit_intrinsics_runtime.go` | Explicit lowering for allocation/free/realloc calls. |
| `internal/vm/intrinsic.go` | Interpreter dispatch for `rt_heap_stats`, `rt_alloc`, `rt_free`, and `rt_realloc`. |
| `internal/vm/intrinsic_debug.go` | VM-side `HeapStats` layout validation and snapshot materialization. This is equivalent debug behavior, not native counter storage. |
| `internal/vm/heap_debug.go`, `heap.go`, `raw_memory.go` | VM heap counters and raw-memory counters used by the interpreter snapshot. They are separate from native `rt_alloc.c` counters. |
| `internal/vm/llvm_native_heap_stats_test.go` | Native LLVM smoke tests: monotonic alloc/live/free behavior and channel allocation deltas through `rt_heap_stats()`. |
| `internal/vm/runtime_v2_heap_accounting_contract_test.go` if retained | Pending behavior checks for sequential alloc/free/realloc/aligned/failed-realloc cases and concurrent worker accounting. |
| `internal/vm/runtime_v2_heap_accounting_static_test.go` if retained | Pending static target-shape checks for ABI, owner shape, cold path, absence of old globals, and aggregation. |
| `docs/RUNTIME.md` and `docs/RUNTIME.ru.md` | Document native allocation counters and say VM counters are separate where possible. |
| `docs/RUNTIME_V2.md` | Names current global atomics as a bottleneck and states the target of shard-local counters aggregated on read. |
| `docs/LANGUAGE.md` and `docs/ATTRIBUTES.md` | Document intrinsic declarations and raw-pointer intrinsic permissions. |

The native `HeapStats` layout is six pointer fields in this order:

1. `alloc_count`
2. `free_count`
3. `live_blocks`
4. `live_bytes`
5. `rc_increments`
6. `rc_decrements`

Native `rt_heap_stats()` currently fills every field with a `BigUint`; the two
RC fields are always `0`.

## Runtime State Availability

The current runtime skeleton is already owner-shaped but has no allocator-facing
accounting API:

- `rt_runtime.c` has one static `rt_runtime runtime_state`.
- `rt_runtime_init_global_n1(ex)` initializes `runtime_state` and wires
  `ex->runtime`.
- `rt_runtime` owns `shards[RT_RUNTIME_SHARD_COUNT]`; today the count is `1`.
- `rt_shard` owns scheduler queues, net poll scratch, fd registry,
  channel-blocking compatibility state, and waiter store.
- `rt_executor` owns the OS synchronization primitives, worker arrays,
  blocking-pool state, task/scopes arrays, and a pointer back to `rt_runtime`.

Current thread-local state can classify some callers but not all accounting
lanes:

- `tls_worker_id` identifies executor workers only.
- `tls_current_task` and `tls_current_id` identify the current task while a
  worker or I/O-drain path polls it.
- The main/synchronous runner path can poll tasks without a valid worker id.
- The I/O thread and blocking workers do not have a valid worker id.
- Compensation workers currently reuse a logical worker id modulo
  `scheduler->worker_count`, so worker id alone is not a unique write lane.

`rt_alloc.c` cannot safely discover runtime state by forcing executor
initialization. `exec_init_once()` and runtime/shard initialization allocate
worker queues, worker arrays, worker contexts, and blocking-pool arrays. Calling
`ensure_exec()` from allocation accounting would couple the allocator to
scheduler initialization and can recurse through the allocation path.

## Selected Direction For Task 5

Task 5 should introduce runtime/shard-owned accounting state with lane-local
write cells and aggregate-on-read stats.

Minimal cell model:

- A cell records monotonic event totals: allocation events, free events,
  allocated bytes, and freed bytes.
- `rt_heap_stats()` aggregates all registered cells, then derives
  `live_blocks = total_alloc_events - total_free_events` and
  `live_bytes = total_alloc_bytes - total_freed_bytes`.
- `record_realloc(old, new)` records one allocation event with `new` bytes and
  one free event with `old` bytes. That preserves the current public behavior
  without a per-cell live-block mutation.
- Native RC totals remain zero until a separate native RC feature exists.

Ownership:

- Store the accounting state under `rt_runtime` or `rt_shard`; with `N=1`, the
  single shard can own all cells.
- Include an explicit cold/external cell for allocations before runtime
  initialization and for threads that do not have a registered runtime lane.
- Register one cell per executor worker thread, one main/synchronous-runner
  cell or explicit cold-route decision, one I/O cell, and one cell per blocking
  worker or per blocking-worker TLS lane.
- Give compensation workers distinct cells or a TLS cell pointer. Do not map
  them only by logical `worker_id`, because more than one thread can share that
  id.

Allocator integration:

- Keep public `rt_alloc`, `rt_free`, `rt_realloc`, and `rt_heap_stats`
  signatures unchanged.
- Keep `rt_alloc.c` behind a narrow accounting API such as
  `record_alloc`, `record_free`, `record_realloc`, and snapshot helpers.
- Let runtime startup set a thread-local accounting-cell pointer for workers,
  the I/O thread, and blocking workers.
- When no thread-local cell is set, record to the explicit cold/external cell.
- Do not make `rt_alloc.c` depend on scheduler queues, worker selection,
  waiter stores, fd registry internals, or `ensure_exec()`.

This model satisfies the Epic 5 hot-path goal because request-path allocation
writes hit a lane-local cell instead of the current process-global counter
cache lines. It also keeps Task 5 small: introduce ownership and selection
shape first, then migrate producer writes in Task 6 and aggregation in Task 7.

## Rejected Models

| Model | Rejection reason |
| --- | --- |
| Keep the current four process-global atomics. | This is the bottleneck Epic 5 exists to remove. Every allocation/free writes the same cache lines. |
| Replace globals with one shard-global atomic counter block. | Current `N=1` still runs many worker threads plus I/O and blocking threads. One shard-global block would preserve the same hot contention under a new owner. |
| Use unsigned per-cell live blocks and live bytes only. | Cross-lane frees are real. A blocking worker can free state allocated by a submitting worker. Per-cell unsigned live counters can underflow before aggregation. |
| Derive the accounting cell by calling `ensure_exec()` in `rt_alloc.c`. | Executor initialization allocates and would make the allocator depend on scheduler startup and locks. |
| Add owner-shard span metadata, remote-free queues, slabs, or bump pools now. | Epic 5 explicitly limits this work to accounting ownership while preserving the current `malloc`, `free`, `realloc`, and aligned-allocation behavior. |

## Task 5 Handoff

Task 5 should first add the type and ownership skeleton, not move every write in
one step. The smallest useful skeleton is:

- a runtime/shard-owned heap-accounting state;
- an explicit cold/external cell;
- registered worker, main/synchronous-runner, I/O, and blocking lane cells, or
  an explicit recorded decision that the synchronous runner uses cold/external
  accounting;
- a thread-local selected-cell pointer or equivalent lane lookup that does not
  require scheduler internals in `rt_alloc.c`;
- a snapshot helper that can aggregate cells later without changing
  `rt_heap_stats()` ABI.

Task 6 should migrate `record_alloc`, `record_free`, and `record_realloc` to
record event totals through the selected cell. Task 7 should change
`rt_heap_stats()` from direct counter loads to aggregation while preserving the
current snapshot-before-result-allocation ordering.

## Open Risks

- Caller-context classification is source-backed but not runtime-trace-proven.
  Task 8 should prove the hot paths with concurrent allocation/free probes.
- Direct libc temporary allocations in native helper files remain outside the
  current heap-stat contract. Changing that boundary is not Epic 5 scope.
- The final cell registry must define how many blocking and compensation cells
  can exist and how they are retained for process-lifetime detached threads.
- Aggregation must guard against impossible aggregate underflow and fail loudly
  or clamp only with an explicit invariant decision.
- Any static test added by Task 4 should avoid assuming exact file names if Task
  5 chooses one accounting module split over another.
