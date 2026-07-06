#include "rt_async_internal.h"

typedef struct rt_join_waiter_route {
    rt_shard* shard;
    rt_waiter_store* store;
} rt_join_waiter_route;

static rt_runtime_status
lock_join_waiter_route(rt_executor* ex, waker_key key, rt_join_waiter_route* out) {
    if (ex == NULL || out == NULL || key.kind != WAKER_JOIN || !waker_valid(key)) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    for (;;) {
        const rt_task* target = get_task(ex, key.id);
        if (target == NULL) {
            return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
        }
        uint32_t route = rt_task_join_owner_shard_id_load(target);
        rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), route);
        rt_waiter_store* store = rt_executor_waiter_store_for_shard(ex, route);
        if (shard == NULL || store == NULL) {
            return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
        }
        rt_shard_lock(shard);
        if (rt_task_join_owner_shard_id_load(target) == route) {
            out->shard = shard;
            out->store = store;
            return RT_RUNTIME_STATUS_OK;
        }
        rt_shard_unlock(shard);
    }
}

static void unlock_join_waiter_route(rt_join_waiter_route route) {
    if (route.shard != NULL) {
        rt_shard_unlock(route.shard);
    }
}

rt_runtime_status
rt_waiter_add_join_waiter(rt_executor* ex, waker_key key, uint64_t task_id, uint32_t owner_hint) {
    rt_join_waiter_route route = {0};
    rt_runtime_status status = lock_join_waiter_route(ex, key, &route);
    if (status != RT_RUNTIME_STATUS_OK) {
        return status;
    }
    status = rt_waiter_store_ensure_cap(route.store);
    if (status == RT_RUNTIME_STATUS_OK) {
        route.store->entries[route.store->len++] = (waiter){key, task_id, owner_hint, 0};
    }
    unlock_join_waiter_route(route);
    return status;
}

void rt_waiter_remove_join_waiter_generation(rt_executor* ex,
                                             waker_key key,
                                             uint64_t task_id,
                                             uint32_t seq) {
    rt_join_waiter_route route = {0};
    if (lock_join_waiter_route(ex, key, &route) != RT_RUNTIME_STATUS_OK) {
        return;
    }
    size_t out = 0;
    for (size_t i = 0; i < route.store->len; i++) {
        waiter w = route.store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id && w.task_id == task_id &&
            (seq == 0 || w.seq == seq)) {
            continue;
        }
        route.store->entries[out++] = w;
    }
    route.store->len = out;
    unlock_join_waiter_route(route);
}

int rt_waiter_pop_join_waiter(rt_executor* ex, waker_key key, uint64_t* out_id) {
    rt_join_waiter_route route = {0};
    if (lock_join_waiter_route(ex, key, &route) != RT_RUNTIME_STATUS_OK) {
        return 0;
    }
    size_t out = 0;
    int found = 0;
    uint64_t found_id = 0;
    for (size_t i = 0; i < route.store->len; i++) {
        waiter w = route.store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            const rt_task* task = get_task(ex, w.task_id);
            if (task == NULL || task_status_load(task) == TASK_DONE ||
                task_cancelled_load(task) != 0) {
                continue;
            }
            if (!found) {
                found = 1;
                found_id = w.task_id;
                continue;
            }
        }
        route.store->entries[out++] = w;
    }
    route.store->len = out;
    unlock_join_waiter_route(route);
    if (found && out_id != NULL) {
        *out_id = found_id;
    }
    return found;
}

size_t rt_waiter_collect_join_waiters(rt_executor* ex,
                                      waker_key key,
                                      uint64_t** batch,
                                      size_t* batch_cap,
                                      const uint64_t* inline_batch) {
    rt_join_waiter_route route = {0};
    if (batch == NULL || batch_cap == NULL ||
        lock_join_waiter_route(ex, key, &route) != RT_RUNTIME_STATUS_OK) {
        return 0;
    }
    size_t batch_len = 0;
    size_t out = 0;
    for (size_t i = 0; i < route.store->len; i++) {
        waiter w = route.store->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            if (batch_len == *batch_cap) {
                size_t next_cap = *batch_cap * 2;
                uint64_t* next = (uint64_t*)rt_alloc((uint64_t)(next_cap * sizeof(uint64_t)),
                                                     _Alignof(uint64_t));
                if (next == NULL) {
                    panic_msg("async: wake batch allocation failed");
                    break;
                }
                memcpy(next, *batch, batch_len * sizeof(uint64_t));
                if (*batch != inline_batch) {
                    rt_free((uint8_t*)*batch,
                            (uint64_t)(*batch_cap * sizeof(uint64_t)),
                            _Alignof(uint64_t));
                }
                *batch = next;
                *batch_cap = next_cap;
            }
            (*batch)[batch_len++] = w.task_id;
            continue;
        }
        route.store->entries[out++] = w;
    }
    route.store->len = out;
    unlock_join_waiter_route(route);
    return batch_len;
}
