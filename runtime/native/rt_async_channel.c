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
    memset(ch, 0, sizeof(rt_channel));
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

// Reclaims the single rt_alloc block rt_channel_new made (header + inline
// buffer): the size is reconstructed deterministically from the stored
// capacity, the same way emitTagValueFromValues-style leaf frees elsewhere
// in the runtime recompute their own allocation size. See rt.h for the
// caller-responsibility contract (no other live holder). Drains every
// still-buffered entry through payload_drop_fn_id first (a no-op walk when
// it's 0, the Copy/inert case): the only caller today, release_entry
// (rt_far_channel.c), only reaches this after confirming no lease or pin
// can resolve the channel anymore, so the buffer is quiescent here without
// this function taking any lock of its own.
// rt_channel_free_when_unlocked reclaims now if this lane holds no scheduler
// lock, and otherwise hands the channel to the lane's deferred work.
//
// Callers reach this holding control because completion bookkeeping runs
// there: mark_done takes the control lane, and a far-channel unpin that drops
// the last pin reclaims from inside it. Draining the buffer under that lock
// runs an element's drop with the scheduler held, which the typed flip turns
// from a latent rule into a deadlock.
typedef struct {
    void** items;
    size_t len;
    size_t cap;
} rt_channel_reclaim_queue;

static _Thread_local rt_channel_reclaim_queue channel_reclaim_queue;

void rt_channel_free_when_unlocked(void* channel) {
    if (channel == NULL) {
        return;
    }
    if (!rt_lane_holds_control() && !rt_lane_holds_any_shard()) {
        rt_channel_free(channel);
        return;
    }
    rt_channel_reclaim_queue* queue = &channel_reclaim_queue;
    if (queue->len == queue->cap) {
        size_t next_cap = queue->cap == 0 ? 4 : queue->cap * 2;
        void** grown = (void**)rt_alloc(next_cap * sizeof(void*), _Alignof(void*));
        if (grown == NULL) {
            // Dropping the pointer would leak the channel silently, which is
            // worse than the rule this queue exists to keep.
            panic_msg("async: channel reclaim queue allocation failed");
            return;
        }
        for (size_t i = 0; i < queue->len; i++) {
            grown[i] = queue->items[i];
        }
        if (queue->items != NULL) {
            rt_free((uint8_t*)queue->items, queue->cap * sizeof(void*), _Alignof(void*));
        }
        queue->items = grown;
        queue->cap = next_cap;
    }
    queue->items[queue->len++] = channel;
}

// rt_channel_reclaim_drain is called by the lane at the moment it releases its
// last scheduler lock, so by construction nothing is held here.
void rt_channel_reclaim_drain(void) {
    rt_channel_reclaim_queue* queue = &channel_reclaim_queue;
    while (queue->len > 0) {
        void* channel = queue->items[--queue->len];
        rt_channel_free(channel);
    }
}

