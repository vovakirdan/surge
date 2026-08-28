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
        panic_msg("async: channel allocation failed");
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
static int channel_stage_locked(
    rt_executor* ex, rt_shard* ch_shard, rt_channel* ch, void* src, rt_park_token* out_token) {
    if (rt_park_pool_acquire_locked(&ch->parks, out_token) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    void* address = NULL;
    if (rt_park_pool_reserve_deliver_locked(&ch->parks, out_token, &address) !=
        RT_SLOT_CONTROL_OK) {
        channel_end_park_locked(ch_shard, ch, out_token);
        return 0;
    }
    rt_shard_unlock(ch_shard);
    rt_value_move_init_detached(ch->ops, address, src);
    rt_shard_lock(ch_shard);
    if (rt_park_pool_commit_deliver_locked(&ch->parks, out_token) != RT_SLOT_CONTROL_OK) {
        panic_msg("async: staged channel value could not be published");
        return 0;
    }
    (void)ex;
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
        if (channel_nothing_queued(ch) && channel_pop_candidate_locked(ch_shard, recv_key, &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                rt_shard_unlock(ch_shard);
                continue;
            }
            if (!staged_live && !channel_stage_locked(ex, ch_shard, ch, src, &staged)) {
                // No slot to stage into. The candidate has already been POPPED,
                // so dropping it here would strand a receiver that is still
                // parked -- which showed up as a violated FIFO order rather
                // than as a hang, because the next sender delivered to a later
                // waiter first. Wake it to re-register instead.
                channel_wake_only(ex, ch_shard, &cand);
                rt_shard_unlock(ch_shard);
                continue;
            }
            // The slot belongs to the handover from here: every path below
            // delivers it, moves it into the buffer, or destroys it, so this
            // task must stop naming it before any of them runs.
            task->resume_slot = (rt_park_token){0};
            if (cand.owner_hint == ch->owner_shard_id) {
                int no_signal = yield_after_handoff && same_shard_worker;
                int pushed = 0;
                int live = channel_deliver_same_shard_locked(ex,
                                                             ch_shard,
                                                             &cand,
                                                             RESUME_CHAN_RECV_VALUE,
                                                             staged,
                                                             no_signal ? 0 : 1,
                                                             &pushed);
                if (!live) {
                    // The candidate died while we staged. The value is in a
                    // slot this channel owns, so give it to the buffer if
                    // there is room and destroy it otherwise -- never strand
                    // it, and never hand it to a task that is gone.
                    if (!channel_stage_into_ring_locked(ex, ch_shard, ch, &staged)) {
                        channel_end_park_locked(ch_shard, ch, &staged);
                    } else {
                        channel_end_park_locked(ch_shard, ch, &staged);
                        rt_shard_unlock(ch_shard);
                        return 1;
                    }
                    rt_shard_unlock(ch_shard);
                    continue;
                }
                rt_shard_unlock(ch_shard);
                channel_compat_broadcast_if_needed(ex, pushed);
                if (yield_after_handoff && prepare_channel_send_yield(task)) {
                    return 0;
                }
                return 1;
            }
            rt_shard_unlock(ch_shard);
            if (!channel_deliver_foreign(ex, &cand, RESUME_CHAN_RECV_VALUE, staged)) {
                rt_shard_lock(ch_shard);
                if (channel_stage_into_ring_locked(ex, ch_shard, ch, &staged)) {
                    channel_end_park_locked(ch_shard, ch, &staged);
                    rt_shard_unlock(ch_shard);
                    return 1;
                }
                channel_end_park_locked(ch_shard, ch, &staged);
                rt_shard_unlock(ch_shard);
                continue;
            }
            if (yield_after_handoff && prepare_channel_send_yield(task)) {
                return 0;
            }
            return 1;
        }
        if (ch->capacity > 0 && channel_buffered(ch) < ch->capacity && staged_live) {
            // The value is in a slot already: move it from there into the
            // buffer. A refusal here means the buffer's single transfer is in
            // flight, or a receiver is taking this very value -- both resolve
            // by parking with the slot still held.
            if (channel_stage_into_ring_locked(ex, ch_shard, ch, &staged)) {
                task->resume_slot = (rt_park_token){0};
                channel_end_park_locked(ch_shard, ch, &staged);
                rt_shard_unlock(ch_shard);
                return 1;
            }
        } else if (ch->capacity > 0 && channel_buffered(ch) < ch->capacity) {
            rt_typed_fifo_ticket ticket;
            rt_slot_control_status reserved = rt_typed_fifo_reserve_push_locked(&ch->ring, &ticket);
            if (reserved == RT_SLOT_CONTROL_BUSY) {
                // The buffer has room; what it does not have at this instant is
                // its single transfer, held by another task's move. Parking
                // would block a send the buffer can take -- and a buffered send
                // that fits must not block -- so come back and look again.
                rt_shard_unlock(ch_shard);
                pending_key = waker_none();
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
                channel_wake_parked_receiver_locked(ex, ch_shard, ch);
                rt_shard_unlock(ch_shard);
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
            if (channel_stage_locked(ex, ch_shard, ch, src, &staged)) {
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
// channel_stage_locked's move into a park slot, the buffered push beside it,
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
            channel_end_park_locked(ch_shard, ch, &task->resume_slot);
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
                channel_end_park_locked(ch_shard, ch, &slot);
                rt_shard_unlock(ch_shard);
                return 1;
            }
            return 2;
        }
        task->resume_kind = RESUME_NONE;
        rt_shard_unlock(own_shard);
        rt_shard_lock(ch_shard);
        rt_typed_fifo_ticket popped;
        rt_slot_control_status claimed = rt_typed_fifo_reserve_pop_locked(&ch->ring, &popped);
        if (claimed == RT_SLOT_CONTROL_BUSY) {
            // A value IS queued for us; another task holds the buffer's single
            // transfer for an instant. Parking here would sleep on a value that
            // is already ours, and a close arriving first would then report an
            // empty channel and lose it. Come back and look again.
            rt_shard_unlock(ch_shard);
            pending_key = waker_none();
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
                    if (!channel_stage_into_ring_locked(ex, ch_shard, ch, &sender_slot)) {
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
                    channel_end_park_locked(ch_shard, ch, &sender_slot);
                    break;
                }
                rt_shard_unlock(ch_shard);
                (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, (rt_park_token){0});
                rt_shard_lock(ch_shard);
            }
            rt_shard_unlock(ch_shard);
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
                    // is nothing here to receive. Wake it to retry, unacked --
                    // it still owns its value; this is a condition, not a
                    // broken invariant.
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
                int pushed = wake_task_on_shard_locked(
                    ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                channel_end_park_locked(ch_shard, ch, &sender_slot);
                rt_shard_unlock(ch_shard);
                channel_compat_broadcast_if_needed(ex, pushed);
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
            channel_end_park_locked(ch_shard, ch, &sender_slot);
            rt_shard_unlock(ch_shard);
            return 1;
        }
        if (ch->closed) {
            rt_shard_unlock(ch_shard);
            return 2;
        }
        waker_key recv_key = channel_recv_key(ch);
        channel_park_prepare_locked(ch_shard, task, recv_key);
        rt_shard_unlock(ch_shard);
        pending_key = recv_key;
        return 0;
    }
}
