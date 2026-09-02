#include "rt_channel_lane.h"

// Channel sync lanes (peel B2): try/compat wrappers, the blocking helper
// loops, and close. Same owner-lock protocol as the async fast lanes; the
// control-era wrappers keep the select slow lane's lock order (control ->
// channel shard) until select migrates.

// The claim/move/finish cycle, spelled once for every caller that owns no lock
// of its own: take the lock, claim, release, run the element's move, take the
// lock again, finish. Every public channel entry point below is this shape.
//
// The release in the middle is the window section 7's internal pin exists for:
// the element's move runs there, generated code that may not run under a
// scheduler lock, and a handle dropped on another lane in that window retires
// the last name of the channel while this operation is still inside it. Each
// entry point therefore pins the object for its own duration -- an operation
// hold, counted apart from the handles a program holds.
bool rt_channel_try_send(void* channel, void* src) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || ch->closed) {
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_channel_pin(ch);
    rt_shard_lock(ch_shard);
    rt_channel_put put;
    uint8_t status = rt_channel_try_send_status_owner_locked(ex, ch_shard, ch, &put);
    if (status != 1) {
        rt_shard_unlock(ch_shard);
        rt_channel_unpin(ch);
        return 0;
    }
    rt_shard_unlock(ch_shard);
    rt_value_move_init_detached(ch->ops, put.address, src);
    rt_shard_lock(ch_shard);
    rt_channel_finish_put_owner_locked(ex, ch_shard, ch, &put);
    rt_shard_unlock(ch_shard);
    rt_channel_unpin(ch);
    // The slot is NOT released here. A delivery hands the value to the receiver
    // that now owns the park; ending it from this side destroys the value the
    // receiver is about to take, which is how a delivered value went missing
    // from its slot before this was written down.
    return 1;
}

bool rt_channel_try_recv(void* channel, void* dst) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_channel_pin(ch);
    rt_shard_lock(ch_shard);
    rt_channel_take take;
    uint8_t status = rt_channel_try_recv_status_owner_locked(ex, ch_shard, ch, &take);
    if (status != 1) {
        rt_shard_unlock(ch_shard);
        rt_channel_unpin(ch);
        return 0;
    }
    rt_shard_unlock(ch_shard);
    if (dst != NULL) {
        rt_value_move_init_detached(ch->ops, dst, take.address);
    } else {
        rt_value_drop_in_place_detached(ch->ops, take.address);
    }
    rt_shard_lock(ch_shard);
    rt_channel_finish_take_owner_locked(ex, ch_shard, ch, &take);
    if (take.kind == RT_CHANNEL_TAKE_FROM_SENDER) {
        channel_end_park_locked(ex, ch_shard, ch, &take.slot);
    }
    rt_shard_unlock(ch_shard);
    rt_channel_unpin(ch);
    return 1;
}

