#include "rt_channel_lane.h"

#include "rt_value_ops.h"

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
    task->resume_bits = 0;
    pending_key = waker_none();
    rt_trace_channel_handoff_yield();
    return 1;
}

// The lookup is emitted by the compiler, so a C stand that links this file
// without compiled Surge code has no definition for it. Weak rather than
// required: such a stand builds channels of inert elements, and a null
// descriptor is exactly the right answer for those.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
extern const rt_value_ops* __surge_value_ops_for(uint64_t type_id) __attribute__((weak));

static const rt_value_ops* channel_element_ops(uint64_t element_type_id) {
    if (element_type_id == 0 || __surge_value_ops_for == NULL) {
        return NULL;
    }
    return __surge_value_ops_for(element_type_id);
}

// Reclaims one element from storage the channel owns.
//
// A null descriptor is not "nothing to do" -- the detached helper refuses one
// on purpose, because "no descriptor" and "no obligation" are different
// statements and only the second is something it can act on. The difference is
// known HERE: a channel whose element the program emitted no descriptor for is
// holding an inert element, and inert elements are exactly what the old zero
// drop-fn id used to mean. So the absence is answered here rather than being
// passed down as a question the helper would be right to reject.
static void channel_reclaim_element(const rt_channel* ch, void* storage) {
    if (ch == NULL || ch->ops == NULL) {
        return;
    }
    rt_value_drop_in_place_detached(ch->ops, storage);
}

void* rt_channel_new(uint64_t capacity, uint64_t element_type_id) {
    uint64_t bytes = channel_alloc_size(capacity);
    rt_channel* ch = (rt_channel*)rt_alloc(bytes, _Alignof(rt_channel));
    if (ch == NULL) {
        panic_msg("async: channel allocation failed");
        return NULL;
    }
    memset(ch, 0, sizeof(rt_channel));
    ch->capacity = capacity;
    ch->ops = channel_element_ops(element_type_id);
    const rt_task* creator = rt_current_task();
    ch->owner_shard_id =
        creator != NULL && creator->owner_shard_valid != 0 ? creator->owner_shard_id : 0;
    if (capacity > 0) {
        ch->buf = (uint64_t*)((uint8_t*)ch + channel_buffer_offset());
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
// still-buffered entry through the element's descriptor first (a no-op walk
// for an inert element, which has none): the only caller today, release_entry
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
    // The drain below runs USER code -- an element's drop -- so no scheduler
    // lock may be held here. Today that means the numeric dispatch; after the
    // typed flip it is the element's drop_in_place, and a drop that reenters
    // the runtime while control is held would deadlock rather than misbehave
    // visibly. Fail closed, naming the lane, instead of leaving the rule to
    // whoever reads the caller contract in rt.h.
    if (rt_lane_holds_control() || rt_lane_holds_any_shard()) {
        panic_msg("async: channel reclaim ran while a scheduler lock was held");
    }
    for (size_t i = 0; i < ch->buf_len; i++) {
        size_t idx = (ch->buf_head + i) % ch->capacity;
        channel_reclaim_element(ch, &ch->buf[idx]);
    }
    uint64_t bytes = channel_alloc_size(ch->capacity);
    rt_async_debug_printf(
        "async chan free ch=%p cap=%llu\n", (void*)ch, (unsigned long long)ch->capacity);
    rt_free((uint8_t*)ch, bytes, _Alignof(rt_channel));
}

// Destroys a value taken out of a channel that will never reach a receiver.
//
// A select's winning recv arm is the caller this exists for. Taking the value
// is what makes that arm ready and the take is not undoable, but the arm has
// nowhere to put it: an arm is `expr => expr`, with no binding for the payload
// between them. Without this the take is a leak, and it is a silent one — the
// program still prints the right answer, because it never had the value.
//
// Runs compiled drop glue, so no shard or control lock may be held here; the
// caller only has to still hold the channel. A Copy/inert element type carries
// no drop id and this costs a load and a branch.
void rt_channel_release_payload(void* channel, uint64_t payload) {
    const rt_channel* ch = channel_from_handle(channel);
    if (ch == NULL) {
        return;
    }
    // The descriptor loads through the address it is given, so the bits are
    // reclaimed from where they sit rather than being passed as a value.
    channel_reclaim_element(ch, &payload);
}

static bool rt_channel_send_inner(void* channel, uint64_t value_bits, int yield_after_handoff) {
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
        task->resume_bits = 0;
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
            task->resume_bits = 0;
            task->park_seq++;
            rt_shard_unlock(own_shard);
            if (rk == RESUME_CHAN_SEND_CLOSED) {
                panic_msg("send on closed channel");
            }
            return 1;
        }
        task->resume_kind = RESUME_NONE;
        task->resume_bits = value_bits;
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
            if (cand.owner_hint == ch->owner_shard_id) {
                int no_signal = yield_after_handoff && same_shard_worker;
                int pushed = 0;
                int live = channel_deliver_same_shard_locked(ex,
                                                             ch_shard,
                                                             &cand,
                                                             RESUME_CHAN_RECV_VALUE,
                                                             value_bits,
                                                             no_signal ? 0 : 1,
                                                             &pushed);
                if (!live) {
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
            if (!channel_deliver_foreign(ex, &cand, RESUME_CHAN_RECV_VALUE, value_bits)) {
                continue;
            }
            if (yield_after_handoff && prepare_channel_send_yield(task)) {
                return 0;
            }
            return 1;
        }
        if (ch->capacity > 0 && ch->buf_len < ch->capacity && buf_push(ch, value_bits)) {
            rt_shard_unlock(ch_shard);
            return 1;
        }
        waker_key send_key = channel_send_key(ch);
        channel_park_prepare_locked(ch_shard, task, send_key);
        rt_shard_unlock(ch_shard);
        pending_key = send_key;
        return 0;
    }
}

