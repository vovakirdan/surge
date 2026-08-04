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
- Work stealing does not run on the connection hot path. It belongs to the CPU
  Tier 2 destination and, if needed, emergency or control-plane paths.
- Shard boundaries are move-only and shard-movable-only: only `own` values whose
  type is not shard-pinned may cross shards. Borrows stay on the source shard.
- Cross-shard operations are syntactically explicit. A value or call that
  crosses a shard boundary is written differently from its same-shard form and
  is visible at the call site. Same-shard cost and cross-shard cost are never
  spelled the same way.
- The runtime eventually exposes an `Io` capability boundary, inspired by Zig's
  `std.Io`, but this boundary must not block the scheduler refactor.

The goal is not to copy Tokio, Seastar, Glommio, or Zig. The goal is to keep the
parts that fit Surge's current lowering and remove the global contention that
current native TCP workloads expose.

Runtime V2 is not only a `runtime/native/` project. From the explicit crossing
phase onward, it also changes the language surface, grammar, semantic analysis,
and async lowering.

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

The Surge-specific lever is ownership. Seastar needs library-level wrappers such
as `foreign_ptr<>` to describe memory owned by another shard. Surge can make the
same rule a language property: `own` shard-movable values may move across
shards, while borrows and shard-pinned resources may not.

The crossing itself is also explicit. Seastar's `submit_to` is explicit by
convention and `foreign_ptr` is an unchecked wrapper. Surge checks both: the
type system rejects illegal payloads, and a distinct construct makes the legal
crossing syntactically visible. Surge makes the boundary legible, not merely
available.

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
- `runtime/native/rt_alloc.c` records alloc/free/realloc events through
  runtime or shard-owned heap-accounting cells. `rt_heap_stats()` aggregates
  those cells on read; the old global heap counters are no longer the source of
  truth.

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
Global scheduler state, global waiter scans, and cross-worker wake placement
remain the main scaling risks. Before Epic 5, global allocation counters were
also on the hot allocation path; heap accounting now uses owner-scoped cells.
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

Types that own shard-registered resources are shard-pinned. A local `File`,
socket, timer, or any value that transitively owns one may not cross a shard by
ordinary value move, even if the outer value is `own T`. Moving the registration
is migration, not message passing.

### 3. No Hot-Path Stealing

Connection tasks are shard-local. A shard may not steal another shard's
connection task just because it is idle. If work must move, the runtime treats it
as migration:

1. detach state from the old shard;
2. transfer ownership with an explicit message;
3. attach state to the new shard;
4. update fd and timer ownership.

Migration is a control-plane feature, not a request-path primitive.

Migration is also the only way to transfer a shard-registered resource. It
re-registers the fd, timer, or equivalent resource on the destination shard and
returns a local handle there. Code that wants to reference such a resource from
another shard uses a `far` handle; it does not send the local resource value
through a channel.

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

### 5. Cross-Shard Ownership Boundary

The shard boundary is move-only, but move-only is not sufficient by itself.

- `own T` may cross a shard boundary by move only if `T` is shard-movable.
- `&T` and `&mut T` may not cross a shard boundary.
- Copyable values may cross by value when the crossing rules permit it. Their
  size does not select a different ownership model.
- shard-pinned values may not cross by ordinary value move.

A borrow across shards would make the source shard's lifetime depend on another
core. That creates cross-shard lifetime tracking, cancellation, and wakeup work.
Runtime V2 avoids that class of dependency by rejecting borrowed cross-shard
payloads in semantic analysis or async lowering.

Shard-movable excludes types that transitively own shard-registered resources:
fds, sockets, timers, registered buffers, and runtime handles tied to a shard's
poll set. For example, `own File` is not enough to cross shards if `File` owns an
fd registered on shard A. Sending it to shard B would leave readiness on A and
logic on B. The compiler rejects that value move. The program must either keep a
`far File` handle or invoke explicit migration, which re-registers the fd on B.

