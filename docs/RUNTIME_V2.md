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
- A Tier 1 shard has one carrier. Its ready queue is shard-local, not a
  worker-private deque shared with peer workers.
- The target connection hot path has no work stealing. Stealing belongs to the
  CPU Tier 2 destination and, if needed, emergency or control-plane paths.
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
- `runtime/native/rt_async_state.c` stored channel, join, timer, scope,
  blocking, and net waiters in one FIFO list, and `pop_waiter()` scanned and
  compacted the whole list for one key. Both are gone as of Wave D step D0:
  waiters live in per-shard stores reached through `rt_waiter_store_for_key`,
  and `pop_waiter` was deleted once nothing called it. The entry recording the
  baseline is kept because the shard split below is the answer to it.
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

Runtime V2 consists of `N` shards. Each Tier 1 shard maps to one carrier OS
thread and one CPU core. A shard owns:

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

`local` in Tier 1 means local to the shard. The carrier may use a run-next slot
or a LIFO queue for locality, but there is no second Tier 1 worker that can
sleep on, steal from, or be signalled for that queue. A worker-private deque and
peer stealing belong to the CPU Tier 2 pool or to the explicitly transitional
topology below.

A run-next slot holds no more than one task and belongs to a single carrier. No
peer may read it, steal from it, or be signalled for it, and in a multi-carrier
group (Section 10) it is the only publication a carrier may make without a wake
credit. A shard with one carrier owns exactly one such slot; in the transitional
topology below each carrier of the shard owns its own.

The transitional `1 shard x N carriers` topology is an exception, not a second
definition of a shard. It remains Tier 1: its tasks stay on the shard and keep
their fd, timer, and channel ownership. It may use intra-shard peer scheduling
while the runtime migrates to `N shards x 1 carrier`, but it must obey the
multi-carrier wake contract in Section 10. Its results do not establish
thread-per-core Tier 1 scaling.

### Shard Ownership With Several Carriers

A shard owns the tables listed above, and ownership names a serializer rather
than a thread: each item is read and written under the shard's lane, which a
thread borrows rather than owns. Where the transitional topology turns one
owner into several threads, that list is refined item by item, not wholesale,
as the run-next slot above is refined to the carrier and `PARKED` to the group
in Section 8. Every cell names its owner -- a carrier, a lane, or the runtime;
a cell that names none is refused; a lane-owned cell several carriers write
under that lane stays single-writer in the only sense that matters; and cells
of different owners are padded apart, asserted at compile time. The threads
that serve a shard are named by the topology and recorded with it, in two
roles never merged: only a carrier executes task turns and owns private work,
and a service agent is a serving thread that is not a carrier of the shard,
driving its transport, readiness, and timers, polling no task, and touching no
carrier's private deque or run-next slot. Serving is not executing, so
eligibility class (Section 10) and carrier affinity (Section 9) survive every
removal, and serving turns external readiness into publication: what is ready,
or past its deadline, reaches the eligible class's queue with an eligible
credit and a notification within a normative budget of 1 ms, measured from the
earlier of the deadline's expiry and the readiness the shard's poll could have
observed, never from when a thread happened to look. The budget binds the
function, not the role: it binds whichever thread serves a shard with no
service agent, and a waiting service arms its wait no later than the shard's
nearest deadline. The budget is the service's, not a promise that the task
runs within 1 ms. While a group is open the set of threads serving each of its
shards is non-empty, and making a task runnable is publication under Section
10 unless a continuity obligation covers it: an eligible credit and a
notification. Outstanding work has three classes, and non-emptiness names none
of them. A runnable obligation is work a carrier could execute now: unless a
continuity obligation covers it, it owes an eligible credit and a
notification, and no thread eligible for it may wait while that credit is
unclaimed; the one covered case is a task reserved in its own carrier's
run-next slot, which binds that carrier alone and no peer. A service
obligation -- an armed deadline, a registered readiness interest -- owes a
live service for its shard, not a carrier credit, until readiness turns it
into a runnable obligation. A maintenance obligation -- a remote-release queue
-- keeps no shard active, creates no credit, and owes only its bound and its
fallback. Every structure that can hold work names its class -- the
subsections below, and equally the fd registry and deadline index of Section
4, the ready queue, run-next slot and public queue of Section 8, and the class
queues of Section 10 -- and one that can hold work and names no class is
refused. Each subsection states what the target `N shards x 1 carrier`
topology still pays, and no rule is priced at zero there until it names the
entrant that topology eliminates and the mechanism eliminating it.

**Timers.** A shard has one timer structure: the deadline index and the timer
waiters of Section 4. Every thread serving the shard arms, cancels, and pops
from it under the shard's lane, and a due sleeper is popped by whichever
serving thread reaches it first; no carrier owns a subset of the shard's
deadlines. A deadline is a reading of the clock in force plus the requested
duration, taken on the shard that owns the timer, and it is popped on that
shard; another shard's minimum is readable only as a published hint. The clock
in force never runs backward, and no runtime advances its own clock because it
found nothing to run: an idle jump is a contract failure under either clock,
not a scheduling liberty. Wall time is what a program gets unless it asks
otherwise, and wall time never outruns the wall -- a millisecond nobody waited
is not payable, so a runtime with nothing to run waits for a deadline it
cannot yet serve. A virtual clock is a test facility, reached only by an
explicit selection at the run's boundary; selecting it changes how much wall
time a deadline costs and nothing else, since the order deadlines fire in, the
batch rule below, and the guarantee that a task resumes no earlier than its
deadline in the clock in force are the same under either. The runtime does not
keep this today, and the selection runs the other way: `surge run --real-time`
opts INTO wall time on the VM and is refused for any other backend, so virtual
is the default where a clock can be chosen and the only clock where it cannot.
The native clock is advanced by idleness -- topped up on every yield tick, and
jumped straight to the next deadline when no readiness wait holds it back --
which is how a sixty-second timeout came to be served in a tenth of a second.
`RV2-DEBT-180` carries that gap, and closing it is what this paragraph asks
for. Arming is a publication: a thread that arms a
deadline becoming the shard's new minimum publishes a group notification
before leaving the arming critical section, and a thread entering a wait reads
that minimum under the same indivisible protocol that governs its wait
transition; an arm that does not lower the minimum owes nothing. An armed
deadline is a service obligation, not a runnable one: while only armed it owes
no carrier credit, and firing is what makes it runnable. It is discharged by
service, so at a safepoint, for every shard of an open group holding one, at
least one thread serving that shard -- carrier or service agent -- must be one
that reaches the index again without waiting for an event other than that
deadline or an owed notification. A batch of `k` due sleepers publishes `k`
eligible credits and a notification; a firing carrier eligible for the batch's
earliest deadline may elide that one member into its free run-next slot and
owe `k - 1`, a carrier-affine sleeper may be elided only by its own carrier,
and a thread with no free run-next slot owes all `k` -- a service agent, which
has no slot at all, and a carrier whose slot is already reserved, answer the
same way. The batch is served in nondecreasing deadline order by the thread
that pops it, and published so a peer observes that order at selection. A
closing group fires or cancels every armed deadline before the last thread
serving its shards leaves. In the target topology the shard's one carrier is
its only serving and only eligible thread, so no credit, notification, or arm
notification arises.

