#ifndef SURGE_RUNTIME_NATIVE_RT_SCHED_TRACE_H
#define SURGE_RUNTIME_NATIVE_RT_SCHED_TRACE_H

#include <stdatomic.h>
#include <stdint.h>

// Where a thread took its OWN next task from is a record of what that thread
// did, so it belongs to that thread and to nobody else. There are exactly two
// kinds of popper, and each gets its own cell:
//
//   * a carrier, which pops for itself and writes only the cell at its own flat
//     carrier index -- one writer, no lock, and none is invented here;
//   * the runtime's control lane, which is every non-carrier thread that drains
//     shard 0 (the single-runner main loop, the io thread, a compensation
//     worker at its limit). They reach the record inside `ready_pop`, which
//     holds shard 0's lock across the pop, so they share a lock.
//
// That is the whole point of the split. A number reported from these cells
// always has writers that share a lock or share an owner, so the dump can name
// the owner of every number it prints, and a reader can tell whose it is. A
// single shared word -- even an atomic one -- would report a number belonging
// to nobody: it would not tear, and it still could not be attributed, which is
// what makes it inadmissible as evidence rather than merely unsafe.
//
// A carrier holds no lock, so publication carries the ordering instead: the
// owner stores release and every reader loads acquire. A dump taken while the
// owners still run is a per-owner sample and not a global cut across owners,
// exactly as the per-lane heap cells are.
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
    // Written by the owner itself on its first record: the cell names its owner
    // rather than the dump guessing it from the topology.
    _Atomic uint32_t shard_id;
    // Two owners never share a cache line. Without this an owner's pop would
    // pay for its neighbour's, so the cost of tracing would depend on which
    // owners sit next to each other in the array rather than on what each did.
    unsigned char pad[RT_SCHED_TRACE_CELL_BYTES - 4U * sizeof(uint64_t) - sizeof(uint32_t)];
} rt_sched_trace_cell;

_Static_assert(sizeof(rt_sched_trace_cell) == RT_SCHED_TRACE_CELL_BYTES,
               "a scheduler trace cell must fill exactly one cache line");
_Static_assert(_Alignof(rt_sched_trace_cell) >= RT_SCHED_TRACE_CELL_BYTES,
               "cells of different owners must not share a cache line");

// `carriers` is the flat carrier count the runtime is about to start: one cell
// is reserved per carrier, plus the control lane's.
void rt_sched_trace_init(uint32_t carriers);
void rt_sched_trace_dump(void);
void rt_trace_sched_record(rt_trace_sched_source source, uint64_t id);
void rt_trace_sched_tier1_steal_denied(void);
void rt_trace_sched_connection_owner_placed(void);
void rt_trace_sched_connection_owner_run(uint32_t owner_shard_id, uint32_t worker_shard_id);

#endif // SURGE_RUNTIME_NATIVE_RT_SCHED_TRACE_H
