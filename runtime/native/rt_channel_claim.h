#ifndef SURGE_RUNTIME_NATIVE_RT_CHANNEL_CLAIM_H
#define SURGE_RUNTIME_NATIVE_RT_CHANNEL_CLAIM_H

#include "rt_async_internal.h"

// The channel's owner-visible RECEIVE CLAIM (Close-wins, owner-lane order).
//
// A rendezvous pops the oldest parked receiver out of the FIFO and then
// releases the owner lane to move the value. Popped, the receiver is in no
// store, and a close crossing that window used to settle every parked peer
// except the one about to be handed a value -- which then received it on a
// closed channel. So removing a receiver from the FIFO is no longer the end
// of the channel's knowledge of it: the pop moves it, atomically under the
// same lane, into this claim, and the claim stays owner-visible until the
// sender retires it by commit or abort, or close settles it first.
//
// The order the owner lane sees is the order that decides (EPICS_CLOSEOUT_PLAN,
// "Close-wins"): commit then close keeps the delivered value; close then
// commit refuses the commit, wakes the receiver as closed, and destroys the
// payload exactly once; an abort after close finds nothing left to requeue.
// While a claim is out, no other send is admitted -- not to the ring either --
// so the claimed receiver's value cannot be overtaken; a send that meets an
// open claim is REFUSED and counts against its retry budget
// (rt_channel_retry.h), and retiring the claim is the release that wakes it.
typedef struct {
    // The claimed receiver's registration, as popped.
    waiter receiver;
    // A sender's rendezvous window is open on it.
    uint8_t active;
    // Close settled the receiver while the window was open; the sender's
    // commit or abort finds this and retires it.
    uint8_t close_won;
} rt_channel_recv_claim;

// All of these run with the channel owner's shard lock held.
// channel_recv_claim_blocks, the admission check, is inline in
// rt_channel_lane.h beside the struct it reads.
int channel_recv_claim_open_locked(rt_channel* ch, const waiter* receiver);
// Commit: non-zero when the claim is still the sender's to deliver on; zero
// when close settled the receiver first (the sender destroys its payload).
int channel_recv_claim_take_locked(rt_executor* ex,
                                   rt_shard* ch_shard,
                                   rt_channel* ch,
                                   const waiter* receiver);
// Abort: the sender delivers nothing. A receiver still parked goes back to
// the HEAD of its FIFO -- it was the oldest, and it has lost nothing.
void channel_recv_claim_abort_locked(rt_executor* ex,
                                     rt_shard* ch_shard,
                                     rt_channel* ch,
                                     const waiter* receiver);
// Close: settle the claimed receiver as closed, exactly once.
void channel_recv_claim_close_locked(rt_executor* ex, rt_shard* ch_shard, rt_channel* ch);

// Re-inserts a popped registration at the head of its key's FIFO and gives
// it back the pin the pop retired.
void channel_push_candidate_front_locked(rt_shard* owner_shard, const waiter* w);

void rt_channel_trace_recovery_dead_receiver(void);
void rt_channel_trace_value_destroyed_in_recovery(void);
size_t rt_channel_claim_trace_append(char* buf, size_t* pos, size_t cap);

#endif // SURGE_RUNTIME_NATIVE_RT_CHANNEL_CLAIM_H
