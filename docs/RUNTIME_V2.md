# Surge Runtime V2 Target Architecture

Status: target architecture, not an implementation contract.

This document captures the intended direction for a future native Surge runtime.
It is deliberately separate from `docs/RUNTIME.md`, which describes the runtime
that exists today. Runtime V2 is a design target used to guide refactors,
benchmarks, and incremental experiments.

## Summary

The target model is:

```text
Tokio-style lowering + Glommio/Seastar-style per-core runtime + Zig-style Io boundary
```

More precisely:

- Surge keeps stackless async lowering: async code compiles to poll state
  machines, suspend branches, and explicit runtime calls.
- The hot path uses a thread-per-core, shared-nothing scheduler. File
  descriptors, request tasks, timers, channel waiters, and hot allocations stay
  on the owning shard.
- Work stealing does not run on the connection hot path. It belongs to the
  emergency, migration, or explicitly distributed path.
- The runtime eventually exposes an `Io` capability boundary, inspired by Zig's
  `std.Io`, but this boundary must not block the scheduler refactor.

The goal is not to copy Tokio, Seastar, Glommio, or Zig. The goal is to keep the
parts that fit Surge's current lowering and remove the global contention that
current native TCP workloads expose.

## Why Not "Tokio + Seastar + Zig" Literally

Tokio-style work stealing and Seastar-style fd ownership conflict on the hot
path. If a connection task is pinned to the shard that owns its fd, another
worker cannot freely steal that task without causing the next fd operation to
cross back to the owner. That reintroduces the cross-core traffic the design is
meant to remove.

Runtime V2 therefore uses Tokio only for the lowering shape: stackless tasks,
cooperative polling, and explicit suspend points. It does not use Tokio's
general-purpose multi-worker scheduling policy for connection tasks.

The hot path is closer to Glommio and Seastar:

- one executor per core or shard;
- cooperative scheduling inside each shard;
- thread-local I/O;
- explicit cross-shard messages when ownership boundaries are crossed.

Zig contributes a different idea: make I/O and concurrency an explicit runtime
capability. This helps testability and future backend selection, but it is not
the first performance lever.

## Evidence From The Current Runtime

The current runtime already has stackless tasks and a multi-worker executor, but
several hot paths still converge on global state.

Code evidence:

- `runtime/native/rt_async_internal.h` has a single `rt_executor` with global
  `tasks`, `inject`, `local_queues`, `waiters`, net poll scratch buffers,
  condition variables, and one executor lock.
- `runtime/native/rt_async_state.c` stores channel, join, timer, scope,
  blocking, and net waiters in one FIFO list. `pop_waiter()` scans and compacts
  the whole list for one key.
- `runtime/native/rt_net.c` rebuilds the network poll set by scanning the
  global waiter list, deduplicating fds, calling `poll()`, and then completing
  matching waiters.
- `runtime/native/rt_async_channel.c` makes direct channel send and receive
  take the global executor lock and use the shared waiter list.
- `runtime/native/rt_alloc.c` records every alloc/free with global relaxed
  atomic counters: `heap_alloc_count`, `heap_free_count`, `heap_live_blocks`,
  and `heap_live_bytes`.

Benchmark evidence:

- `docs/2026-06-25-runtime-net-scheduler-refactor-plan.md` shows tiny TCP
  traffic regressing when the native runtime uses more workers. In the baseline
  probe, `SURGE_THREADS=1` reached about `30k rps` for 32-client `ping`, while
  `SURGE_THREADS=8` dropped to about `10k rps`.
- Removing per-poll allocation and counting net waiters cleaned up counters but
  did not fix throughput. That points away from small allocation churn and
  toward scheduler and ownership churn.
- Bounded I/O-thread draining after net readiness was the first patch that moved
  throughput and tail latency together. That supports the locality hypothesis:
  running the ready continuation near the I/O event helps more than simply
  reducing poll allocation.
- The same plan notes that channel request/reply gets about 2.7x slower under
  8 workers, which matches the global waiter and global lock hypothesis.

The strongest current theory is therefore:

```text
Global scheduler state, global waiter scans, cross-worker wake placement, and
global allocation counters prevent the runtime from scaling linearly.
```

## Target Architecture

### 1. Shards

Runtime V2 consists of `N` shards. Each shard normally maps to one OS thread and
one CPU core. A shard owns:

- a local ready queue;
- a run-next or LIFO handoff slot;
- local timers;
- local fd registry;
- local waiter tables;
- local channel wait queues for channels owned by the shard;
- local heap stats and hot allocation pools;
- runtime traces for that shard.

The process may still have a runtime object that contains all shards, but the
connection hot path must not require one global lock.