**Readiness and the fd registry.** A shard's fd registry and its readiness wait
are one resource. At most one thread is inside that wait: it enters by claiming
the shard's poll lease and leaves by releasing it, and the lease is claimed,
released, and observed under the lane guarding the registry rows. Every reader
reads the registry under that lane -- snapshot, has-waiters predicate, and idle
sample alike -- and a refused claim is arbitration, not a fault. An open
registered interest is a service obligation: it owes no carrier credit until
the fd is ready, and coverage is owed by the thread that stops looking. A
carrier enters the group wait only when the shard holds no such interest, or
the lease is held by a thread -- carrier or service agent -- whose readiness
wait a group notification can end; a carrier seeing an open interest and a free
lease claims it instead, that observation and its transition to waiting
linearized under that lane, a held lease being the only observable coverage of
that class. A lease holder covers its shard's readiness class and no other, and
releasing it is a safepoint. For a carrier, entering the wait is leaving the
turn, so it first discharges its continuity obligation, running rather than
leaving behind a task whose affinity forbids transfer. The wait runs under the
bounded, traced budget of Section 8, and a holder that neither completes
readiness nor releases the lease within it is a contract failure, not a slow
poll. Deliverability, not membership, is the rule, and the readiness wait has
one wake source, not one per mechanism: every notification able to reach a
thread inside it -- readiness, an eligible credit, an inbound record, shutdown
-- is delivered by writing the shard's existing wake pipe, in the lane section
publishing the credit, whenever a thread may be inside that wait, tested under
the same indivisible protocol that governs the wait transition; a publisher
that observes no such thread may elide the write. The byte only interrupts the
poll: the durable credit and the indivisible no-work gate are what make the
wakeup correct. No peer publishes `PARKED` for a shard whose
lease is held, and the coverage obligation names every thread admitted to claim
the lease, not the carriers alone. In the target topology the lease is
uncontended, its owner field and budget accounting debug-only, and the credit
obligation gone; the lane rule remains, so a whole-runtime readiness question
costs one shard lock per shard per sample and belongs to the control plane.

**Channel wait queues.** A channel is serialized by the lane of its owning
shard, and that lane is not a carrier: whatever thread performs the operation
borrows it, so "same-shard rendezvous" means same owner lane, not same line of
execution. Two lanes meet in an operation: the channel's storage -- buffer,
park slots, and the registrations on its two keys -- answers to the owner's
lane, a task's resume mailbox to that task's own owner lane, and only the token
crosses. An operation is a sequence of critical sections: no generated move,
copy, or drop runs under the owner lock, so an operation that leaves the lane
first converts what it decided into explicit claims -- the storage it will
write, the registration it consumed, the admission test that authorized the
transfer, and the object itself, pinned for the released window -- and re-tests
at the commit every predicate it did not convert. A claim is bounded by its
operation, not by a turn, so no carrier's safepoint says anything about claims
outstanding on the channels its shard owns. A claim is refused, never queued;
the refused operation returns to selection as a publication like any other,
counting against the public queue's service bound. The retry budget names its
exhaustion: the operation parks on the channel's own key, the release of the
claim that refused it wakes that key, and shutdown terminates the retry rather
than let it re-enter selection. A local channel linearizes on its owner lane: a
value takes its position when that lane commits it into the channel's storage,
and a rendezvous is admissible only while the buffer holds nothing and nothing
is on its way in, tested at the commit. A send that loses that test has taken
no position: it joins the buffer if the buffer will take it, otherwise wakes
the receiver it popped, unacked, and parks holding its slot; destroying the
value is never a legal recovery. The target topology removes the peer-carrier
entrant and admits a task of another shard, and the last handle drop is an
entrant of both, so this is not free there.

**Scope accounting.** A scope's accounting state -- live-child count, fail-fast
flag with the child that raised it, child list -- is owned by the scope's
owning shard; "local" means local to that shard, not same-thread. One
mechanism, designated once for the runtime, serializes every read and write of
it; a read or write outside it, or under another lock than its writers hold, is
a contract failure, a read that only decides whether to enter included. A join
reads every answer from one indivisible observation of that state, at its first
test and again at the verify after registering on the scope's wait key;
registration is published before the verify reads, and register-then-verify is
mandatory wherever a drain is waited for, teardown included. A task's committed
kind is sealed by the single read-modify-write of its cancellation gate at its
commit and never re-derived by accounting; the completion retires the child
and, when that kind is a cancellation in a fail-fast scope, raises the flag
indivisibly against any join, so no observer sees a scope drained and not
fail-fast when the draining child is the one that raised it. Only a member
raises fail-fast, and the raiser cancels every member still live before
returning to selection. Membership is decided at the child's creation, by one
writer, under the serializer, and never re-derived; a second writer of a
child's scope identity, on any lane, is a contract failure. No later operation
adopts a live task: scheduling an already created task runs it under the scope
that created it and enrols it nowhere.

**Owner ruling 2026-08-29 -- the fact a checker reads is a write-once
`creation_scope_key` on the task.** A task records, at creation, the scope that
created it or `NO_SCOPE`, and that word is never written again. The refusal this
section requires compares that key against the current scope; it does NOT
reconstruct a task's origin from its parent, its waiters or its placement, and
two attempts to write it without the key were wrong in both directions -- they
refused a legal program and missed four spellings of the one they exist to
refuse, three of which produced different runtime answers. The key is write-once
provenance and therefore survives a quiescent repin of the owner lane. It is
RUNTIME state, not a public API: no program is given a scope id, and nothing in
the language surface grows to carry one. A scope belongs to its owner lane for
its whole life and never migrates silently; the one transfer is a quiescent
repin, admissible only under the serializer with no member live, no completion
outstanding for a member, and no registration outstanding on the scope's wait
key, re-registering every waiter on the destination lane before it publishes
the new owner; a repin missing one of these is a contract failure, not a slow
path. Publication and count are one critical section
where child and scope share a shard and cannot be where they do not, since a
carrier holds at most one shard's lock: there the count joins through the scope
subscription on the owning edge's generation, in order, never to a count
excluding it. Nothing the serializer protects is dereferenced after release,
and a scope's wait key resolves to its store from the key alone. Draining
discharges two obligations, neither substituting for the other: the draining
completion finds the joiner's registration or the joiner's verify sees the
drained count, and the carrier, after leaving the serializer, publishes an
eligible credit for the joiner's class and notifies; the serializer is never
held across a wake, a park, a cancel walk, or generated code. An exiting
carrier completes the accounting step of every child it runs or cancels, and an
undrainable scope is recorded at shutdown with its id, count, and last live
child. The cost is nothing where scope, owner, and children share one shard
with one carrier; with several carriers a registration merges into the critical
section publishing the child, one acquisition instead of two; elsewhere it is a
message. No rule here licenses a steady completion path reaching a scope on
another shard through the process-wide control lane.

**Owner ruling 2026-08-31 -- a task's owning shard is decided at its creation
and is never re-derived on the request path.** The rule above is stated of the
scope; it holds of the shard in the same words. A task's owning shard is
decided at the task's creation, by one writer, before the task is published to
any run queue and to any waiter store, and it is not re-derived afterwards; a
second writer of a task's owning shard on the request path is a contract
failure, not a slow path. The one transfer is the quiet repin, and it is driven
from the control-plane lane. A task is quiet when it is `WAITING`, is not
queued, has no wake in flight, and is named by no entry of any waiter store; a
`RUNNING` task is never quiet, and therefore a running task never repins
itself. Because the repin moves the serializer under which the task is decided,
it runs holding the old and the new shard's locks at once, acquired in
ascending `shard_id` order; every wait key is re-registered on the destination
BEFORE the new owner is published, and a wake follows only AFTER that
publication. A holder of an already popped reference to the task revalidates
the task's wait key under the serializer before acting on the reference: an
empty store does not mean no references outstanding. While a legal repin
remains in the tree, every path that selects a lock by the owning shard
re-reads that shard under the lock it took; where ownership is immutable after
creation, that revalidation is discharged by the immutability instead. Which of
the two holds is stated in the code at the site, not left to the reader.

