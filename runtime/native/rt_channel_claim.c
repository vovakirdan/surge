#include "rt_async_trace.h"
#include "rt_channel_lane.h"

// The receive claim: see rt_channel_claim.h. Everything here runs under the
// channel owner's shard lock; the two deliveries release it the way every
// peer delivery in the lane does.

static _Atomic uint64_t claims_opened;
static _Atomic uint64_t claims_close_won;
static _Atomic uint64_t claims_aborted;
static _Atomic uint64_t recoveries_dead_receiver;
static _Atomic uint64_t values_destroyed_in_recovery;

// always_inline for the reason its twin in rt_channel_retry.c is: the claim
// opens and retires on every rendezvous send that finds a waiting receiver,
// the runtime is compiled without -O (RV2-DEBT-333), and out of line this is
// a call that reads one flag which is off by default and returns.
static inline __attribute__((always_inline)) void claim_trace_increment(_Atomic uint64_t* counter) {
    if (rt_exec_trace_enabled()) {
        (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
    }
}

static int claim_names(const rt_channel_recv_claim* claim, const waiter* receiver) {
    return claim->receiver.task_id == receiver->task_id && claim->receiver.seq == receiver->seq;
}

int channel_recv_claim_open_locked(rt_channel* ch, const waiter* receiver) {
    rt_channel_recv_claim* claim = &ch->recv_claim;
    if (claim->active != 0) {
        return 0;
    }
    claim->receiver = *receiver;
    claim->active = 1;
    claim->close_won = 0;
    claim_trace_increment(&claims_opened);
    return 1;
}

int channel_recv_claim_take_locked(rt_executor* ex,
                                   rt_shard* ch_shard,
                                   rt_channel* ch,
                                   const waiter* receiver) {
    rt_channel_recv_claim* claim = &ch->recv_claim;
    if (claim->close_won != 0 && claim_names(claim, receiver)) {
        // Close settled this receiver while the window was open: the commit
        // is refused and the retirement is the sender's.
        claim->close_won = 0;
        return 0;
    }
    if (claim->active == 0 || !claim_names(claim, receiver)) {
#ifdef RV2_CLAIM_OVERTAKE_NEGATIVE_CONTROL
        // Rule 13, continued: the overtaking send commits past the claim.
        return 1;
#else
        panic_msg("async: a rendezvous commit found no claim on its receiver");
        return 0;
#endif
    }
#ifndef RV2_CLOSE_WINS_NEGATIVE_CONTROL
    if (ch->closed != 0) {
        // The channel closed with this claim open and close did not settle
        // it. The pop and the open are one hold of the lane, so this cannot
        // happen in the tree; the model decides by the closed flag at commit
        // all the same: settle the receiver as closed here, once, and refuse.
        channel_recv_claim_close_locked(ex, ch_shard, ch);
        claim->close_won = 0;
        return 0;
    }
#endif
    claim->active = 0;
    // The claim was what refused every later send; retiring it is their
    // release (rt_channel_retry.h).
    rt_channel_claim_released_locked(ex, ch_shard, ch);
    return 1;
}

void channel_recv_claim_abort_locked(rt_executor* ex,
                                     rt_shard* ch_shard,
                                     rt_channel* ch,
                                     const waiter* receiver) {
    rt_channel_recv_claim* claim = &ch->recv_claim;
    if (claim->close_won != 0 && claim_names(claim, receiver)) {
        // Close already woke the receiver as closed: nothing to put back.
        claim->close_won = 0;
        claim_trace_increment(&claims_aborted);
        rt_channel_claim_released_locked(ex, ch_shard, ch);
        return;
    }
    if (claim->active == 0 || !claim_names(claim, receiver)) {
        return;
    }
    claim->active = 0;
    claim_trace_increment(&claims_aborted);
    const rt_task* peer = get_task(ex, receiver->task_id);
    if (channel_candidate_valid(peer, receiver)) {
        // Still parked on this key, still the oldest: back to the head, not
        // woken to re-register behind everyone who arrived meanwhile.
        channel_push_candidate_front_locked(ch_shard, receiver);
    }
    rt_channel_claim_released_locked(ex, ch_shard, ch);
}

void channel_recv_claim_close_locked(rt_executor* ex, rt_shard* ch_shard, rt_channel* ch) {
    rt_channel_recv_claim* claim = &ch->recv_claim;
    if (claim->active == 0) {
        return;
    }
#ifdef RV2_CLOSE_WINS_NEGATIVE_CONTROL
    // Rule 13: close does not see the claim, and the late commit delivers a
    // value on a closed channel.
    (void)ex;
    (void)ch_shard;
    return;
#else
    claim->active = 0;
    claim->close_won = 1;
    claim_trace_increment(&claims_close_won);
    waiter cand = claim->receiver;
    if (cand.owner_hint == ch->owner_shard_id) {
        int pushed = 0;
        int live = channel_deliver_same_shard_locked(
            ex, ch_shard, &cand, RESUME_CHAN_RECV_CLOSED, (rt_park_token){0}, 1, &pushed);
        if (live && !pushed) {
            rt_shard_unlock(ch_shard);
            channel_compat_broadcast_if_needed(ex, 0);
            rt_shard_lock(ch_shard);
        }
    } else {
        rt_shard_unlock(ch_shard);
        (void)channel_deliver_foreign(ex, &cand, RESUME_CHAN_RECV_CLOSED, (rt_park_token){0});
        rt_shard_lock(ch_shard);
    }
    // The refused senders wake, find the channel closed, and answer for it.
    rt_channel_claim_released_locked(ex, ch_shard, ch);
#endif
}

void channel_push_candidate_front_locked(rt_shard* owner_shard, const waiter* w) {
    rt_waiter_store* store = &owner_shard->waiter_store;
    if (rt_waiter_store_ensure_cap(store) != RT_RUNTIME_STATUS_OK) {
        fatal_oom_msg("async: waiter allocation failed");
        return;
    }
    size_t at = store->len;
    for (size_t i = 0; i < store->len; i++) {
        if (store->entries[i].key.kind == w->key.kind && store->entries[i].key.id == w->key.id) {
            at = i;
            break;
        }
    }
    memmove(&store->entries[at + 1], &store->entries[at], (store->len - at) * sizeof(waiter));
    store->entries[at] = *w;
    store->len++;
    rt_channel_key_registered(w->key);
}

// The control-lane pair's third member: a claim that will not be finished.
// The caller holds control and the operation pin, moved nothing, and gives
// back what the claim took -- the delivery reservation and the park slot, or
// the ring cell -- and the receiver, if it is still parked. Idempotent: a
// second call finds RT_CHANNEL_PUT_NONE.
void rt_channel_abandon_send_locked(rt_executor* ex, void* channel, rt_channel_put* put) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || put == NULL || put->kind == RT_CHANNEL_PUT_NONE) {
        return;
    }
    rt_channel_assert_pinned(ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    if (put->kind == RT_CHANNEL_PUT_INTO_RING) {
        (void)rt_typed_fifo_abandon_push_locked(&ch->ring, &put->ticket);
    } else if (put->kind == RT_CHANNEL_PUT_INTO_PARK) {
        (void)rt_park_pool_abandon_deliver_locked(&ch->parks, &put->slot);
        channel_end_park_locked(ex, ch_shard, ch, &put->slot);
        if (put->has_candidate) {
            channel_recv_claim_abort_locked(ex, ch_shard, ch, &put->candidate);
        }
    }
    put->kind = RT_CHANNEL_PUT_NONE;
    rt_shard_unlock(ch_shard);
}