Owned payloads have one typed-carrier representation. The destination owner
provides exact-sized, correctly aligned storage and the compiler supplies the
monomorphic move/copy/clone/drop operations for the concrete type. Inline
composites cross inline; handle-backed fields remain handles and keep their own
allocation-owner/remote-free rules. No small-value word encoding or universal
heap-pointer fallback exists. The normative storage, place, ordinary-call, and
carrier contract is
[`runtime-v2-epics/23-storage-model-and-typed-carrier-abi.md`](runtime-v2-epics/23-storage-model-and-typed-carrier-abi.md).

This is the central place where V2 should differ from a plain Seastar or
Glommio clone. The scheduler supplies shard locality, but the language supplies
the legal cross-shard ownership transfer.

### 6. Explicit Crossing

Move-only and shard-movable typing decide what may cross a shard boundary. They
do not make where a crossing happens visible. A same-shard channel send and a
cross-shard channel send must not be spelled the same way: identical syntax with
different cost is a hidden cliff, which is exactly what the cost model forbids.

Runtime V2 therefore makes crossing a distinct, visible construct.

Epic 11 selected and implemented the compile-time crossing surface for parser
and semantic analysis: `far T`, `on dst { ... }`, `spawn on dst { ... }`,
inferred crossing effects, `@shard_movable`, and `@shard_pinned`. Supported
LLVM/native lowering and Phase 4 transport verticals subsequently landed and
are indexed in `runtime-v2-epics/README.md`; unsupported forms remain
deterministic diagnostics, and Epic 23b owns the remaining erased-carrier
cutover.

**Far handles.** A capability that targets another shard has a distinct type,
written `far T`. `far Chan<T>` is a channel endpoint owned by another shard;
`far Task<T>` is a handle to a task running on another shard. A
`far` handle is produced only by an explicitly distributed operation. Local
operations on a `far` handle do not type-check, for the same reason a borrow
cannot cross: the operation would imply cross-shard lifetime or wakeup work.

**The crossing constructs.** The legal ways to make a crossing visible are:

```text
on dst { work }              // immediate placement crossing; returns TaskResult<T>
spawn on dst { work }        // placed child task; returns far Task<T>
```

`dst` is a `Placement` such as `pool`, `distributed`, `shard(id)`, or a computed
placement value, or an accepted owner-anchored `far` handle destination such as
`far Channel<T>` / the closed `far TcpConn` control-operation set. The
constructs:

- admits only `own` shard-movable values or copyable captures into `work`;
  borrowed captures and shard-pinned captures do not type-check;
- use `ret` inside the crossing block; `return` cannot escape through it;
- infer a function-level crossing effect in semantic analysis;
- lower supported forms to distinct cross-shard resume/message kinds;
  unsupported forms remain deterministic diagnostics as indexed in the roadmap.

Because every crossing is one of these constructs, every crossing is visible in
source. Reading a function body, the programmer sees each point where work
leaves the local shard before compilation.

**One construct, three crossings.** Cross-shard channel send, distributed spawn,
and Tier 2 offload are the same construct with a different `dst`. This is why
the runtime needs one cross-shard message path, not three: the surface unifies
them and the wake-fd mechanism carries all of them.

### 7. Channels

Channels have an owning shard.

A channel endpoint owned by another shard has type `far Chan<T>`. Operations on
a local `Chan<T>` are the fast path below. Operations on a `far Chan<T>` are
illegal outside the crossing construct, so a remote send is always visibly
remote.

Same-shard send/recv uses local queues:

- match a waiting receiver or sender;
- write resume state;
- push the resumed task into the run-next slot or local queue;
- avoid global condition variables.

Cross-shard send/recv sends a message to the owning shard. The owning shard
performs the queue operation and returns a completion message if needed.

Only `own` shard-movable values or copyable payloads may cross this boundary.
Borrowed and shard-pinned payloads stay local.

Bounded cross-shard send is a request/ack protocol, not a one-way enqueue:

1. the sender shard posts a send request to the channel owner;
2. the owner admits the value if capacity or a receiver is available;
3. if the channel is full, the owner parks the sender's wait token in its local
   send-wait queue;
4. when capacity opens, the owner completes the send and wakes the sender shard.

