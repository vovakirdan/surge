#include "rt_async_trace.h"
#include "rt_channel_lane.h"

// The bounded claim-retry budget (RV2-DEBT-277). Redesigned from the
// codex/rv2-channel-retry lane's patch rather than transplanted: the lane's
// single counter per logical operation stays, and it gains the prefix of the
// refusals before the one that exhausts it, so a select parks on every
// channel that refused it (rt_channel_retry.h). The release-side wake lives
// here rather than in rt_channel_lane.h, whose size gate it would cross.

static _Atomic uint64_t claim_refusals[RT_CHANNEL_CLAIM_REFUSAL_COUNT];
static _Atomic uint64_t retry_republications;
static _Atomic uint64_t retry_budget_exhaustions;
static _Atomic uint64_t max_retries_per_operation;
static _Atomic uint64_t claim_releases;

static void trace_increment(_Atomic uint64_t* counter) {
    if (rt_exec_trace_enabled() && counter != NULL) {
        (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
    }
}

static void trace_max(_Atomic uint64_t* counter, uint64_t value) {
    if (!rt_exec_trace_enabled() || counter == NULL) {
        return;
    }
    uint64_t current = atomic_load_explicit(counter, memory_order_relaxed);
    while (current < value &&
           !atomic_compare_exchange_weak_explicit(
               counter, &current, value, memory_order_relaxed, memory_order_relaxed)) {
    }
}

// Remembers the refused arm in the prefix ring, oldest first, one entry per
// distinct {channel, direction}: a select refused on the same arm twice is
// still parked on that arm once.
static void retry_remember(rt_channel_retry_state* st,
                           const rt_channel* channel,
                           rt_channel_retry_operation arm,
                           rt_channel_claim_refusal_cause cause) {
    uint64_t id = (uint64_t)(uintptr_t)channel;
    for (uint8_t i = 0; i < st->prefix_len; i++) {
        if (st->prefix[i].channel == id && st->prefix[i].operation == (uint8_t)arm) {
            st->prefix[i].cause = (uint8_t)cause;
            return;
        }
    }
    if (st->prefix_len < RT_CHANNEL_RETRY_PREFIX) {
        st->prefix[st->prefix_len++] = (rt_channel_retry_refusal){id, (uint8_t)arm, (uint8_t)cause};
        return;
    }
    // Full: drop the oldest, keep the newest. A select with more than eight
    // distinct refusing arms parks on the last eight it met, which is the
    // budget's own bound on how many refusals it counted.
    for (uint8_t i = 1; i < RT_CHANNEL_RETRY_PREFIX; i++) {
        st->prefix[i - 1] = st->prefix[i];
    }
    st->prefix[RT_CHANNEL_RETRY_PREFIX - 1] =
        (rt_channel_retry_refusal){id, (uint8_t)arm, (uint8_t)cause};
}

int rt_channel_retry_refused(rt_task* task,
                             rt_channel_retry_operation operation,
                             uint64_t key_id,
                             const rt_channel* channel,
                             rt_channel_retry_operation arm,
                             rt_channel_claim_refusal_cause cause) {
    if (task == NULL || operation == RT_CHANNEL_RETRY_NONE) {
        // Nothing to count against: never "exhausted", which would send a
        // caller down the park path with no task to park.
        return 0;
    }
    if ((unsigned)cause < (unsigned)RT_CHANNEL_CLAIM_REFUSAL_COUNT) {
        trace_increment(&claim_refusals[cause]);
    }
    rt_channel_retry_state* st = &task->channel_retry;
    if (st->operation != (uint8_t)operation || st->key_id != key_id) {
        // Another logical operation: the previous one made progress or
        // completed, and its count and its prefix go with it.
        *st = (rt_channel_retry_state){0};
        st->operation = (uint8_t)operation;
        st->key_id = key_id;
    }
    if (channel != NULL) {
        retry_remember(st, channel, arm, cause);
    }
    if (st->count < RT_CHANNEL_RETRY_BUDGET) {
        st->count++;
        trace_max(&max_retries_per_operation, st->count);
        if (st->count == RT_CHANNEL_RETRY_BUDGET) {
            trace_increment(&retry_budget_exhaustions);
        }
    }
    return st->count >= RT_CHANNEL_RETRY_BUDGET;
}

void rt_channel_retry_republished(void) {
    trace_increment(&retry_republications);
}

void rt_channel_retry_reset(rt_task* task) {
    if (task == NULL) {
        return;
    }
    // Every successful claim resets the budget; a task that was never
    // refused has nothing to reset, and clearing the prefix array here on
    // every channel operation was a measurable share of a local select's
    // cost (2026-09-04, select-send-scalar).
    if (task->channel_retry.count == 0 && task->channel_retry.prefix_len == 0 &&
        task->channel_retry.operation == RT_CHANNEL_RETRY_NONE) {
        return;
    }
    task->channel_retry = (rt_channel_retry_state){0};
}

void rt_channel_wait_after_claim_refusal_locked(rt_shard* channel_shard,
                                                rt_channel* channel,
                                                rt_task* task,
                                                rt_channel_retry_operation operation,
                                                rt_channel_claim_refusal_cause cause) {
    waker_key retry_key = operation == RT_CHANNEL_RETRY_SEND ? channel_send_retry_key(channel)
                                                             : channel_recv_retry_key(channel);
    int exhausted = rt_channel_retry_refused(
        task, operation, (uint64_t)(uintptr_t)channel, channel, operation, cause);
    if (exhausted) {
        // Exhausted: park on the channel's own retry key. The registration is
        // made here, under the owner lane the refusal was observed under, so
        // the release that would wake it cannot slip between the two.
        channel_park_prepare_locked(channel_shard, task, retry_key);
        rt_shard_unlock(channel_shard);
        pending_key = retry_key;
        return;
    }
    rt_channel_retry_republished();
    rt_shard_unlock(channel_shard);
    pending_key = waker_none();
}

#ifndef RV2_DEBT_277_RETRY_HANDOFF_NEGATIVE_CONTROL
// Select subscriptions (seq == 0, one select parked across several keys) share
// a retry key with generation-qualified direct waiters but are not part of
// their FIFO. Pop one requested class without disturbing the order of the
// other.
static int channel_pop_retry_class_locked(rt_shard* owner_shard,
                                          waker_key key,
                                          int select_subscription,
                                          waiter* out) {
    rt_waiter_store* store = &owner_shard->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        int is_select = w.seq == 0;
        if (w.key.kind != key.kind || w.key.id != key.id || is_select != select_subscription) {
            continue;
        }
        memmove(&store->entries[i], &store->entries[i + 1], (store->len - i - 1) * sizeof(waiter));
        store->len--;
        rt_channel_key_retired(key, 1);
        *out = w;
        return 1;
    }
    return 0;
}

