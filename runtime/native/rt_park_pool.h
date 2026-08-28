#ifndef RT_PARK_POOL_H
#define RT_PARK_POOL_H

#include <stddef.h>
#include <stdint.h>

#include "rt_slot_control.h"
#include "rt_typed_carrier_abi.generated.h"

// The park slots a channel hands out to tasks waiting on it.
//
// WHY THE CHANNEL OWNS THESE AND THE TASK DOES NOT. A parked task's poll
// function can leave by longjmp, so anything the task itself owned across the
// park would be lost with no one left to free it. The channel outlives every
// park on it, so it owns the bytes, the lifecycle header and the cleanup; the
// task carries only a capability token naming a slot it may act on. That also
// keeps the mailbox control-only: a token is three integers, not a payload.
//
// WHY EVERY SLOT CARRIES A GENERATION, WHEN THE QUEUE NEEDS ONLY ONE. Many
// tasks are parked at once, so many slots are live at once, and a slot released
// by one task is handed straight to the next. A wake or a cancel that was
// already in flight when the first task left must not touch the second task's
// slot -- and index alone cannot tell those two apart, because it is the same
// index. The generation is what separates them, so it is per slot here even
// though the queue next door needs only one for the whole ring.
//
// The lock rule is the queue's: functions suffixed _locked expect the channel's
// lock and run no element operation. A value is moved in or out with the lock
// released, between a reserve and a commit.
typedef struct rt_park_pool rt_park_pool;

// A capability to act on one slot, for one park. Three integers, copied freely,
// and inert the moment that park ends.
typedef struct {
    const rt_park_pool* owner;
    uint64_t index;
    uint64_t generation;
} rt_park_token;

// What a slot holds, from the pool's point of view. The task's own view is the
// token, which cannot see any of this.
typedef struct {
    uint64_t generation;
    uint32_t next_free;
    uint8_t live;
    // Reservation state is PER SLOT, not per pool.
    //
    // A first draft put one reservation on the whole pool, reasoning from the
    // queue next door where exactly one typed transfer is in flight. That
    // reasoning does not carry: a queue has one ring and its cells are taken in
    // order, while a pool hands every parked task a slot of its OWN, and two
    // tasks on different slots have nothing to serialise. The multi-shard
    // channel test found it immediately -- a delivery to one task refused
    // because an unrelated task was mid-transfer on another slot.
    uint8_t reserved;
} rt_park_slot;

struct rt_park_pool {
    const rt_value_ops* operations;
    rt_slot_control control;
    rt_slot_header* headers;
    rt_park_slot* slots;
    uint8_t* payloads;
    uint64_t capacity;
    uint64_t live;
    uint64_t next_generation;
    uint32_t first_free;
};

size_t rt_park_pool_alloc_size(const rt_value_ops* operations, uint64_t capacity);

rt_slot_control_status rt_park_pool_init(rt_park_pool* pool,
                                         const rt_value_ops* operations,
                                         uint64_t capacity,
                                         void* storage,
                                         size_t storage_size);

uint64_t rt_park_pool_live(const rt_park_pool* pool);

// Takes a slot for a task about to park. INVALID_STATE when the pool is full,
// which is a condition the caller handles rather than a defect.
rt_slot_control_status rt_park_pool_acquire_locked(rt_park_pool* pool, rt_park_token* out);

// True while the token still names the park it was issued for. Every other
// entry point checks this itself; it is exported because a caller holding a
// token from before a suspension usually wants to ask before it acts.
int rt_park_pool_token_is_live(const rt_park_pool* pool, const rt_park_token* token);

// Reserves the token's slot for a value about to be delivered into it, and
// hands back where to write. STALE when the park has ended, BUSY when another
// transfer is in flight, INVALID_STATE when the slot already holds a value.
rt_slot_control_status rt_park_pool_reserve_deliver_locked(rt_park_pool* pool,
                                                           const rt_park_token* token,
                                                           void** out_address);

// Publishes the delivered value.
rt_slot_control_status rt_park_pool_commit_deliver_locked(rt_park_pool* pool,
                                                          const rt_park_token* token);

// Abandons a delivery whose value never arrived; the slot stays empty.
rt_slot_control_status rt_park_pool_abandon_deliver_locked(rt_park_pool* pool,
                                                           const rt_park_token* token);

// Reserves the token's slot for the woken task to move its value out of.
rt_slot_control_status rt_park_pool_reserve_take_locked(rt_park_pool* pool,
                                                        const rt_park_token* token,
                                                        void** out_address);

rt_slot_control_status rt_park_pool_commit_take_locked(rt_park_pool* pool,
                                                       const rt_park_token* token);

// Begins ending a park, with the owner's lock held: refuses a stale token or a
// slot with a transfer in flight, marks the slot so nothing else may act on it,
// retires the value's lifecycle through the control, and hands back the bytes
// to destroy -- NULL when the slot is already empty, which is the ordinary case
// for a park whose value was delivered.
//
// The whole slot-control cycle happens HERE, before the callback, exactly as it
// did when a release was one call: the header is cleared first so a re-entrant
// caller cannot find the same value to destroy twice. What the caller gets is
// bytes nobody else can reach -- the slot is reserved and still live, so it is
// neither deliverable nor acquirable -- and it destroys them with the lock
// RELEASED, then calls finish.
rt_slot_control_status
rt_park_pool_begin_release_locked(rt_park_pool* pool, const rt_park_token* token, void** out_value);

// Ends the park the matching begin started: returns the slot to the free list.
// Owner's lock held.
rt_slot_control_status rt_park_pool_finish_release_locked(rt_park_pool* pool,
                                                          const rt_park_token* token);

// Both halves at once, for an owner nothing else can touch: the unit stand, and
// a quiescent teardown. A LIVE channel uses the pair above, because this one
// runs the drop and the free-list write in the same breath and only one of
// those may hold the owner's lock.
rt_slot_control_status rt_park_pool_release(rt_park_pool* pool, const rt_park_token* token);

// Destroys every value still held and ends every park. Runs element drops, so
// it must be called with no owner lock held.
void rt_park_pool_drain(rt_park_pool* pool);

// The two halves of a teardown, for an owner whose destruction other lanes can
// observe. A drain ends one park and destroys its value before it looks at the
// next, which leaves the pool half torn down while an element's drop runs; an
// owner that must be quiescent before the first drop ends EVERY park first.
//
// Detach runs no element operation: it ends every park, moves every slot's
// generation forward so no token issued before can name a slot again, and
// empties the free list so nothing can acquire one. The payload bytes stay
// where they are; the initialized headers are what the drop half reads.
void rt_park_pool_detach_all_locked(rt_park_pool* pool);

// Destroys the values the matching detach left behind, exactly once each. Runs
// element drops, so no owner lock may be held.
void rt_park_pool_drop_detached(rt_park_pool* pool);

#endif
