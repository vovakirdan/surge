#include "rt_channel_refcount.h"

#include "rt_channel_lane.h"
#include "rt_sync_point.h"

// The channel's two reference kinds and the single reclaim they lead to. See
// rt_channel_refcount.h for why they are counted apart.

static rt_channel* refcount_channel(void* channel) {
    return channel == NULL ? NULL : (rt_channel*)channel;
}

// The reclaim decision, taken by whichever release retired the last thing that
// named the channel.
//
// Both kinds retire independently, so two lanes can each observe the pair at
// zero -- the one that dropped the last handle and the one that released the
// last pin. The exchange is what makes exactly one of them the reclaimer and
// the other a no-op; without it the pair would be freed twice. It is NOT the
// "mark the object dying" step section 7 describes for teardown: that one runs
// under the owner lock and detaches the initialized slots. This says only WHO
// reclaims, never what the reclaim does.
static void channel_reclaim_if_unreferenced(rt_channel* ch) {
    if (atomic_load_explicit(&ch->handle_refs, memory_order_acquire) != 0 ||
        atomic_load_explicit(&ch->pins, memory_order_acquire) != 0) {
        return;
    }
    if (atomic_exchange_explicit(&ch->reclaiming, (uint8_t)1, memory_order_acq_rel) != 0) {
        return;
    }
    // The window the deterministic proof holds: the last release has been
    // observed and the object has not been handed over yet.
    RT_SYNC_POINT(SP_CHANNEL_LAST_RELEASE_BEFORE_FREE);
    rt_channel_free_when_unlocked(ch);
}

void rt_channel_handle_refs_init(void* channel) {
    rt_channel* ch = refcount_channel(channel);
    if (ch == NULL) {
        return;
    }
    // Zeroing and minting are one step on purpose: a channel must never be
    // observable with a count of zero, not even for the instant between the
    // memset that clears the header and the store that claims the creator's
    // handle.
    memset(ch, 0, sizeof(rt_channel));
    atomic_store_explicit(&ch->handle_refs, 1, memory_order_relaxed);
}

void rt_channel_handle_retain(void* channel) {
    rt_channel* ch = refcount_channel(channel);
    if (ch == NULL) {
        return;
    }
    // Relaxed: a retain can only be issued by a lane that already holds a live
    // handle, so it cannot be the edge that publishes the object to anyone.
    // Atomic all the same -- two lanes copying a shared handle at once are the
    // ordinary case, and a lost increment frees the object under a live holder.
    (void)RT_DEBT155_HANDLE_ACQUIRE(&ch->handle_refs);
}

void rt_channel_handle_drop(void* channel) {
    rt_channel* ch = refcount_channel(channel);
    if (ch == NULL) {
        return;
    }
    if (atomic_load_explicit(&ch->handle_refs, memory_order_relaxed) == 0) {
        // An over-release: give nothing back rather than wrapping the count to
        // UINT32_MAX and stranding the object forever. task_release_lane_aware
        // guards the same way for the same reason.
        return;
    }
    if (RT_DEBT155_HANDLE_RELEASE(&ch->handle_refs) != 1) {
        return;
    }
    channel_reclaim_if_unreferenced(ch);
}

void rt_channel_pin(void* channel) {
    rt_channel* ch = refcount_channel(channel);
    if (ch == NULL) {
        return;
    }
    (void)atomic_fetch_add_explicit(&ch->pins, 1, memory_order_relaxed);
}

void rt_channel_unpin(void* channel) {
    rt_channel* ch = refcount_channel(channel);
    if (ch == NULL) {
        return;
    }
    if (atomic_load_explicit(&ch->pins, memory_order_relaxed) == 0) {
        return;
    }
    if (atomic_fetch_sub_explicit(&ch->pins, 1, memory_order_acq_rel) != 1) {
        return;
    }
    channel_reclaim_if_unreferenced(ch);
}

// How many tasks are still registered on either of the channel's two keys.
//
// Reads the owner shard's store under that shard's own lock, which is legal
// here only because rt_channel_free has already refused to run on a lane that
// holds one. Releasing that lock is also the moment a lane runs its deferred
// work, so a reclaim queued behind this one can run from inside this call --
// bounded, and harmless, because the queue hands each channel out exactly once
// before freeing it.
static size_t channel_registered_waiters(rt_executor* ex, const rt_channel* ch) {
    rt_shard* owner = ex == NULL ? NULL : channel_owner_shard(ex, ch);
    if (owner == NULL) {
        return 0;
    }
    const waker_key keys[2] = {channel_recv_key(ch), channel_send_key(ch)};
    size_t found = 0;
    rt_shard_lock(owner);
    const rt_waiter_store* store = &owner->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        for (size_t k = 0; k < 2; k++) {
            if (store->entries[i].key.kind == keys[k].kind &&
                store->entries[i].key.id == keys[k].id) {
                found++;
            }
        }
    }
    rt_shard_unlock(owner);
    return found;
}

void rt_channel_assert_reclaimable(void* channel) {
    const rt_channel* ch = channel == NULL ? NULL : (const rt_channel*)channel;
    if (ch == NULL) {
        return;
    }
    if (RT_DEBT155_STILL_NAMED(atomic_load_explicit(&ch->handle_refs, memory_order_acquire)) != 0) {
        panic_msg("async: a channel was reclaimed while a handle still named it");
        return;
    }
    if (RT_DEBT155_STILL_NAMED(atomic_load_explicit(&ch->pins, memory_order_acquire)) != 0) {
        panic_msg("async: a channel was reclaimed while an operation still pinned it");
        return;
    }
    // A registered waiter is meant to hold a PIN, and once it does (C3) the
    // assertion above answers for it. Until then this is a debug invariant and
    // says so: it reports rather than refuses, because the deferred stale-key
    // removal in rt_task_park.c deliberately leaves an entry outliving its
    // channel today (RV2-DEBT-199), and that row must keep passing.
    if (!rt_async_debug_enabled()) {
        return;
    }
    size_t registered = channel_registered_waiters(ensure_exec(), ch);
    if (registered != 0) {
        rt_async_debug_printf("async chan free ch=%p with waiters=%llu\n",
                              (const void*)ch,
                              (unsigned long long)registered);
    }
}