**Heap cells and allocation pools.** Heap accounting is per lane, and a lane
is not always a carrier: the lanes are the carriers, the main lane, the I/O
lane, each blocking-pool and compensation worker, and one process-wide cold
lane for threads that own no cell. A carrier, blocking, or compensation lane
has one writer, which reads and writes it with no synchronization on the hot
path; main and cold are multi-writer by construction. "Owner" here is the
lane, not the shard, so a pool, free list, or cell more than one carrier
reaches on the allocation path is shared state. The padding rule above binds
lane cells, pool heads, and free-list heads, and debug instrumentation may not
change which cells share a line; it binds the target topology too, which also
indexes one cell array by carrier, so this subsection is not free there
either. A release issued away from the allocation owner is a remote release
even inside one shard, since Section 10 lets that topology steal within it.
The rule is written over release, not `free`: growing a block in place is a
release of the old block, so a reallocation issued away from the owner
allocates on the issuing lane, copies, and remotely releases; only the owner
grows in place. The allocator records the owning lane in page or span
metadata, and a remote release is enqueued on the owner's remote-release
queue, the only structure here two lanes may write on the ordinary path -- the
fallback at the bound below is the one exception, and it is cold by
construction; cross-shard release is one case of this rule, not a second. A
non-empty remote-release queue is not runnable work: it creates no credit, no
coverage obligation, and does not make its owner's wait illegal. It is bounded
all the same, and what happens at the bound is fixed rather than policy:
memory is never a reason to wait and never a reason to wake an owner, so a
release that finds the queue full takes a cold, ownership-neutral fallback
instead of blocking or signalling. Depth, maximum depth, drain age and
fallback releases are traced; the owner drains the queue at a safepoint and
before allocating. A span is carved by its owner into objects of one size
class; only a span holding no live object returns to the process allocator, so
releasing one object from a lane that does not own it can never dissolve a
span whose other objects are still live. An owner that leaves hands its pools
and queue to a survivor, or -- when no span it owns holds a live object --
drains them and returns the memory, before releasing its lane cell; a group
closes only when no owner tag names an exited lane. A summed snapshot is not a
global cut: a per-lane difference is not a live-block count, and a difference
of two sums is an exact allocation budget only over a window in which no other
lane allocated, so a gate comparing an allocation delta against an exact
number names the lanes it required quiet and measures it.

**Owner ruling 2026-08-29 -- an allocation budget has TWO phases and they are
measured separately.** `initialization_budget` runs from the cold state to the
end of the first working batch. `steady_structural_budget` runs over the
batches after an explicitly named warmup or setup, and only those. Averaging the
expensive first batch into the rest is forbidden, and so is hiding it silently
inside a warmup: a row whose first batch costs more than its later ones has two
numbers, not one number and a rounding. Each window takes its snapshots on a
NAMED quiet lane set and ends only after the drain or cleanup that window
requires. A row that reads one figure in warmup and another in every measured
pair is not a budget that needs re-pinning -- it is a row that has not yet said
which of its two budgets each figure belongs to. Counters are
reported per lane and in total, every reported figure names the lane set it
covers, and reading another lane's counters stays off the request path.

**Traces and counters.** The trace line is refined per counter, not wholesale.
A record of a carrier's own action -- a wake, an elision, a steal, a refused
steal, a wait, a local streak -- is carrier-owned and carries the carrier
index beside the shard id. A record of shard-owned state -- inbound queue
length, park state, fd registry rows, public-queue age -- is lane-owned,
written under the lane, and carries the shard id alone. Public-queue age
belongs to the queue, not a carrier, so a queue no carrier polls reports a
rising age instead of no sample; a carrier owns what it did, its local
selections since its last public poll and its crossings of the bound. A
carrier has no lock and this section creates none: carrier cells are published
release and read acquire, and lane-owned cells are read under their lane, one
at a time, by a reader holding no other. A carrier publishing into another
group's state counts it on its own cells while the receiver counts the
acceptance on its own lane, so a legal cross-group publication is never an
ownership violation. A number the runtime reads in order to decide -- an
admission count, a wake credit, a wait-predicate input -- is runtime state,
always present, not a trace: its counters report events, its outstanding value
is read from the state itself, never summed from events. Every elision site
carries its own owned counter. Each record is emitted by one `write` whose
result is checked; a short or refused write is counted as a dropped record,
not discarded; a per-owner breakdown is one record per owner, so no record
grows with the carrier count. Being always present binds the synchronization
too: a cell whose atomicity, publication, or ownership is armed by a reporting
switch is a contract failure, because the build that reports nothing has to
decide what the reporting build decides. Reporting that only reports is
switchable. A debug invariant is neither of those two: it is armed in the
gated builds -- the `runtime-v2-*` gates and the sanitizer lanes -- and may be
absent from a release build, while any state it reads that a runtime decision
also reads stays always present whether or not the invariant is armed. A
reported counter is admissible evidence when its owner is named: carrier-owned
per carrier, lane-owned per shard naming the lane. A row may not publish a
number whose writers share neither a lock nor an owner; it names the reporting
mode it ran in and is compared only within that mode. Both refinements
collapse in the target topology, where the lane has one carrier.

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

An accept socket per shard is half of the mechanism. The other half is that the
acceptor is a task on that same shard, waiting on that same shard's socket. A
value naming the whole listener group is shard-pinned as a whole and is not
handed out to acceptors sitting on different shards; the shard-local acceptor
`i` takes member `i` of the group and waits on member `i` only. A reference to
another shard's member is `far`.

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

Both symmetric forms of it are forbidden on the accept path: moving the *task*
to the shard that owns the newly accepted fd on every accept, and moving that
connection's *fd registration* to the acceptor's shard on every accept. The
first is migration of a task, the second is migration of a resource under the
paragraph below; both sit on the request path, so neither is licensed by the
other's absence. A handler is co-located with its connection by creating the
handler on the connection's shard, not by moving either of them afterwards.

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

A same-shard handoff is still scheduler publication. In a multi-carrier worker
group it follows the Section 10 wake contract: only a non-stealable run-next
handoff may use continuity elision; publication into a public or stealable queue
must issue an eligible worker-group credit.

Section 1 says where a value takes its position: the owner lane commits it into
the channel's storage. What that position implies for a program is an order,
and the order is the channel's promise. A direct handoff does not overtake a
write already committed into the ring, and it does not overtake a sender the
lane let in before it -- both a sender whose value has already taken its
position and one that only queued on the channel's send key, so the order
parked senders are refilled in is part of the promise rather than a scheduling
artifact. It is an order over commits, not over the moments sends began: two
sends racing to start may commit in either order, and once one has committed
nothing later passes it.

The ring half of this holds today: both send paths gate the direct handoff on
the buffer being empty with nothing on its way in, so a committed write cannot
be passed. The sender half does not. `RV2-DEBT-298` records that the owner lane
is released across the element move, so two senders admitted in order can
commit in the other, and that a refill may serve a later candidate than the one
that queued first -- the runtime orders admission, and not always that.

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

