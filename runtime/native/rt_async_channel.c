#include "rt_channel_lane.h"

// Async channel fast lanes (peel B2): send and recv run on the channel
// owner's shard lane; see rt_channel_lane.h for the shared protocol.

uint32_t rt_channel_owner_shard_id(const rt_channel* ch) {
    return ch != NULL ? ch->owner_shard_id : 0;
}

// Binds a channel created outside task context (e.g. a transport dispatcher
// minting an owner-side channel) to its owning shard.
void rt_channel_bind_owner_shard(void* channel, uint32_t shard_id) {
    rt_channel* ch = channel_from_handle(channel);
    if (ch != NULL) {
        ch->owner_shard_id = shard_id;
    }
}

// The lookup is emitted by the compiler, so a C stand that links this file
// without compiled Surge code has no definition for it. Weak rather than
// required: such a stand builds channels of inert elements, and the
// descriptor a caller passes directly is what those use.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
extern const rt_value_ops* __surge_value_ops_for(uint64_t type_id) __attribute__((weak));

const rt_value_ops* rt_channel_element_ops_for(uint64_t element_type_id) {
    if (element_type_id == 0 || __surge_value_ops_for == NULL) {
        return rt_channel_opaque_word_ops();
    }
    const rt_value_ops* ops = __surge_value_ops_for(element_type_id);
    // A type this process never compiled a descriptor for still crossed the
    // boundary as a word, so that is what it is treated as here.
    return ops != NULL ? ops : rt_channel_opaque_word_ops();
}

void* rt_channel_new(uint64_t capacity, const rt_value_ops* ops, uint64_t element_type_id) {
    if (ops == NULL) {
        // A channel with no descriptor cannot size a cell, so there is nothing
        // honest to build. The two callers that can reach this are a far
        // create whose payload type this process never compiled, and a C stand
        // that passed nothing; both are told rather than handed a channel that
        // would mis-stride on first use.
        panic_msg("async: channel needs its element descriptor");
        return NULL;
    }
    uint64_t bytes = channel_alloc_size(ops, capacity);
    size_t align =
        ops->layout.align > _Alignof(rt_channel) ? ops->layout.align : _Alignof(rt_channel);
    rt_channel* ch = (rt_channel*)rt_alloc(bytes, align);
    if (ch == NULL) {
        fatal_oom_msg("async: channel allocation failed");
        return NULL;
    }
    rt_channel_handle_refs_init(ch);
    ch->capacity = capacity;
    ch->ops = ops;
    ch->element_type_id = element_type_id;
    const rt_task* creator = rt_current_task();
    ch->owner_shard_id =
        creator != NULL && creator->owner_shard_valid != 0 ? creator->owner_shard_id : 0;

    uint8_t* base = (uint8_t*)ch;
    uint64_t offset = channel_align_up(channel_ring_offset(), (uint64_t)align);
    size_t ring_bytes = rt_typed_fifo_alloc_size(ops, capacity);
    if (rt_typed_fifo_init(&ch->ring, ops, capacity, base + offset, ring_bytes) !=
        RT_SLOT_CONTROL_OK) {
        panic_msg("async: channel ring layout refused");
        return NULL;
    }
    offset = channel_align_up(offset + (uint64_t)ring_bytes, (uint64_t)align);
    size_t park_bytes = rt_park_pool_alloc_size(ops, channel_park_capacity(capacity));
    if (rt_park_pool_init(
            &ch->parks, ops, channel_park_capacity(capacity), base + offset, park_bytes) !=
        RT_SLOT_CONTROL_OK) {
        panic_msg("async: channel park pool layout refused");
        return NULL;
    }
    rt_async_debug_printf(
        "async chan new ch=%p cap=%llu\n", (void*)ch, (unsigned long long)capacity);
    return ch;
}

// Destroys a value taken out of a channel that will never reach a receiver.
//
// A select's winning recv arm is the caller this exists for. Taking the value
// is what makes that arm ready and the take is not undoable, but the arm has
// nowhere to put it: an arm is `expr => expr`, with no binding for the payload
// between them. Without this the take is a leak, and it is a silent one -- the
// program still prints the right answer, because it never had the value.
//
// `storage` is the select operation's own staging slot, which is where the take
// moved the value. Runs the element's drop, so no shard or control lock may be
// held here.
//
// It takes no pin of its own, and reads the channel's descriptor, so the
// caller's pin from the claim that took the value must still be standing: with
// no lock held, this is precisely the moment another lane could retire the last
// handle. Checked rather than assumed, because the descriptor read below is the
// use-after-free that would otherwise be the only report.
void rt_channel_release_payload(void* channel, void* storage) {
    const rt_channel* ch = channel_from_handle(channel);
    if (ch == NULL || storage == NULL) {
        return;
    }
    rt_channel_assert_pinned(ch);
    rt_value_drop_in_place_detached(ch->ops, storage);
}

