#include "rt_async_internal.h"
#include "rt_sync_point.h"

static rt_shard* rt_task_join_waiter_shard(rt_executor* ex, uint64_t task_id) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    const rt_task* task = get_task(ex, task_id);
    return rt_runtime_shard(runtime, rt_task_join_owner_shard_id_load(task));
}

// Owner resolution for non-net keys (dependency map section 5): join keys use
// the task's atomic join-owner route, while blocking keys carry the parked-on
// task's id and use its scheduler owner shard. Timer keys USED to do the same
// and no longer do: like a channel key, a timer key outlives the object it
// names, so it carries its shard instead (see timer_key, rt_async_waiter.c).
// A blocking key can be reached the same way -- wake_key_all mints one from a
// job's task id in rt_async_blocking.c -- and still resolves by dereference
// here; that arm is the same hazard, unproven and unfixed. Join add/remove and
// collect-all wake operations must not call this helper as a split "store
// now, lock later" sequence: rt_async_waiter.c / rt_task_park.c delegate to the
// join-route helpers, which resolve the join route, lock that shard, and
// revalidate the route under the lock before touching the store. Scope keys,
// like timer and channel keys, stamp their stable owner while the object is
// live; routing must not dereference a scope that may already be freed.
//
// Channel keys resolve WITHOUT touching the channel (RV2-DEBT-199). The claim
// that stood here — "channels are never freed" — was false: rt_far_channel.c's
// release_entry calls rt_channel_free once the last lease and the last pin are
// gone, and a channel-keyed waiter entry can outlive that (the wake path
// captures a parked task's channel key, drops the owner shard lock, and only
// then calls remove_waiter_generation; the woken task's own completion is what
// unpins and frees). Both arms below therefore read the owner shard the key
// CARRIES, stamped by channel_send_key / channel_recv_key while the channel was
// alive; nothing here dereferences key.id.
rt_waiter_store* rt_waiter_store_for_key(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key)) {
        return NULL;
    }
    if (waker_is_net(key)) {
        // Defensive arm: the add/remove and on-owner wake paths resolve net
        // keys through the fd-registry-aware helper in rt_async_waiter.c;
        // only generic wake paths land here, and they never carry net keys
        // today.
        return rt_executor_waiter_store_for_shard(ex, rt_net_owner_shard_for_key(ex, key, 0));
    }
    switch ((waker_kind)key.kind) {
        case WAKER_JOIN:
            return rt_shard_waiter_store(rt_task_join_waiter_shard(ex, key.id));
        case WAKER_TIMER:
            // On the shard the key carries, never on the task it names: a timer
            // key outlives its task, so looking the id up here and reading its
            // placement reads freed memory.
            return rt_executor_waiter_store_for_shard(ex, key.owner_shard_id);
        case WAKER_BLOCKING:
            return rt_shard_waiter_store(rt_task_owner_shard(ex, get_task(ex, key.id)));
        case WAKER_SCOPE:
#ifdef RV2_DEBT_283_NEGATIVE_CONTROL
            return rt_shard_waiter_store(rt_scope_owner_shard(ex, get_scope(ex, key.id)));
#endif
        case WAKER_REMOTE_SPAWN_REPLY:
        case WAKER_REMOTE_TASK_REPLY:
            return rt_executor_waiter_store_for_shard(ex, key.owner_shard_id);
        case WAKER_CHAN_SEND:
        case WAKER_CHAN_RECV:
            return rt_executor_waiter_store_for_shard(ex, RT_DEBT199_CHANNEL_OWNER_SHARD(key));
        default:
            return rt_executor_waiter_store(ex);
    }
}

// Lock owner for a key's store: the shard whose lock guards it, or NULL for
// the control-lane store (scope keys), which the control lock guards.
rt_shard* rt_waiter_key_shard(rt_executor* ex, waker_key key) {
    if (ex == NULL || !waker_valid(key)) {
        return NULL;
    }
    if (waker_is_net(key)) {
        uint32_t owner_shard_id = rt_net_owner_shard_probe_locked(
            ex, (int)key.id, tls_worker_ctx != NULL ? tls_worker_ctx->shard_id : 0);
        return rt_runtime_shard(rt_executor_runtime(ex),
                                owner_shard_id != UINT32_MAX ? owner_shard_id : 0);
    }
    switch ((waker_kind)key.kind) {
        case WAKER_JOIN:
            return rt_task_join_waiter_shard(ex, key.id);
        case WAKER_TIMER:
            return rt_runtime_shard(rt_executor_runtime(ex), key.owner_shard_id);
        case WAKER_BLOCKING:
            return rt_task_owner_shard(ex, get_task(ex, key.id));
        case WAKER_SCOPE:
#ifdef RV2_DEBT_283_NEGATIVE_CONTROL
            return rt_scope_owner_shard(ex, get_scope(ex, key.id));
#endif
        case WAKER_REMOTE_SPAWN_REPLY:
        case WAKER_REMOTE_TASK_REPLY:
            return rt_runtime_shard(rt_executor_runtime(ex), key.owner_shard_id);
        case WAKER_CHAN_SEND:
        case WAKER_CHAN_RECV:
            return rt_runtime_shard(rt_executor_runtime(ex), RT_DEBT199_CHANNEL_OWNER_SHARD(key));
        default:
            return rt_runtime_shard0(rt_executor_runtime(ex));
    }
}

