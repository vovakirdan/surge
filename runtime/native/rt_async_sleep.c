#include "rt_async_internal.h"
#include "rt_sync_point.h"

// Per-shard sleep store (spike D7). The waiter store keeps
// the park registration for timer keys; this store is the deadline index
// that replaces the whole-task-table scans. Both are owner-shard state. The
// atomic min_deadline mirror lets tick paths peek other shards without
// their locks; mutations happen under the owning lane (control lock in the
// nested phase, the shard lock after the peel).

static void sleep_store_update_min(rt_sleep_store* store) {
    uint64_t min = store->len > 0 ? store->entries[0].deadline : UINT64_MAX;
    atomic_store_explicit(&store->min_deadline, min, memory_order_release);
}

// Zeroed storage would read as "deadline 0 pending"; an empty store must
// mirror UINT64_MAX or idle paths spin on a phantom timer.
void rt_sleep_store_init(rt_sleep_store* store) {
    if (store == NULL) {
        return;
    }
    store->entries = NULL;
    store->len = 0;
    store->cap = 0;
    atomic_store_explicit(&store->min_deadline, UINT64_MAX, memory_order_release);
}

rt_runtime_status rt_sleep_store_add(rt_sleep_store* store, uint64_t deadline, uint64_t task_id) {
    if (store == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    if (store->len == store->cap) {
        size_t next_cap = store->cap == 0 ? 8 : store->cap * 2;
        if (next_cap > SIZE_MAX / sizeof(rt_sleep_entry)) {
            return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
        }
        rt_sleep_entry* next =
            (rt_sleep_entry*)rt_realloc((uint8_t*)store->entries,
                                        (uint64_t)(store->cap * sizeof(rt_sleep_entry)),
                                        (uint64_t)(next_cap * sizeof(rt_sleep_entry)),
                                        _Alignof(rt_sleep_entry));
        if (next == NULL) {
            return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
        }
        store->entries = next;
        store->cap = next_cap;
    }
    // Sorted by (deadline, task_id): equal-deadline sleepers wake in task-id
    // order, preserving the pre-split whole-table scan order per shard.
    size_t at = store->len;
    while (at > 0 && (store->entries[at - 1].deadline > deadline ||
                      (store->entries[at - 1].deadline == deadline &&
                       store->entries[at - 1].task_id > task_id))) {
        store->entries[at] = store->entries[at - 1];
        at--;
    }
    store->entries[at] = (rt_sleep_entry){deadline, task_id};
    store->len++;
    sleep_store_update_min(store);
    return RT_RUNTIME_STATUS_OK;
}

int rt_sleep_store_remove(rt_sleep_store* store, uint64_t task_id) {
    if (store == NULL || store->len == 0) {
        return 0;
    }
    for (size_t i = 0; i < store->len; i++) {
        if (store->entries[i].task_id != task_id) {
            continue;
        }
        memmove(&store->entries[i],
                &store->entries[i + 1],
                (store->len - i - 1) * sizeof(rt_sleep_entry));
        store->len--;
        sleep_store_update_min(store);
        return 1;
    }
    return 0;
}

int rt_sleep_store_pop_due(rt_sleep_store* store, uint64_t now, uint64_t* out_task_id) {
    if (store == NULL || store->len == 0 || store->entries[0].deadline > now) {
        return 0;
    }
    if (out_task_id != NULL) {
        *out_task_id = store->entries[0].task_id;
    }
    memmove(&store->entries[0], &store->entries[1], (store->len - 1) * sizeof(rt_sleep_entry));
    store->len--;
    sleep_store_update_min(store);
    return 1;
}

uint64_t rt_sleep_store_min(const rt_sleep_store* store) {
    if (store == NULL) {
        return UINT64_MAX;
    }
    return atomic_load_explicit(&store->min_deadline, memory_order_acquire);
}

void rt_sleep_store_destroy(rt_sleep_store* store) {
    if (store == NULL) {
        return;
    }
    if (store->entries != NULL && store->cap > 0) {
        rt_free((uint8_t*)store->entries,
                (uint64_t)(store->cap * sizeof(rt_sleep_entry)),
                _Alignof(rt_sleep_entry));
    }
    store->entries = NULL;
    store->len = 0;
    store->cap = 0;
    atomic_store_explicit(&store->min_deadline, UINT64_MAX, memory_order_release);
}

uint64_t rt_clock_now(const rt_executor* ex) {
    return ex != NULL ? atomic_load_explicit(&ex->now_ms, memory_order_relaxed) : 0;
}

uint64_t rt_clock_tick(rt_executor* ex) {
    if (ex == NULL) {
        return 0;
    }
    return atomic_fetch_add_explicit(&ex->now_ms, 1, memory_order_relaxed) + 1;
}

// Monotonic idle advance: never moves the clock backward even if the caller
// computed the target from a stale minimum.
int rt_clock_advance_to(rt_executor* ex, uint64_t target) {
    if (ex == NULL) {
        return 0;
    }
    uint64_t current = atomic_load_explicit(&ex->now_ms, memory_order_relaxed);
    while (current < target) {
        if (atomic_compare_exchange_weak_explicit(
                &ex->now_ms, &current, target, memory_order_relaxed, memory_order_relaxed)) {
            return 1;
        }
    }
    return 0;
}

// Wake every due sleeper in the shard's store. Drains the due batch under
// the shard's lock, then wakes outside it (D5 collect-then-wake: wake_task
// takes the sleeper's owner lock, which is this same shard). Callers hold
// the control lock and no shard lock. Returns the number of sleepers woken.
size_t rt_sleep_fire_due_on_shard(rt_executor* ex, rt_shard* shard, uint64_t now) {
    rt_sleep_store* store = shard != NULL ? &shard->sleep_store : NULL;
    if (store == NULL || rt_sleep_store_min(store) > now) {
        return 0;
    }
    uint64_t batch[32];
    size_t woken = 0;
    for (;;) {
        size_t batch_len = 0;
        rt_shard_lock(shard);
        uint64_t task_id = 0;
        while (batch_len < sizeof(batch) / sizeof(batch[0]) &&
               rt_sleep_store_pop_due(store, now, &task_id)) {
            batch[batch_len++] = task_id;
        }
        // Claimed under the SAME lock that popped them: a sleeper is out of
        // the store from here until wake_task puts it in a ready queue, and in
        // that gap nothing else in the shard mentions it. An idle sample
        // landing here used to read the executor as empty and advance the
        // virtual clock past the very deadline that just fired.
        size_t claimed = RT_DEBT190_PUBLISHING(batch_len);
        rt_sched_publishing_begin_locked(shard, claimed);
        rt_shard_unlock(shard);
        if (batch_len == 0) {
            return woken;
        }
        RT_SYNC_POINT(SP_SLEEP_FIRED_BEFORE_WAKE);
        for (size_t i = 0; i < batch_len; i++) {
            wake_task(ex, batch[i], 1);
            woken++;
        }
        rt_sched_publishing_end(shard, claimed);
    }
}