This round trip is intentionally a slow path. Normal request handlers should use
same-shard channels unless they explicitly model distributed work.

A `select` containing any `far` arm is type-visibly remote. Local `select`, where
all arms are local, is the fast path. A remote `select` is never a compile
error; it is denied the fast path and lowered to the slow coordinator. "Rejected
from hot-path lowering" means rejected from the fast lowering, not rejected from
the language. Stale completions from canceled arms are ignored by generation
tokens.

Remote `select` linearizes on the owner lane: the winner is decided on the
channel owner's shard, exactly where the owner's own local `select` would
decide it. No caller-lane ordering or fairness is promised across the shard
boundary — two callers selecting over the same channels observe an owner-lane
order, not their submission order. Timeout and default arms are evaluated on
the caller's side in every lowering.

The pre-Epic-23b implementation still contains a sync-channel compatibility
fallback. It is current-state debt, not target architecture: Epic 23b deletes
that carrier path rather than preserving it behind an adapter. Hot code uses
direct async channel operations.

### 8. Cross-Shard Wakeups

Each shard owns an inbound message queue and a wake fd in its local poll set.
On Linux this wake fd should be `eventfd`; other platforms can use the native
equivalent or a pipe fallback.

Cross-shard send uses wake elision, but the elision is a StoreLoad hazard, not a
simple flag check. A relaxed "is it parked" test loses wakeups: a producer can
enqueue, observe the consumer as still running, skip the wake, and the consumer
can then park on the message it never saw.

Each shard has an atomic park state. The protocol is:

Consumer park:

1. drain inbound queue and poll fds;
2. if no work remains, store `state = PARKED`;
3. re-check the inbound queue after the `PARKED` store;
4. if non-empty, store `state = RUNNING` and loop without sleeping;
5. if empty, call `poll()` with the wake fd in the set;
6. on wake, store `state = RUNNING`, drain the wake fd, and loop.

Producer send:

1. push the message to the target shard's inbound queue;
2. load the target shard's park state;
3. if `PARKED`, write the wake fd;
4. if `RUNNING`, skip the wake.

The enqueue publish step and the `PARKED` store must carry sequentially
consistent ordering, so producer and consumer agree on a single order. Then
either the producer sees `PARKED` and wakes the shard, or the consumer's re-check
sees the message and does not sleep. A release/acquire pair is insufficient for
this park race.

The runtime must not write the wake fd for every message. A syscall per
cross-shard send would erase much of the shared-nothing benefit. The `wake-fd
writes` counter measures elision efficiency, not correctness. A lost wakeup
shows up as a latency cliff or a hang, not in that counter. Add a debug
invariant: no `PARKED` shard may have a non-empty inbound queue at a safepoint.

The inbound transport queue is bounded. Data messages reserve target-shard byte
credits for their physical envelope, padding, exact payload, and transport-owned
sidecars before enqueue; if no credit is available, the sender task parks on
its own shard until the target returns credit. The reserved control lane carries
bounded protocol metadata only. Credit-return and cancellation metadata may use
it, but a completion or reply containing arbitrary `T` uses the data lane or a
pre-reserved reply budget. Credit returns may be coalesced; cancellation and
completion records remain distinct and generation-checked. Backpressure must
not block the bounded messages that release backpressure. Exact accounting,
jumbo reservations, and cleanup are specified by the typed-carrier design.

### 9. Structured Concurrency

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

**Distributed spawn is an explicit crossing.** A local spawn may capture borrows
of the parent; parent and child share a shard, so the borrows stay valid. A
distributed spawn is written `spawn on distributed { ... }` and is checked
move-only plus shard-movable: it may capture `own` shard-movable values or
copyable values, never `&T`, `&mut T`, or shard-pinned resources of the parent.
The construct that makes the crossing visible is also the point where the
no-borrow-across-shards and no-implicit-resource-migration rules are enforced.
Joining a distributed child returns through a `far Task<T>`, so the join is
itself a visible crossing.