void rt_waiter_migrate_join_waiters(rt_executor* ex,
                                    uint64_t task_id,
                                    uint32_t from_shard_id,
                                    uint32_t to_shard_id) {
    if (ex == NULL || from_shard_id == to_shard_id) {
        return;
    }
    rt_shard* from_shard = rt_runtime_shard(rt_executor_runtime(ex), from_shard_id);
    rt_shard* to_shard = rt_runtime_shard(rt_executor_runtime(ex), to_shard_id);
    rt_waiter_store* from = rt_executor_waiter_store_for_shard(ex, from_shard_id);
    rt_waiter_store* to = rt_executor_waiter_store_for_shard(ex, to_shard_id);
    // Do NOT early-out on from->len here: since join registration
    // runs under the source shard lock (not the control lock), so from->len is
    // written concurrently and reading it unlocked is a data race
    // (RV2-DEBT-019, surfaced by the completion-pin TSan stress via F2 adoption
    // consuming a CONNECTION-placed target). The batch loop below reads from->len
    // under the source shard lock; an empty source simply returns after the
    // first locked pass.
    if (from == NULL || to == NULL || from == to) {
        return;
    }
    // Legacy old-order migration kept for the RV2-DEBT-020 negative-control
    // build only. It drains the old store before publishing the join route, so
    // a late registration can land on the old store after the final drain and
    // miss completion once wakes route to the new owner.
    waker_key key = join_key(task_id);
    for (;;) {
        waiter moved[16];
        size_t moved_len = 0;
        if (from_shard != NULL) {
            rt_shard_lock(from_shard);
        }
        size_t out = 0;
        for (size_t i = 0; i < from->len; i++) {
            waiter w = from->entries[i];
            if (w.key.kind == key.kind && w.key.id == key.id &&
                moved_len < sizeof(moved) / sizeof(moved[0])) {
                moved[moved_len++] = w;
                continue;
            }
            from->entries[out++] = w;
        }
        from->len = out;
        if (from_shard != NULL) {
            rt_shard_unlock(from_shard);
        }
        if (moved_len == 0) {
            return;
        }
        if (to_shard != NULL) {
            rt_shard_lock(to_shard);
        }
        for (size_t i = 0; i < moved_len; i++) {
            if (rt_waiter_store_ensure_cap(to) != RT_RUNTIME_STATUS_OK) {
                fatal_oom_msg("async: waiter allocation failed");
                break;
            }
            to->entries[to->len++] = moved[i];
        }
        if (to_shard != NULL) {
            rt_shard_unlock(to_shard);
        }
        if (moved_len < sizeof(moved) / sizeof(moved[0])) {
            return;
        }
    }
}

void rt_waiter_publish_join_owner_and_migrate(rt_executor* ex,
                                              rt_task* task,
                                              uint32_t from_shard_id,
                                              uint32_t to_shard_id) {
    if (ex == NULL || task == NULL || from_shard_id == to_shard_id) {
        return;
    }
    rt_shard* from_shard = rt_runtime_shard(rt_executor_runtime(ex), from_shard_id);
    rt_shard* to_shard = rt_runtime_shard(rt_executor_runtime(ex), to_shard_id);
    rt_waiter_store* from = rt_executor_waiter_store_for_shard(ex, from_shard_id);
    rt_waiter_store* to = rt_executor_waiter_store_for_shard(ex, to_shard_id);
    if (from == NULL || to == NULL || from == to) {
        rt_task_join_owner_shard_id_store(task, to_shard_id);
        return;
    }
    waker_key key = join_key(task->id);
    int published = 0;
    for (;;) {
        waiter moved[16];
        size_t moved_len = 0;
        if (from_shard != NULL) {
            rt_shard_lock(from_shard);
        }
        if (!published) {
            // Publish the join route while holding the old route's shard lock.
            // A stale registrar that already selected the old route either got
            // in before this store and is drained below, or re-reads the changed
            // route under this same lock and retries on the new store.
            rt_task_join_owner_shard_id_store(task, to_shard_id);
            RT_SYNC_POINT(SP_MIGRATE_GAP);
            published = 1;
        }
        size_t out = 0;
        for (size_t i = 0; i < from->len; i++) {
            waiter w = from->entries[i];
            if (w.key.kind == key.kind && w.key.id == key.id &&
                moved_len < sizeof(moved) / sizeof(moved[0])) {
                moved[moved_len++] = w;
                continue;
            }
            from->entries[out++] = w;
        }
        from->len = out;
        if (from_shard != NULL) {
            rt_shard_unlock(from_shard);
        }
        if (moved_len == 0) {
            return;
        }
        if (to_shard != NULL) {
            rt_shard_lock(to_shard);
        }
        for (size_t i = 0; i < moved_len; i++) {
            if (rt_waiter_store_ensure_cap(to) != RT_RUNTIME_STATUS_OK) {
                fatal_oom_msg("async: waiter allocation failed");
                break;
            }
            to->entries[to->len++] = moved[i];
        }
        if (to_shard != NULL) {
            rt_shard_unlock(to_shard);
        }
        if (moved_len < sizeof(moved) / sizeof(moved[0])) {
            return;
        }
    }
}
