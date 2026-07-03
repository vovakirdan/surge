#include "rt_async_internal.h"

#include <limits.h>

waker_key waker_none(void) {
    waker_key key = {WAKER_NONE, 0};
    return key;
}

int waker_valid(waker_key key) {
    return key.kind != WAKER_NONE && key.id != 0;
}

waker_key join_key(uint64_t id) {
    waker_key key = {WAKER_JOIN, id};
    return key;
}

waker_key timer_key(uint64_t id) {
    waker_key key = {WAKER_TIMER, id};
    return key;
}

waker_key scope_key(uint64_t id) {
    waker_key key = {WAKER_SCOPE, id};
    return key;
}

waker_key blocking_key(uint64_t id) {
    waker_key key = {WAKER_BLOCKING, id};
    return key;
}

waker_key channel_send_key(const rt_channel* ch) {
    waker_key key = {WAKER_CHAN_SEND, (uint64_t)(uintptr_t)ch};
    return key;
}

waker_key channel_recv_key(const rt_channel* ch) {
    waker_key key = {WAKER_CHAN_RECV, (uint64_t)(uintptr_t)ch};
    return key;
}

waker_key net_accept_key(int fd) {
    waker_key key = {WAKER_NET_ACCEPT, (uint64_t)fd};
    return key;
}

waker_key net_read_key(int fd) {
    waker_key key = {WAKER_NET_READ, (uint64_t)fd};
    return key;
}

waker_key net_write_key(int fd) {
    waker_key key = {WAKER_NET_WRITE, (uint64_t)fd};
    return key;
}

int waker_is_net(waker_key key) {
    waker_kind kind = (waker_kind)key.kind;
    return kind == WAKER_NET_ACCEPT || kind == WAKER_NET_READ || kind == WAKER_NET_WRITE;
}

static void net_waiter_added(rt_waiter_store* store, waker_key key) {
    if (store != NULL && waker_is_net(key)) {
        store->net_len++;
    }
}

static void net_waiters_removed(rt_waiter_store* store, waker_key key, size_t count) {
    if (store == NULL || count == 0 || !waker_is_net(key)) {
        return;
    }
    if (count >= store->net_len) {
        store->net_len = 0;
        return;
    }
    store->net_len -= count;
}

// Task 6 fd-registry-waiter-bridge: registry interest mirrors waiter-store
// membership exactly. Attach runs after a successful append so interest never
// exists without a waiter; detach runs only when the caller's same-pass scan
// proved the last waiter for the key left the store. Since Task 7 the
// registry is the only poll input, so a failed attach may not strand a
// parked waiter: net_wait_current_task re-verifies the row after
// prepare_park and undoes the park on a miss (fd-registry-attach-miss
// resolution); the debug print below records the miss itself.
uint32_t rt_net_owner_shard_for_key(rt_executor* ex, waker_key key, uint32_t fallback_shard_id) {
    if (ex == NULL || !waker_is_net(key) || key.id > (uint64_t)INT_MAX) {
        return fallback_shard_id;
    }
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        const rt_fd_registry* registry = rt_executor_fd_registry_const_for_shard(ex, i);
        const rt_fd_entry* entry = rt_fd_registry_find_const(registry, (int)key.id);
        if (entry != NULL && entry->close_state == RT_FD_CLOSE_STATE_OPEN) {
            return (uint32_t)i;
        }
    }
    return fallback_shard_id;
}

static int fd_registry_bridge_net_attach(rt_executor* ex, waker_key key, uint32_t* out_owner) {
    if (!waker_is_net(key)) {
        return 0;
    }
    rt_fd_registry* registry = NULL;
    uint32_t owner_shard_id = 0;
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        rt_fd_registry* candidate = rt_executor_fd_registry_for_shard(ex, i);
        const rt_fd_entry* entry = rt_fd_registry_find_const(candidate, (int)key.id);
        if (entry != NULL && entry->close_state == RT_FD_CLOSE_STATE_OPEN) {
            registry = candidate;
            owner_shard_id = (uint32_t)i;
            break;
        }
    }
    if (registry == NULL && shard_count == 1) {
        registry = rt_executor_fd_registry_for_shard(ex, 0);
        owner_shard_id = 0;
    }
    rt_runtime_status status = registry != NULL ? rt_fd_registry_attach_net_interest(registry, key)
                                                : RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    if (status != RT_RUNTIME_STATUS_OK && rt_async_debug_enabled()) {
        rt_async_debug_printf("fd-registry-attach-miss kind=%u fd=%llu status=%d\n",
                              (unsigned)key.kind,
                              (unsigned long long)key.id,
                              (int)status);
    }
    if (status == RT_RUNTIME_STATUS_OK) {
        if (out_owner != NULL) {
            *out_owner = owner_shard_id;
        }
        return 1;
    }
    return 0;
}