void rt_channel_free(void* channel) {
    rt_channel* ch = channel_from_handle(channel);
    // The drains below run USER code -- an element's drop_in_place -- so no
    // scheduler lock may be held here: a drop that reenters the runtime while
    // control is held would deadlock rather than misbehave visibly. Fail
    // closed, naming the lane, instead of leaving the rule to whoever reads
    // the caller contract in rt.h.
    if (rt_lane_holds_control() || rt_lane_holds_any_shard()) {
        panic_msg("async: channel reclaim ran while a scheduler lock was held");
    }
    // Everything the channel still owns: what is buffered, and what was staged
    // for or delivered to a park that never completed. Each is destroyed
    // exactly once by its owner's own drain.
    rt_typed_fifo_drain(&ch->ring);
    rt_park_pool_drain(&ch->parks);
    size_t align = ch->ops != NULL && ch->ops->layout.align > _Alignof(rt_channel)
                       ? ch->ops->layout.align
                       : _Alignof(rt_channel);
    uint64_t bytes = channel_alloc_size(ch->ops, ch->capacity);
    rt_async_debug_printf(
        "async chan free ch=%p cap=%llu\n", (void*)ch, (unsigned long long)ch->capacity);
    rt_free((uint8_t*)ch, bytes, align);
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
void rt_channel_release_payload(void* channel, void* storage) {
    const rt_channel* ch = channel_from_handle(channel);
    if (ch == NULL || storage == NULL) {
        return;
    }
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
        (void)rt_park_pool_release(&ch->parks, out_token);
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
        waiter cand;
        if (channel_pop_candidate_locked(ch_shard, recv_key, &cand)) {
            if (cand.seq == 0) {
                channel_wake_only(ex, ch_shard, &cand);
                rt_shard_unlock(ch_shard);
                continue;
            }
            rt_park_token staged;
            if (!channel_stage_locked(ex, ch_shard, ch, src, &staged)) {
                // No slot to stage into. The candidate has already been POPPED,
                // so dropping it here would strand a receiver that is still
                // parked -- which showed up as a violated FIFO order rather
                // than as a hang, because the next sender delivered to a later
                // waiter first. Wake it to re-register instead.
                channel_wake_only(ex, ch_shard, &cand);
                rt_shard_unlock(ch_shard);
                continue;
            }
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
                    if (!channel_stage_into_ring_locked(ch_shard, ch, &staged)) {
                        rt_shard_unlock(ch_shard);
                        (void)rt_park_pool_release(&ch->parks, &staged);
                        rt_shard_lock(ch_shard);
                    } else {
                        rt_shard_unlock(ch_shard);
                        (void)rt_park_pool_release(&ch->parks, &staged);
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
                if (channel_stage_into_ring_locked(ch_shard, ch, &staged)) {
                    rt_shard_unlock(ch_shard);
                    (void)rt_park_pool_release(&ch->parks, &staged);
                    return 1;
                }
                rt_shard_unlock(ch_shard);
                (void)rt_park_pool_release(&ch->parks, &staged);
                continue;
            }
            if (yield_after_handoff && prepare_channel_send_yield(task)) {
                return 0;
            }
            return 1;
        }
        if (ch->capacity > 0 && channel_buffered(ch) < ch->capacity) {
            rt_typed_fifo_ticket ticket;
            if (rt_typed_fifo_reserve_push_locked(&ch->ring, &ticket) == RT_SLOT_CONTROL_OK) {
                rt_shard_unlock(ch_shard);
                rt_value_move_init_detached(ch->ops, ticket.address, src);
                rt_shard_lock(ch_shard);
                if (rt_typed_fifo_commit_push_locked(&ch->ring, &ticket) != RT_SLOT_CONTROL_OK) {
                    rt_shard_unlock(ch_shard);
                    panic_msg("async: buffered channel value could not be published");
                    return 1;
                }
                rt_shard_unlock(ch_shard);
                return 1;
            }
        }
        // Parking with the value still in our own frame is what the old word
        // mailbox did, and it is exactly what a longjmp out of this poll would
        // lose. Stage into a slot the CHANNEL owns first, and park holding
        // only the token: a receiver refilling the ring later moves it out of
        // there, and a cancellation destroys it through the pool's drain.
        // Re-entering after a park that did not complete: the value is ALREADY
        // in a slot, and staging it again would strand the first one. That is
        // not hypothetical -- it exhausted the pool within a few rounds, after
        // which every sender parked with nothing staged and the whole channel
        // stopped moving.
        //
        // Every park slot may also be taken outright. Park ANYWAY with nothing
        // staged: the sender still owns its value, and a receiver that pops it
        // finds no slot and wakes it to retry. Returning without parking was
        // the first version of this, and it hung for the plainer reason that
        // nothing was registered to wake.
        rt_park_token staged_for_park = task->resume_slot;
        if (!rt_park_pool_token_is_live(&ch->parks, &staged_for_park)) {
            staged_for_park = (rt_park_token){0};
            (void)channel_stage_locked(ex, ch_shard, ch, src, &staged_for_park);
        }
        task->resume_slot = staged_for_park;
        waker_key send_key = channel_send_key(ch);
        channel_park_prepare_locked(ch_shard, task, send_key);
        rt_shard_unlock(ch_shard);
        pending_key = send_key;
        return 0;
    }
}

