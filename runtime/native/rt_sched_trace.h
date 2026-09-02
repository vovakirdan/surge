#ifndef SURGE_RUNTIME_NATIVE_RT_SCHED_TRACE_H
#define SURGE_RUNTIME_NATIVE_RT_SCHED_TRACE_H

#include <stdatomic.h>
#include <stdint.h>

// Where a thread took its OWN next task from is a record of what that thread
// did, so it belongs to that thread and to nobody else. A steal that thread
// attempted and had refused, and the connection task it ran, are records of the
// same kind and are taken on the same path -- inside the pop -- so they live on
// the same cell rather than in a word shared by everyone.
//
// There are exactly three owners here, and each of them has a cell:
//
//   * a carrier, which pops for itself and writes only the cell at its own flat
//     carrier index -- one writer, no lock, and none is invented here;
//   * the runtime's control lane, which is every non-carrier thread that drains
//     shard 0 (the single-runner main loop, the io thread, a compensation
//     worker at its limit). They reach the record inside `ready_pop`, which
//     holds shard 0's lock across the pop, so they share a lock.
//   * the runtime, which owns exactly the events that no carrier and no lane
//     performed: a placement made by whichever thread created the task, a
//     pop-path record whose owner cannot be named, and a record the dump itself
//     could not put on the wire. These have many writers and no common lock,
//     and they are admissible for the reason the model gives and for no other:
//     their writers share an OWNER. None of them is recording its own action --
//     it is adding to the runtime's count of something the runtime did or
//     failed to attribute -- so the number the dump prints is the runtime's and
//     a reader can tell whose it is.
//
// That last clause is the whole of the exemption, and it is narrow on purpose:
// a quantity is the runtime's only when no single owner performed the event. A
// pop is performed by whoever popped it; so is a steal it was refused. Neither
// can ever be the runtime's, which is why one shared word for pops had to go
// and why no new one may appear. A single shared word -- even an atomic one --
// would report a number belonging to nobody: it would not tear, and it still
// could not be attributed, which is what makes it inadmissible as evidence
// rather than merely unsafe.
//
// No owner holds a lock the reader can take, so publication carries the
// ordering instead: every owner stores release and every reader loads acquire.
// A dump taken while the owners still run is a per-owner sample and not a
// global cut across owners, exactly as the per-lane heap cells are.
#define RT_SCHED_TRACE_CELL_BYTES 64U

// The shard a cell's owner belongs to, before that owner has recorded anything
// and therefore before it has said which shard it serves.
#define RT_SCHED_TRACE_SHARD_NONE UINT32_MAX

typedef enum {
    RT_TRACE_SCHED_SRC_LOCAL = 0,
    RT_TRACE_SCHED_SRC_INJECT = 1,
    RT_TRACE_SCHED_SRC_STEAL = 2,
} rt_trace_sched_source;

typedef struct rt_sched_trace_cell {
    _Alignas(RT_SCHED_TRACE_CELL_BYTES) _Atomic uint64_t local_pops;
    _Atomic uint64_t inject_pops;
    _Atomic uint64_t steal_pops;
    // A fingerprint of WHICH pops this owner made, as a wrapping sum of a
    // finalized mix per pop. Summed rather than chained on purpose: addition
    // composes, so a reader can add the owners' fingerprints over the owner set
    // the dump names and get the fingerprint of the whole run, while a chain
    // would only fingerprint the order one owner happened to be handed -- and
    // that order is not a property of the schedule, it is a property of how the
    // carriers divided it, which varies from run to run even under a seed.
    _Atomic uint64_t pop_mix;
    // A steal this owner attempted and the placement policy refused. A refused
    // steal is a record of what this owner tried to do, and it is taken inside
    // the same pop as the counts above, by the same thread, under the same lock
    // discipline -- so it needs no ownership argument of its own.
    _Atomic uint64_t steal_denied;
    // A connection task this owner ran, split by whether the shard it was
    // running on is the shard that owns the connection. Also taken inside the
    // pop, by the thread that popped it.
    _Atomic uint64_t conn_run_local;
    _Atomic uint64_t conn_run_mismatch;
    // Written by the owner itself on its first record: the cell names its owner
    // rather than the dump guessing it from the topology.
    _Atomic uint32_t shard_id;
    // Two owners never share a cache line. Without this an owner's pop would
    // pay for its neighbour's, so the cost of tracing would depend on which
    // owners sit next to each other in the array rather than on what each did.
    unsigned char pad[RT_SCHED_TRACE_CELL_BYTES - 7U * sizeof(uint64_t) - sizeof(uint32_t)];
} rt_sched_trace_cell;