**Channel lifetime.** `Channel<T>` is a copyable handle at the language surface
and stays one. The runtime object behind it is shared and deterministically
reference counted: copying a handle retains, dropping a copy releases, and the
last release destroys the object. Destruction drops every payload the channel
still owns, so a channel is not a place values go to be forgotten.

`Copy` does not mean "memcpy and never drop" under the typed-carrier model. A
descriptor separates `copy_init` from `drop_in_place`, so a handle may stay one
machine word while its copy and drop carry work.

Two reference kinds are distinguished. User handle references are the copies a
program holds. Internal pins are held by a registered waiter, a select
subscription, or a claimed detached operation. Both keep the object alive, which
is what makes it impossible to free a channel while a generated callback is
still running outside the owner lock.

**`close` is not `destroy`.** Closing forbids further sends, settles parked
senders, and wakes receivers. It does NOT discard values already published into
the buffer: `recv` yields `nothing` only when the channel is closed AND empty,
so a closed channel is still drained by its receivers. Destruction is what the
last release performs, and only there do the remaining initialized payloads get
dropped. Teardown order is: under the owner lock mark the object dying, detach
its initialized slots, invalidate generations; release the lock; run
`drop_in_place` on the detached values; free the storage. No destructor and no
user operation runs under the channel lock.

**The scheduler mailbox carries control only.** A resumed task learns that a
value is ready for it and which generation the slot belongs to; the value itself
never travels as a scheduler word. Every payload lives in typed storage: a
sending task stages into its own slot, the channel owns ring slots and one
rendezvous slot, a receiving task owns a typed resume slot, and a `select`
operation owns its own staging slot. This is the same rule as the typed-carrier
paragraph in the ownership section above, stated for the one path that most
invites an exception - the direct same-shard handoff.

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

**Scope.** In the target topology, `PARKED` is the state of the one Tier 1 shard
transport loop. It is not the state of a worker pool and it does not describe a
private worker deque. A Tier 1 shard then has one carrier, so a same-shard local
continuation has no sleeping peer to wake.

The transitional `1 shard x N carriers` topology needs a different reading:
`PARKED` may be published only after every carrier in the shard's worker group
has completed the group no-work handshake (Section 10). One sleeping carrier may
not mark the whole shard `PARKED`. Every transport delivery into such a shard is
an external producer for its worker group: after publishing the inbound record
it must issue an eligible group credit, whether the aggregate transport state
reads `RUNNING` or `PARKED`. The wake fd remains the cross-shard transport edge;
the group credit and group notification make the accepted record runnable for a
carrier. This prevents a busy carrier from making a non-empty inbound queue look
served while all peer carriers remain asleep.

Two rules keep the aggregate state honest. A carrier that leaves the group wait
publishes `RUNNING` for the shard before it consumes any inbound record, so the
state is never `PARKED` while a carrier is serving the queue, and the
`PARKED`-with-inbound invariant is evaluated at a safepoint, not mid-turn.
And a carrier may wait inside the readiness poll as well as in the group wait:
both are wait states of the same group, so a group notification must be
deliverable to whichever one a carrier is in. For the readiness wait that
source is not a second primitive: it is the shard's existing wake fd, written
by the publisher as Section 1 requires. The group wait keeps its own
notification mechanism, and merging the two is later work. The readiness poll
observes that source alongside readiness, and it does so under a
bounded, traced budget; a poll that cannot be woken by a group notification is
not a legal wait state for a carrier of that group.

The inbound transport queue is bounded, and on the transport that exists the
budget is slots.

**Owner ruling 2026-08-29 -- `SP_CARRIER_CREDIT_PARKED` goes, in its present
meaning.** A physical byte credit does not exist for pointer transport, so a
probe asserting `payload_bytes = 8192` and a peak resident-byte figure is
asserting a model this document no longer has. The SCENARIO the probe covers is
still worth proving and it is renamed to what it actually is:
`SP_TRANSPORT_DATA_SLOT_TASK_PARKED` -- a task parked on exhausted DATA-SLOT
admission, after which a cancellation or shutdown travels the reserved control
lane and every data slot comes back. The word `CARRIER` is wrong in the old name
for a second reason: an async crossing parks the TASK, not the carrier. A cross-shard message is a fixed `rt_transport_msg` envelope
whose `payload` is a pointer into a refcount graph the transport neither copies
nor owns, and every construction site in the tree sets `payload_len` to zero.
An exact payload cannot be derived from such a pointer: the graph is shared, is
not copied, and has no stable per-message cost, so the root's allocated size, a
walk of the graph, and an alias estimate are three spellings of a number the
transport does not have. What a pointer message actually spends is one envelope
and one queue entry, so one envelope and one queue entry are what it is charged
for. `payload_len` means the bytes of a buffer the transport itself owns, and
on this path that is zero.

A byte budget is a measurement only where the transport owns the bytes: a
serialized message, or a buffer explicitly handed over at enqueue and held until
the target drains it. No such path exists yet, and until one does a physical
byte credit is false precision rather than an unfinished feature.
`RT_TRANSPORT_MSG_CREDIT_CONTROL` and the `credit_stalls` counter are reserved
names for that path -- no site in the runtime builds a credit-control message
and nothing increments `credit_stalls` -- so neither is a partial implementation
of this model and neither may be read as one.

The bound therefore carries two budgets, a data-slot credit for each pointer
envelope and a separate control reserve, and the reserve is what a data backlog
may not consume. A bound whose control reserve can be spent by data is a
contract failure rather than a tuning mistake, because the messages that release
backpressure -- the credit return, the completion, the cancellation ack, the
shutdown wake -- are exactly the ones a backlog starves, and a larger number
moves that deadlock without removing it. The reserve carries bounded protocol
metadata only; a completion or reply carrying arbitrary `T` is data traffic and
is budgeted as data. The tree does not hold this line today:
`rt_transport_msg_is_control` routes `RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION`
and `RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY` onto the same
`RT_TRANSPORT_CONTROL_QUEUE_CAP` entries as `RT_TRANSPORT_MSG_CREDIT_CONTROL`
and `RT_TRANSPORT_MSG_SHUTDOWN_WAKE`, so sixteen queued completions leave the
transport refusing the next release message with
`RT_TRANSPORT_STATUS_QUEUE_FULL`. Credit returns may be coalesced; cancellation
and completion records remain distinct and generation-checked.

A same-shard publication is not transport and reserves nothing: it never enters
the inbound queue, so this bound is a cost of crossing rather than a cost of
sending. The transitional `1 shard x N carriers` topology adds no second
reservation either -- the carriers of one shard share one inbound queue, and the
group credit of Section 10, not a per-carrier transport credit, is what makes an
accepted record runnable for one of them.

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

Fixing membership at creation changes the language contract, not only the
topology. Today a task created outside a scope and scheduled inside one is
adopted by it: `spawn` lowers to the runtime wake, which writes the current
scope's identity into a target that has none, on a lane other than the one
child registration uses. The adoption is partial, which is worse than silent
membership: the child is never counted, so the scope does not join it, yet its
cancelled result still raises the scope's fail-fast flag and cancels the real
members. Semantic analysis rejects that `spawn` with a diagnostic of its own
rather than change its meaning in silence.

