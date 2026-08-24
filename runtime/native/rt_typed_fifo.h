#ifndef RT_TYPED_FIFO_H
#define RT_TYPED_FIFO_H

#include <stddef.h>
#include <stdint.h>

#include "rt_slot_control.h"
#include "rt_typed_carrier_abi.generated.h"

// A homogeneous FIFO of typed slots: one descriptor for the whole queue, one
// byte of lifecycle per cell, and payload storage laid out at the element's own
// stride.
//
// WHAT THIS REPLACES. A channel's ring is an array of uint64_t today, so every
// element is squeezed through a machine word on the way in and reconstituted on
// the way out, and a Channel<nothing> of capacity N still pays 8N bytes for
// values that have no bytes. Here the queue asks the descriptor how wide an
// element is and lays out exactly that, which is zero for a zero-sized element.
//
// ONE CONTROL, NOT ONE PER CELL. rt_slot_control is 144 bytes and carries the
// claim/reservation machinery; per cell that would dwarf the values. It is
// therefore per QUEUE, and rt_slot_begin_generation_locked rebinds it onto the
// cell an operation is about. The API already refuses to rebind while a claim
// or reservation is live, so the rebinding is the owner-wide admission gate
// rather than a lock of its own: many cells may hold values, but exactly one
// typed transfer is in flight at a time.
//
// GENERATION IS NOT OPTIONAL. A cell is reused as the queue turns, and a wake
// or a cancel that was in flight when the previous occupant left must not touch
// the value that arrived after it. Every reservation stamps a generation, and a
// commit presented with a stale one is refused rather than applied.
//
// THE LOCK RULE. Every function suffixed _locked expects the owner's lock and
// runs no element operation: reserving hands back an address, the caller moves
// or drops the value with the lock released, and the commit takes the lock
// again. That ordering is why a channel reclaim may no longer run under the
// scheduler, and it is the same ordering here.
typedef struct rt_typed_fifo rt_typed_fifo;

// A reservation: which cell, and which turn of that cell. Presented back to the
// commit so a late caller cannot write into a cell that has moved on.
typedef struct {
    uint64_t index;
    uint64_t generation;
    void* address;
} rt_typed_fifo_ticket;

struct rt_typed_fifo {
    const rt_value_ops* operations;
    rt_slot_control control;
    rt_slot_header* headers;
    uint8_t* payloads;
    uint64_t capacity;
    uint64_t head;
    uint64_t len;
    // The generation counter is per QUEUE, not per cell. A per-cell array would
    // cost eight bytes each -- exactly the word-per-cell price this queue exists
    // to stop paying -- and it would buy nothing, because only one reservation
    // is outstanding at a time. A ticket is therefore valid precisely when it is
    // THE outstanding reservation, which is a stricter test than matching a
    // cell's stored generation, and it costs two words for the whole queue.
    uint64_t next_generation;
    uint64_t reserved_generation;
    uint64_t reserved_index;
    int reserved;
};

// Bytes one queue needs, including its headers, generations and payloads. A
// zero-sized element contributes zero payload bytes, and a zero capacity
// contributes nothing at all.
size_t rt_typed_fifo_alloc_size(const rt_value_ops* operations, uint64_t capacity);

// Lays a queue out inside storage the caller owns -- for a channel that is the
// same single allocation the channel header lives in.
rt_slot_control_status rt_typed_fifo_init(rt_typed_fifo* fifo,
                                          const rt_value_ops* operations,
                                          uint64_t capacity,
                                          void* storage,
                                          size_t storage_size);

uint64_t rt_typed_fifo_len(const rt_typed_fifo* fifo);
uint64_t rt_typed_fifo_capacity(const rt_typed_fifo* fifo);

// True when nothing is queued and nothing is on its way in: no cell holds a
// value and no transfer is outstanding. Handing a value straight to a waiting
// receiver instead of queueing it keeps FIFO order only while this holds. A
// value in flight has already taken its place at the tail, ahead of anything
// handed over now, and `len` does not count it -- a push counts at its commit,
// not at its reservation -- so the length alone answers the wrong question.
int rt_typed_fifo_nothing_queued_locked(const rt_typed_fifo* fifo);

// Reserves the tail cell for a value about to arrive. Returns BUSY when a
// reservation is already outstanding, INVALID_STATE when the queue is full.
rt_slot_control_status rt_typed_fifo_reserve_push_locked(rt_typed_fifo* fifo,
                                                         rt_typed_fifo_ticket* out);

// Publishes the value the caller move-initialized into the reserved address.
rt_slot_control_status rt_typed_fifo_commit_push_locked(rt_typed_fifo* fifo,
                                                        const rt_typed_fifo_ticket* ticket);

// Abandons a reservation whose value never arrived. The cell stays empty.
rt_slot_control_status rt_typed_fifo_abandon_push_locked(rt_typed_fifo* fifo,
                                                         const rt_typed_fifo_ticket* ticket);

// Reserves the head cell for a value about to leave. INVALID_STATE when empty.
rt_slot_control_status rt_typed_fifo_reserve_pop_locked(rt_typed_fifo* fifo,
                                                        rt_typed_fifo_ticket* out);

// Retires the head cell after the caller moved the value out of it.
rt_slot_control_status rt_typed_fifo_commit_pop_locked(rt_typed_fifo* fifo,
                                                       const rt_typed_fifo_ticket* ticket);

// Drops every value still in the queue, exactly once each, and empties it.
// Runs element drops, so it must be called with no owner lock held.
void rt_typed_fifo_drain(rt_typed_fifo* fifo);

#endif
