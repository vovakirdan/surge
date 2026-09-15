#ifndef SURGE_RT_CHANNEL_REFILL_H
#define SURGE_RT_CHANNEL_REFILL_H

// Refilling a buffered channel's ring from one same-shard parked sender.
//
// Both places that free a ring cell -- an async receive and a synchronous
// take -- hand the space to the next parked sender the same way. They differ
// only in what they do next, so the transfer answers which of three things
// happened and the caller keeps its own loop control.

#include "rt_channel_lane.h"

typedef enum {
    // The sender was woken, unacked, to retry with the value it still holds.
    RT_CHANNEL_REFILL_RETRY = 0,
    // The ring refused the staged value; the consumed registration was woken,
    // unacked, so the sender places its own value.
    RT_CHANNEL_REFILL_REFUSED = 1,
    // The value moved into the ring and the sender was acked.
    RT_CHANNEL_REFILL_STAGED = 2,
} rt_channel_refill_status;

// Caller holds ch_shard's lock and has validated `sender` against its popped
// waiter. `retry_only` forces the retry answer for a caller that may not run a
// detached move (it holds the control lane).
static inline rt_channel_refill_status channel_refill_from_parked_sender_locked(
    rt_executor* ex, rt_shard* ch_shard, rt_channel* ch, rt_task* sender, int retry_only) {
    rt_park_token sender_slot = sender->resume_slot;
    if (retry_only || !rt_park_pool_token_is_live(&ch->parks, &sender_slot)) {
        // Parked holding its own value (the pool was full when it parked), or
        // not movable from here. Do NOT ack it: an ack says "your send
        // completed", and nothing was delivered.
        (void)wake_task_on_shard_locked(
            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
        return RT_CHANNEL_REFILL_RETRY;
    }
    // The detached move can cancel and await the sender. Keep its mailbox
    // alive until the final ack or wake below.
    task_add_ref(sender);
    if (!channel_stage_into_ring_locked(ex, ch_shard, ch, &sender_slot, NULL)) {
        // The ring refused: its single transfer is in flight, or a receiver is
        // taking this very value. The pop consumed this sender's registration,
        // so nothing else will ever call on it; leaving it parked is a task
        // stranded on a channel that has forgotten it.
        (void)wake_task_on_shard_locked(
            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
        task_release_lane_aware(ex, sender);
        return RT_CHANNEL_REFILL_REFUSED;
    }
    sender->resume_kind = RESUME_CHAN_SEND_ACK;
    sender->resume_slot = (rt_park_token){0};
    // A parked sender has a waiter entry, so the leaf enqueues it.
    (void)wake_task_on_shard_locked(
        ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
    task_release_lane_aware(ex, sender);
    channel_end_park_locked(ex, ch_shard, ch, &sender_slot);
    return RT_CHANNEL_REFILL_STAGED;
}

#endif