**Distributed spawn is an explicit crossing.** A local spawn may capture borrows
of the parent, but common shard ownership is not itself a same-thread guarantee
in the transitional multi-carrier topology. A child that captures `&T` or
`&mut T` is carrier-affine from its creation to its completion; a borrow that
ends earlier does not release the affinity unless the runtime carries an
explicit affinity-release event the steal predicate can test. Semantic analysis
records the affinity at the spawn that creates it and rejects an explicit
placement that contradicts it; keeping the affinity at run time is the
scheduler's part, whose steal and handoff predicates refuse that task on any
carrier other than its parent's, exactly as placement refuses a connection task
on a non-owner shard, and trace every refusal. A wake of such a task is a
publication into its class (Section 10) unless the waking thread is its own
carrier, which may elide it as any eligible carrier may; it is never a push
into a deque that carrier cannot reach, since a task its own carrier cannot
see is unreachable even when the steal predicate refuses it. A distributed
spawn is written
`spawn on distributed { ... }` and is checked move-only plus shard-movable: it
may capture `own` shard-movable values or copyable values, never `&T`, `&mut T`,
or shard-pinned resources of the parent. The construct that makes the crossing
visible is also the point where the no-borrow-across-shards and
no-implicit-resource-migration rules are enforced. Joining a distributed child
returns through a `far Task<T>`, so the join is itself a visible crossing.

**Owner ruling 2026-08-28 -- affinity is a function of the CAPTURE SET, not of
the parent-child edge.** A child of a carrier-affine task is itself affine only
when it captures something borrowed; capturing nothing borrowed, or capturing
`own`, leaves it free to run on any carrier whatever its parent is. Affinity is
therefore not inherited down the tree, and it IS transitive through borrowing: a
grandchild that borrows from a frame which itself borrows is affine to the same
carrier, because the frame it reads is alive only while that carrier is. The
rule is the one above applied again at each spawn, recorded at the spawn that
creates it and never propagated from the parent's state.

Inheriting affinity down the tree was refused because it spends parallelism that
is legitimately available: a fan-out of children that borrow nothing would be
pinned to one carrier for no reason the ownership model can state.

**A `blocking { ... }` body may not capture a borrow, and is refused exactly as
a crossing is.** The body executes on a Tier 2 pool thread, which is not the
parent's carrier and is not a carrier at all, so a borrow inside it is the same
violation as a borrow crossing a shard.

There is no deadlock between affinity and the blocking pool, and this is worth
stating because one was assumed to exist: submitting a blocking body PARKS the
submitting task rather than occupying its carrier, so an affine task waiting on
a blocking body leaves its carrier free for the work that will wake it.

**Owner ruling 2026-08-29 -- publication does not promise a first poll.** A far
task that is cancelled after publication guarantees only that the task EXISTS
and can be cancelled; it does not guarantee its body is ever entered. If the
cancellation linearizes while the task is `PUBLISHED` and before `STARTED`, the
target withdraws the record, releases the admission it took, and answers
`Cancelled()` WITHOUT running the body. If `STARTED` linearizes first, the
cancellation is an ordinary cooperative one and the body observes it.

A row requiring the body to leave a witness in the first case asserts something
this model does not promise. What such a row must prove instead is the terminal
`Cancelled()`, the release of the slot the publication took, and the absence of
a second completion.

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

Tier 2 has two destinations. `blocking { ... }` is a bounded-admission service
with a configured worker limit for work that blocks in syscalls; its threads may
park indefinitely. `spawn on pool { ... }` is the accepted source shape for
future CPU-bound placed work that must not block and may be stolen internally.
Mixing them in one pool lets a blocked syscall stall queued CPU work, so the
boundary is explicit.

The CPU destination is the target stealing pool. This is the one target path
where work stealing is the correct tool: the work is CPU-bound and does not care
which core runs it, so balancing it by stealing costs nothing the connection hot
path pays. The transitional `1 shard x N carriers` topology may steal only
within one shard and only tasks eligible for the stealing carrier; it is a
compatibility cost, not a Tier 1 target property. Tier 1 never steals across a
shard boundary.

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

#### Multi-Carrier Worker Wakeups

CPU Tier 2 is the target scheduler with multiple peer workers. During the
`1 shard x N carriers` transition, Tier 1 also has a multi-carrier scheduler.
Both use this wake protocol, but they do not acquire the same ownership rights:
Tier 2 tasks hold no borrow into Tier 1 state and no shard-pinned resource;
transitional Tier 1 tasks remain on their owning shard and never become Tier 2
work.

A worker group has a public injection queue, worker-private deques, and an
eligibility class for each task. A task's eligibility class is the set of
workers that may execute it. The placement layer assigns it when the task is
published and stores it with the task; it follows from shard placement, from
carrier affinity (Section 9), and from any task-class restriction. A worker
knows the classes it may execute from its own identity, and a group with no
restrictions has exactly one class.

Each class has its own publication queue and credit counter, created lazily at
the first publication into it and kept while the group is open; a group with no
restrictions has one queue, one counter, and no cost. That partition is the
public injection queue: a class's queue is the group's public queue for that
class, the private deques are unchanged, a worker's no-work predicate reads
every class queue its identity admits, and the bounded service latency below is
owed per class queue, so a singleton class is polled on the same bound as any
other. A class's set of waiters is not lazy: a worker waits registered for
every class its identity admits, including one nothing has been published into,
so none sleeps into the gap between a class's first credit and its first queue.
Publication and notification are addressed to the class, never to the group and
never to a thread outside it; a carrier-affine task (Section 9) is normally a
singleton class whose only eligible worker is its carrier.

A worker may wait only after it has observed a
consistent no-work predicate: its own deque, the public queue, and every task
it is eligible to steal contain no task, and no eligible wake credit is pending.
Satisfying that predicate is what Section 8 calls completing the group no-work
handshake. This predicate is normative; the implementation may use
a lock, epochs, per-deque metadata, or an explicit
`SEARCHING -> PARKING -> WAITING` handshake.
Release/acquire ordering makes an already-published task visible, but it does
not close the race to sleep. The no-work observation and the transition to
waiting must be linearized against credit publication: either both sides run
under one shared lock, or both sides are store-then-load with sequentially
consistent ordering as in Section 8, or the transition is a single competing
read-modify-write.

Wake credits are non-negative counted permits in a task's eligibility class,
not a boolean. A producer publishes at least one eligible credit for every task
it makes runnable that is not covered by a continuity obligation. It publishes
the task with release ordering, then the credit with release ordering, then
notifies. A worker observes the credit with acquire ordering before it scans
queues. A worker may consume a credit only when it can execute the credit's
class. It then performs the full no-work check before waiting again. A consumed
credit is never returned: a worker that finds no eligible task has observed an
allowed spurious wake, and it cannot tell from inside whether an eligible peer
claimed the task or it lost a steal race to it. The full no-work check is what
keeps that from becoming a lost wakeup. Over-crediting is a policy
cost. Under-crediting is a correctness failure. An implementation may encode
permits as a monotonic generation rather than a decrementing counter only if it
preserves these at-least-once observations.

A notification must be able to reach a worker that is eligible for the credit's
class. Waking a worker that may not consume the credit does not discharge the
notification: a notification is addressed to the credit's class and must reach
at least one waiter that could consume it, which for a singleton class is its
only worker; whether it reaches one or all of them stays policy, and reaching
none is a lost wakeup. A notification carrying no credit -- the arm
notification of Section 1, a shutdown transition -- is addressed to the threads
serving the shard, and the rule above binds credit-bearing notifications only.
The credit is the correctness
obligation and the notification is the policy decision on top of it: a producer
may batch or suppress notifications only when, under the same indivisible
protocol that governs the wait transition, it observes that an eligible worker
is already awake or searching. One notification may therefore serve a batch of
credits, but no credit may go unpublished.

