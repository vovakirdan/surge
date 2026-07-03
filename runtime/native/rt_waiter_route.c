#include "rt_async_internal.h"

// Owner resolution for non-net keys (dependency map section 5): join, timer,
// and blocking keys carry the parked-on task's id, so its owner shard stores
// the waiters and completion wakes stay owner-local. Scope keys are
// control-lane state. Channel keys stay on the shard-0 compatibility store
// until the Task 10 channel-owner migration. Resolution is stable while a
// registration exists: the only post-spawn owner change is the accept
// transition, which migrates join entries (rt_task_replace_owner).
rt_waiter_store* rt_waiter_store_for_key(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key)) {
        return NULL;
    }
    if (waker_is_net(key)) {
        // Defensive arm: the add/remove/pop paths resolve net keys through
        // the fd-registry-aware helper in rt_async_waiter.c; only generic
        // wake paths land here, and they never carry net keys today.
        return rt_executor_waiter_store_for_shard(ex, rt_net_owner_shard_for_key(ex, key, 0));
    }
    switch ((waker_kind)key.kind) {
        case WAKER_JOIN:
        case WAKER_TIMER:
        case WAKER_BLOCKING:
            return rt_shard_waiter_store(rt_task_owner_shard(ex, get_task(ex, key.id)));
        case WAKER_SCOPE:
            return &ex->control_waiters;
        default:
            return rt_executor_waiter_store(ex);
    }
}

void rt_waiter_migrate_join_waiters(rt_executor* ex,
                                    uint64_t task_id,
                                    uint32_t from_shard_id,
                                    uint32_t to_shard_id) {
    if (ex == NULL || from_shard_id == to_shard_id) {
        return;
    }
    rt_waiter_store* from = rt_executor_waiter_store_for_shard(ex, from_shard_id);
    rt_waiter_store* to = rt_executor_waiter_store_for_shard(ex, to_shard_id);
    if (from == NULL || to == NULL || from == to || from->len == 0) {
        return;
    }
    waker_key key = join_key(task_id);
    size_t out = 0;
    for (size_t i = 0; i < from->len; i++) {
        waiter w = from->entries[i];
        if (w.key.kind == key.kind && w.key.id == key.id) {
            if (rt_waiter_store_ensure_cap(to) != RT_RUNTIME_STATUS_OK) {
                panic_msg("async: waiter allocation failed");
                return;
            }
            to->entries[to->len++] = w;
            continue;
        }
        from->entries[out++] = w;
    }
    from->len = out;
}