### 2. FD Ownership

Each accepted connection belongs to exactly one shard. The owning shard handles:

- readiness registration;
- read and write waiters;
- parser and connection buffers;
- request task state;
- response writes;
- per-connection timers.

On Linux, `SO_REUSEPORT` is the preferred accept distribution mechanism because
each shard can own an accept socket and receive connections directly. The
fallback can be a single acceptor plus explicit handoff, but that fallback is
not the ideal hot path.

### 3. No Hot-Path Stealing

Connection tasks are shard-local. A shard may not steal another shard's
connection task just because it is idle. If work must move, the runtime treats it
as migration:

1. detach state from the old shard;
2. transfer ownership with an explicit message;
3. attach state to the new shard;
4. update fd and timer ownership.

Migration is a control-plane feature, not a request-path primitive.

### 4. Local Waiters

Runtime V2 removes the single global waiter list from the hot path.

Waiters are stored by owner:

- fd read/write waiters live in the fd registry entry;
- channel send/recv waiters live in the channel object;
- join waiters live with the target task or the owning scope;
- timer waiters live in the shard timer structure;
- blocking completions return to the owning shard.

Cancellation stores back references so cleanup is proportional to the number of
registrations made by that task, not to total system waiters.

### 5. Channels

Channels have an owning shard.

Same-shard send/recv uses local queues:

- match a waiting receiver or sender;
- write resume state;
- push the resumed task into the run-next slot or local queue;
- avoid global condition variables.

Cross-shard send/recv sends a message to the owning shard. The owning shard
performs the queue operation and returns a completion message if needed.

The current sync channel compatibility path remains a fallback. Hot code should
use direct async channel operations.

### 6. Structured Concurrency

Spawn is shard-local by default. A task spawned inside a request handler runs on
the parent shard unless the program explicitly requests distributed work.

This rule keeps structured concurrency cheap:

- child registration stays local;
- `join_all` stays local for normal request trees;
- failfast cancellation stays local;
- completion accounting does not require a shared atomic on every child exit.

For explicitly distributed scopes, the scope has an owning shard. Child
completion and cancellation are messages to that owner. This is acceptable for
low-fanout distributed work, but it must not be the default per-request shape.

### 7. Allocation And Heap Stats

Shared-nothing requires more than removing locks. It also requires removing
shared cache lines from the hot path.

Current `rt_alloc` updates global atomic counters on every allocation and free.
Runtime V2 changes this:

- each shard keeps local heap counters;
- `rt_heap_stats()` aggregates counters on read;
- hot runtime objects come from shard-local slab or bump allocators;
- connection buffers, task states, waiter nodes, and parser scratch memory are
  allocated and freed on the owning shard;
- request-path code must not touch shared refcounts or global heap counters.

This is a core design requirement, not a late optimization.

### 8. Io Boundary

Runtime V2 should eventually expose an `Io` or `Runtime` boundary:

- net, timers, filesystem, blocking work, entropy, channels, and task spawning
  route through it;
- tests can inject deterministic or failing implementations;
- future backends can select threaded, evented, or io_uring behavior.

This boundary should not be passed through every hot call if it makes code noisy.
The preferred first shape is ambient per-shard access:

```text
current_shard() -> RuntimeShard*
```

Explicit passing can remain available for tests, bootstrap code, and advanced
embedding. This keeps the Zig-inspired boundary without turning every I/O call
into plumbing.

### 9. I/O Backends

The first V2 backend should be the simplest backend that proves the ownership
model:

1. shard-local `poll` or `epoll` on Linux;
2. `kqueue` for BSD/macOS if needed;
3. `io_uring` after ownership, allocation, cancellation, and buffer lifetime are
   stable.

`io_uring` can reduce syscalls and support completion-oriented I/O, but it will
not fix global scheduler ownership by itself.

## Current Problems And V2 Resolution

| Current problem | Current shape | V2 resolution |
| --- | --- | --- |
| Global executor lock | One lock owns tasks, queues, waiters, timers, net scratch, and shutdown. | Shard-local scheduler state; global state only for control plane. |
| Global waiter list | All waiter kinds share one FIFO list. | Owner-local wait queues keyed by fd, channel, task, timer, or scope. |
| O(n) wake and park | `pop_waiter()` scans and compacts the full waiter list. | O(1) or O(k) operations on the owner-local queue. |
| Net poll rebuild churn | Net polling rebuilds the fd set from global waiters. | Shard-local fd registry persists across poll cycles. |
| Cross-worker wake churn | Non-worker wakes enter global inject; worker wakes can signal other workers. | Net readiness resumes the task on the owning shard. |
| I/O thread as partial worker | The current patch drains ready inject tasks after net readiness. | A shard runs its own net-ready continuations or drains a net-woken queue only. |
| Expensive channel handoff | Channel send/recv uses global lock plus shared waiters. | Same-shard channel handoff is local; cross-shard handoff is explicit messaging. |
| Global allocation counters | `rt_alloc` touches shared atomics on every alloc/free. | Per-shard heap stats and shard-local hot object pools. |
| Cross-shard structured concurrency | If children spread by default, join/cancel become broadcasts. | Spawn is local by default; distributed work is explicit. |