static int fd_registry_bridge_net_detach_if_last(rt_executor* ex,
                                                 waker_key key,
                                                 size_t removed,
                                                 size_t remaining_same_key,
                                                 uint32_t* out_owner) {
    if (removed == 0 || !waker_is_net(key)) {
        return 0;
    }
    int removed_open_interest = 0;
    if (remaining_same_key == 0) {
        size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
        for (size_t i = 0; i < shard_count; i++) {
            rt_fd_registry* registry = rt_executor_fd_registry_for_shard(ex, i);
            if (rt_fd_registry_find_const(registry, (int)key.id) == NULL) {
                continue;
            }
            removed_open_interest = rt_fd_registry_net_interest_present(registry, key);
            rt_fd_registry_detach_net_interest(registry, key);
            if (out_owner != NULL) {
                *out_owner = (uint32_t)i;
            }
            break;
        }
    }
    if (rt_async_debug_enabled()) {
        // Debug consistency check: recount same-key waiters independently and
        // require stale interest (flag set with zero waiters) to be impossible.
        const rt_waiter_store* store = rt_executor_waiter_store_const(ex);
        size_t recount = 0;
        for (size_t i = 0; store != NULL && i < store->len; i++) {
            waker_key k = store->entries[i].key;
            if (k.kind == key.kind && k.id == key.id) {
                recount++;
            }
        }
        int interest = 0;
        size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
        for (size_t i = 0; i < shard_count; i++) {
            const rt_fd_registry* registry = rt_executor_fd_registry_const_for_shard(ex, i);
            if (rt_fd_registry_find_const(registry, (int)key.id) == NULL) {
                continue;
            }
            interest = rt_fd_registry_net_interest_present(registry, key);
            break;
        }
        if (recount != remaining_same_key || (recount == 0 && interest)) {
            rt_async_debug_printf(
                "fd-registry-bridge mismatch kind=%u fd=%llu remaining=%zu recount=%zu "
                "interest=%d\n",
                (unsigned)key.kind,
                (unsigned long long)key.id,
                remaining_same_key,
                recount,
                interest);
        }
    }
    return removed_open_interest;
}

static void fd_registry_bridge_notify_removed_interest(rt_executor* ex,
                                                       waker_key key,
                                                       int removed_open_interest,
                                                       uint32_t owner_shard_id) {
    if (ex == NULL || !removed_open_interest || !waker_is_net(key)) {
        return;
    }
    // Remove-side only: the current poll snapshot may still contain this key.
    // Readiness completion already comes from the poller path and must not
    // write an extra wake byte.
    (void)rt_net_wake_poll_on_shard(ex, owner_shard_id);
    pthread_cond_signal(&ex->io_cv);
}

