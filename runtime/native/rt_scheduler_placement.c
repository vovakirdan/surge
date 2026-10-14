#include "rt_async_internal.h"
#include "rt_sync_point.h"

static int scheduler_has_queued_work(const rt_scheduler* scheduler) {
    if (scheduler == NULL) {
        return 0;
    }
    return scheduler->inject.len > 0 || scheduler->local_queue.len > 0;
}

uint64_t rt_runtime_total_worker_count(const rt_runtime* runtime) {
    if (runtime == NULL || runtime->shard_count < 1 ||
        runtime->shard_count > RT_RUNTIME_MAX_SHARDS) {
        return 0;
    }
    return runtime->shard_count;
}

rt_shard* rt_task_owner_shard(rt_executor* ex, const rt_task* task) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (runtime != NULL && task != NULL && task->owner_shard_valid != 0) {
        rt_shard* shard = rt_runtime_shard(runtime, task->owner_shard_id);
        if (shard != NULL) {
            return shard;
        }
        panic_msg("async: invalid task owner shard");
        return NULL;
    }
    panic_msg("async: task has no owner shard");
    return NULL;
}

uint32_t rt_task_owner_shard_id(rt_executor* ex, const rt_task* task) {
    const rt_shard* shard = rt_task_owner_shard(ex, task);
    return shard != NULL ? shard->shard_id : UINT32_MAX;
}

rt_scheduler* rt_task_scheduler(rt_executor* ex, const rt_task* task) {
    return rt_shard_scheduler(rt_task_owner_shard(ex, task));
}

void rt_task_assign_spawn_owner(rt_task* task) {
    if (task == NULL || task->owner_shard_valid != 0) {
        return;
    }
    // D3 universal assignment: a spawn without inherited placement belongs
    // to the spawning worker's shard; non-worker threads spawn onto shard 0.
    uint32_t shard_id = rt_debug_current_worker_shard_id();
    if (shard_id == UINT32_MAX) {
        shard_id = 0;
    }
    rt_task_set_placement(task, shard_id, TASK_PLACEMENT_GENERIC);
}

void rt_task_set_placement(rt_task* task, uint32_t shard_id, uint8_t placement_class) {
    if (task == NULL) {
        return;
    }
    task->owner_shard_id = shard_id;
    task->owner_shard_valid = 1;
    task->placement_class = placement_class;
    rt_task_join_owner_shard_id_store(task, shard_id);
    if (placement_class == TASK_PLACEMENT_CONNECTION) {
        rt_trace_sched_connection_owner_placed();
    }
}

void rt_task_replace_owner(rt_executor* ex,
                           rt_task* task,
                           uint32_t shard_id,
                           uint8_t placement_class) {
    rt_trace_owner_replaced();
    if (task == NULL) {
        return;
    }
    // Join waiters route through join_owner_shard_id, not the scheduler
    // placement fields directly. Publishing that route before migration sends
    // late registrations to the destination; migration then moves entries that
    // already landed on the old shard. The negative-control build restores the
    // old order so the deterministic SP_MIGRATE_GAP proof strands a joiner.
    // Current callers cannot complete the re-owned task between route publish
    // and migration: they either re-own rt_current_task before its poll returns
    // or re-own a net-accept waiter before wake_net_task can enqueue it.
    uint32_t old_shard_id = rt_task_join_owner_shard_id_load(task);
    if (old_shard_id != shard_id) {
#ifdef RV2_DEBT_020_NEGATIVE_CONTROL
        rt_waiter_migrate_join_waiters(ex, task->id, old_shard_id, shard_id);
        RT_SYNC_POINT(SP_MIGRATE_GAP);
        rt_task_join_owner_shard_id_store(task, shard_id);
#else
        rt_waiter_publish_join_owner_and_migrate(ex, task, old_shard_id, shard_id);
#endif
    }
    rt_task_set_placement(task, shard_id, placement_class);
}

void rt_task_inherit_placement(rt_task* task, const rt_task* parent) {
    if (task == NULL || parent == NULL) {
        return;
    }
    rt_task_set_placement(
        task, rt_task_owner_shard_id(ensure_exec(), parent), parent->placement_class);
}

void rt_debug_assert_no_parked_with_work(rt_executor* ex, uint32_t shard_id) {
    rt_runtime* runtime = ex != NULL ? ex->runtime : NULL;
    rt_shard* shard = rt_runtime_shard(runtime, shard_id);
    const rt_scheduler* scheduler = rt_shard_scheduler_const(shard);
    int has_work = scheduler_has_queued_work(scheduler);
    if (!has_work && rt_transport_inbound_len_locked(shard) == 0) {
        return;
    }
    if (shard != NULL && rt_transport_inbound_len_locked(shard) != 0) {
        shard->transport.parked_with_work_violations++;
    }
    rt_trace_parked_with_work();
    panic_msg("async: parked-with-work invariant violated");
}