// Stages `src` into a park slot this channel owns, with the owner lock
// RELEASED across the element's move.
//
// The claim is what makes the release safe. Reserving marks the slot in use,
// and the pool refuses to end that park while a reservation is outstanding, so
// a receiver cancelled inside the window cannot free the bytes the move is
// still writing. What a cancellation does instead is destroy the value
// afterwards, through the slot's own drain -- which is the consuming rule the
// storage model states for a cancelled waiter, rather than a value stranded
// between two locks.
//
// Returns 0 when no slot was free. That is not a failure: the caller still owns
// its value and parks holding it, exactly as it did before any of this existed.
//
// Shared with the send lane in rt_async_channel_send.c, which is why it has
// external linkage: it is the one piece of the protocol both loops perform.
int rt_channel_stage_locked(
    rt_executor* ex, rt_shard* ch_shard, rt_channel* ch, void* src, rt_park_token* out_token) {
    if (rt_park_pool_acquire_locked(&ch->parks, out_token) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    void* address = NULL;
    if (rt_park_pool_reserve_deliver_locked(&ch->parks, out_token, &address) !=
        RT_SLOT_CONTROL_OK) {
        channel_end_park_locked(ex, ch_shard, ch, out_token);
        return 0;
    }
    rt_shard_unlock(ch_shard);
    rt_value_move_init_detached(ch->ops, address, src);
    rt_shard_lock(ch_shard);
    if (rt_park_pool_commit_deliver_locked(&ch->parks, out_token) != RT_SLOT_CONTROL_OK) {
        panic_msg("async: staged channel value could not be published");
        return 0;
    }
    // The delivery commit released the slot's reservation, which a taker saw
    // as BUSY while the move ran.
    rt_channel_claim_released_locked(ex, ch_shard, ch);
    return 1;
}

// The receive loop below and the send loop in rt_async_channel_send.c share one
// hold on the object: rt_channel_pin around the whole operation, because each
// loop releases the owner shard lock at every step that runs generated code
// and a handle dropped on another lane inside such a window would otherwise
// free the storage a move is writing into. The reasoning is written once, above
// channel_send_pinned in the send lane.
static uint8_t rt_channel_recv_inner(void* channel, void* dst);

uint8_t rt_channel_recv(void* channel, void* dst) {
    rt_channel_pin(channel);
    uint8_t status = rt_channel_recv_inner(channel, dst);
    rt_channel_unpin(channel);
    return status;
}

static uint8_t rt_channel_recv_inner(void* channel, void* dst) {
    rt_executor* ex = ensure_exec();
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL) {
        return 2;
    }
    if (rt_current_task_id() == 0) {
        panic_msg("async channel recv outside task");
        return 2;
    }
    rt_task* task = rt_current_task();
    if (task == NULL) {
        panic_msg("async: missing current task");
        return 2;
    }
    if (task_cancelled_load(task) != 0) {
        rt_channel_retry_reset(task);
        // A sender may already have delivered a value into this mailbox
        // (channel_deliver_same_shard_locked/channel_deliver_foreign) before
        // this task's cancellation landed: candidate validation only blocks
        // FUTURE deliveries to an already-cancelled peer, not one already in
        // flight. That delivery bypassed this task's own compiled suspend
        // state entirely (RV2-DEBT-059's abandoned-state mechanism never
        // sees it), so this is the only place left to reclaim it.
        if (task->resume_kind == RESUME_CHAN_RECV_VALUE) {
            rt_shard* ch_shard = channel_owner_shard(ex, ch);
            rt_shard_lock(ch_shard);
            channel_end_park_locked(ex, ch_shard, ch, &task->resume_slot);
            rt_shard_unlock(ch_shard);
        }
        task->resume_kind = RESUME_NONE;
        task->resume_slot = (rt_park_token){0};
        return 0;
    }
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard* own_shard = rt_task_owner_shard(ex, task);
    waker_key send_key = channel_send_key(ch);
    for (;;) {
        // Consume-or-arm under our owner lock (see rt_channel_send_inner):
        // peers deliver values under this lock; anywhere else the check
        // races the delivery and re-arming would overwrite it.
        rt_shard_lock(own_shard);
        uint8_t rk = task->resume_kind;
        if (rk == RESUME_CHAN_RECV_VALUE || rk == RESUME_CHAN_RECV_CLOSED) {
            rt_park_token slot = task->resume_slot;
            task->resume_kind = RESUME_NONE;
            task->resume_slot = (rt_park_token){0};
            task->park_seq++;
            rt_shard_unlock(own_shard);
            if (rk == RESUME_CHAN_RECV_VALUE) {
                // The value is in a slot the channel owns; take it out with no
                // lock held, then end the park.
                void* from = NULL;
                rt_shard_lock(ch_shard);
                rt_slot_control_status taken =
                    rt_park_pool_reserve_take_locked(&ch->parks, &slot, &from);
                rt_shard_unlock(ch_shard);
                if (taken != RT_SLOT_CONTROL_OK) {
                    panic_msg("async: delivered channel value was not in its slot");
                    return 2;
                }
                if (dst != NULL) {
                    rt_value_move_init_detached(ch->ops, dst, from);
                } else {
                    rt_value_drop_in_place_detached(ch->ops, from);
                }
                rt_shard_lock(ch_shard);
                (void)rt_park_pool_commit_take_locked(&ch->parks, &slot);
                // The commit released the slot this take held BUSY.
                rt_channel_claim_released_locked(ex, ch_shard, ch);
                channel_end_park_locked(ex, ch_shard, ch, &slot);
                rt_shard_unlock(ch_shard);
                rt_channel_retry_reset(task);
                return 1;
            }
            rt_channel_retry_reset(task);
            return 2;
        }
        task->resume_kind = RESUME_NONE;
        rt_shard_unlock(own_shard);
        rt_shard_lock(ch_shard);
        rt_typed_fifo_ticket popped;
        rt_slot_control_status claimed = rt_typed_fifo_reserve_pop_locked(&ch->ring, &popped);
        if (claimed == RT_SLOT_CONTROL_BUSY) {
            // A value IS queued for us; another task holds the buffer's single
            // transfer for an instant. Parking on readiness here would sleep
            // on a value that is already ours, and a close arriving first
            // would then report an empty channel and lose it. The claim is
            // REFUSED and counted instead: seven times the receive comes back
            // and looks again, the eighth parks on the channel's retry key
            // until the claim that refused it is released (rt_channel_retry.h).
            rt_channel_wait_after_claim_refusal_locked(
                ch_shard, ch, task, RT_CHANNEL_RETRY_RECV, RT_CHANNEL_CLAIM_REFUSAL_RING_POP);
            return 0;
        }
        if (claimed == RT_SLOT_CONTROL_OK) {
            rt_shard_unlock(ch_shard);
            if (dst != NULL) {
                rt_value_move_init_detached(ch->ops, dst, popped.address);
            } else {
                rt_value_drop_in_place_detached(ch->ops, popped.address);
            }
            rt_shard_lock(ch_shard);
            if (rt_typed_fifo_commit_pop_locked(&ch->ring, &popped) != RT_SLOT_CONTROL_OK) {
                rt_shard_unlock(ch_shard);
                panic_msg("async: buffered channel value could not be retired");
                return 2;
            }
            // The commit released the single transfer this receive held.
            rt_channel_claim_released_locked(ex, ch_shard, ch);
            // Refill from a parked sender: same-shard senders hand their
            // value over inline; foreign senders are woken to retry so the
            // value never travels outside its owner's lock.
            waiter cand;
            while (channel_buffered(ch) < ch->capacity &&
                   channel_pop_candidate_locked(ch_shard, send_key, &cand)) {
                if (cand.seq == 0) {
                    channel_wake_only(ex, ch_shard, &cand);
                    continue;
                }
                if (cand.owner_hint == ch->owner_shard_id) {
                    rt_task* sender = get_task(ex, cand.task_id);
                    if (!channel_candidate_valid(sender, &cand)) {
                        continue;
                    }
                    rt_park_token sender_slot = sender->resume_slot;
                    if (!rt_park_pool_token_is_live(&ch->parks, &sender_slot)) {
                        // Parked holding its own value, because the pool was
                        // full when it parked. Wake it to retry -- and do NOT
                        // ack it: an ack says "your send completed", and this
                        // one delivered nothing, so the value would be dropped
                        // by a sender that believed it had been sent.
                        (void)wake_task_on_shard_locked(
                            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                        continue;
                    }
                    if (!channel_stage_into_ring_locked(ex, ch_shard, ch, &sender_slot, NULL)) {
                        // The buffer refused: its single transfer is in
                        // flight, or a receiver is taking this very value.
                        // This sender's registration was consumed by the pop
                        // above, so nothing else will ever call on it -- wake
                        // it, unacked, to place its own value. Leaving it here
                        // is a task parked forever on a channel that has
                        // forgotten it, which is exactly how this was found.
                        (void)wake_task_on_shard_locked(
                            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                        break;
                    }
                    sender->resume_kind = RESUME_CHAN_SEND_ACK;
                    sender->resume_slot = (rt_park_token){0};
                    // A parked sender has a waiter entry, so the leaf
                    // enqueues it; compat senders never park entries
                    // while RUNNING, so no compat fallback is needed.
                    (void)wake_task_on_shard_locked(
                        ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                    channel_end_park_locked(ex, ch_shard, ch, &sender_slot);
                    break;
                }
                rt_shard_unlock(ch_shard);
                (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, (rt_park_token){0});
                rt_shard_lock(ch_shard);
            }
            rt_shard_unlock(ch_shard);
            rt_channel_retry_reset(task);
            return 1;
        }
        waiter cand;
        if (channel_pop_candidate_locked(ch_shard, send_key, &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                rt_shard_unlock(ch_shard);
                continue;
            }
            if (cand.owner_hint == ch->owner_shard_id) {
                rt_task* sender = get_task(ex, cand.task_id);
                if (!channel_candidate_valid(sender, &cand)) {
                    rt_shard_unlock(ch_shard);
                    continue;
                }
                rt_park_token sender_slot = sender->resume_slot;
                void* from = NULL;
                if (rt_park_pool_reserve_take_locked(&ch->parks, &sender_slot, &from) !=
                    RT_SLOT_CONTROL_OK) {
                    // The sender parked without staging (a full pool), so there
                    // is nothing here to receive, or its slot is mid-transfer.
                    // Wake it to retry, unacked -- it still owns its value;
                    // this is a condition, not a broken invariant -- and look
                    // at the next candidate.
                    (void)wake_task_on_shard_locked(
                        ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                    rt_shard_unlock(ch_shard);
                    continue;
                }
                sender->resume_kind = RESUME_CHAN_SEND_ACK;
                sender->resume_slot = (rt_park_token){0};
                rt_shard_unlock(ch_shard);
                if (dst != NULL) {
                    rt_value_move_init_detached(ch->ops, dst, from);
                } else {
                    rt_value_drop_in_place_detached(ch->ops, from);
                }
                rt_shard_lock(ch_shard);
                (void)rt_park_pool_commit_take_locked(&ch->parks, &sender_slot);
                // The commit released the sender's slot, BUSY while we took.
                rt_channel_claim_released_locked(ex, ch_shard, ch);
                int pushed = wake_task_on_shard_locked(
                    ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                channel_end_park_locked(ex, ch_shard, ch, &sender_slot);
                rt_shard_unlock(ch_shard);
                channel_compat_broadcast_if_needed(ex, pushed);
                rt_channel_retry_reset(task);
                return 1;
            }
            // Foreign parked sender: its value is in this channel's pool, so
            // only the token crosses the lock boundary.
            rt_park_token sender_slot;
            if (!channel_claim_foreign_sender_locked(ex, ch_shard, ch, &cand, &sender_slot)) {
                // Gone, or parked holding its own value and woken to retry.
                rt_shard_unlock(ch_shard);
                continue;
            }
            void* from = NULL;
            rt_slot_control_status taken =
                rt_park_pool_reserve_take_locked(&ch->parks, &sender_slot, &from);
            rt_shard_unlock(ch_shard);
            if (taken != RT_SLOT_CONTROL_OK) {
                panic_msg("async: foreign parked sender had no staged value");
                return 2;
            }
            if (dst != NULL) {
                rt_value_move_init_detached(ch->ops, dst, from);
            } else {
                rt_value_drop_in_place_detached(ch->ops, from);
            }
            rt_shard_lock(ch_shard);
            (void)rt_park_pool_commit_take_locked(&ch->parks, &sender_slot);
            rt_channel_claim_released_locked(ex, ch_shard, ch);
            channel_end_park_locked(ex, ch_shard, ch, &sender_slot);
            rt_shard_unlock(ch_shard);
            rt_channel_retry_reset(task);
            return 1;
        }
        if (ch->closed) {
            rt_shard_unlock(ch_shard);
            rt_channel_retry_reset(task);
            return 2;
        }
        waker_key recv_key = channel_recv_key(ch);
        channel_park_prepare_locked(ch_shard, task, recv_key);
        rt_shard_unlock(ch_shard);
        pending_key = recv_key;
        return 0;
    }
}