Continuity is task-specific, not merely a property of a running owner. An elided
task occupies a non-stealable run-next slot, and the owner must claim that exact
task at its next scheduler selection before it starts another task. The
continuity debt discharges at that claim, or when the carrier republishes the
reserved task into a queue reachable by eligible peers with an eligible credit
and a notification. It discharges in no other way. A private deque entry is not
such a reservation: peers may steal it, and the owner may choose another source
at selection. Therefore a local deque push, a public injection push, and every
external publication always issue eligible credits. The simple deque rule
`queue.len > 1` is never a model-level reason to elide a credit.

A carrier has at most one outstanding continuity debt, because the run-next slot
holds exactly one task. A producer that would elide into an occupied slot
publishes into a queue reachable by eligible peers with a credit and a
notification instead, and displacing a task already in the slot is itself such a
publication. Elision is also available only to a carrier that may execute the
task it elides: handing a task to a slot its holder is not eligible to run is a
publication like any other and must issue an eligible credit. A turn that makes
two tasks runnable therefore elides at most one of them.

A wait that can suspend the task instead of the carrier must do so. Inside an
async body every suspension point — awaiting a task, joining a scope, sending to
or receiving from a channel, waiting on readiness, a timeout, a `select`, or a
crossing — parks the waiting task and returns the carrier to scheduler
selection, where it claims its reserved task if it has one. This is the
stackless poll-state-machine lowering stated in the Summary, and it costs the
carrier nothing; Section 4 is what makes the resume cheap, because the waiter
already lives with its owner. Only a path that must abandon the carrier itself
reaches the obligation below: a blocking syscall, carrier exit, an `await`
written outside an async body, and the synchronous channel wait Section 7
records as debt. A carrier that suspends its own scheduler path while holding a
task no peer may claim is a contract failure, not a policy choice.

If a carrier cannot claim its continuity task immediately, it must first move
every task covered by its continuity or private-queue obligations into a queue
reachable by eligible peers, publish one eligible credit for each task, and
notify. This includes the owner's run-next task. A task whose affinity prevents
that transfer may not be left behind: the carrier runs it to completion, or
refuses the transition that would make it unavailable. A primitive that cannot
be refused at that point is a contract failure, not a policy choice; only
shutdown may resolve such a task by cancelling it. A path that blocks, enters a
syscall or a sync wait, abandons its carrier, or otherwise leaves the current
turn without returning to selection has this obligation before it leaves.

A scheduler safepoint is a point at which a carrier holds no task mid-turn:
between finishing one task and selecting the next, and immediately before it
waits. The invariant holds per eligibility class and per obligation class
(Section 1), not per group: at a safepoint, for every class holding a runnable
obligation -- runnable work, an unclaimed eligible credit, or an unserved wake
notification -- at least one worker eligible for that class must be covering it
— executing the group's scheduler path, leaving the wait, releasable by an
eligible credit, or running a task from which it is guaranteed to return
to scheduler selection. A worker that has left the scheduler path into a wait it
cannot be released from covers no class. Counting only whether some worker is
busy is not enough: a class whose only eligible carrier is inside a task it
cannot leave is uncovered even though the group looks alive.
For every shard holding a service obligation -- an armed deadline, an open
readiness interest -- at least one thread serving it, carrier or service agent,
must be one that obligation can release. A maintenance obligation makes no wait
illegal and no worker covers it; its owner drains it as Section 1 requires.
This is the debug-invariant counterpart to Section 8's `PARKED` inbound check;
it detects a stranded group as a contract failure instead of leaving it to
appear as a hang.

The public injection queue also has bounded service latency: a worker must poll
it after a bounded number of local selections. The bound is a scheduler policy
parameter and must be traced, so a chain of local continuations cannot starve
external arrivals. The bound outranks continuity: when it is reached, the
carrier serves the public queue first and republishes its outstanding run-next
task with an eligible credit, because a continuity debt may delay one task but
may never postpone the bound indefinitely.

Shutdown is part of the wait predicate. Closing a worker group rejects or
transfers new work, publishes shutdown wake transitions for every waiter, and
drains or transfers queued work before worker exit. Waiters re-check shutdown
before sleeping; credits and notifications are discarded only after no waiter
can consume them. Shutdown therefore cannot strand a worker on an empty credit
wait. A carrier-affine task cannot be transferred, so shutdown resolves it on
its own carrier: the exiting carrier runs or cancels every task pinned to it
before it leaves, and the group is closed only when no carrier-affine task
remains.

Continuity does not promise preemption or a latency deadline: a CPU-bound task
that does not yield delays its owner's next turn. Tier 2 accepts that cost
because it is explicitly off the connection hot path; workloads that need a
stronger latency bound need a bounded cooperative quantum. This protocol buys
locality only through the run-next slot. Its cost is a distinct synchronization
domain, queue visibility for stealing, durable credits, and observability. The
runtime must trace run-next elisions, credits by eligibility class, wake
transitions, suppressed notifications, steals, refused steals, waits,
public-queue age, maximum local streak, and blocking-pool admission stalls by
cause. These counters measure policy; the liveness invariant is correctness.

Every cost in this section is paid by a scheduler that has peer workers. The
target Tier 1 topology has none: with one carrier per shard there are no
credits, no notifications, no stealing, and no eligibility bookkeeping, and the
run-next slot degenerates into plain locality. The protocol is therefore the
price of the transitional topology and of Tier 2, not of the thread-per-core
model. The transitional topology also pays a second price that belongs in the
same place: a request tree whose children capture borrows is carrier-affine, so
a `join_all` fan-out over such children runs on one carrier and is not a
parallelism lever there. A program that wants intra-shard parallelism in that
topology must spawn children that capture no borrow.

#### Blocking Tier 2 Pool

The syscall-blocking pool is a separate, bounded-admission service. It has a
configured worker limit and a bounded queue; it does not provision unbounded
threads, use worker-private deque continuity, or steal, because a submitted job
may block indefinitely. When the pool has no admission capacity, the submitting
task parks asynchronously until a completion returns capacity. If every worker
is blocked indefinitely, later submissions make no progress by design: they are
backpressured rather than accumulated without bound. Admission is bounded by the
worker limit and by the queue together, and each has its own counter, so an
admission stall is a measured number rather than a hang. Every admitted job
creates an eligible durable wake credit, and the pool's wait and shutdown
predicates follow the same at-least-once, visibility, and drain-or-transfer
rules above. It has its own queue, capacity accounting, and observability; it
must not inherit CPU-pool wake elision merely because both destinations are
Tier 2.

Backpressure only works while the waiting side can make progress elsewhere, so
a job admitted to the blocking pool may not itself require blocking-pool
admission, directly or transitively: the runtime refuses or accounts such a
submission rather than parking it behind its own completion. Shutdown of this
pool drains or cancels queued jobs and refuses new submissions, with one
exception to the drain-or-transfer rule above: it does not wait for an admitted
job that is already blocked in a syscall. Such a job's thread is detached at
exit, so a pool whose workers block indefinitely cannot hold the runtime open,
and shutdown records the job rather than blocking on it.

### 11. Allocation And Heap Stats

Shared-nothing requires more than removing locks. It also requires removing
shared cache lines from the hot path.

Epic 5 completed the first allocator step: heap accounting no longer writes one
process-global counter set on every allocation and free. Current `rt_alloc`,
`rt_free`, and `rt_realloc` record events into runtime or shard-owned
accounting cells, including an explicit cold path, and `rt_heap_stats()`
aggregates those cells on read.

