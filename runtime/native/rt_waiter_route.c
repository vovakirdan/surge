#include "rt_async_internal.h"

// Owner resolution for non-net keys (dependency map section 5): join, timer,
// and blocking keys carry the parked-on task's id, so its owner shard stores
// the waiters and completion wakes stay owner-local. Scope keys carry the
// scope id and route to the scope's PINNED owner shard (Epic 8 Task 9, S5-Q10,
// revising Epic 7 D8): the scope's owner_shard_id is fixed at rt_scope_enter,
// so both the join_all park and the child-done wake serialize on that one
// shard's store lock; a freed/absent scope (monotonic, never-reused ids)
// resolves to shard 0 via rt_scope_owner_shard, draining nothing. Channel keys
// stay on the shard-0 compatibility store until the Task 10 channel-owner
// migration. Resolution is stable while a registration exists: the only
// post-spawn task owner change is the accept transition, which migrates join
// entries (rt_task_replace_owner); scopes never migrate.
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
            return rt_shard_waiter_store(rt_scope_owner_shard(ex, get_scope(ex, key.id)));
        case WAKER_CHAN_SEND:
        case WAKER_CHAN_RECV:
            // Channel keys embed the channel pointer; channels are never
            // freed and their owner never changes, so resolution is stable.
            return rt_executor_waiter_store_for_shard(
                ex, rt_channel_owner_shard_id((const rt_channel*)(uintptr_t)key.id));
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
        case WAKER_TIMER:
        case WAKER_BLOCKING:
            return rt_task_owner_shard(ex, get_task(ex, key.id));
        case WAKER_SCOPE:
            return rt_scope_owner_shard(ex, get_scope(ex, key.id));
        case WAKER_CHAN_SEND:
        case WAKER_CHAN_RECV:
            return rt_runtime_shard(
                rt_executor_runtime(ex),
                rt_channel_owner_shard_id((const rt_channel*)(uintptr_t)key.id));
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
    // Do NOT early-out on from->len here: since Epic 8 Task 7 join registration
    // runs under the source shard lock (not the control lock), so from->len is
    // written concurrently and reading it unlocked is a data race
    // (RV2-DEBT-019, surfaced by the completion-pin TSan stress via F2 adoption
    // consuming a CONNECTION-placed target). The batch loop below reads from->len
    // under the source shard lock; an empty source simply returns after the
    // first locked pass.
    if (from == NULL || to == NULL || from == to) {
        return;
    }
    // Extract under the source lock, append under the destination lock:
    // never two shard locks at once. Batches repeat until the source has no
    // matching entry left.
    //
    // Post-Task-7 interleaving (RV2-DEBT-020 re-derivation). The pre-Task-7
    // comment here claimed "the caller holds the control lock, so no same-key
    // registration can interleave between the two holds" -- that is now FALSE.
    // Since Epic 8 Task 7, a joiner's join-key registration (rt_task_poll's
    // register-then-verify via add_waiter/prepare_park) runs under the target's
    // owner SHARD lock, not the control lock, so a same-key registration CAN
    // land between the source-unlock and the destination-lock; a partial final
    // batch also leaves such a late arrival on the source shard, where the
    // eventual completion wake (routed to the target's NEW owner shard) will not
    // scan it. The two callers of rt_task_replace_owner split on whether this
    // strands the late joiner:
    //   - F2 adoption (rt_task_poll_adopt_placement) fires ONLY on a target the
    //     joiner already observed TASK_DONE. A racing joiner's register-then-
    //     verify re-checks TASK_DONE after registering and self-consumes the
    //     completed target (rt_async_task.c), so it never depends on the
    //     migration moving its entry -- provably benign.
    //   - the accept transition (rt_net_accept_group.c) self-replaces a still-
    //     RUNNING rt_current_task(); register-then-verify's re-check does NOT
    //     help a not-yet-DONE target, so the micro-window is not proven benign
    //     here. It is the carried residual RV2-DEBT-020, owned by the future
    //     net-handle/accept epic (whether any joiner races the acceptor's own
    //     accept-time self-replace is that epic's join-structure analysis).
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
                panic_msg("async: waiter allocation failed");
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