void rt_channel_release_orphan_put(rt_executor* ex, void* channel, rt_channel_put* put) {
    rt_channel* ch = channel_from_handle(channel);
    if (ex == NULL || ch == NULL || put == NULL || put->kind != RT_CHANNEL_PUT_ORPHAN) {
        return;
    }
    rt_channel_assert_pinned(ch);
    rt_shard* ch_shard = channel_owner_shard(ex, ch);
    rt_shard_lock(ch_shard);
    // Exactly once: the drop runs with the owner lane released inside.
    channel_end_park_locked(ex, ch_shard, ch, &put->slot);
    rt_shard_unlock(ch_shard);
    put->kind = RT_CHANNEL_PUT_NONE;
}

void rt_channel_trace_recovery_dead_receiver(void) {
    claim_trace_increment(&recoveries_dead_receiver);
}

void rt_channel_trace_value_destroyed_in_recovery(void) {
    claim_trace_increment(&values_destroyed_in_recovery);
}

size_t rt_channel_claim_trace_append(char* buf, size_t* pos, size_t cap) {
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_recv_claims_opened",
                        atomic_load_explicit(&claims_opened, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_recv_claims_close_won",
                        atomic_load_explicit(&claims_close_won, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_recv_claims_aborted",
                        atomic_load_explicit(&claims_aborted, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_rendezvous_recoveries_dead_receiver",
                        atomic_load_explicit(&recoveries_dead_receiver, memory_order_relaxed));
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "channel_values_destroyed_in_recovery",
                        atomic_load_explicit(&values_destroyed_in_recovery, memory_order_relaxed));
    return *pos;
}