// Owner-locked non-blocking recv core: 0 = not ready, 1 = value, 2 = closed,
// 3 = the single-transfer claim was REFUSED (`out_take->refusal_cause` names
// what refused it; the caller counts it, rt_channel_retry.h).
// Caller holds the channel owner's shard lock. Foreign parked senders are
// woken to retry rather than consumed (their value stays under their own
// lock), so a foreign-only sender queue reports "not ready".
//
// `out_bits` is where the value goes and is required. This core is the only
// place that can take a buffered entry or ack a parked sender, and both are
// irreversible: a caller with no sink was not asking a question, it was
// destroying the answer. Callers that do not want the value must still take it
// and then release it (rt_channel_release_payload), which is a decision the
// caller can see and this core cannot make — it does not know what the bits
// own, and it holds a lock under which compiled drop glue must not run.
uint8_t rt_channel_try_recv_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                rt_channel_take* out_take) {
    if (out_take == NULL) {
        panic_msg("async: channel try-recv with nowhere to put the value");
        return 0;
    }
    *out_take = (rt_channel_take){0};
    out_take->refusal_cause = RT_CHANNEL_CLAIM_REFUSAL_COUNT;
    rt_slot_control_status claimed = rt_typed_fifo_reserve_pop_locked(&ch->ring, &out_take->ticket);
    if (claimed == RT_SLOT_CONTROL_OK) {
        out_take->kind = RT_CHANNEL_TAKE_FROM_RING;
        out_take->address = out_take->ticket.address;
        return 1;
    }
    if (claimed == RT_SLOT_CONTROL_BUSY) {
        // The buffer holds an older value than any parked sender's, and its
        // single transfer is in flight -- taking from a sender here would
        // deliver the newer value first. That is a REFUSED claim, not "not
        // ready": the caller counts it against its budget (rt_channel_retry.h).
        out_take->refusal_cause = RT_CHANNEL_CLAIM_REFUSAL_RING_POP;
        return 3;
    }
    waiter cand;
    while (channel_pop_candidate_locked(ch_shard, channel_send_key(ch), &cand)) {
        if (cand.seq == 0) {
            channel_wake_only(ex, ch_shard, &cand);
            continue;
        }
        if (cand.owner_hint == ch->owner_shard_id) {
            rt_task* sender = get_task(ex, cand.task_id);
            if (!channel_candidate_valid(sender, &cand)) {
                continue;
            }
            rt_park_token slot = sender->resume_slot;
            void* from = NULL;
            if (rt_park_pool_reserve_take_locked(&ch->parks, &slot, &from) != RT_SLOT_CONTROL_OK) {
                // Parked holding its own value, or its slot mid-transfer. Its
                // registration has just been consumed by the pop, so nothing
                // else will wake it: wake it here, unacked, to retry, and look
                // at the next sender -- a try must not read "refused" while a
                // later parked sender could still hand its value over.
                (void)wake_task_on_shard_locked(
                    ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                continue;
            }
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_slot = (rt_park_token){0};
            out_take->kind = RT_CHANNEL_TAKE_FROM_SENDER;
            out_take->address = from;
            out_take->slot = slot;
            out_take->sender_task_id = cand.task_id;
            return 1;
        }
        rt_shard_unlock(ch_shard);
        (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, (rt_park_token){0});
        rt_shard_lock(ch_shard);
    }
    if (ch->closed) {
        return 2;
    }
    return 0;
}

// Finishes what the core claimed, with the lock reacquired by the caller after
// the move. Retires the ring cell or the sender's slot, and wakes the sender
// whose value was taken.
void rt_channel_finish_take_owner_locked(rt_executor* ex,
                                         rt_shard* ch_shard,
                                         rt_channel* ch,
                                         const rt_channel_take* take) {
    if (take == NULL) {
        return;
    }
    if (take->kind == RT_CHANNEL_TAKE_FROM_RING) {
        if (rt_typed_fifo_commit_pop_locked(&ch->ring, &take->ticket) != RT_SLOT_CONTROL_OK) {
            panic_msg("async: buffered channel value could not be retired");
        }
        // The commit released the single transfer this take held.
        rt_channel_claim_released_locked(ex, ch_shard, ch);
        // Retiring a cell freed capacity, so a sender parked on a full channel
        // can move in. Without this the space appears and nobody is told: the
        // sender sleeps forever while the buffer has room, which is what a
        // full-channel park looked like before this was restored.
        waiter cand;
        while (channel_buffered(ch) < ch->capacity &&
               channel_pop_candidate_locked(ch_shard, channel_send_key(ch), &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                continue;
            }
            if (cand.owner_hint != ch->owner_shard_id) {
                rt_shard_unlock(ch_shard);
                (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, (rt_park_token){0});
                rt_shard_lock(ch_shard);
                continue;
            }
            rt_task* sender = get_task(ex, cand.task_id);
            if (!channel_candidate_valid(sender, &cand)) {
                continue;
            }
            // Moving the sender's value into the freed cell runs the
            // element's move, and this core is ALSO reached from the
            // control-lane wrappers (select, and a blocking helper called
            // outside a task). A generated operation may not run with control
            // held, and releasing the channel shard does not release control,
            // so where control is held the sender is woken to place its own
            // value instead of having it moved for it.
            rt_park_token sender_slot = sender->resume_slot;
            if (rt_lane_holds_control() || !rt_park_pool_token_is_live(&ch->parks, &sender_slot) ||
                !channel_stage_into_ring_locked(ex, ch_shard, ch, &sender_slot, NULL)) {
                // Parked with nothing staged, the buffer refused, or the move
                // has nowhere legal to run: wake it to retry rather than
                // leaving it asleep, and leave it UNACKED -- nothing of its was
                // delivered, and an ack would tell it otherwise.
                (void)wake_task_on_shard_locked(
                    ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                continue;
            }
            sender->resume_kind = RESUME_CHAN_SEND_ACK;
            sender->resume_slot = (rt_park_token){0};
            (void)wake_task_on_shard_locked(
                ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
            channel_end_park_locked(ex, ch_shard, ch, &sender_slot);
            break;
        }
        return;
    }
    if (take->kind != RT_CHANNEL_TAKE_FROM_SENDER) {
        return;
    }
    (void)rt_park_pool_commit_take_locked(&ch->parks, &take->slot);
    // The commit released the sender's slot, claimed BUSY while the take ran.
    rt_channel_claim_released_locked(ex, ch_shard, ch);
    rt_task* sender = get_task(ex, take->sender_task_id);
    if (sender != NULL) {
        (void)wake_task_on_shard_locked(
            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
    }
}

// Owner-locked non-blocking send core: 0 = not ready, 1 = sent, 2 = closed,
// 3 = the single-transfer claim was REFUSED (`out_put->refusal_cause`).
uint8_t rt_channel_try_send_status_owner_locked(rt_executor* ex,
                                                rt_shard* ch_shard,
                                                rt_channel* ch,
                                                rt_channel_put* out_put) {
    if (out_put == NULL) {
        panic_msg("async: channel try-send with nowhere to take the value from");
        return 0;
    }
    *out_put = (rt_channel_put){0};
    out_put->refusal_cause = RT_CHANNEL_CLAIM_REFUSAL_COUNT;
    if (ch->closed) {
        return 2;
    }
    waiter cand;
    // Rendezvous only while the buffer has nothing older to hand over first;
    // see channel_nothing_queued. With a queue standing, this send joins it.
    while (channel_nothing_queued(ch) &&
           channel_pop_candidate_locked(ch_shard, channel_recv_key(ch), &cand)) {
        if (cand.seq == 0) {
            channel_wake_only(ex, ch_shard, &cand);
            continue;
        }
        // Claim a slot for this receiver before the value exists in it. The
        // pool refuses to end that park while the reservation stands, so a
        // cancellation inside the caller's unlocked window cannot free the
        // bytes the move is writing.
        rt_park_token slot;
        if (rt_park_pool_acquire_locked(&ch->parks, &slot) != RT_SLOT_CONTROL_OK) {
            // Nowhere to stage the value for this receiver. Its registration
            // is already consumed by the pop, so wake it to re-register
            // instead of leaving it parked on a channel that has forgotten it.
            channel_wake_only(ex, ch_shard, &cand);
            break;
        }
        void* address = NULL;
        if (rt_park_pool_reserve_deliver_locked(&ch->parks, &slot, &address) !=
            RT_SLOT_CONTROL_OK) {
            channel_end_park_locked(ex, ch_shard, ch, &slot);
            channel_wake_only(ex, ch_shard, &cand);
            break;
        }
        out_put->kind = RT_CHANNEL_PUT_INTO_PARK;
        out_put->address = address;
        out_put->slot = slot;
        out_put->candidate = cand;
        out_put->has_candidate = 1;
        return 1;
    }
    if (ch->capacity > 0 && channel_buffered(ch) < ch->capacity) {
        rt_slot_control_status reserved =
            rt_typed_fifo_reserve_push_locked(&ch->ring, &out_put->ticket);
        if (reserved == RT_SLOT_CONTROL_OK) {
            out_put->kind = RT_CHANNEL_PUT_INTO_RING;
            out_put->address = out_put->ticket.address;
            return 1;
        }
        if (reserved == RT_SLOT_CONTROL_BUSY) {
            // The single transfer is in flight: not "not ready", REFUSED. The
            // caller counts it against its budget (rt_channel_retry.h).
            out_put->refusal_cause = RT_CHANNEL_CLAIM_REFUSAL_RING_PUSH;
            return 3;
        }
    }
    return 0;
}

// Publishes what the caller moved into the claimed place, with the lock
// reacquired. A park put also hands the receiver its token and wakes it.
void rt_channel_finish_put_owner_locked(rt_executor* ex,
                                        rt_shard* ch_shard,
                                        rt_channel* ch,
                                        rt_channel_put* put) {
    if (put == NULL) {
        return;
    }
    if (put->kind == RT_CHANNEL_PUT_INTO_RING) {
        if (rt_typed_fifo_commit_push_locked(&ch->ring, &put->ticket) != RT_SLOT_CONTROL_OK) {
            panic_msg("async: buffered channel value could not be published");
            return;
        }
        // The commit released the single transfer this put held.
        rt_channel_claim_released_locked(ex, ch_shard, ch);
        channel_wake_parked_receiver_locked(ex, ch_shard, ch);
        return;
    }
    if (put->kind != RT_CHANNEL_PUT_INTO_PARK) {
        return;
    }
    if (rt_park_pool_commit_deliver_locked(&ch->parks, &put->slot) != RT_SLOT_CONTROL_OK) {
        panic_msg("async: staged channel value could not be published");
        return;
    }
    // The delivery commit released the slot's reservation.
    rt_channel_claim_released_locked(ex, ch_shard, ch);
    int pushed = 0;
    if (!channel_deliver_same_shard_locked(
            ex, ch_shard, &put->candidate, RESUME_CHAN_RECV_VALUE, put->slot, 1, &pushed)) {
        // The receiver died while we moved. The value belongs to the channel,
        // so put it in the buffer if there is room, and end the park either way:
        // a successful stage leaves the slot empty and ending it just hands
        // the slot back, while a refused one leaves the value there and ending
        // the park is what destroys it.
        (void)channel_stage_into_ring_locked(ex, ch_shard, ch, &put->slot, NULL);
        channel_end_park_locked(ex, ch_shard, ch, &put->slot);
        return;
    }
    if (!pushed) {
        rt_shard_unlock(ch_shard);
        channel_compat_broadcast_if_needed(ex, 0);
        rt_shard_lock(ch_shard);
    }
}

// Control-era compatibility wrappers for the select slow lane: callers hold
// the control lock and no shard lock; the channel lock nests here.
// These two are called with the CONTROL lock already held, so they cannot run
// the element's move themselves: they claim, and the caller -- which is the
// only one that can release control -- moves and then finishes. That is the
// same claim/move/commit split as everywhere else, drawn one level further out
// because the lock is one level further out.
//
// THE PIN THAT COVERS THEM BELONGS TO THE CALLER, and each of the four checks
// that it exists. They cannot take one per call: the window that needs the hold
// is the caller's release of the control lock BETWEEN a claim and its finish,
// and a pin taken and dropped inside one call is gone before that window opens.
// So the contract is the caller's, and a caller that forgot it is refused here
// rather than at the use-after-free that would follow.
uint8_t rt_channel_claim_recv_locked(rt_executor* ex, void* channel, rt_channel_take* out_take) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || out_take == NULL) {
        return 0;
    }
    rt_channel_assert_pinned(ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    uint8_t status = rt_channel_try_recv_status_owner_locked(ex, ch_shard, ch, out_take);
    rt_shard_unlock(ch_shard);
    return status;
}

void rt_channel_finish_recv_locked(rt_executor* ex, void* channel, const rt_channel_take* take) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || take == NULL) {
        return;
    }
    rt_channel_assert_pinned(ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    rt_channel_finish_take_owner_locked(ex, ch_shard, ch, take);
    if (take->kind == RT_CHANNEL_TAKE_FROM_SENDER) {
        channel_end_park_locked(ex, ch_shard, ch, &take->slot);
    }
    rt_shard_unlock(ch_shard);
}

uint8_t rt_channel_claim_send_locked(rt_executor* ex, void* channel, rt_channel_put* out_put) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || out_put == NULL) {
        return 0;
    }
    rt_channel_assert_pinned(ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    uint8_t status = rt_channel_try_send_status_owner_locked(ex, ch_shard, ch, out_put);
    rt_shard_unlock(ch_shard);
    return status;
}

void rt_channel_finish_send_locked(rt_executor* ex, void* channel, rt_channel_put* put) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || put == NULL) {
        return;
    }
    rt_channel_assert_pinned(ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    rt_channel_finish_put_owner_locked(ex, ch_shard, ch, put);
    rt_shard_unlock(ch_shard);
    // Not released here: see rt_channel_try_send. The receiver owns the park
    // the value was delivered into.
}

