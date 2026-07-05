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

// Lock-free-safe owner resolution: registries mutate under shard locks, so
// a control-free caller probes one shard at a time under that shard's lock,
// own shard first (the r/w fast path: connection tasks run on their fd's
// owner). Returns UINT32_MAX when no open row exists.
uint32_t rt_net_owner_shard_probe_locked(rt_executor* ex, int fd, uint32_t hint_shard_id) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t k = 0; k <= shard_count; k++) {
        size_t i;
        if (k == 0) {
            if (hint_shard_id >= shard_count) {
                continue;
            }
            i = hint_shard_id;
        } else {
            i = k - 1;
            if (i == hint_shard_id) {
                continue;
            }
        }
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            continue;
        }
        rt_shard_lock(shard);
        const rt_fd_entry* entry = rt_fd_registry_find_const(&shard->fd_registry, fd);
        int open = entry != NULL && entry->close_state == RT_FD_CLOSE_STATE_OPEN;
        rt_shard_unlock(shard);
        if (open) {
            return (uint32_t)i;
        }
    }
    if (shard_count == 1) {
        return 0;
    }
    return UINT32_MAX;
}

static size_t waiter_store_count_key(const rt_waiter_store* store, waker_key key) {
    size_t count = 0;
    for (size_t i = 0; store != NULL && i < store->len; i++) {
        waker_key k = store->entries[i].key;
        if (k.kind == key.kind && k.id == key.id) {
            count++;
        }
    }
    return count;
}

static size_t count_net_waiters_for_key_all_shards(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_is_net(key)) {
        return 0;
    }
    size_t count = 0;
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        count += waiter_store_count_key(rt_executor_waiter_store_const_for_shard(ex, i), key);
    }
    return count;
}