## Refactor Policy

A broad structural refactor is useful, but only if it follows the V2 ownership
boundaries. A cosmetic file split before those boundaries are clear would add
diff noise without reducing risk.

The safe refactor rule is:

```text
Introduce V2-shaped structures with N=1 first. Keep behavior identical.
```

This allows tests to validate structure before concurrency changes. It also
separates two hard problems:

- reorganizing runtime state;
- changing scheduling and ownership semantics.

Before changing scheduling, document which scheduler properties are language
contracts and which are current implementation artifacts. The VM backend and
golden tests may implicitly depend on FIFO ordering that the native runtime
should not promise forever.

## Migration Plan

### Phase 0: Contract And Structure

- Define scheduler semantics that are part of the language contract.
- Add V2-shaped shard structs with `N=1`.
- Move fields from `rt_executor` into `rt_runtime` and `rt_shard` without
  changing behavior.
- Keep the current public ABI stable.

### Phase 1: Local Waiters With N=1

- Move net waiters, timers, task join waiters, and channel waiters into
  owner-local structures.
- Keep `N=1` so concurrency behavior remains unchanged.
- Preserve cancellation and select cleanup through back references.

### Phase 2: Local FD Registry

- Replace poll-set rebuild from global waiters with a persistent shard fd
  registry.
- Keep one shard.
- Prove counter changes against the existing tiny TCP probe.

### Phase 3: N>1 Accept Ownership

- Enable multiple shards.
- Use `SO_REUSEPORT` on Linux where available.
- Keep accepted connections on the accepting shard.
- Disable work stealing for connection tasks.

### Phase 4: Cross-Shard Messaging

- Add explicit messages for cross-shard channel operations, cancellation,
  distributed scopes, and controlled migration.
- Keep spawn local by default.

### Phase 5: Shard-Local Allocation

- Replace global heap counters with per-shard counters.
- Add shard-local pools for task state, waiters, connection buffers, and parser
  scratch memory.
- Aggregate heap stats only when requested.

### Phase 6: Optional io_uring Backend

- Add io_uring after the ownership and lifetime model is stable.
- Treat it as a backend under the same shard and `Io` boundary.

## Benchmark Plan

The current 32-connection probe is useful for regression detection, but it is
not enough to judge shared-nothing scaling. With 8 shards, 32 connections means
about four connections per shard under perfect distribution, and `SO_REUSEPORT`
can be skewed at low connection counts.

Runtime V2 should be judged with:

- 1, 8, and 32 connections for small-load latency and regression checks;
- 1k and 10k connections for shared-nothing scaling;
- single-shard and multi-shard rows;
- pipelined and non-pipelined TCP rows;
- mixed CPU/TCP rows with bounded CPU tasks;
- trace counters for cross-shard messages, global path usage, shard imbalance,
  local queue depth, allocation counters, and fd readiness batches.

Success means multi-shard runtime does not regress small-load latency badly and
materially improves many-connection throughput and tail latency.

## Sources

- Tokio runtime documentation:
  https://docs.rs/tokio/latest/tokio/runtime/index.html
- Tokio scheduler notes:
  https://tokio.rs/blog/2019-10-scheduler
- Seastar shared-nothing design:
  https://seastar.io/shared-nothing/
- Seastar tutorial:
  https://github.com/scylladb/seastar/blob/master/doc/tutorial.md
- Glommio crate documentation:
  https://docs.rs/glommio/latest/glommio/
- Glommio engineering overview:
  https://www.datadoghq.com/blog/engineering/introducing-glommio/
- Zig 0.15.1 release notes:
  https://ziglang.org/download/0.15.1/release-notes.html
- Zig 0.16.0 release notes:
  https://ziglang.org/download/0.16.0/release-notes.html
- Zig devlog:
  https://ziglang.org/devlog/2026/
- Linux `io_uring` manual:
  https://man7.org/linux/man-pages/man7/io_uring.7.html
- Linux `SO_REUSEPORT` socket option:
  https://man7.org/linux/man-pages/man7/socket.7.html