static void channel_blocking_yield(void) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (rt_wait_current_worker_wakeup(ex, current)) {
        return;
    }
    void* task = checkpoint();
    if (task == NULL) {
        return;
    }
    rt_task_await(task, NULL, NULL);
}

void rt_channel_send_blocking(void* channel, void* src) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return;
    }
    rt_async_debug_printf("async chan send start ch=%p\n", (void*)ch);
    // One operation hold for the whole helper, including the retries: the
    // control-lane loop below claims, RELEASES control to run the element's
    // move, and retakes it to finish, and the channel must not be reclaimed in
    // between.
    rt_channel_pin(ch);
    if (rt_current_task() != NULL) {
        rt_trace_channel_task_blocking_send();
        while (!rt_channel_send(channel, src)) {
            if (current_task_cancelled(ex)) {
                pending_key = waker_none();
                rt_channel_unpin(ch);
                return;
            }
            channel_blocking_yield();
        }
        pending_key = waker_none();
        rt_async_debug_printf("async chan send ok ch=%p\n", (void*)ch);
        rt_channel_unpin(ch);
        return;
    }
    for (;;) {
        rt_control_lock(ex);
        rt_channel_put put;
        uint8_t status = rt_channel_claim_send_locked(ex, channel, &put);
        rt_control_unlock(ex);
        if (status == 1) {
            rt_value_move_init_detached(ch->ops, put.address, src);
            rt_control_lock(ex);
            rt_channel_finish_send_locked(ex, channel, &put);
            rt_control_unlock(ex);
            rt_async_debug_printf("async chan send ok ch=%p\n", (void*)ch);
            rt_channel_unpin(ch);
            return;
        }
        if (status == 2) {
            rt_async_debug_printf("async chan send closed ch=%p\n", (void*)ch);
            rt_channel_unpin(ch);
            panic_msg("send on closed channel");
            return;
        }
        channel_blocking_yield();
    }
}