Cross-shard cancellation uses generation tokens. A distributed child, scope
subscription, cancellation request, and completion message carry the generation
of the owning scope edge. If a child completes while cancellation is in flight,
or cancellation arrives after a new edge reused the same storage, the receiver
rejects stale messages by generation. The generation rule used for remote
`select` applies to distributed scopes too.

### 10. Execution Tiers

Runtime V2 separates hot shard-local work from work that should leave the shard.

- Tier 1 is the per-shard hot path: connection tasks, fd readiness, local
  timers, local channels, parser state, and normal request continuations.
- Tier 2 is the offload tier for work that should leave the connection shard:
  blocking calls, bounded CPU-heavy work, and compatibility operations.

Tier 2 has two destinations. Existing `blocking { ... }` is a dynamically sized
pool for work that blocks in syscalls; its threads may park indefinitely.
`spawn on pool { ... }` is the accepted source shape for future CPU-bound placed
work that must not block and may be stolen internally. Mixing them in one pool
lets a blocked syscall stall queued CPU work, so the boundary is explicit.

The CPU destination is a stealing pool, and stealing lives here and nowhere
else. This is the one place where work stealing is the correct tool: the work is
CPU-bound and does not care which core runs it, so balancing it by stealing
costs nothing the hot path pays. Tier 1 never steals; the CPU Tier 2 destination
always may.

Tier 2 is the lever for CPU skew. A hot shard offloads CPU-heavy work to Tier 2,
where stealing rebalances it across cores, without putting a steal on any
connection's hot path. I/O skew, such as one fat connection bound to one shard's
fd, is a different problem with a different lever: migration. Skew therefore has
two levers, not zero - Tier 2 for CPU, migration for I/O.

Entry to CPU Tier 2 is the crossing construct with a Tier 2 placement:
`spawn on pool { ... }`. OS-blocking work continues to use the existing
`blocking { ... }` expression. Placed work obeys the same move-only and
shard-movable capture rule as a shard boundary, checked in semantic analysis.
The syntax stays explicit, and one rule governs all crossing captures.

Tier 2 completion returns to the caller's shard through the same cross-shard
completion path as shard-to-shard work. Tier 2 code cannot hold borrows into
Tier 1 shard-local state.

### 11. Allocation And Heap Stats

Shared-nothing requires more than removing locks. It also requires removing
shared cache lines from the hot path.

Epic 5 completed the first allocator step: heap accounting no longer writes one
process-global counter set on every allocation and free. Current `rt_alloc`,
`rt_free`, and `rt_realloc` record events into runtime or shard-owned
accounting cells, including an explicit cold path, and `rt_heap_stats()`
aggregates those cells on read.

Runtime V2 still needs later allocator ownership work:

- hot runtime objects come from shard-local slab or bump allocators;
- connection buffers, task states, waiter nodes, and parser scratch memory are
  allocated and freed on the owning shard;
- the allocator records the owning shard in page or span metadata;
- freeing on a non-owner shard enqueues the pointer on the owner's remote-free
  queue;
- the owner drains remote frees at scheduler safepoints or before allocation;
- request-path code must not touch shared refcounts or global heap counters.

The first allocator step was not a slab allocator. Slab or bump pools come
later, after the scheduler result is measurable.

### 12. Io Boundary

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

Ambient `current_shard()` provides local capabilities only. Acquiring or acting
on a `far` handle is never ambient: it goes through the explicit crossing
construct. The ambient boundary removes local plumbing noise without hiding
cross-shard cost.

### 13. I/O Backends

The first V2 backend should be the simplest backend that proves the ownership
model:

1. shard-local `poll` or `epoll` on Linux;
2. `kqueue` for BSD/macOS if needed;
3. `io_uring` after ownership, allocation, cancellation, and buffer lifetime are
   stable.

`io_uring` can reduce syscalls and support completion-oriented I/O, but it will
not fix global scheduler ownership by itself.

## Non-Goals And Tradeoffs

Runtime V2 has a clear sweet spot. Every workload is expressible and priced;
none is unsupported.

