#include "rt_channel_lane.h"

// Async channel SEND lane. Split out of rt_async_channel.c with no change in
// behaviour: the receive loop and the constructor stay there, the send loop
// and its yield handshake live here. The one helper both loops share --
// staging a value into a park slot the channel owns -- is
// rt_channel_stage_locked, declared in rt_channel_lane.h and defined beside the
// receive loop; nothing else crosses the seam.

static int prepare_channel_send_yield(rt_task* task) {
    if (task == NULL) {
        return 0;
    }
    // The compiler emits this helper only when the ready path immediately
    // reaches a recv suspend point. Re-polling consumes this ack without
    // repeating the already completed send.
    task->resume_kind = RESUME_CHAN_SEND_ACK;
    pending_key = waker_none();
    rt_trace_channel_handoff_yield();
    return 1;
}

static bool rt_channel_send_inner(void* channel, void* src, int yield_after_handoff) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 1;
    }
    if (rt_current_task_id() == 0) {
        panic_msg("async channel send outside task");
        return 1;
    }
    rt_task* task = rt_current_task();
    if (task == NULL) {
        panic_msg("async: missing current task");
        return 1;
    }
    if (task_cancelled_load(task) != 0) {
        rt_channel_retry_reset(task);
        task->resume_kind = RESUME_NONE;
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard* own_shard = rt_task_owner_shard(ex, task);
    waker_key recv_key = channel_recv_key(ch);
    int same_shard_worker = tls_worker_ctx != NULL && tls_worker_ctx->shard == ch_shard;
    for (;;) {
        // Consume-or-arm under our owner lock: peers ack a registered send
        // under this lock, so checking and re-arming the mailbox anywhere
        // else races the ack and can overwrite it (stranding both sides).
        // Bumping park_seq on consume retires any entry this task still has
        // in the store, so a later pop of it validates false.
        rt_shard_lock(own_shard);
        uint8_t rk = task->resume_kind;
        if (rk == RESUME_CHAN_SEND_ACK || rk == RESUME_CHAN_SEND_CLOSED) {
            task->resume_kind = RESUME_NONE;
            task->park_seq++;
            rt_shard_unlock(own_shard);
            rt_channel_retry_reset(task);
            if (rk == RESUME_CHAN_SEND_CLOSED) {
                panic_msg("send on closed channel");
            }
            return 1;
        }
        task->resume_kind = RESUME_NONE;
        rt_shard_unlock(own_shard);
        rt_shard_lock(ch_shard);
        if (ch->closed) {
            rt_shard_unlock(ch_shard);
            rt_channel_retry_reset(task);
            panic_msg("send on closed channel");
            return 1;
        }
        // Where this send's value IS. A park that did not complete moved it
        // into a slot the CHANNEL owns, and that move consumed `src`: reading
        // `src` a second time would give one heap value two owners. So once a
        // slot exists, every path below sends from the slot and never from the
        // husk `src` has become.
        rt_park_token staged = task->resume_slot;
        int staged_live = rt_park_pool_token_is_live(&ch->parks, &staged);
        waiter cand;
        // A rendezvous jumps the queue by construction, so it is legal only
        // when there is no queue: a value handed to a parked receiver while
        // older values sit in the buffer arrives before them. That is not a
        // race between locks -- it is what a candidate-first order means once
        // a receiver can park with a full buffer, which it can, because the
        // buffer refuses a pop while its single transfer is in flight.
        if (channel_recv_claim_blocks(ch)) {
            // Another sender's rendezvous is out on the channel's oldest
            // receiver (rt_channel_claim.h): nothing is admitted -- not the
            // ring either -- until it is retired, or the value that receiver
            // was promised would be overtaken. A REFUSED claim, counted; the
            // retirement is the release that wakes an exhausted retrier.
            rt_channel_wait_after_claim_refusal_locked(
                ch_shard, ch, task, RT_CHANNEL_RETRY_SEND, RT_CHANNEL_CLAIM_REFUSAL_RENDEZVOUS);
            return 0;
        }
        if (channel_nothing_queued(ch) && channel_pop_candidate_locked(ch_shard, recv_key, &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                rt_shard_unlock(ch_shard);
                continue;
            }
            // The pop and the claim are ONE hold of the lane: from here the
            // popped receiver is the channel's CLAIM, owner-visible while the
            // staging below releases the lane to move the value, so a close
            // crossing that window settles it (Close-wins). Opening it after
            // the stage -- the first port did -- left a receiver in no store
            // for the length of a move.
#ifndef RV2_CLAIM_OPEN_AFTER_STAGE_NEGATIVE_CONTROL
            (void)channel_recv_claim_open_locked(ch, &cand);
#endif
            if (!staged_live && !rt_channel_stage_locked(ex, ch_shard, ch, src, &staged)) {
                // No slot to stage into. The candidate has already been POPPED,
                // so dropping it here would strand a receiver that is still
                // parked -- which showed up as a violated FIFO order rather
                // than as a hang, because the next sender delivered to a later
                // waiter first. It was the oldest: the abort puts it back at
                // the head (or finds close settled it meanwhile).
#ifdef RV2_CLAIM_OPEN_AFTER_STAGE_NEGATIVE_CONTROL
                channel_push_candidate_front_locked(ch_shard, &cand);
#else
                channel_recv_claim_abort_locked(ex, ch_shard, ch, &cand);
#endif
                rt_shard_unlock(ch_shard);
                continue;
            }
#ifdef RV2_CLAIM_OPEN_AFTER_STAGE_NEGATIVE_CONTROL
            // Rule 13: the claim opens only after the lane was released and
            // retaken across the move.
            (void)channel_recv_claim_open_locked(ch, &cand);
#endif
            // The slot belongs to the handover from here: every path below
            // delivers it, moves it into the buffer, or keeps it staged, so
            // this task must stop naming it before any of them runs.
            task->resume_slot = (rt_park_token){0};
            if (cand.owner_hint == ch->owner_shard_id) {
                int no_signal = yield_after_handoff && same_shard_worker;
                int pushed = 0;
                if (!channel_recv_claim_take_locked(ex, ch_shard, ch, &cand)) {
                    // Close won: the receiver was already woken as closed. The
                    // payload is destroyed exactly once, here, and the next
                    // pass answers "send on closed channel".
                    channel_end_park_locked(ex, ch_shard, ch, &staged);
                    rt_shard_unlock(ch_shard);
                    continue;
                }
                int live = channel_deliver_same_shard_locked(ex,
                                                             ch_shard,
                                                             &cand,
                                                             RESUME_CHAN_RECV_VALUE,
                                                             staged,
                                                             no_signal ? 0 : 1,
                                                             &pushed);
                if (!live) {
                    // The candidate died while we staged. The value is in a
                    // slot this channel owns: give it to the buffer if there
                    // is room, and otherwise KEEP it staged -- this send has
                    // taken no position, the next pass parks holding its slot
                    // or meets the next receiver. Destroying it is never a
                    // legal recovery (RV2-DEBT-276): `src` is a husk by now.
                    rt_channel_trace_recovery_dead_receiver();
                    if (channel_stage_into_ring_locked(ex, ch_shard, ch, &staged, NULL)) {
                        channel_end_park_locked(ex, ch_shard, ch, &staged);
                        rt_shard_unlock(ch_shard);
#ifndef RV2_DEBT_277_RECOVERY_RESET_NEGATIVE_CONTROL
                        // Sent, by way of the buffer: this operation is
                        // complete, and its refusal count goes with it.
                        rt_channel_retry_reset(task);
#endif
                        return 1;
                    }
#ifdef RV2_DEBT_276_NEGATIVE_CONTROL
                    // Rule 13: the pre-fix recovery -- destroy the staged
                    // value and re-enter the loop with an emptied `src`.
                    rt_channel_trace_value_destroyed_in_recovery();
                    channel_end_park_locked(ex, ch_shard, ch, &staged);
#else
                    task->resume_slot = staged;
#endif
                    rt_shard_unlock(ch_shard);
                    continue;
                }
                rt_shard_unlock(ch_shard);
                channel_compat_broadcast_if_needed(ex, pushed);
                rt_channel_retry_reset(task);
                if (yield_after_handoff && prepare_channel_send_yield(task)) {
                    return 0;
                }
                return 1;
            }
            // Foreign receiver: the commit point is the take, under this lane
            // -- the owner-lane order is what decides against a close.
            if (!channel_recv_claim_take_locked(ex, ch_shard, ch, &cand)) {
                channel_end_park_locked(ex, ch_shard, ch, &staged);
                rt_shard_unlock(ch_shard);
                continue;
            }
            rt_shard_unlock(ch_shard);
            if (!channel_deliver_foreign(ex, &cand, RESUME_CHAN_RECV_VALUE, staged)) {
                rt_shard_lock(ch_shard);
                rt_channel_trace_recovery_dead_receiver();
                if (channel_stage_into_ring_locked(ex, ch_shard, ch, &staged, NULL)) {
                    channel_end_park_locked(ex, ch_shard, ch, &staged);
                    rt_shard_unlock(ch_shard);
#ifndef RV2_DEBT_277_RECOVERY_RESET_NEGATIVE_CONTROL
                    rt_channel_retry_reset(task);
#endif
                    return 1;
                }
#ifdef RV2_DEBT_276_NEGATIVE_CONTROL
                rt_channel_trace_value_destroyed_in_recovery();
                channel_end_park_locked(ex, ch_shard, ch, &staged);
#else
                task->resume_slot = staged;
#endif
                rt_shard_unlock(ch_shard);
                continue;
            }
            rt_channel_retry_reset(task);
            if (yield_after_handoff && prepare_channel_send_yield(task)) {
                return 0;
            }
            return 1;
        }
        if (ch->capacity > 0 && channel_buffered(ch) < ch->capacity && staged_live) {
            // The value is in a slot already: move it from there into the
            // buffer. A refusal here means the buffer's single transfer is in
            // flight, or a receiver is taking this very value -- a REFUSED
            // claim either way, counted against the send's budget; exhausted,
            // it parks on the retry key with the slot still held.
            rt_channel_claim_refusal_cause refusal;
            if (channel_stage_into_ring_locked(ex, ch_shard, ch, &staged, &refusal)) {
                task->resume_slot = (rt_park_token){0};
                channel_end_park_locked(ex, ch_shard, ch, &staged);
                rt_shard_unlock(ch_shard);
                rt_channel_retry_reset(task);
                return 1;
            }
            if (refusal != RT_CHANNEL_CLAIM_REFUSAL_COUNT) {
                rt_channel_wait_after_claim_refusal_locked(
                    ch_shard, ch, task, RT_CHANNEL_RETRY_SEND, refusal);
                return 0;
            }
        } else if (ch->capacity > 0 && channel_buffered(ch) < ch->capacity) {
            rt_typed_fifo_ticket ticket;
            rt_slot_control_status reserved = rt_typed_fifo_reserve_push_locked(&ch->ring, &ticket);
            if (reserved == RT_SLOT_CONTROL_BUSY) {
                // The buffer has room; what it does not have at this instant is
                // its single transfer, held by another task's move. Parking on
                // readiness would block a send the buffer can take -- and a
                // buffered send that fits must not block -- so the claim is
                // REFUSED and counted: seven times the send comes back and
                // looks again, the eighth parks on the channel's retry key
                // until the claim that refused it is released
                // (rt_channel_retry.h, RV2-DEBT-277).
                rt_channel_wait_after_claim_refusal_locked(
                    ch_shard, ch, task, RT_CHANNEL_RETRY_SEND, RT_CHANNEL_CLAIM_REFUSAL_RING_PUSH);
                return 0;
            }
            if (reserved == RT_SLOT_CONTROL_OK) {
                rt_shard_unlock(ch_shard);
                rt_value_move_init_detached(ch->ops, ticket.address, src);
                rt_shard_lock(ch_shard);
                if (rt_typed_fifo_commit_push_locked(&ch->ring, &ticket) != RT_SLOT_CONTROL_OK) {
                    rt_shard_unlock(ch_shard);
                    panic_msg("async: buffered channel value could not be published");
                    return 1;
                }
                // The commit released the single transfer this send held.
                rt_channel_claim_released_locked(ex, ch_shard, ch);
                channel_wake_parked_receiver_locked(ex, ch_shard, ch);
                rt_shard_unlock(ch_shard);
                rt_channel_retry_reset(task);
                return 1;
            }
        }
        // Parking with the value still in our own frame is what the old word
        // mailbox did, and it is exactly what a longjmp out of this poll would
        // lose. Stage into a slot the CHANNEL owns first, and park holding
        // only the token: a receiver refilling the ring later moves it out of
        // there, and a cancellation destroys it through the pool's drain. A
        // re-entry that already owns a slot keeps it -- staging again would
        // strand the first one, and there is nothing left in `src` to stage.
        //
        // Every park slot may also be taken outright. Park ANYWAY with nothing
        // staged: the sender still owns its value, and a receiver that pops it
        // finds no slot and wakes it to retry. Returning without parking was
        // the first version of this, and it hung for the plainer reason that
        // nothing was registered to wake.
        if (!staged_live) {
            staged = (rt_park_token){0};
            if (rt_channel_stage_locked(ex, ch_shard, ch, src, &staged)) {
                // Staging released the lock across the move, and a receiver
                // may have parked inside that window -- it would have found no
                // value and no registration from us, because ours comes after.
                // Both parked is a deadlock, so look again before parking. The
                // value is in a slot now, so the next pass reaches the park
                // with the lock held continuously since its last look at the
                // waiter store, and this window cannot open twice.
                task->resume_slot = staged;
                rt_shard_unlock(ch_shard);
                continue;
            }
        }
        task->resume_slot = staged;
        waker_key send_key = channel_send_key(ch);
        channel_park_prepare_locked(ch_shard, task, send_key);
        rt_shard_unlock(ch_shard);
        pending_key = send_key;
        return 0;
    }
}