// The runtime's own cell. It is a cell and not a handful of file-scope words
// for the same reason every other owner has one: a number at file scope is
// written from whichever path reaches it and read at the dump, and nothing in
// its shape says whose it is. Here the shape says it -- these are the runtime's
// three, they sit on the runtime's line, and the dump prints them under
// `owner=runtime`.
typedef struct rt_sched_trace_runtime_cell {
    // A connection task given an owner shard. The placement is made by whoever
    // created the task, which may be a thread that owns no cell at all, so it
    // is nobody's own action to record.
    _Alignas(RT_SCHED_TRACE_CELL_BYTES) _Atomic uint64_t conn_placed;
    // A pop-path record whose owner cannot be named. Putting it on some other
    // owner's cell would publish, under that owner's name, a number the owner
    // did not produce.
    _Atomic uint64_t unowned_pops;
    // A record the dump could not put on the wire whole. Reported, never
    // discarded: a reader summing the owner records has to know its sum is
    // short.
    _Atomic uint64_t dropped_records;
    // Carrier affinity, four records. A task pinned to the worker creating it;
    // a publication of a pinned task credited to its carrier by another
    // thread; a pop that met a pinned task and was not the carrier; a pinned
    // task an exiting carrier cancelled unpolled at shutdown. The pin and the
    // shutdown cancel are made by threads that may own no cell; the addressed
    // wake by whichever thread published. The refusal IS a popper's own
    // action, and it still lives here rather than on the owner's cell: that
    // cell's line is full, and widening every owner's line to make room for a
    // defence counter would double the footprint of tracing for a number the
    // publication route is meant to keep at zero. It is bumped with an atomic
    // read-modify-write, like the rest of this cell.
    _Atomic uint64_t carrier_pinned;
    _Atomic uint64_t carrier_addressed_wakes;
    _Atomic uint64_t carrier_steal_denied;
    _Atomic uint64_t carrier_shutdown_cancelled;
    // The runtime does not share a line with the owners it is reporting on
    // either.
    unsigned char pad[RT_SCHED_TRACE_CELL_BYTES - 7U * sizeof(uint64_t)];
} rt_sched_trace_runtime_cell;

_Static_assert(sizeof(rt_sched_trace_cell) == RT_SCHED_TRACE_CELL_BYTES,
               "a scheduler trace cell must fill exactly one cache line");
_Static_assert(_Alignof(rt_sched_trace_cell) >= RT_SCHED_TRACE_CELL_BYTES,
               "cells of different owners must not share a cache line");
_Static_assert(sizeof(rt_sched_trace_runtime_cell) == RT_SCHED_TRACE_CELL_BYTES,
               "the runtime's cell must fill exactly one cache line");
_Static_assert(_Alignof(rt_sched_trace_runtime_cell) >= RT_SCHED_TRACE_CELL_BYTES,
               "cells of different owners must not share a cache line");

// `carriers` is the flat carrier count the runtime is about to start: one cell
// is reserved per carrier, plus the control lane's and the runtime's.
void rt_sched_trace_init(uint32_t carriers);
void rt_sched_trace_dump(void);
void rt_trace_sched_record(rt_trace_sched_source source, uint64_t id);
void rt_trace_sched_tier1_steal_denied(void);
void rt_trace_sched_connection_owner_placed(void);
void rt_trace_sched_connection_owner_run(uint32_t owner_shard_id, uint32_t worker_shard_id);
void rt_trace_sched_carrier_pinned(void);
void rt_trace_sched_carrier_addressed_wake(void);
void rt_trace_sched_carrier_steal_denied(void);
void rt_trace_sched_carrier_shutdown_cancelled(void);

#endif // SURGE_RUNTIME_NATIVE_RT_SCHED_TRACE_H