Thread-per-core without Tier 1 stealing gives up automatic fairness in the
connection hot path. That is deliberate: Tier 1 fd ownership stays stable, CPU
skew goes through `spawn on pool`, and I/O skew goes through migration.
Migration and rebalancing remain control-plane features. The trigger policy is
still open; candidate signals are shard queue depth, byte rate, CPU budget, and
tail latency.

Cross-shard-heavy workloads are more expensive on V2, not incorrect. If a
program fans out on every request, sends through many remote channels, or
selects across many remote owners, message round trips can cost more than a
global lock would. That cost is explicit and proportional; see Cost Model And
Levers. It is paid only by the crossings the program actually writes, and every
such crossing is visible in source. V2 is optimized for shard-local workloads
like surgekv. The failure mode V2 refuses is a hidden cost, not an expensive one:
no workload is unsupported, only differently priced.

V2 also does not require `io_uring` first. A shard-local `poll` or `epoll`
backend is enough to prove the ownership model. `io_uring` belongs after the
lifetime, cancellation, and allocation contracts are stable.

## Open Decisions

Decided (2026-07-08, Epic 11 / design change D17): the crossing effect is NOT a
surface keyword. An explicit function-level `crosses` marker was prototyped and
then removed; instead the crossing effect is INFERRED at semantic analysis and
stored in function metadata, with no programmer-facing keyword or requirement.
`on dst { }`, `spawn on`, and `far Task<T>.await()`/`.cancel()` are valid in any
function. Epic 11 implements direct/intra-module sema inference for these forms
and direct calls. Higher-order/function-type propagation and possible exported
cross-module effect metadata remain tracked as `RV2-DEBT-024` if Phase 4
lowering needs them.

## Cost Model And Levers

Runtime V2 treats cost visibility as part of the language contract. Legibility
is enforced by the crossing construct, not left to documentation: a remote
operation cannot be written as a local one, so the Predictable column is a
compiler guarantee, not a convention.

The contract is: no operation is forbidden; every operation states its cost
class and the lever the programmer uses to control it. An operation is judged
Present, meaning there is always a legal way and never "not supported here";
Proportional, meaning cost scales with the crossings actually written, not as a
global tax; and Predictable, meaning the cost is legible from source. A change
that adds a fast path without a Present, Proportional, Predictable slow-path
complement reintroduces a cliff and is rejected. This table is the acceptance
criterion for the refactor.

| Operation | Performance | Placement | Predictable | Runtime lever |
| --- | --- | --- | --- | --- |
| Same-shard fd readiness | ✓ | ✓ | ✓ | FD owner resumes the task locally. |
| Same-shard channel send | ✓ | ✓ | ✓ | Channel owner and task owner are the same shard. |
| Cross-shard send (`own` shard-movable value, unbounded)¹ | ~ | ✓ | ✓ | Future remote channel operation sends an owned payload to the channel owner through the explicit crossing surface. |
| Cross-shard bounded send | ~ | ~ | ✓ | Receiver-owned request/ack models capacity and backpressure. |
| Remote `select` over `far` arms | ~ | ~ | ~² | Slow coordinator uses generation tokens and stale completion rejection. |
| Local spawn / `join_all` request tree | ✓ | ✓ | ✓ | Spawn is shard-local by default. |
| Distributed spawn / join | ~ | ✓ | ✓ | `spawn on distributed { ... }`; join via `far Task<T>`. |
| `blocking {}` syscall offload | ~ | ✓ | ✓ | Existing `blocking { ... }` pool, completion to owner. |
| CPU skew, hot shard | ~ | ✓ | ✓ | `spawn on pool { ... }`; Tier 2 steals internally once lowered. |
| I/O skew, fat connection | ~ | ~ | ~ | Migration control plane; trigger heuristic open. |
| Shard-pinned resource transfer | ~ | ~ | ✓ | Explicit migration re-registers the fd, timer, or equivalent resource on the destination shard. |
| Cross-shard free, `own` shard-movable value dropped remotely | ✓ | ✓ | ✓ | Allocator routes free to owner; move makes it visible. |

Every row is Present: no operation is unsupported. The columns rank cost, not
availability.