uint8_t rt_channel_recv_blocking(void* channel, void* dst) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 2;
    }
    rt_async_debug_printf("async chan recv start ch=%p\n", (void*)ch);
    // Pinned for the same reason the send helper is: the control-lane loop
    // below releases control across the element's move.
    rt_channel_pin(ch);
    if (rt_current_task() != NULL) {
        rt_trace_channel_task_blocking_recv();
        for (;;) {
            uint8_t status = rt_channel_recv(channel, dst);
            if (status != 0) {
                pending_key = waker_none();
                if (status == 1) {
                    rt_async_debug_printf("async chan recv ok ch=%p\n", (void*)ch);
                } else if (status == 2) {
                    rt_async_debug_printf("async chan recv closed ch=%p\n", (void*)ch);
                }
                rt_channel_unpin(ch);
                return status;
            }
            if (current_task_cancelled(ex)) {
                pending_key = waker_none();
                rt_channel_unpin(ch);
                return 2;
            }
            channel_blocking_yield();
        }
    }
    for (;;) {
        rt_control_lock(ex);
        rt_channel_take take;
        uint8_t status = rt_channel_claim_recv_locked(ex, channel, &take);
        rt_control_unlock(ex);
        if (status == 1 || status == 2) {
            if (status == 1) {
                if (dst != NULL) {
                    rt_value_move_init_detached(ch->ops, dst, take.address);
                } else {
                    rt_value_drop_in_place_detached(ch->ops, take.address);
                }
                rt_control_lock(ex);
                rt_channel_finish_recv_locked(ex, channel, &take);
                rt_control_unlock(ex);
                rt_async_debug_printf("async chan recv ok ch=%p\n", (void*)ch);
            } else if (status == 2) {
                rt_async_debug_printf("async chan recv closed ch=%p\n", (void*)ch);
            }
            rt_channel_unpin(ch);
            return status;
        }
        channel_blocking_yield();
    }
}

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
