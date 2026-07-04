#ifndef SURGE_RUNTIME_NATIVE_RT_CHANNEL_LANE_H
#define SURGE_RUNTIME_NATIVE_RT_CHANNEL_LANE_H

#include "rt_async_internal.h"

// Private channel-lane header shared by rt_async_channel.c (async fast
// lanes) and rt_channel_sync.c (try/compat/blocking/close lanes). The
// struct and helpers are static inline so each lane compiles the exact
// same owner-lock protocol without exporting channel internals.

struct rt_channel {
    uint64_t capacity;
    // Owner shard (D5 channel slice): fixed at creation to the creating
    // task's shard (shard 0 outside tasks); channel waiter keys live in the
    // owner's store and never move.
    uint32_t owner_shard_id;
    uint8_t closed;
    uint64_t* buf;
    size_t buf_len;
    size_t buf_head;
};

// Channel lane (peel B2): a channel's buffer and its two waiter keys live
// under the channel owner's shard lock; entry APIs no longer take the
// control lane. Peer delivery is candidate/validate: pop one entry under
// the channel lock, deliver same-shard peers inline (the channel lock IS
// their owner lock), and go through control -> peer-owner for foreign
// peers so the owner read stays stable. A dead candidate never consumes a
// value: the caller retries with the next. Foreign parked SENDERS are
// woken to retry instead of having their value read remotely, so buffered
// refill never strands a value between locks.

// Sync-channel compat fallback: a peer that was RUNNING inside a blocking
// helper is not enqueued by the leaf wake; its OS worker sleeps on compat_cv
// under the control lock, so that broadcast is its only wake (the token is
// already set). Called with no shard lock held.
static inline void channel_compat_broadcast_if_needed(rt_executor* ex, int pushed) {
    if (pushed) {
        return;
    }
    const rt_channel_blocking_compat* compat = rt_executor_channel_blocking_compat_const(ex);
    if (compat == NULL ||
        atomic_load_explicit(&compat->channel_blocked_workers, memory_order_acquire) == 0) {
        return;
    }
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    pthread_cond_broadcast(&ex->compat_cv);
    if (need_control) {
        rt_control_unlock(ex);
    }
}

static inline rt_shard* channel_owner_shard(rt_executor* ex, const rt_channel* ch) {
    return rt_runtime_shard(rt_executor_runtime(ex), ch->owner_shard_id);
}

// Caller holds the channel owner's lock; FIFO per key.
static inline int channel_pop_candidate_locked(rt_shard* owner_shard, waker_key key, waiter* out) {
    rt_waiter_store* store = &owner_shard->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            memmove(
                &store->entries[i], &store->entries[i + 1], (store->len - i - 1) * sizeof(waiter));
            store->len--;
            *out = w;
            return 1;
        }
    }
    return 0;
}

// Candidate validation (spike candidate/validate): the popped entry is only
// deliverable while the peer's current park still targets this key. A live
// peer that consumed an earlier wake and re-registered elsewhere leaves its
// old entry semantically dead; delivering into it would overwrite resume
// state for an unrelated park (observable as a foreign value surfacing from
// a reused channel address). park_key holds the key from registration until
// the wake leaf clears it — through the park commit, which resets only the
// prepared flag — so key equality alone is the liveness test. Like
// pop_waiter's stale-skip deref, the cross-lock read is benign: a mismatch
// drops the candidate, and an exact match on this key means the peer still
// parks (or re-parked) on this same channel, so delivery is due.
static inline int channel_candidate_valid(const rt_task* peer, const waiter* w) {
    return peer != NULL && task_status_load(peer) != TASK_DONE && task_cancelled_load(peer) == 0 &&
           peer->park_key.kind == w->key.kind && peer->park_key.id == w->key.id &&
           peer->park_seq == w->seq;
}