¹ Guaranteed predictable: cross-shard send uses the explicit crossing
construct, so it is syntactically distinct from same-shard send at the call
site.

² Remote `select` is only partially predictable because the surface exposes that
it is remote, but the runtime cost depends on arm count, owner distribution, and
cancellation traffic.

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
| Heap allocation ownership | Heap accounting uses runtime/shard-owned cells, but allocation still uses the current `malloc`/`free` strategy. | Shard-local hot object pools and owner-routed frees. |
| Cross-shard value lifetime | A value can be used and dropped away from its allocation owner. | Only `own` shard-movable values cross shards; the allocator routes non-owner frees to the owning shard. |
| Shard-pinned resource move | `own File` could move to another shard while its fd remains registered on the old shard. | Types that transitively own shard-registered resources are shard-pinned; ordinary sends are rejected and explicit migration re-registers the resource. |
| Invisible cross-shard cost | A remote operation could be spelled like a local one. | Crossing is a distinct typed construct (`far` + `on` / `spawn on`); cost is legible before compile. |
| Lost cross-shard wakeup | A producer can enqueue, see the target as running, skip the wake, and race with target parking. | Wake elision uses the seq-cst park protocol: `PARKED` store, queue re-check, and debug invariant. |
| Unbounded inbound transport | Cross-shard messages can grow memory without backpressure. | Inbound transport is bounded by target credits with a reserved control lane for release messages. |
| Remote bounded channels | A bounded send across shards needs receiver-side capacity state. | The receiver shard owns capacity and completes sends through request/ack. |
| Remote select | Selecting over remote channels creates multi-shard subscriptions and cancellation. | Local select is fast; remote select is denied the fast path and lowered to a slow coordinator. |
| Cross-shard cancellation staleness | A child can complete while cancel is in flight, or storage can be reused. | Distributed scope cancel and completion messages carry generation tokens; stale messages are ignored. |
| Load skew | Work stealing can hide skew but conflicts with fd ownership on the connection path. | CPU skew uses Tier 2 stealing; I/O skew uses migration and control-plane rebalancing. |
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

Current note: Phase 4 crossed both workstreams. Parser/sema, LLVM/native
lowering, shard messaging, remote tasks/channels/select, migration, crossing
drop activation, and the owner-routed reclamation core now exist; current
status is indexed in `runtime-v2-epics/README.md`. The next atomic cutover is
Epic 23b's inline storage and typed-carrier ABI. Compiler and runtime still
close together: neither half is accepted while the other retains an erased
payload fallback.

### Phase 0: Contract And Structure

Implementation note, 2026-06-26: this design phase is broader than the first
implementation epic. Epic 1 records the contracts, rules, evidence shape, and
known debt. Epic 2 implements only the `N=1` `rt_runtime`/`rt_shard`
structure. The accepted crossing syntax and sema checks (`far`, `on`,
`spawn on`, shard-movable enforcement) are later Epic 11 work relative to that
initial structure pass; cross-shard lowering remains after the compile-time
surface.

- Define scheduler semantics that are part of the language contract.
- Define the shard boundary rule: only `own` shard-movable values may cross
  shards; borrows and shard-pinned resources may not.
- Define the crossing surface as a language contract: the `far` handle type,
  `on dst { ... }`, `spawn on dst { ... }`, inferred crossing effects, and the
  move-only capture rule. This requires a spec draft update, not only a runtime
  change.
- Define shard-movable versus shard-pinned types. Types that transitively own
  shard-registered resources require `far` handles or explicit migration.
- Add V2-shaped shard structs with `N=1`.
- Move fields from `rt_executor` into `rt_runtime` and `rt_shard` without
  changing behavior.
- Keep the then-current behavior while the initial shard structure is moved.
  This was a Phase 0 staging constraint, not a compatibility promise. The
  pre-0.2 runtime API/ABI and generated objects are explicitly not preserved by
  the later typed-carrier cutover: live in-tree callers migrate atomically and
  obsolete entrypoints are deleted without wrappers or dual paths.

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

### Phase 2.5: Heap Accounting Cells