bool rt_channel_send(void* channel, uint64_t value_bits) {
    return rt_channel_send_inner(channel, value_bits, 0);
}

bool rt_channel_send_yield(void* channel, uint64_t value_bits) {
    return rt_channel_send_inner(channel, value_bits, 1);
}

uint8_t rt_channel_recv(void* channel, uint64_t* out_bits) {
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
            channel_reclaim_element(ch, &task->resume_bits);
        }
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
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
            if (rk == RESUME_CHAN_RECV_VALUE && out_bits != NULL) {
                *out_bits = task->resume_bits;
            }
            task->resume_kind = RESUME_NONE;
            task->resume_bits = 0;
            task->park_seq++;
            rt_shard_unlock(own_shard);
            return rk == RESUME_CHAN_RECV_VALUE ? 1 : 2;
        }
        task->resume_kind = RESUME_NONE;
        task->resume_bits = 0;
        rt_shard_unlock(own_shard);
        rt_shard_lock(ch_shard);
        uint64_t val = 0;
        if (buf_pop(ch, &val)) {
            // Refill from a parked sender: same-shard senders hand their
            // value over inline; foreign senders are woken to retry so the
            // value never travels outside its owner's lock.
            waiter cand;
            while (ch->buf_len < ch->capacity &&
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
                    if (buf_push(ch, sender->resume_bits)) {
                        sender->resume_kind = RESUME_CHAN_SEND_ACK;
                        sender->resume_bits = 0;
                        // A parked sender has a waiter entry, so the leaf
                        // enqueues it; compat senders never park entries
                        // while RUNNING, so no compat fallback is needed.
                        (void)wake_task_on_shard_locked(
                            ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                    }
                    break;
                }
                rt_shard_unlock(ch_shard);
                (void)channel_deliver_foreign(ex, &cand, RESUME_NONE, 0);
                rt_shard_lock(ch_shard);
            }
            rt_shard_unlock(ch_shard);
            if (out_bits != NULL) {
                *out_bits = val;
            }
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
                uint64_t bits = sender->resume_bits;
                sender->resume_kind = RESUME_CHAN_SEND_ACK;
                sender->resume_bits = 0;
                int pushed = wake_task_on_shard_locked(
                    ex, ch_shard, sender, channel_wake_force_inject_enabled(), 0, 1, NULL);
                rt_shard_unlock(ch_shard);
                channel_compat_broadcast_if_needed(ex, pushed);
                if (out_bits != NULL) {
                    *out_bits = bits;
                }
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
            uint64_t bits = 0;
            rt_task* sender = get_task(ex, cand.task_id);
            if (channel_candidate_valid(sender, &cand)) {
                rt_shard* sender_shard = rt_task_owner_shard(ex, sender);
                rt_shard_lock(sender_shard);
                int pushed = 0;
                if (channel_candidate_valid(sender, &cand)) {
                    bits = sender->resume_bits;
                    sender->resume_kind = RESUME_CHAN_SEND_ACK;
                    sender->resume_bits = 0;
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
            if (out_bits != NULL) {
                *out_bits = bits;
            }
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