// The operation's own hold on the object, and the reason the entry points below
// are wrappers rather than the loops themselves.
//
// The loop RELEASES the owner shard lock at every step that runs generated
// code, because no element move or drop may run under a scheduler lock:
// rt_channel_stage_locked's move into a park slot, the buffered push beside it,
// each of the four takes in the receive loop, channel_end_park_locked's drop
// and channel_stage_into_ring_locked's move in rt_channel_lane.h, and the three
// helpers that release the owner lock to take a peer's. A handle dropped on
// another lane inside any of those windows retires the last NAME of the channel
// while this operation is still inside it, and without a pin the reclaim would
// free the storage the move is writing into.
//
// One pin for the whole operation covers every window by construction. A pin
// per window would have to be paired down each of the loop's dozen early
// returns, and the first one missed is either a channel that leaks forever or
// exactly the free this is here to stop.
static bool channel_send_pinned(void* channel, void* src, int yield_after_handoff) {
    rt_channel_pin(channel);
    bool done = rt_channel_send_inner(channel, src, yield_after_handoff);
    rt_channel_unpin(channel);
    return done;
}

bool rt_channel_send(void* channel, void* src) {
    return channel_send_pinned(channel, src, 0);
}

bool rt_channel_send_yield(void* channel, void* src) {
    return channel_send_pinned(channel, src, 1);
}