rt_runtime_status rt_waiter_store_ensure_cap(rt_waiter_store* store) {
    if (store == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    if (store->len < store->cap) {
        return RT_RUNTIME_STATUS_OK;
    }
    size_t next_cap = 16;
    if (store->cap != 0) {
        if (store->cap > SIZE_MAX / 2U) {
            return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
        }
        next_cap = store->cap * 2U;
    }
    if (store->cap > SIZE_MAX / sizeof(waiter) || next_cap > SIZE_MAX / sizeof(waiter)) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    size_t old_size = store->cap * sizeof(waiter);
    size_t new_size = next_cap * sizeof(waiter);
    waiter* next = (waiter*)rt_realloc(
        (uint8_t*)store->entries, (uint64_t)old_size, (uint64_t)new_size, _Alignof(waiter));
    if (next == NULL) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    store->entries = next;
    store->cap = next_cap;
    return RT_RUNTIME_STATUS_OK;
}

static void clear_accept_winner_wait_keys(rt_executor* ex, waker_key key, uint32_t owner_shard_id) {
    if (ex == NULL || key.kind != WAKER_NET_ACCEPT || key.id > (uint64_t)INT_MAX) {
        return;
    }
    int ready_fd = (int)key.id;
    for (size_t i = 1; i < ex->tasks_cap; i++) {
        rt_task* task = ex->tasks[i];
        if (task == NULL || task->net_ready_accept_valid == 0 || task->wait_keys_len == 0) {
            continue;
        }
        if (task->net_ready_accept_fd != ready_fd ||
            task->net_ready_accept_owner_shard != owner_shard_id) {
            continue;
        }
        clear_wait_keys(ex, task);
    }
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key_on_owner(rt_executor* ex,
                                                                   waker_key key,
                                                                   uint32_t owner_shard_id) {
    rt_waiter_completion result = {0, 0};
    if (ex == NULL || !waker_valid(key) || !waker_is_net(key)) {
        return result;
    }
    rt_waiter_store* store = rt_executor_waiter_store(ex);
    if (store == NULL || store->len == 0) {
        return result;
    }
    size_t out = 0;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind != key.kind || w.key.id != key.id) {
            store->entries[out++] = w;
            continue;
        }
        result.removed++;
        const rt_task* task = get_task(ex, w.task_id);
        if (task == NULL || task_status_load(task) == TASK_DONE || task_cancelled_load(task) != 0) {
            continue;
        }
        if (key.kind == WAKER_NET_ACCEPT) {
            rt_task* mutable_task = get_task(ex, w.task_id);
            if (mutable_task != NULL) {
                if (rt_async_debug_enabled()) {
                    rt_async_debug_printf("net-accept-ready task=%llu fd=%llu owner=%u\n",
                                          (unsigned long long)w.task_id,
                                          (unsigned long long)key.id,
                                          owner_shard_id);
                }
                mutable_task->net_ready_accept_valid = 1;
                mutable_task->net_ready_accept_fd = (int)key.id;
                mutable_task->net_ready_accept_owner_shard = owner_shard_id;
                rt_task_set_placement(mutable_task, owner_shard_id, TASK_PLACEMENT_CONNECTION);
            }
        }
        result.woken++;
        wake_task(ex, w.task_id, 0);
    }
    store->len = out;
    net_waiters_removed(store, key, result.removed);
    // Completion removed every waiter of this key, so no same-key entry remains.
    (void)fd_registry_bridge_net_detach_if_last(ex, key, result.removed, 0, NULL);
    clear_accept_winner_wait_keys(ex, key, owner_shard_id);
    return result;
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key(rt_executor* ex, waker_key key) {
    return rt_executor_wake_net_waiters_for_key_on_owner(ex, key, 0);
}

void ensure_waiter_cap(rt_executor* ex) {
    rt_runtime_status status = rt_waiter_store_ensure_cap(rt_executor_waiter_store(ex));
    if (status == RT_RUNTIME_STATUS_ALLOCATION_FAILED) {
        panic_msg("async: waiter allocation failed");
    }
}

static void ensure_wait_keys_cap(rt_task* task, size_t want) {
    if (task == NULL) {
        return;
    }
    if (task->wait_keys_cap >= want) {
        return;
    }
    size_t next_cap = task->wait_keys_cap == 0 ? 4 : task->wait_keys_cap;
    while (next_cap < want) {
        next_cap *= 2;
    }
    size_t old_size = task->wait_keys_cap * sizeof(waker_key);
    size_t new_size = next_cap * sizeof(waker_key);
    waker_key* next = (waker_key*)rt_realloc(
        (uint8_t*)task->wait_keys, (uint64_t)old_size, (uint64_t)new_size, _Alignof(waker_key));
    if (next == NULL) {
        panic_msg("async: wait key allocation failed");
        return;
    }
    task->wait_keys = next;
    task->wait_keys_cap = next_cap;
}

void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    // Caller holds ex->lock; compaction preserves relative order of other waiters.
    rt_waiter_store* store = rt_executor_waiter_store(ex);
    if (store == NULL || store->len == 0) {
        return;
    }
    size_t out = 0;
    size_t removed = 0;
    size_t kept_same_key = 0;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.task_id == task_id && w.key.kind == key.kind && w.key.id == key.id) {
            removed++;
            continue;
        }
        if (w.key.kind == key.kind && w.key.id == key.id) {
            kept_same_key++;
        }
        store->entries[out++] = w;
    }
    store->len = out;
    net_waiters_removed(store, key, removed);
    uint32_t owner_shard_id = 0;
    int removed_open_interest =
        fd_registry_bridge_net_detach_if_last(ex, key, removed, kept_same_key, &owner_shard_id);
    fd_registry_bridge_notify_removed_interest(ex, key, removed_open_interest, owner_shard_id);
}