static size_t channel_retry_select_count_locked(const rt_shard* owner_shard, waker_key key) {
    size_t count = 0;
    const rt_waiter_store* store = &owner_shard->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        const waiter* w = &store->entries[i];
        if (w->seq == 0 && w->key.kind == key.kind && w->key.id == key.id) {
            count++;
        }
    }
    return count;
}
#endif

// A retry-budget waiter is arbitration-only: it is not the channel's oldest
// ordinary sender or receiver, and waking it must not consume or reorder
// either FIFO -- the distinct retry keys make that structural.
//
// A release may find a select row before its task commits WAITING, or after a
// sibling arm already made it READY; in both cases the wake token is harmless
// and the row must not consume the sole handoff. So: drain every live select
// row present in the owner-lane snapshot (a foreign select wake may release
// and reacquire the lane, and rows registered afterwards belong to the next
// release), then wake at most the oldest still-valid direct waiter.
static void channel_wake_retry_waiter_locked(rt_executor* ex, rt_shard* ch_shard, waker_key key) {
    waiter cand;
#ifdef RV2_DEBT_277_RETRY_HANDOFF_NEGATIVE_CONTROL
    // Rule 13: one mixed FIFO lets whichever class is first consume the
    // release, stranding a select behind a direct waiter or the reverse.
    while (channel_pop_candidate_locked(ch_shard, key, &cand)) {
        const rt_task* peer = get_task(ex, cand.task_id);
        if (peer == NULL || task_status_load(peer) == TASK_DONE || task_cancelled_load(peer) != 0) {
            continue;
        }
        if (cand.seq != 0 && !channel_candidate_valid(peer, &cand)) {
            continue;
        }
        channel_wake_only(ex, ch_shard, &cand);
        return;
    }
#else
    size_t select_count = channel_retry_select_count_locked(ch_shard, key);
    for (size_t i = 0; i < select_count; i++) {
        if (!channel_pop_retry_class_locked(ch_shard, key, 1, &cand)) {
            break;
        }
        const rt_task* peer = get_task(ex, cand.task_id);
        if (peer == NULL || task_status_load(peer) == TASK_DONE || task_cancelled_load(peer) != 0) {
            continue;
        }
        channel_wake_only(ex, ch_shard, &cand);
    }
    while (channel_pop_retry_class_locked(ch_shard, key, 0, &cand)) {
        const rt_task* peer = get_task(ex, cand.task_id);
        if (!channel_candidate_valid(peer, &cand)) {
            continue;
        }
        channel_wake_only(ex, ch_shard, &cand);
        return;
    }
#endif
}

