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
    if (atomic_load_explicit(&ch->dying, memory_order_acquire) != 0) {
        // Reached through something that should already have been retired: the
        // reclaim set this under the owner lock after the assertion below found
        // no handle and no pin. Say so here rather than let the operation write
        // into storage that is about to be freed.
        panic_msg("async: an operation entered a channel that is being destroyed");
        return;
    }
    (void)RT_CHANNEL_PIN_ACQUIRE(&ch->pins);
}

void rt_channel_unpin(void* channel) {
    rt_channel* ch = refcount_channel(channel);
    if (ch == NULL) {
        return;
    }
    if (atomic_load_explicit(&ch->pins, memory_order_relaxed) == 0) {
        return;
    }
    if (RT_CHANNEL_PIN_RELEASE(&ch->pins) != 1) {
        return;
    }
    channel_reclaim_if_unreferenced(ch);
}

// The two hooks the waiter-store mutation points call. A store entry naming a
// channel key IS one of section 7's internal pins, for the whole time the entry
// exists: a registered waiter's, or a select subscription's.
//
// Reading the key as a pointer is legal here and nowhere else. A key is opaque
// identity precisely because it can outlive its object -- but that is the state
// these two exist to end, and each is called at the instant the entry is
// created or destroyed, under the store's own lock, with the object provably
// alive: at registration because the operation building the key holds it, and
// at retirement because the entry being retired is itself a hold.
static rt_channel* channel_from_key(waker_key key) {
    if (key.kind != WAKER_CHAN_SEND && key.kind != WAKER_CHAN_RECV) {
        return NULL;
    }
    return (rt_channel*)(uintptr_t)key.id;
}

void rt_channel_key_registered(waker_key key) {
    rt_channel_pin(channel_from_key(key));
}

void rt_channel_key_retired(waker_key key, size_t count) {
    rt_channel* ch = channel_from_key(key);
    if (ch == NULL) {
        return;
    }
    for (size_t i = 0; i < count; i++) {
        rt_channel_unpin(ch);
    }
}

// How many tasks are still registered on either of the channel's two keys.
// Reads the owner shard's store, which the caller has already locked.
static size_t channel_registered_waiters_locked(const rt_shard* owner, const rt_channel* ch) {
    const waker_key keys[2] = {channel_recv_key(ch), channel_send_key(ch)};
    size_t found = 0;
    const rt_waiter_store* store = &owner->waiter_store;
    for (size_t i = 0; i < store->len; i++) {
        for (size_t k = 0; k < 2; k++) {
            if (store->entries[i].key.kind == keys[k].kind &&
                store->entries[i].key.id == keys[k].id) {
                found++;
            }
        }
    }
    return found;
}

void rt_channel_assert_reclaimable_locked(const struct rt_shard* owner, void* channel) {
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
    if (owner == NULL) {
        return;
    }
    // A registered entry IS a pin, so reaching here with one means the two
    // counts disagree with the store they are supposed to summarise. This used
    // to print under a debug flag and continue, which refused nothing: a
    // channel freed under a live registration left a key naming freed storage,
    // and the only thing that noticed was a reader of the log.
    size_t registered = RT_CHANNEL_REGISTERED(channel_registered_waiters_locked(owner, ch));
    if (registered != 0) {
        panic_msg("async: a channel was reclaimed while a waiter was registered on it");
    }
}

// WHERE THE RECLAIM RUNS, AND WHY IT IS NOT WHERE THE LAST RELEASE HAPPENS.
//
// Reclaiming destroys what the channel still holds, and destroying an element
// runs its own drop -- generated code, which may not run under a scheduler
// lock. Callers reach the last release holding one as a matter of course:
// completion bookkeeping takes the control lane, and a far-channel unpin that
// drops the last hold runs from inside it. So the release does not decide;
// this queue does. A lane that holds nothing reclaims now, and a lane that
// holds a lock hands the object over until rt_lane.c calls the drain below at
// the moment it lets go.
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