void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    // Caller holds ex->lock; waiters are consumed FIFO per key by pop_waiter.
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    rt_waiter_store* store = rt_executor_waiter_store(ex);
    rt_runtime_status status = rt_waiter_store_ensure_cap(store);
    if (status == RT_RUNTIME_STATUS_ALLOCATION_FAILED) {
        panic_msg("async: waiter allocation failed");
        return;
    }
    if (status != RT_RUNTIME_STATUS_OK) {
        return;
    }
    store->entries[store->len++] = (waiter){key, task_id};
    net_waiter_added(store, key);
    uint32_t owner_shard_id = 0;
    if (fd_registry_bridge_net_attach(ex, key, &owner_shard_id)) {
        (void)rt_net_wake_poll_on_shard(ex, owner_shard_id);
    }
}

void clear_wait_keys(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL || task->wait_keys_len == 0) {
        return;
    }
    for (size_t i = 0; i < task->wait_keys_len; i++) {
        remove_waiter(ex, task->wait_keys[i], task->id);
    }
    task->wait_keys_len = 0;
}

void add_wait_key(rt_executor* ex, rt_task* task, waker_key key) {
    if (ex == NULL || task == NULL || !waker_valid(key)) {
        return;
    }
    ensure_wait_keys_cap(task, task->wait_keys_len + 1);
    if (task->wait_keys == NULL) {
        return;
    }
    task->wait_keys[task->wait_keys_len++] = key;
    add_waiter(ex, key, task->id);
}

// NOTES (MT iteration 2):
// - prepare_park pre-registers waiters under ex->lock to avoid wake-before-park races for user
// tasks.
// - Channel waiters now share the executor waiters list (FIFO per key via pop_waiter), so wake is
// O(n).
// - Documented primitives like Semaphore/Condition/Mutex/RwLock have no native runtime impl yet.
void prepare_park(rt_executor* ex, rt_task* task, waker_key key, int already_added) {
    if (ex == NULL || task == NULL || !waker_valid(key)) {
        return;
    }
    if (!already_added) {
        if (!(task->park_prepared && task->park_key.kind == key.kind &&
              task->park_key.id == key.id)) {
            add_waiter(ex, key, task->id);
        }
    }
    task->park_key = key;
    task->park_prepared = 1;
}

int pop_waiter(rt_executor* ex, waker_key key, uint64_t* out_id) {
    // Caller holds ex->lock; stale/done/cancelled waiters are dropped while scanning.
    rt_waiter_store* store = rt_executor_waiter_store(ex);
    if (store == NULL || !waker_valid(key) || store->len == 0) {
        return 0;
    }
    size_t out = 0;
    size_t removed = 0;
    size_t kept_same_key = 0;
    int found = 0;
    uint64_t found_id = 0;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            const rt_task* task = get_task(ex, w.task_id);
            if (task == NULL || task_status_load(task) == TASK_DONE ||
                task_cancelled_load(task) != 0) {
                removed++;
                continue;
            }
            if (!found) {
                found = 1;
                found_id = w.task_id;
                removed++;
                continue;
            }
            kept_same_key++;
        }
        store->entries[out++] = w;
    }
    store->len = out;
    net_waiters_removed(store, key, removed);
    (void)fd_registry_bridge_net_detach_if_last(ex, key, removed, kept_same_key, NULL);
    if (found && out_id != NULL) {
        *out_id = found_id;
    }
    return found;
}