Section 1's "Heap cells and allocation pools" is the normative statement; this
list is what the allocator still owes it, and it is keyed by LANE, not by
shard, wherever the two differ:

- hot runtime objects come from lane-local slab or bump allocators;
- connection buffers, task states, waiter nodes, and parser scratch memory are
  allocated and released on the owning lane;
- the allocator records the owning lane in page or span metadata, and a span is
  carved by its owner into objects of one size class;
- releasing on a non-owner lane enqueues the pointer on the owner's
  remote-release queue, which is bounded, and a release that finds it full
  takes the cold ownership-neutral fallback rather than waiting or waking;
- only a span holding no live object returns to the process allocator;
- the owner drains remote releases at scheduler safepoints or before
  allocation;
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

Left open by the 2026-08-27 rulings, and deliberately not answered in the text
above. Whether one monotonic base serves every shard or each shard reads its
own, and what `sleep(0)` is -- a yield, or a deadline at the current reading.
What names and enables the test clock, and whether a shard refuses to arm,
refuses to start, or bounds its advance when the monotonic clock is unreadable.
Whether the channel's commit order survives `close` for a sender the close
settled, and whether a position a `select` arm took and then destroyed counts
in the order a later receiver is judged against. The remote-release queue's
bound as a number and a unit, whether there is one queue per owner or one per
owner-and-releasing-lane pair, and which mechanism the words "cold,
ownership-neutral fallback" name -- the span's own release accounting, or a
process-wide cold structure some later drain reconciles -- and whether the
fallback rate carries a budget or only a report. Whether the mode a row names
covers only the reporting switches or the topology and sanitizer configuration
with them, and whether the paired trace-off/trace-on run is paired per switch
or once for the whole enabled set. Each of these changes what an implementation
must do, so each waits for the owner rather than for whoever writes the lane.

Left open by the 2026-08-28 transport rulings, and deliberately not answered in
the text above. Whether the control reserve is accounted independently of the
data budget or subtracted from it, and what each budget is as a number of slots.
Whether a completion or reply carrying arbitrary `T` takes an ordinary data slot
or a reply slot reserved when its request was admitted. What a sender does when
no slot is available -- park on its own shard until one returns, or keep the
drain-and-retry the tree performs at `RT_TRANSPORT_STATUS_QUEUE_FULL`.
**ANSWERED 2026-08-28: the sender PARKS on its own shard, and
`RT_TRANSPORT_STATUS_QUEUE_FULL` stops being an answer a program can observe.**
Saturation is backpressure, which is the ruling the blocking pool already
carries; the status stays as an internal result of the enqueue call, because the
parking code has to be told there is no room, but no crossing answers a program
with it and no language surface gains a failure arm for it. The drain-and-retry
the tree performs today is replaced by that park. Two obligations travel with it
and are not optional: an admission stall must be a MEASURED NUMBER rather than a
hang, the same counter requirement the blocking pool carries, so a saturated
transport is distinguishable from a stopped one; and a park that can never be
released must still be reachable by cancellation, because a receiver that is
gone would otherwise turn backpressure into a silent permanent sleep -- scope
cancellation and timeout answer that, not a status.
Whether
`payload_len` stays on the pointer path as the length of a transport-owned
buffer, zero for every message the tree builds, or leaves that path until a
serialized transport needs it. And whether `RT_TRANSPORT_MSG_CREDIT_CONTROL` and
`credit_stalls` are held as reserved names for that transport or removed until
it exists. Each names a mechanism the transport must have before a bound can be
claimed, so each waits for the owner rather than for whoever writes the lane.

## Refusal Of A Result Type's Own Storage

**Owner ruling 2026-08-29, written here rather than left to be derived from the
storage model's Non-Goals.** An ordinary filesystem or network error stays what
it is: a value of the result type. But a refusal to allocate the result TAG or
the error object itself is FATAL. `OutOfMemory` is not added to any result type,
because a value reporting that a value could not be created does not solve the
problem it reports -- it needs the same storage that was just refused.

Every runtime entry point that answers a result therefore has exactly two
outcomes, and the boundary belongs in its own contract: a VALID RESULT, or a
TERMINAL FATAL. There is no third answer, and in particular no null answer for
generated code to store and then read a discriminant through. Thirty-one entry
points -- seventeen the filesystem, nine sockets, five carrying their own
answer -- return a bare `NULL` on refusal today, and closing that boundary is
the work this ruling creates.

## The Shape Of A Fatal Report

**Owner ruling 2026-08-29.** Every fatal report reads:

```
surge: fatal [<CODE>]: <static-or-provided-message>
```

for example `surge: fatal [RT_OOM]: could not allocate FsResult`. `panic_msg`
routes through the same emitter under the code `PANIC`. A reachable LLVM trap is
replaced by a call to `rt_fatal_static(RT_TRAP, ...)`; a bare `llvm.trap()` is
permitted ONLY for a backend invariant proved unreachable, never for a refusal a
program can provoke.

Composing an out-of-memory message must not allocate, which is why the type name
in it is a static literal rather than a formatted one.

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
| O(n) wake and park | `pop_waiter()` scanned and compacted the full waiter list; it was deleted in Wave D step D0 after its last caller went away. | O(1) or O(k) operations on the owner-local queue. |
| Net poll rebuild churn | Net polling rebuilds the fd set from global waiters. | Shard-local fd registry persists across poll cycles. |
| Cross-worker wake churn | Non-worker wakes enter global inject; worker wakes can signal other workers. Nothing credits a sleeping peer for a task pushed onto a private deque, so a same-shard push can be lost to a carrier that never returns to selection. | Target Tier 1 has one carrier per shard and no peer wake. Transitional `1 shard x N carriers` and Tier 2 use counted at-least-once credits per eligibility class; the only credit-free publication is a task placed in the publisher's own run-next slot and claimed at its next selection. |
| I/O thread as partial worker | The current patch drains ready inject tasks after net readiness. | A shard runs its own net-ready continuations or drains a net-woken queue only. |
| Expensive channel handoff | Channel send/recv uses global lock plus shared waiters. | Same-shard channel handoff is local; cross-shard handoff is explicit messaging. |
| Heap allocation ownership | Heap accounting uses runtime/shard-owned cells, but allocation still uses the current `malloc`/`free` strategy. | Shard-local hot object pools and owner-routed frees. |
| Cross-shard value lifetime | A value can be used and dropped away from its allocation owner. | Only `own` shard-movable values cross shards; the allocator routes non-owner frees to the owning shard. |
| Shard-pinned resource move | `own File` could move to another shard while its fd remains registered on the old shard. | Types that transitively own shard-registered resources are shard-pinned; ordinary sends are rejected and explicit migration re-registers the resource. |
| Invisible cross-shard cost | A remote operation could be spelled like a local one. | Crossing is a distinct typed construct (`far` + `on` / `spawn on`); cost is legible before compile. |
| Lost cross-shard wakeup | A producer can enqueue, see the target as running, skip the wake, and race with target parking. | Wake elision uses the seq-cst park protocol: `PARKED` store, queue re-check, and debug invariant. |
| Unbounded inbound transport | Cross-shard messages can grow memory without backpressure. | Inbound transport is bounded in slots, one data credit per pointer envelope, with a control reserve a data backlog cannot spend; byte credits wait for a transport-owned buffer. |
| Remote bounded channels | A bounded send across shards needs receiver-side capacity state. | The receiver shard owns capacity and completes sends through request/ack. |
| Remote select | Selecting over remote channels creates multi-shard subscriptions and cancellation. | Local select is fast; remote select is denied the fast path and lowered to a slow coordinator. |
| Cross-shard cancellation staleness | A child can complete while cancel is in flight, or storage can be reused. | Distributed scope cancel and completion messages carry generation tokens; stale messages are ignored. |
| Load skew | Work stealing can hide skew but conflicts with fd ownership on the connection path. | CPU skew uses Tier 2 stealing; I/O skew uses migration and control-plane rebalancing. |
| Channel lifetime | A local channel is never freed - `rt_channel_free` has one production caller, and a local `Channel<T>` matches no drop predicate - so the object and every buffered value leak by construction. | The runtime object is deterministically reference counted behind the copyable handle; the last release drops the payloads it still owns and frees the storage. |
| Payload in the scheduler mailbox | A same-shard handoff writes the value into the receiving task's `resume_bits`, and a parked sender holds its value in its own, so a payload can live in four places and none of them is typed. | The mailbox carries control only; every payload lives in a typed slot owned by the sender, the channel, the receiver, or the select operation. |
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