void rt_channel_reclaim_drain(void) {
    rt_channel_reclaim_queue* queue = &channel_reclaim_queue;
    while (queue->len > 0) {
        void* channel = queue->items[--queue->len];
        rt_channel_free(channel);
    }
    if (queue->items == NULL) {
        return;
    }
    // The queue is per LANE, and a lane can stop existing: any thread that
    // reaches a last release owns one of these, not just the workers. Nothing
    // runs on a thread's way out, so an array kept for the next batch is an
    // array that leaks when there is no next batch. It is given back the moment
    // the queue empties instead -- one allocation per batch of deferred
    // reclaims, which is a rate nothing measures, against a leak per thread.
    //
    // Safe under the re-entrancy this drain has by construction: rt_channel_free
    // takes the owner lock, so a release inside it queues here and runs a nested
    // drain when that lock goes. The nested one frees the array and leaves len
    // at zero; the outer loop re-reads len and this guard, and both are correct
    // for an array that is already gone.
    rt_free((uint8_t*)queue->items, queue->cap * sizeof(void*), _Alignof(void*));
    queue->items = NULL;
    queue->cap = 0;
}

// Teardown, in the order section 7 of docs/RUNTIME_V2.md prescribes:
//
//   "under the owner lock mark the object dying, detach its initialized slots,
//    invalidate generations; release the lock; run drop_in_place on the
//    detached values; free the storage. No destructor and no user operation
//    runs under the channel lock."
//
// Every clause is load-bearing, and none of them is bookkeeping.
//
// UNDER THE OWNER LOCK, because that is the lock that answers for the ring, the
// park pool and the two waiter keys. Taking it is what lets the quiescence
// assertion read the store at all: the count of registrations and the pin count
// it is supposed to summarise are only consistent with each other while the
// lock that mutates both is held.
//
// DETACH BEFORE ANY DROP, because an element's drop is user code and may re-
// enter the runtime. A slot-by-slot drain hands it an object that is half torn
// down -- some values gone, some still findable, a park pool still handing out
// slots. Detaching everything first means the only thing left to find is empty.
//
// INVALIDATE GENERATIONS, because a park token or a ring ticket taken before
// this names a turn that no longer exists; a commit presented afterwards must
// be refused rather than published into storage about to be freed.
//
// RELEASE THE LOCK BEFORE THE DROPS, because generated code may not run under a
// scheduler lock -- the same rule the operations themselves keep by releasing
// the owner lock across every move.
void rt_channel_free(void* channel) {
    rt_channel* ch = channel_from_handle(channel);
    if (ch == NULL) {
        return;
    }
    // This function TAKES the owner lock, so a lane that already holds one is
    // in the wrong place: it would deadlock on its own shard, or nest a second
    // lock the lane model forbids. Fail closed naming the lane, rather than
    // leaving the rule to whoever reads the caller contract in rt.h.
    if (rt_lane_holds_control() || rt_lane_holds_any_shard()) {
        panic_msg("async: channel reclaim ran while a scheduler lock was held");
        return;
    }
    rt_executor* ex = ensure_exec();
    rt_shard* owner = ex == NULL ? NULL : channel_owner_shard(ex, ch);

    rt_typed_fifo_detached buffered;
    if (owner != NULL) {
        rt_shard_lock(owner);
    }
    rt_channel_assert_reclaimable_locked(owner, ch);
    atomic_store_explicit(&ch->dying, (uint8_t)1, memory_order_release);
    rt_typed_fifo_detach_all_locked(&ch->ring, &buffered);
    rt_park_pool_detach_all_locked(&ch->parks);
    if (owner != NULL) {
        rt_shard_unlock(owner);
    }

    // Everything the channel still owned: what was buffered, and what was
    // staged for or delivered to a park that never completed. Each is
    // destroyed exactly once, by the half of its own owner that runs unlocked.
    rt_typed_fifo_drop_detached(&ch->ring, &buffered);
    rt_park_pool_drop_detached(&ch->parks);

    size_t align = ch->ops != NULL && ch->ops->layout.align > _Alignof(rt_channel)
                       ? ch->ops->layout.align
                       : _Alignof(rt_channel);
    uint64_t bytes = channel_alloc_size(ch->ops, ch->capacity);
    rt_async_debug_printf(
        "async chan free ch=%p cap=%llu\n", (void*)ch, (unsigned long long)ch->capacity);
    rt_free((uint8_t*)ch, bytes, align);
}
