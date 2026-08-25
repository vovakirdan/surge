#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"

#include <pthread.h>
#include <string.h>

// Freed-channel waiter row (Wave D, D0.6).
//
// rt_waiter_route.c resolves WAKER_CHAN_SEND / WAKER_CHAN_RECV by casting
// key.id back to rt_channel* and DEREFERENCING it (rt_channel_owner_shard_id),
// under a comment claiming channels are never freed. rt_far_channel.c's
// release_entry calls rt_channel_free. This row makes the two meet.
//
// The window is wake_task_with_policy's DEFERRED stale-key removal
// (rt_task_park.c): the wake captures the parked task's channel key under the
// owner shard lock, RELEASES that lock, and only then calls
// remove_waiter_generation -- which re-derives the store and the lock owner
// from the key by dereferencing the channel. In that gap the woken task can
// run to completion, and its completion is exactly what unpins the far-channel
// entry and frees the channel object.
//
// The interleaving is pinned by SP_WAKE_BEFORE_STALE_REMOVAL, which already
// sits in that gap.

static void*
start_anchored_send(rtb_anchored_state* state, const rt_far_task_handle* anchor, uint64_t value) {
    memset(state, 0, sizeof(*state));
    state->anchor = *anchor;
    state->value = value;
    state->body_poll_id = POLL_RTB_ANCHORED_BODY;
    return __task_create(POLL_RTB_ANCHORED_CALLER, state, rt_channel_opaque_word_ops());
}

typedef struct freed_waiter_canceller {
    rt_executor* ex;
    uint64_t body_task_id;
} freed_waiter_canceller;

// Runs on its own thread so the harness thread stays free to drive the
// rendezvous: cancel_task is control-held by contract, and the wake it
// performs is what parks this thread inside the stale-removal gap.
static void* freed_waiter_cancel_thread(void* arg) {
    freed_waiter_canceller* canceller = (freed_waiter_canceller*)arg;
    rt_control_lock(canceller->ex);
    cancel_task(canceller->ex, canceller->body_task_id);
    rt_control_unlock(canceller->ex);
    return NULL;
}

// Counts store entries whose key names `channel_bits`. The comparison is on
// the key VALUE only -- the harness never dereferences the freed address.
static size_t freed_waiter_entries_for(rt_executor* ex, uint32_t shard_id, uint64_t channel_bits) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    if (shard == NULL) {
        return 0;
    }
    size_t count = 0;
    rt_shard_lock(shard);
    const rt_waiter_store* store = &shard->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        waker_kind kind = (waker_kind)store->entries[i].key.kind;
        if ((kind == WAKER_CHAN_SEND || kind == WAKER_CHAN_RECV) &&
            store->entries[i].key.id == channel_bits) {
            count++;
        }
    }
    rt_shard_unlock(shard);
    return count;
}

static uint64_t freed_waiter_body_task_id(const rtb_anchored_state* state) {
    rt_remote_task_pending* pending = state->pending;
    if (pending == NULL) {
        return 0;
    }
    return pending->handle.task_id;
}

int rtb_mode_anchored_freed_channel_waiter(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("freed-waiter mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    if (channel == NULL) {
        return rtb_fail("freed-waiter anchor did not resolve");
    }
    // Kept as an integer from here on: the object under it is freed mid-row.
    const uint64_t channel_bits = (uint64_t)(uintptr_t)channel;
    // Occupy the single slot so the next anchored send parks owner-side and
    // leaves a channel-POINTER-keyed waiter in shard 1's store.
    rtb_anchored_state prefill;
    void* prefill_caller = start_anchored_send(&prefill, &minted.handle, 5);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("freed-waiter prefill failed");
    }
    rtb_anchored_state state;
    void* caller = start_anchored_send(&state, &minted.handle, 6);
    (void)caller;
    if (!rtb_wait_u32(&state.body_ran, 1, 5000)) {
        return rtb_fail("freed-waiter body did not start");
    }
    uint64_t body_task_id = 0;
    for (uint32_t i = 0; i < 5000 && body_task_id == 0; i++) {
        body_task_id = freed_waiter_body_task_id(&state);
        if (body_task_id == 0) {
            rtb_sleep_us(1000);
        }
    }
    if (body_task_id == 0) {
        return rtb_fail("freed-waiter body task id never bound");
    }
    // The body must actually be PARKED on the channel key, or the wake below
    // captures no stale key and the row proves nothing.
    rt_task* body = get_task(ex, body_task_id);
    int parked = 0;
    for (uint32_t i = 0; i < 5000 && !parked; i++) {
        parked = body != NULL && task_status_load(body) == TASK_WAITING &&
                 task_enqueued_load(body) == 0 && body->park_key.kind == WAKER_CHAN_SEND &&
                 body->park_key.id == channel_bits;
        if (!parked) {
            rtb_sleep_us(1000);
        }
    }
    if (!parked) {
        return rtb_fail("freed-waiter body never parked on the channel send key");
    }
    if (freed_waiter_entries_for(ex, minted.handle.owner_shard_id, channel_bits) != 1) {
        return rtb_fail("freed-waiter park left no pointer-keyed entry to orphan");
    }
    // Drop the only lease. The dispatch-time pin still holds the entry, so
    // nothing is reclaimed yet -- but active_leases is now 0, so the unpin at
    // the body's reply edge becomes the reclaim, and the reclaim frees the
    // channel object the parked waiter's key points at.
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("freed-waiter release failed");
    }
    if (rt_far_channel_debug_live_count(ex) == 0) {
        return rtb_fail("freed-waiter entry was reclaimed while pinned");
    }
    unsigned before = rt_sync_point_reached_count(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL);
    freed_waiter_canceller canceller = {ex, body_task_id};
    pthread_t thread;
    if (pthread_create(&thread, NULL, freed_waiter_cancel_thread, &canceller) != 0) {
        return rtb_fail("freed-waiter cancel thread failed to start");
    }
    if (!rt_sync_point_wait_until_after(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL, before)) {
        (void)pthread_join(thread, NULL);
        return rtb_fail("freed-waiter wake never reached the stale-removal gap");
    }
    // The wake is parked in the gap holding the captured channel key. Let the
    // woken body complete: its reply edge unpins the last hold on the entry,
    // release_entry runs, and rt_channel_free releases the object the key
    // still names.
    int reclaimed = 0;
    for (uint32_t i = 0; i < 20000 && !reclaimed; i++) {
        reclaimed = rt_far_channel_debug_live_count(ex) == 0;
        if (!reclaimed) {
            rtb_sleep_us(1000);
        }
    }
    if (!reclaimed) {
        rt_sync_point_open();
        (void)pthread_join(thread, NULL);
        return rtb_fail("freed-waiter channel was never reclaimed in the gap");
    }
    // Release the parked wake. Everything after this line routes a key whose
    // channel is already freed: remove_waiter_generation -> rt_waiter_key_shard
    // / rt_waiter_store_for_key. The negative-control build resolves that by
    // dereferencing the freed object; the fixed build reads the shard the key
    // carries.
    rt_sync_point_open();
    (void)pthread_join(thread, NULL);
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL) != before + 1) {
        return rtb_fail("freed-waiter gap was not exercised exactly once");
    }
    // Positive claim: the deferred removal reached the RIGHT store and retired
    // the orphaned entry. A route that lands on the wrong shard leaves it.
    if (freed_waiter_entries_for(ex, minted.handle.owner_shard_id, channel_bits) != 0) {
        return rtb_fail("freed-waiter stale channel entry survived the deferred removal");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