void rt_channel_claim_released_locked(rt_executor* ex, rt_shard* ch_shard, const rt_channel* ch) {
    rt_channel_trace_claim_released();
    // Nobody stands on either retry key: the walk below would find nothing,
    // and it walked the whole owner store four times on every send and
    // receive (2026-09-04, select-send-scalar). The count is exact under the
    // owner lock this is called with (rt_channel_key_registered/retired).
    if (ch->retry_waiters == 0) {
        return;
    }
#ifdef RV2_DEBT_277_WAKE_NEGATIVE_CONTROL
    // Rule 13: a release that wakes nobody. The waker stays compiled so the
    // mutant differs from the tree by this one call.
    (void)ex;
    (void)ch_shard;
    (void)ch;
    (void)channel_wake_retry_waiter_locked;
#else
    channel_wake_retry_waiter_locked(ex, ch_shard, channel_send_retry_key(ch));
    channel_wake_retry_waiter_locked(ex, ch_shard, channel_recv_retry_key(ch));
#endif
}

void rt_channel_trace_claim_released(void) {
    trace_increment(&claim_releases);
}

size_t rt_channel_trace_append(char* buf, size_t* pos, size_t cap) {
    static const char* const refusal_names[RT_CHANNEL_CLAIM_REFUSAL_COUNT] = {
        "channel_claim_refusals_ring_push",
        "channel_claim_refusals_ring_pop",
        "channel_claim_refusals_park_take",
        "channel_claim_refusals_rendezvous",
    };
    for (size_t i = 0; i < RT_CHANNEL_CLAIM_REFUSAL_COUNT; i++) {
        trace_append_kv_u64(buf,
                            pos,
                            cap,
                            refusal_names[i],
                            atomic_load_explicit(&claim_refusals[i], memory_order_relaxed));
    }
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_retry_republications",
                        atomic_load_explicit(&retry_republications, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_retry_budget_exhaustions",
                        atomic_load_explicit(&retry_budget_exhaustions, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_max_retries_per_operation",
                        atomic_load_explicit(&max_retries_per_operation, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_claim_releases",
                        atomic_load_explicit(&claim_releases, memory_order_relaxed));
    return rt_channel_claim_trace_append(buf, pos, cap);
}
