#include "rt_async_internal.h"

static int scheduler_has_queued_work(const rt_scheduler* scheduler) {
    if (scheduler == NULL) {
        return 0;
    }
    if (scheduler->inject.len > 0) {
        return 1;
    }
    if (scheduler->local_queues == NULL || scheduler->worker_count == 0) {
        return 0;
    }
    for (uint32_t i = 0; i < scheduler->worker_count; i++) {
        if (scheduler->local_queues[i].len > 0) {
            return 1;
        }
    }
    return 0;
}

uint64_t rt_runtime_total_worker_count(const rt_runtime* runtime) {
    if (runtime == NULL || runtime->shard_count < 1 ||
        runtime->shard_count > RT_RUNTIME_MAX_SHARDS) {
        return 0;
    }
    uint64_t total = 0;
    for (size_t i = 0; i < runtime->shard_count; i++) {
        const rt_scheduler* scheduler = rt_shard_scheduler_const(&runtime->shards[i]);
        if (scheduler != NULL) {
            total += scheduler->worker_count;
        }
    }
    return total;
}

rt_scheduler* rt_task_scheduler(rt_executor* ex, const rt_task* task) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    if (runtime != NULL && task != NULL && task->owner_shard_valid != 0) {
        rt_shard* shard = rt_runtime_shard(runtime, task->owner_shard_id);
        rt_scheduler* scheduler = rt_shard_scheduler(shard);
        if (scheduler != NULL) {
            return scheduler;
        }
        panic_msg("async: invalid task owner shard");
        return NULL;
    }
    return rt_executor_scheduler(ex);
}

void rt_task_set_placement(rt_task* task, uint32_t shard_id, uint8_t placement_class) {
    if (task == NULL) {
        return;
    }
    task->owner_shard_id = shard_id;
    task->owner_shard_valid = 1;
    task->placement_class = placement_class;
    if (placement_class == TASK_PLACEMENT_CONNECTION) {
        rt_trace_sched_connection_owner_placed();
    }
}

void rt_task_inherit_placement(rt_task* task, const rt_task* parent) {
    if (task == NULL || parent == NULL || parent->owner_shard_valid == 0) {
        return;
    }
    rt_task_set_placement(task, parent->owner_shard_id, parent->placement_class);
}

int rt_task_can_steal_from_shard(const rt_task* task, uint32_t shard_id) {
    if (task == NULL) {
        return 0;
    }
    if (task->owner_shard_valid == 0 || task->placement_class != TASK_PLACEMENT_CONNECTION) {
        return 1;
    }
    return task->owner_shard_id == shard_id;
}

int rt_task_can_steal_from_shard_or_trace_denied(const rt_task* task, uint32_t shard_id) {
    if (task == NULL) {
        return 0;
    }
    if (rt_task_can_steal_from_shard(task, shard_id)) {
        return 1;
    }
    rt_trace_sched_tier1_steal_denied();
    return 0;
}

void rt_debug_assert_no_parked_with_work(rt_executor* ex, uint32_t shard_id) {
    const rt_runtime* runtime = ex != NULL ? ex->runtime : NULL;
    const rt_shard* shard = rt_runtime_shard_const(runtime, shard_id);
    const rt_scheduler* scheduler = rt_shard_scheduler_const(shard);
    if (!scheduler_has_queued_work(scheduler)) {
        return;
    }
    rt_trace_parked_with_work();
    panic_msg("async: parked-with-work invariant violated");
}
