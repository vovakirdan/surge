#include "rt_async_internal.h"

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
    for (size_t i = 0; i < store->len; i++) {
        waiter w = store->entries[i];
        if (w.task_id == task_id && w.key.kind == key.kind && w.key.id == key.id) {
            removed++;
            continue;
        }
        store->entries[out++] = w;
    }
    store->len = out;
    net_waiters_removed(store, key, removed);
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
        }
        store->entries[out++] = w;
    }
    store->len = out;
    net_waiters_removed(store, key, removed);
    if (found && out_id != NULL) {
        *out_id = found_id;
    }
    return found;
}