static size_t remove_waiter_from_store_seq(rt_waiter_store* store,
                                           waker_key key,
                                           uint64_t task_id,
                                           uint32_t seq,
                                           size_t* out_kept_same_key) {
    if (out_kept_same_key != NULL) {
        *out_kept_same_key = 0;
    }
    if (store == NULL || store->len == 0) {
        return 0;
    }
    size_t out = 0;
    size_t removed = 0;
    size_t kept_same_key = 0;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.task_id == task_id && w.key.kind == key.kind && w.key.id == key.id &&
            (seq == 0 || w.seq == seq)) {
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
    if (out_kept_same_key != NULL) {
        *out_kept_same_key = kept_same_key;
    }
    return removed;
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

static int fd_registry_bridge_net_detach_if_last_on_owner(rt_executor* ex,
                                                          waker_key key,
                                                          uint32_t owner_shard_id,
                                                          size_t removed,
                                                          size_t remaining_same_key) {
    if (removed == 0 || !waker_is_net(key)) {
        return 0;
    }
    int removed_open_interest = 0;
    if (remaining_same_key == 0) {
        rt_fd_registry* registry = rt_executor_fd_registry_for_shard(ex, owner_shard_id);
        if (rt_fd_registry_find_const(registry, (int)key.id) != NULL) {
            removed_open_interest = rt_fd_registry_net_interest_present(registry, key);
            rt_fd_registry_detach_net_interest(registry, key);
        }
    }
    if (rt_async_debug_enabled()) {
        // Debug consistency check: recount same-key waiters independently and
        // require stale interest (flag set with zero waiters) to be impossible.
        size_t recount = count_net_waiters_for_key_all_shards(ex, key);
        int interest = 0;
        const rt_fd_registry* registry =
            rt_executor_fd_registry_const_for_shard(ex, owner_shard_id);
        if (rt_fd_registry_find_const(registry, (int)key.id) != NULL) {
            interest = rt_fd_registry_net_interest_present(registry, key);
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
    // write an extra wake byte. The wake byte interrupts an in-flight poll;
    // an io thread between polls re-checks its predicates within a slice, so
    // no condvar signal is needed here (a shard lock is held).
    (void)rt_net_wake_poll_on_shard(ex, owner_shard_id);
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
    uint64_t bound = rt_task_table_snapshot(ex);
    for (uint64_t i = 1; i < bound; i++) {
        rt_task* task = get_task(ex, i);
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
    rt_shard* owner_shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    rt_waiter_store* store = rt_executor_waiter_store_for_shard(ex, owner_shard_id);
    if (owner_shard == NULL || store == NULL || store->len == 0) {
        return result;
    }
    // Collect-then-wake (D5): pop every matching entry and detach the fd
    // interest under the store owner's lock, then validate and wake outside
    // it. Waking under the held store lock would self-deadlock the
    // same-shard fast case (owner lock is the store lock).
    uint64_t inline_batch[16];
    uint64_t* batch = inline_batch;
    size_t batch_cap = sizeof(inline_batch) / sizeof(inline_batch[0]);
    size_t batch_len = 0;
    rt_shard_lock(owner_shard);
    size_t out = 0;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind != key.kind || w.key.id != key.id) {
            store->entries[out++] = w;
            continue;
        }
        result.removed++;
        if (batch_len == batch_cap) {
            size_t next_cap = batch_cap * 2;
            uint64_t* next =
                (uint64_t*)rt_alloc((uint64_t)(next_cap * sizeof(uint64_t)), _Alignof(uint64_t));
            if (next == NULL) {
                panic_msg("async: net wake batch allocation failed");
                break;
            }
            memcpy(next, batch, batch_len * sizeof(uint64_t));
            if (batch != inline_batch) {
                rt_free(
                    (uint8_t*)batch, (uint64_t)(batch_cap * sizeof(uint64_t)), _Alignof(uint64_t));
            }
            batch = next;
            batch_cap = next_cap;
        }
        batch[batch_len++] = w.task_id;
    }
    store->len = out;
    net_waiters_removed(store, key, result.removed);
    // Completion removed every waiter of this key, so no same-key entry remains.
    (void)fd_registry_bridge_net_detach_if_last_on_owner(
        ex, key, owner_shard_id, result.removed, 0);
    rt_shard_unlock(owner_shard);
    // Accept completion re-places the winner's owner and scans the task
    // table for sibling cleanup: control-lane work (D4). A worker calling
    // this control-free takes the lane for accept keys only; read/write
    // completions stay on the fast path.
    int need_control = key.kind == WAKER_NET_ACCEPT && !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    if (batch_len > 0) {
        rt_trace_collect_wake_batch();
    }
    for (size_t i = 0; i < batch_len; i++) {
        rt_task* task = get_task(ex, batch[i]);
        if (task == NULL || task_status_load(task) == TASK_DONE || task_cancelled_load(task) != 0) {
            continue;
        }
        if (key.kind == WAKER_NET_ACCEPT) {
            if (rt_async_debug_enabled()) {
                rt_async_debug_printf("net-accept-ready task=%llu fd=%llu owner=%u\n",
                                      (unsigned long long)batch[i],
                                      (unsigned long long)key.id,
                                      owner_shard_id);
            }
            task->net_ready_accept_valid = 1;
            task->net_ready_accept_fd = (int)key.id;
            task->net_ready_accept_owner_shard = owner_shard_id;
            rt_task_replace_owner(ex, task, owner_shard_id, TASK_PLACEMENT_CONNECTION);
        }
        result.woken++;
        wake_net_task(ex, batch[i]);
    }
    if (batch != inline_batch) {
        rt_free((uint8_t*)batch, (uint64_t)(batch_cap * sizeof(uint64_t)), _Alignof(uint64_t));
    }
    clear_accept_winner_wait_keys(ex, key, owner_shard_id);
    if (need_control) {
        rt_control_unlock(ex);
    }
    return result;
}

rt_waiter_completion rt_executor_wake_net_waiters_for_key(rt_executor* ex, waker_key key) {
    return rt_executor_wake_net_waiters_for_key_on_owner(
        ex, key, rt_net_owner_shard_for_key(ex, key, 0));
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

static size_t remove_waiter_from_store(rt_waiter_store* store,
                                       waker_key key,
                                       uint64_t task_id,
                                       size_t* out_kept_same_key) {
    return remove_waiter_from_store_seq(store, key, task_id, 0, out_kept_same_key);
}

// Generation-qualified removal (Epic 8 blocker fix): the deferred stale-key
// removal in wake_task_with_policy runs after the owner lock is released,
// and the woken task can re-register the same channel key in that window.
// Removing by (key, task) alone would eat the fresh registration and strand
// the new park; qualifying by the captured park generation removes only the
// entry the wake actually orphaned. seq == 0 keeps the unqualified behavior
// for non-channel keys, whose entries carry seq 0.
void remove_waiter_generation(rt_executor* ex, waker_key key, uint64_t task_id, uint32_t seq) {
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    if (!waker_is_net(key)) {
        size_t kept_same_key = 0;
        rt_shard* store_shard = rt_waiter_key_shard(ex, key);
        if (store_shard != NULL) {
            rt_shard_lock(store_shard);
        }
        (void)remove_waiter_from_store_seq(
            rt_waiter_store_for_key(ex, key), key, task_id, seq, &kept_same_key);
        if (store_shard != NULL) {
            rt_shard_unlock(store_shard);
        }
        return;
    }
    remove_waiter(ex, key, task_id);
}

void remove_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    // Caller holds either the control lock or nothing, never a shard lock
    // (mirrors add_waiter): the control-free join-poll/completion callers reach
    // here without ex->lock since Epic 8 Task 7/8. This takes the key's store
    // owner shard lock internally (net keys scan every shard's store, so those
    // still need the control lane); compaction preserves the relative order of
    // the other waiters.
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    int net_key = waker_is_net(key);
    if (!net_key) {
        size_t kept_same_key = 0;
        rt_shard* store_shard = rt_waiter_key_shard(ex, key);
        if (store_shard != NULL) {
            rt_shard_lock(store_shard);
        }
        (void)remove_waiter_from_store(
            rt_waiter_store_for_key(ex, key), key, task_id, &kept_same_key);
        if (store_shard != NULL) {
            rt_shard_unlock(store_shard);
        }
        return;
    }
    uint32_t owner_shard_id = rt_net_owner_shard_probe_locked(
        ex, (int)key.id, tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : 0);
    if (owner_shard_id == UINT32_MAX) {
        owner_shard_id = 0;
    }
    rt_waiter_store* owner_store = rt_executor_waiter_store_for_shard(ex, owner_shard_id);
    size_t owner_kept_same_key = 0;
    size_t owner_removed = 0;
    size_t shard_count = rt_runtime_shard_count(rt_executor_runtime(ex));
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), i);
        rt_waiter_store* shard_store = rt_executor_waiter_store_for_shard(ex, i);
        size_t kept_on_shard = 0;
        if (shard != NULL) {
            rt_shard_lock(shard);
        }
        size_t removed_on_shard =
            remove_waiter_from_store(shard_store, key, task_id, &kept_on_shard);
        if (shard_store == owner_store) {
            owner_removed = removed_on_shard;
            owner_kept_same_key = kept_on_shard;
        }
        if (shard != NULL) {
            rt_shard_unlock(shard);
        }
    }
    rt_shard* owner_shard = rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id);
    if (owner_shard != NULL) {
        rt_shard_lock(owner_shard);
    }
    int removed_open_interest = fd_registry_bridge_net_detach_if_last_on_owner(
        ex, key, owner_shard_id, owner_removed, owner_kept_same_key);
    if (owner_shard != NULL) {
        rt_shard_unlock(owner_shard);
    }
    fd_registry_bridge_notify_removed_interest(ex, key, removed_open_interest, owner_shard_id);
}

