#include "rt_channel_lane.h"

// Close, moved out of rt_channel_sync.c with no change in the walk: what it
// gained is the receive claim (rt_channel_claim.h). A receiver popped for a
// rendezvous is in no FIFO, so the four-key drain below cannot find it; the
// claim is where the channel still knows it, and it is settled FIRST -- it
// was the head of its FIFO, and it is the one a late commit would otherwise
// hand a value on a closed channel.
void rt_channel_close(void* channel) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || ch->closed) {
        return;
    }
    rt_async_debug_printf("async chan close ch=%p\n", (void*)ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    // Four keys, not two: a task whose claim retries ran out is parked on the
    // channel's RETRY key (rt_channel_retry.h), a registration close must
    // settle exactly like the sender or receiver it is.
    waker_key keys[4] = {channel_recv_key(ch),
                         channel_send_key(ch),
                         channel_recv_retry_key(ch),
                         channel_send_retry_key(ch)};
    const uint8_t resumes[4] = {RESUME_CHAN_RECV_CLOSED,
                                RESUME_CHAN_SEND_CLOSED,
                                RESUME_CHAN_RECV_CLOSED,
                                RESUME_CHAN_SEND_CLOSED};
#ifndef RV2_DEBT_277_CLOSE_NEGATIVE_CONTROL
    const size_t key_count = 4;
#else
    const size_t key_count = 2;
#endif
    // Closing settles every parked peer, and each pop below retires the pin its
    // registration held. The last one can be the last hold on the object, so
    // this operation holds one of its own for the whole walk -- otherwise the
    // loop would be draining a channel it had just handed to the reclaim.
    rt_channel_pin(ch);
    rt_shard_lock(ch_shard);
    if (ch->closed) {
        rt_shard_unlock(ch_shard);
        rt_channel_unpin(ch);
        return;
    }
    ch->closed = 1;
    channel_recv_claim_close_locked(ex, ch_shard, ch);
    for (size_t k = 0; k < key_count; k++) {
        waiter cand;
        while (channel_pop_candidate_locked(ch_shard, keys[k], &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                continue;
            }
            if (cand.owner_hint == ch->owner_shard_id) {
                int pushed = 0;
                int live = channel_deliver_same_shard_locked(
                    ex, ch_shard, &cand, resumes[k], (rt_park_token){0}, 1, &pushed);
                if (live && !pushed) {
                    rt_shard_unlock(ch_shard);
                    channel_compat_broadcast_if_needed(ex, 0);
                    rt_shard_lock(ch_shard);
                }
                continue;
            }
            rt_shard_unlock(ch_shard);
            (void)channel_deliver_foreign(ex, &cand, resumes[k], (rt_park_token){0});
            rt_shard_lock(ch_shard);
        }
    }
    rt_shard_unlock(ch_shard);
    rt_channel_unpin(ch);
}