// Wake-only candidates (seq == 0) come from add_waiter registrations —
// select arms parked across several keys. They have no value mailbox: wake
// the task so it re-polls its arms and treat the pop as a non-event for the
// rendezvous (the caller keeps its value and keeps scanning or parks).
static inline void channel_wake_only(rt_executor* ex, rt_shard* ch_shard, const waiter* w) {
    rt_task* peer = get_task(ex, w->task_id);
    if (peer == NULL || task_status_load(peer) == TASK_DONE) {
        return;
    }
    if (w->owner_hint == ch_shard->shard_id) {
        (void)wake_task_on_shard_locked(ex, ch_shard, peer, 1, 0, 1, NULL);
        return;
    }
    rt_shard_unlock(ch_shard);
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    peer = get_task(ex, w->task_id);
    if (peer != NULL && task_status_load(peer) != TASK_DONE) {
        rt_shard* peer_shard = rt_task_owner_shard(ex, peer);
        rt_shard_lock(peer_shard);
        if (task_status_load(peer) != TASK_DONE) {
            (void)wake_task_on_shard_locked(ex, peer_shard, peer, 1, 0, 1, NULL);
        }
        rt_shard_unlock(peer_shard);
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    rt_shard_lock(ch_shard);
}

// Caller holds the channel owner's lock and the candidate's owner hint
// equals that shard: validate and deliver inline.
static inline int channel_deliver_same_shard_locked(rt_executor* ex,
                                                    rt_shard* owner_shard,
                                                    const waiter* w,
                                                    uint8_t resume_kind_value,
                                                    uint64_t resume_bits,
                                                    int signal_ready,
                                                    int* out_pushed) {
    rt_task* peer = get_task(ex, w->task_id);
    if (!channel_candidate_valid(peer, w)) {
        return 0;
    }
    peer->resume_kind = resume_kind_value;
    peer->resume_bits = resume_bits;
    int pushed = wake_task_on_shard_locked(
        ex, owner_shard, peer, channel_wake_force_inject_enabled(), 0, signal_ready, NULL);
    if (out_pushed != NULL) {
        *out_pushed = pushed;
    }
    return 1;
}

// Caller holds no lock. Validates and delivers to a foreign-owner peer
// under control -> peer owner (D5 collect-then-wake).
static inline int channel_deliver_foreign(rt_executor* ex,
                                          const waiter* w,
                                          uint8_t resume_kind_value,
                                          uint64_t resume_bits) {
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    int live = 0;
    rt_task* peer = get_task(ex, w->task_id);
    if (channel_candidate_valid(peer, w)) {
        rt_shard* peer_shard = rt_task_owner_shard(ex, peer);
        rt_shard_lock(peer_shard);
        int pushed = 0;
        if (channel_candidate_valid(peer, w)) {
            peer->resume_kind = resume_kind_value;
            peer->resume_bits = resume_bits;
            pushed = wake_task_on_shard_locked(ex, peer_shard, peer, 1, 0, 1, NULL);
            live = 1;
        }
        rt_shard_unlock(peer_shard);
        if (live) {
            channel_compat_broadcast_if_needed(ex, pushed);
        }
    }
    if (need_control) {
        rt_control_unlock(ex);
    }
    return live;
}

// Registers the current task on a channel key while the channel owner's
// lock is held (add_waiter would self-deadlock on the same shard). The
// park itself commits later through the wake_token dance.
static inline void
channel_park_prepare_locked(rt_shard* owner_shard, rt_task* task, waker_key key) {
    rt_waiter_store* store = &owner_shard->waiter_store;
    // Dedupe against the store itself: absorbed wakes and compat_cv wakeups
    // re-enter this path without their entry having been popped; appending
    // again would strand a duplicate that outlives this park and, once the
    // guest reuses the channel address, misdelivers into an unrelated park.
    // A matched entry must be re-armed with a fresh generation: it may be a
    // leftover from a superseded park (same task, same key — e.g. a reused
    // channel address across park generations), and an entry whose seq lags
    // task->park_seq validates false at delivery, which would pop-and-drop
    // the registration and strand this park forever.
    for (size_t i = 0; i < store->len; i++) {
        waiter* w = &store->entries[i];
        if (w->task_id == task->id && w->key.kind == key.kind && w->key.id == key.id) {
            task->park_seq++;
            if (task->park_seq == 0) {
                task->park_seq = 1;
            }
            w->seq = task->park_seq;
            task->park_key = key;
            task->park_prepared = 1;
            return;
        }
    }
    if (rt_waiter_store_ensure_cap(store) != RT_RUNTIME_STATUS_OK) {
        panic_msg("async: waiter allocation failed");
        return;
    }
    uint32_t owner_hint = task->owner_shard_valid != 0 ? task->owner_shard_id : 0;
    task->park_seq++;
    if (task->park_seq == 0) {
        task->park_seq = 1;
    }
    store->entries[store->len++] = (waiter){key, task->id, owner_hint, task->park_seq};
    task->park_key = key;
    task->park_prepared = 1;
}

static inline uint64_t channel_align_up(uint64_t size, uint64_t align) {
    if (align == 0) {
        return size;
    }
    uint64_t rem = size % align;
    if (rem == 0) {
        return size;
    }
    uint64_t add = align - rem;
    if (size > UINT64_MAX - add) {
        panic_msg("async: channel allocation overflow");
        return 0;
    }
    return size + add;
}

static inline uint64_t channel_buffer_offset(void) {
    return channel_align_up((uint64_t)sizeof(rt_channel), (uint64_t) _Alignof(uint64_t));
}

static inline uint64_t channel_alloc_size(uint64_t capacity) {
    if (capacity == 0) {
        return (uint64_t)sizeof(rt_channel);
    }
    uint64_t offset = channel_buffer_offset();
    if (capacity > (UINT64_MAX - offset) / (uint64_t)sizeof(uint64_t)) {
        panic_msg("async: channel allocation overflow");
        return 0;
    }
    return offset + capacity * (uint64_t)sizeof(uint64_t);
}

static inline rt_channel* channel_from_handle(void* handle) {
    if (handle == NULL) {
        panic_msg("async: null channel handle");
        return NULL;
    }
    return (rt_channel*)handle;
}

static inline int buf_push(rt_channel* ch, uint64_t value_bits) {
    if (ch == NULL || ch->capacity == 0 || ch->buf_len >= ch->capacity || ch->buf == NULL) {
        return 0;
    }
    size_t idx = (ch->buf_head + ch->buf_len) % ch->capacity;
    ch->buf[idx] = value_bits;
    ch->buf_len++;
    return 1;
}

static inline int buf_pop(rt_channel* ch, uint64_t* out_bits) {
    if (ch == NULL || ch->buf_len == 0 || ch->buf == NULL) {
        return 0;
    }
    if (out_bits != NULL) {
        *out_bits = ch->buf[ch->buf_head];
    }
    ch->buf_head = (ch->buf_head + 1) % ch->capacity;
    ch->buf_len--;
    return 1;
}

#endif