bool rt_channel_send(void* channel, void* src) {
    return rt_channel_send_inner(channel, src, 0);
}

bool rt_channel_send_yield(void* channel, void* src) {
    return rt_channel_send_inner(channel, src, 1);
}

uint8_t rt_channel_recv(void* channel, void* dst) {
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
            (void)rt_park_pool_release(&ch->parks, &task->resume_slot);
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
                rt_shard_unlock(ch_shard);
                (void)rt_park_pool_release(&ch->parks, &slot);
                return 1;
            }
            return 2;
        }
        task->resume_kind = RESUME_NONE;
        rt_shard_unlock(own_shard);
        rt_shard_lock(ch_shard);
        rt_typed_fifo_ticket popped;
        if (rt_typed_fifo_reserve_pop_locked(&ch->ring, &popped) == RT_SLOT_CONTROL_OK) {
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
                        // Parked without staging, because the pool was full.
                        // Wake it and let it retry; its value is still its own.
                        sender->resume_kind = RESUME_CHAN_SEND_ACK;
                        (void)wake_task_on_shard_locked(
                            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                        continue;
                    }
                    if (channel_stage_into_ring_locked(ch_shard, ch, &sender_slot)) {
                        sender->resume_kind = RESUME_CHAN_SEND_ACK;
                        sender->resume_slot = (rt_park_token){0};
                        // A parked sender has a waiter entry, so the leaf
                        // enqueues it; compat senders never park entries
                        // while RUNNING, so no compat fallback is needed.
                        (void)wake_task_on_shard_locked(
                            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                        rt_shard_unlock(ch_shard);
                        (void)rt_park_pool_release(&ch->parks, &sender_slot);
                        rt_shard_lock(ch_shard);
                    }
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
                    // is nothing here to receive. Wake it to retry and keep
                    // looking; this is a condition, not a broken invariant.
                    sender->resume_kind = RESUME_CHAN_SEND_ACK;
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
                rt_shard_unlock(ch_shard);
                (void)rt_park_pool_release(&ch->parks, &sender_slot);
                channel_compat_broadcast_if_needed(ex, pushed);
                return 1;
            }
            rt_shard_unlock(ch_shard);
            // Foreign parked sender: read its value and ack under its
            // owner's lock (direct handoff, no buffer interplay).
            int need_control = !rt_lane_holds_control();
            if (need_control) {
                rt_control_lock(ex);
            }
            int live = 0;
            rt_park_token sender_slot = (rt_park_token){0};
            rt_task* sender = get_task(ex, cand.task_id);
            if (channel_candidate_valid(sender, &cand)) {
                rt_shard* sender_shard = rt_task_owner_shard(ex, sender);
                rt_shard_lock(sender_shard);
                int pushed = 0;
                if (channel_candidate_valid(sender, &cand)) {
                    // The staged value lives in the CHANNEL's pool, not in the
                    // sender, so a foreign sender's value never travels
                    // outside a lock: only its token is read here.
                    sender_slot = sender->resume_slot;
                    sender->resume_kind = RESUME_CHAN_SEND_ACK;
                    sender->resume_slot = (rt_park_token){0};
                    pushed = wake_task_on_shard_locked(ex, sender_shard, sender, 1, 0, 1, NULL);
                    live = 1;
                }
                rt_shard_unlock(sender_shard);
                if (live) {
                    channel_compat_broadcast_if_needed(ex, pushed);
                }
            }
            if (need_control) {
                rt_control_unlock(ex);
            }
            if (!live) {
                continue;
            }
            void* from = NULL;
            rt_shard_lock(ch_shard);
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
            rt_shard_unlock(ch_shard);
            (void)rt_park_pool_release(&ch->parks, &sender_slot);
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