void add_waiter(rt_executor* ex, waker_key key, uint64_t task_id) {
    // Registers under the store owner's shard lock (D2: the caller holds
    // either the control lock or nothing, never a shard lock). FIFO per key
    // is consumed by pop_waiter. task_id is the caller's current task, so
    // the deref and its owner read are stable without the control lane.
    if (ex == NULL || !waker_valid(key)) {
        return;
    }
    const rt_task* task = get_task(ex, task_id);
    uint32_t owner_hint = task != NULL && task->owner_shard_valid != 0 ? task->owner_shard_id : 0;
    rt_runtime_status status = RT_RUNTIME_STATUS_OK;
    if (waker_is_net(key)) {
        uint32_t owner_shard_id = rt_net_owner_shard_probe_locked(ex, (int)key.id, owner_hint);
        rt_shard* owner_shard = owner_shard_id != UINT32_MAX
                                    ? rt_runtime_shard(rt_executor_runtime(ex), owner_shard_id)
                                    : NULL;
        if (owner_shard == NULL) {
            if (rt_async_debug_enabled()) {
                rt_async_debug_printf("fd-registry-attach-miss kind=%u fd=%llu status=%d\n",
                                      (unsigned)key.kind,
                                      (unsigned long long)key.id,
                                      (int)RT_RUNTIME_STATUS_INVALID_ARGUMENT);
            }
            return;
        }
        rt_shard_lock(owner_shard);
        rt_waiter_store* store = &owner_shard->waiter_store;
        status = rt_waiter_store_ensure_cap(store);
        if (status == RT_RUNTIME_STATUS_OK) {
            store->entries[store->len++] = (waiter){key, task_id, owner_hint, 0};
            net_waiter_added(store, key);
            rt_runtime_status attach =
                rt_fd_registry_attach_net_interest(&owner_shard->fd_registry, key);
            if (attach == RT_RUNTIME_STATUS_OK) {
                (void)rt_net_wake_poll_on_shard(ex, owner_shard_id);
            } else if (rt_async_debug_enabled()) {
                rt_async_debug_printf("fd-registry-attach-miss kind=%u fd=%llu status=%d\n",
                                      (unsigned)key.kind,
                                      (unsigned long long)key.id,
                                      (int)attach);
            }
        }
        rt_shard_unlock(owner_shard);
    } else {
        rt_waiter_store* store = rt_waiter_store_for_key(ex, key);
        rt_shard* store_shard = rt_waiter_key_shard(ex, key);
        if (store_shard != NULL) {
            rt_shard_lock(store_shard);
        }
        status = rt_waiter_store_ensure_cap(store);
        if (status == RT_RUNTIME_STATUS_OK) {
            store->entries[store->len++] = (waiter){key, task_id, owner_hint, 0};
        }
        if (store_shard != NULL) {
            rt_shard_unlock(store_shard);
        }
    }
    if (status == RT_RUNTIME_STATUS_ALLOCATION_FAILED) {
        panic_msg("async: waiter allocation failed");
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
    uint32_t owner_shard_id = 0;
    int net_key = waker_is_net(key);
    if (net_key) {
        owner_shard_id = rt_net_owner_shard_probe_locked(
            ex, (int)key.id, tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : 0);
        if (owner_shard_id == UINT32_MAX) {
            owner_shard_id = 0;
        }
    }
    rt_waiter_store* store = net_key ? rt_executor_waiter_store_for_shard(ex, owner_shard_id)
                                     : rt_waiter_store_for_key(ex, key);
    if (store == NULL || !waker_valid(key) || store->len == 0) {
        return 0;
    }
    rt_shard* store_shard = rt_waiter_key_shard(ex, key);
    if (store_shard != NULL) {
        rt_shard_lock(store_shard);
    }
    size_t out = 0;
    size_t removed = 0;
    size_t kept_same_key = 0;
    int found = 0;
    uint64_t found_id = 0;
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            // Stale-skip deref: legal while every pop_waiter caller holds the
            // control lock; the channel peel (B2) must switch its callers to
            // the candidate/validate pattern before dropping control.
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
    if (net_key) {
        (void)fd_registry_bridge_net_detach_if_last_on_owner(
            ex, key, owner_shard_id, removed, kept_same_key);
    }
    if (store_shard != NULL) {
        rt_shard_unlock(store_shard);
    }
    if (found && out_id != NULL) {
        *out_id = found_id;
    }
    return found;
}