- Completed by Epic 5: allocation/free/realloc accounting writes through
  runtime or shard-owned cells, including cold accounting.
- The underlying `malloc`, `free`, `realloc`, and aligned-allocation behavior
  stayed unchanged.
- Heap stats aggregate only when requested.
- Phase 3 can measure scheduler sharding without the old global heap-counter
  source of truth on the allocation path.

### Phase 3: N>1 Accept Ownership

- Completed by Epic 6 for the native TCP accept/readiness path under the
  preserved global executor lock.
- Multiple runtime shards are enabled through bounded runtime configuration:
  `RT_RUNTIME_MAX_SHARDS` plus runtime `shard_count`.
- Linux uses per-shard `SO_REUSEPORT` listener groups where available.
- Accepted connections stay on the accepting shard, and their fd registry,
  waiters, readiness polling, close, cancellation cleanup, and shutdown wake
  use the owner shard's net state.
- Tier 1 connection tasks are placed on the owner shard and are not stolen by
  non-owner shards.
- Phase 3 does not claim lock-level throughput scaling: `rt_executor.lock`
  still protects the remaining global executor state.
- Epic 7 then split that lock: scheduler queues, worker sleep/wake, waiter
  stores, sleep timers (atomic virtual clock + per-shard sorted stores), and
  channel ownership run on per-shard lanes under `rt_shard.lock`, with a
  reduced control lane for task lifecycle, scopes, select, shutdown, and the
  sync-channel compatibility wait. Lane order (control -> at most one shard
  lock) is asserted at runtime; `runtime-v2-lock-check` gates eight code-shape
  properties and nine cross-shard behavior modes in CI. Task lifecycle is the
  remaining steady-path control consumer (`RV2-DEBT-016`) and is the Epic 8
  entry point.
- Epic 8 then moved task lifecycle and same-owner scope bookkeeping off the
  control lane: task create/publish uses a segmented never-moved-slot task
  table on the owner shard lane, worker join polling and normal done completion
  are owner-shard-local, and scope enter/register/join-all/exit plus the
  `scope_key` waiter store run on the scope's pinned owner shard. `done_cv` /
  `compat_cv` stay external/main-thread-await compatibility only and are counted
  separately. Control-lane steady-state dropped from ~26.4 to ~9.36
  acquisitions per request on the 8-shard/1024 row, and the 8-shard/1024 total
  is at least as fast as the 1-shard row; a per-commit trace-counter gate
  (`TestRuntimeV2PerfControlLaneGate`, `runtime-v2-perf-check`) guards it
  (`RV2-DEBT-016` closed). The 8-shard/1024 net starvation (`RV2-DEBT-015`) was
  a placement funnel — stdlib net wrapper tasks pinned every durable task to
  shard 0 — fixed by join-consume placement adoption (F2,
  `rt_task_poll_adopt_placement`): a joiner consuming a DONE connection-placed
  child adopts its placement, so the durable serve pipeline follows the
  accepting shard. Residual control on the net bench is external-await compat
  and the cross-owner scope fallback, both reassigned to their owners; no
  syntax, parser, or Phase-4 crossing work was done.
- Epic 9 then closed the local safety debts that Epic 8 carried before Phase 4:
  cancellation racing `RUNNING -> WAITING` park now sets an unconditional owner
  wake token (`RV2-DEBT-023`), owner replacement publishes and revalidates an
  atomic join-owner route before migrating join waiters (`RV2-DEBT-020`), and
  external await/completion use a seq-cst StoreLoad handshake plus a guarded
  post-`DONE` `done_cv` broadcast helper (`RV2-DEBT-022`). These changes are
  runtime-only and proof-first: deterministic test-only sync points provide
  positive and negative controls, while Phase 4 messaging, eventfd credits,
  remote `select`, shard-movable checks, and syntax remain unimplemented.