### What `checkpoint()` Promises

Settled by the model owner on 2026-08-31, answering the first case the paragraph
above anticipated.

`checkpoint().await()` is **not** a fairness primitive. Its contract is to finish
the current poll, check for cancellation, and return the task to scheduler
selection. It does **not** promise that some other Ready task — woken by a join
or by any other path — runs before this task is polled again. The opportunity to
schedule other work is real; the ordering guarantee is not.

Two consequences are load-bearing.

`force_inject` is a wake **placement** policy, not the carrier of a language
guarantee. Join wakes do not force injection while net wakes do
(`rt_task_park.c`), and that asymmetry is a scheduling decision answerable on
scheduling grounds — starvation of the oldest connection, in the net case. It is
not evidence of a fairness defect on the join path, and neither a broad flip
(every join wake) nor a narrow one (join against a checkpoint task) is justified
by this model. Changing it needs a scheduling argument, not a fairness one.

The fairness properties F1-F3 in `docs/CONCURRENCY.md` are properties of the
**VM's deterministic single-worker scheduler profile**. They are not properties
of the `checkpoint()` abstraction, and not properties of single-worker execution
in general — the native runtime at one worker is not thereby obliged to
reproduce them. This is consistent with V2 giving up automatic fairness in the
connection hot path (see Non-Goals And Tradeoffs) and with parallel mode
promising no global FIFO order.

The corpus records this distinction rather than restating it. `t04_fairness`,
`t15_fairness_round_robin`, and `t16_no_starvation_chatty` carry an
`.order-backends` sidecar naming the VM: their recorded interleaving is checked
on the VM, and on native the same rows are compared as a multiset of lines, so
the work each task owes is still asserted while the order floats.
`t11_loop_checkpoint` is not marked — it has one task and records a value, not
an interleaving.

Measured on `e34b7db8`, native at `SURGE_THREADS=8`, five runs each: all three
marked rows produced the recorded set of lines every time, and the recorded
order in none of them reliably — `t04_fairness` matched twice in five, which is
the clearest evidence that the interleaving was never a native promise. No task
was starved in any run; `t16`'s two quiet tasks finished all three prints every
time, never later than the eleventh line. Native gives up the *order*, not the
*progress*.

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
  stores, sleep timers (atomic virtual clock + per-shard sorted stores -- the
  virtual clock is the shipped state the 2026-08-27 Р2 ruling removes, tracked
  by `RV2-DEBT-180`, and the gated code shape is re-derived when the default
  flips), and
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
one-word payload ABI is replaced end to end by Epic 23b,
`runtime-v2-epics/23b-inline-storage-and-typed-carriers.md`; it owns typed
task/channel/select/blocking/far payloads. The pointer transport keeps the slot
budget of Section 8; the physical byte credits that epic also plans belong to a
transport-owned buffer and wait for one.
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
- Implement bounded inbound transport queues budgeted in slots, one data credit
  per pointer envelope, with a control reserve for bounded
  credit-return/cancellation/progress metadata that a data backlog cannot spend.
  A completion or reply carrying arbitrary `T` is data traffic and is budgeted
  as data rather than against the reserve.
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
- identical `shards x carriers` rows: `1x1` legacy control, `1xN`
  transitional multi-carrier, and `Nx1` target topology;
- pipelined and non-pipelined TCP rows;
- mixed CPU/TCP rows with bounded CPU tasks;
- trace counters for cross-shard messages, wake-fd writes, inbound transport
  credit stalls, remote-free queue depth, remote bounded-channel round trips,
  remote `select` uses, stale generation drops, global path usage, shard
  imbalance, local queue depth, allocation counters, and fd readiness batches;
- the Section 10 counters alongside them: run-next elisions, wake credits by
  eligibility class, wake transitions, suppressed notifications, steals, refused
  steals, waits, public-queue age, maximum local streak, and blocking-pool
  admission stalls by cause. Every elision site carries its own counter, so an
  elision rate is a measured number before the protocol changes it, not an
  estimate afterwards;
- the counters the shard-ownership rules of Section 1 make checkable: timer
  fires by serving-thread kind, arm notifications published and suppressed,
  timer run-next elisions as their own site, batches served out of deadline
  order, and timer service lateness as a distribution rather than a mean;
  poll lease grants, refusals, maximum hold, and non-carrier grants per
  shard, wake writes attributed at the writer, and empty readiness slices;
  revoked rendezvous admissions by cause and by entrant class, claim
  refusals, retry republications and budget exhaustions, and a hard zero for
  values destroyed in recovery; a hard zero for unserialized scope
  accounting and for a fail-fast raised after a drained answer; allocation
  counters per lane with the peer allocation delta and the lanes started in
  the window, remote-release balance, maximum depth, drain age and fallback
  releases named per owner, free-path registry acquisitions per lane, and a
  hard zero for heap cells straddling a cache line; dropped trace records
  with their owner named, and public-queue bound crossings per carrier. A
  row also names the reporting mode it ran in and is compared only within
  it.

Every latency and throughput row must name the time-measuring harness, its
warmup, duration, percentile method, and connection distribution. An
allocation-only carrier benchmark cannot establish a latency or throughput
claim. No topology may be declared faster, slower, or ready to replace the
default until the same time-measuring harness has reported all three
`shards x carriers` rows.

Five rules make a latency or throughput row admissible. A row records `shards`
and `carriers per shard` as two separate fields with the exact environment that
produced them: a harness that derives the carrier count from the shard count
cannot express `1xN` and `Nx1` at once and cannot produce this matrix. A row the
harness skipped, truncated, or lost to a timeout is not a reported row: it
records the requests actually completed and the wall time actually elapsed, and
a comparison missing any of the three rows fails rather than printing a
placeholder. A percentile computed from a batch mean replicated once per request
is a throughput restatement and may not be published as a tail, so a pipelined
row reports per-request latency or reports no percentile at all. And a row
states the evidence that the load generator was not the bottleneck: client and
server CPU during the run, and a client-scaling row at a fixed server
configuration. And a row names the reporting mode it ran in and is compared only
within it: a traced run and an untraced one measure different programs, so the
price of reporting is read off a paired run, never off two rows taken in
different modes.

Success means the `Nx1` target meets its predeclared small-load latency threshold
against the `1x1` control and materially improves many-connection throughput and
tail latency against both `1x1` and the transitional `1xN` row. The owner must
set those thresholds before collecting the comparison; an allocation-only row
cannot substitute for them.

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