- Epic 10 then closed the owner-safety cleanup needed before any explicit
  crossing surface: native TCP public handles are stable runtime handle ids,
  not OS fds or pointers, and every net entrypoint canonicalizes copied
  `TcpConn`/`TcpListener` handles before touching fd, owner, closed, or
  generation fields. Stale copied handles are removed from the runtime handle
  table on close and fail with `NET_ERR_NOT_CONNECTED`; the fd registry
  generation check remains the owner-locked lifetime proof for live canonical
  handles. `stdlib/http::serve` no longer launders raw `TcpConn.__opaque`
  values through `Channel<int>` workers; fixed accept workers handle accepted
  connections owner-locally and the Runtime V2 HTTP owner gate covers
  `SURGE_SHARDS=1,2,8`. This is still Phase 3 owner-safety, not Phase 4
  cross-shard messaging or resource migration.
- At this Phase 3 checkpoint, cross-shard messaging, crossing lowering,
  remote-free routing, and alternate I/O backends were later phases. The live
  production status is now indexed below and in
  `runtime-v2-epics/README.md`; this dated checkpoint is not a current-state
  claim.

### Phase 4: Cross-Shard Messaging And Shard-Movable Values

Current status is maintained in `runtime-v2-epics/README.md`. The production
LLVM/native vertical includes the inbound transport spine, placement task
crossings, remote channels, far-handle sharing, remote `select`, migration,
crossing drop activation, and the owner-routed reclamation core. The remaining
one-word payload ABI and placeholder credit model are replaced end to end by
Epic 23b, `runtime-v2-epics/23b-inline-storage-and-typed-carriers.md`; it owns
typed task/channel/select/blocking/far payloads and physical byte credits.
Distributed scopes, `pool` execution, and any VM transport not expressly
implemented by an active epic remain future work with deterministic
diagnostics.

- Add per-shard inbound queues and wake fds.
- Signal a target shard according to the PARKED-state wake protocol, not by a
  relaxed empty-to-non-empty queue check.
- Add explicit messages for cross-shard channel operations, cancellation,
  distributed scopes, and controlled migration.
- Enforce move-only and shard-movable boundaries for payloads.
- Use the Epic 11 syntax surface rather than reopening keyword choice:
  `far T`, `on dst { ... }`, `spawn on dst { ... }`, inferred crossing effects,
  `@shard_movable`, and `@shard_pinned`.
- Complete backend/lowering integration: async lowering emits cross-shard resume
  kinds for `on` and `spawn on`, and unavailable backends keep deterministic
  diagnostics until transport is enabled.
- Split Tier 2 lowering destinations according to accepted placement values:
  existing `blocking { ... }` for syscall-blocking work and `spawn on pool`
  for CPU-bound placed work with internal stealing.
- Implement the wake-elision park protocol with sequentially consistent
  enqueue/PARKED ordering and the PARKED-with-non-empty-queue debug invariant.
- Implement bounded inbound transport queues with physical target byte credits
  and a reserved control lane for bounded credit-return/cancellation/progress
  metadata. Arbitrary typed completion/reply payloads use credited data storage.
- Implement bounded remote send as receiver-owned request/ack.
- Lower remote `select` to a slow coordinator with generation-based
  cancellation; remote `select` is not a compile error.
- Apply generation tokens to cross-shard distributed-scope cancel and completion
  messages.
- Keep spawn local by default.

### Phase 5: Shard-Aware Allocation And Hot Pools

- Add owner-shard metadata to allocator pages or spans.
- Route non-owner frees to the owner's remote-free queue.
- Drain remote frees at scheduler safepoints or before allocation.
- Add shard-local pools for task state, waiters, connection buffers, and parser
  scratch memory.

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
- trace counters for cross-shard messages, wake-fd writes, inbound transport
  credit stalls, remote-free queue depth, remote bounded-channel round trips,
  remote `select` uses, stale generation drops, global path usage, shard
  imbalance, local queue depth, allocation counters, and fd readiness batches.

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
- Seastar `foreign_ptr` implementation:
  https://github.com/scylladb/seastar/blob/master/include/seastar/core/foreign_ptr.hh
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
- Linux `eventfd` manual:
  https://man7.org/linux/man-pages/man2/eventfd.2.html
- Linux `SO_REUSEPORT` socket option:
  https://man7.org/linux/man-pages/man7/socket.7.html
