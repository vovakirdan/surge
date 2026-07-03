#include "rt_async_internal.h"

#include <limits.h>
#include <poll.h>
#include <unistd.h>

static rt_runtime runtime_state;

static uint32_t rt_runtime_detect_cpu_count(void) {
    long cpus = sysconf(_SC_NPROCESSORS_ONLN);
    if (cpus <= 0) {
        return 1;
    }
    if (cpus > (long)UINT32_MAX) { // NOLINT(runtime/int)
        return UINT32_MAX;
    }
    return (uint32_t)cpus;
}

static void rt_shard_scheduler_destroy(rt_scheduler* scheduler) {
    if (scheduler == NULL) {
        return;
    }
    if (scheduler->local_queues != NULL && scheduler->worker_count > 0) {
        rt_free((uint8_t*)scheduler->local_queues,
                (uint64_t)scheduler->worker_count * (uint64_t)sizeof(rt_deque),
                _Alignof(rt_deque));
    }
    memset(scheduler, 0, sizeof(*scheduler));
}

static void rt_net_poll_scratch_destroy(rt_net_poll_scratch* scratch) {
    if (scratch == NULL) {
        return;
    }
    if (scratch->fds != NULL && scratch->fds_cap > 0) {
        rt_free((uint8_t*)scratch->fds,
                (uint64_t)scratch->fds_cap * (uint64_t)sizeof(rt_fd_poll_interest),
                _Alignof(rt_fd_poll_interest));
    }
    if (scratch->pfds != NULL && scratch->pfds_cap > 0) {
        rt_free((uint8_t*)scratch->pfds,
                (uint64_t)scratch->pfds_cap * (uint64_t)sizeof(struct pollfd),
                _Alignof(struct pollfd));
    }
    memset(scratch, 0, sizeof(*scratch));
}

static void rt_shard_destroy(rt_shard* shard) {
    if (shard == NULL) {
        return;
    }
    rt_shard_scheduler_destroy(rt_shard_scheduler(shard));
    rt_net_poll_scratch_destroy(rt_shard_net_poll_scratch(shard));
    rt_net_poll_wake_close(&shard->net_poll_wake);
    rt_fd_registry_free(rt_shard_fd_registry(shard));
    rt_heap_accounting_destroy(&shard->heap_accounting);
    memset(shard, 0, sizeof(*shard));
}

static void rt_runtime_destroy(rt_runtime* runtime) {
    if (runtime == NULL) {
        return;
    }
    size_t shard_count = runtime->shard_count;
    if (shard_count > RT_RUNTIME_MAX_SHARDS) {
        shard_count = RT_RUNTIME_MAX_SHARDS;
    }
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard_destroy(&runtime->shards[i]);
    }
    memset(runtime, 0, sizeof(*runtime));
}

void rt_runtime_destroy_global(void) {
    rt_runtime_destroy(&runtime_state);
}

static rt_runtime_status rt_heap_status_to_runtime_status(rt_heap_accounting_status status) {
    if (status == RT_HEAP_ACCOUNTING_OK) {
        return RT_RUNTIME_STATUS_OK;
    }
    if (status == RT_HEAP_ACCOUNTING_ALLOCATION_FAILED) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
}

static rt_runtime_status
rt_shard_init(rt_runtime* runtime, rt_executor* ex, rt_shard* shard, size_t shard_index) {
    if (runtime == NULL || ex == NULL || shard == NULL || shard_index > UINT32_MAX) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    shard->runtime = runtime;
    shard->executor = ex;
    shard->shard_id = (uint32_t)shard_index;
    shard->net_poll_wake.read_fd = -1;
    shard->net_poll_wake.write_fd = -1;
    rt_runtime_status status =
        rt_heap_status_to_runtime_status(rt_heap_accounting_init(&shard->heap_accounting));
    if (status != RT_RUNTIME_STATUS_OK) {
        return status;
    }
    // Redundant with the memset today, but the registry lifecycle must stay
    // explicit: init pairs with rt_fd_registry_free once shutdown exists.
    return rt_fd_registry_init(rt_shard_fd_registry(shard));
}

rt_runtime_status rt_runtime_init_global(rt_executor* ex, size_t shard_count) {
    if (ex == NULL || shard_count < 1 || shard_count > RT_RUNTIME_MAX_SHARDS) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    rt_runtime* runtime = &runtime_state;
    memset(runtime, 0, sizeof(*runtime));
    runtime->shard_count = shard_count;
    for (size_t i = 0; i < shard_count; i++) {
        rt_runtime_status status = rt_shard_init(runtime, ex, &runtime->shards[i], i);
        if (status != RT_RUNTIME_STATUS_OK) {
            rt_runtime_destroy(runtime);
            ex->runtime = NULL;
            return status;
        }
    }
    ex->runtime = runtime;
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime_status rt_runtime_init_shard_schedulers(rt_runtime* runtime,
                                                   uint32_t worker_count,
                                                   uint8_t sched_mode_value,
                                                   uint64_t sched_seed) {
    if (runtime == NULL || runtime->shard_count < 1 ||
        runtime->shard_count > RT_RUNTIME_MAX_SHARDS) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    for (size_t i = 0; i < runtime->shard_count; i++) {
        rt_runtime_status status = rt_shard_scheduler_init(
            &runtime->shards[i], worker_count, sched_mode_value, sched_seed);
        if (status != RT_RUNTIME_STATUS_OK) {
            for (size_t j = 0; j < i; j++) {
                rt_shard_scheduler_destroy(rt_shard_scheduler(&runtime->shards[j]));
            }
            return status;
        }
    }
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime* rt_executor_runtime(rt_executor* ex) {
    return ex != NULL ? ex->runtime : NULL;
}

rt_shard* rt_runtime_shard0(rt_runtime* runtime) {
    return rt_runtime_shard(runtime, 0);
}

rt_shard* rt_runtime_shard(rt_runtime* runtime, size_t index) {
    if (runtime == NULL || runtime->shard_count < 1 || index >= runtime->shard_count ||
        index >= RT_RUNTIME_MAX_SHARDS) {
        return NULL;
    }
    return &runtime->shards[index];
}

const rt_shard* rt_runtime_shard_const(const rt_runtime* runtime, size_t index) {
    if (runtime == NULL || runtime->shard_count < 1 || index >= runtime->shard_count ||
        index >= RT_RUNTIME_MAX_SHARDS) {
        return NULL;
    }
    return &runtime->shards[index];
}

size_t rt_runtime_shard_count(const rt_runtime* runtime) {
    return runtime != NULL ? runtime->shard_count : 0;
}

rt_heap_accounting* rt_executor_heap_accounting(rt_executor* ex) {
    rt_shard* shard = rt_runtime_shard0(rt_executor_runtime(ex));
    return shard != NULL ? &shard->heap_accounting : NULL;
}

rt_heap_accounting* rt_runtime_global_heap_accounting(void) {
    rt_shard* shard = rt_runtime_shard0(&runtime_state);
    return shard != NULL ? &shard->heap_accounting : NULL;
}

rt_scheduler* rt_shard_scheduler(rt_shard* shard) {
    return shard != NULL ? &shard->scheduler : NULL;
}

const rt_scheduler* rt_shard_scheduler_const(const rt_shard* shard) {
    return shard != NULL ? &shard->scheduler : NULL;
}

rt_scheduler* rt_executor_scheduler(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* shard = rt_runtime_shard0(runtime);
    return rt_shard_scheduler(shard);
}

const rt_scheduler* rt_executor_scheduler_const(const rt_executor* ex) {
    return rt_shard_scheduler_const(rt_runtime_shard_const(ex != NULL ? ex->runtime : NULL, 0));
}

rt_net_poll_scratch* rt_shard_net_poll_scratch(rt_shard* shard) {
    return shard != NULL ? &shard->net_poll_scratch : NULL;
}

rt_net_poll_scratch* rt_executor_net_poll_scratch(rt_executor* ex) {
    return rt_executor_net_poll_scratch_for_shard(ex, 0);
}

rt_net_poll_scratch* rt_executor_net_poll_scratch_for_shard(rt_executor* ex, size_t shard_index) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_index);
    return rt_shard_net_poll_scratch(shard);
}

rt_channel_blocking_compat* rt_shard_channel_blocking_compat(rt_shard* shard) {
    return shard != NULL ? &shard->channel_blocking_compat : NULL;
}

const rt_channel_blocking_compat* rt_shard_channel_blocking_compat_const(const rt_shard* shard) {
    return shard != NULL ? &shard->channel_blocking_compat : NULL;
}

rt_channel_blocking_compat* rt_executor_channel_blocking_compat(rt_executor* ex) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* shard = rt_runtime_shard0(runtime);
    return rt_shard_channel_blocking_compat(shard);
}

const rt_channel_blocking_compat* rt_executor_channel_blocking_compat_const(const rt_executor* ex) {
    return rt_shard_channel_blocking_compat_const(
        rt_runtime_shard_const(ex != NULL ? ex->runtime : NULL, 0));
}

rt_waiter_store* rt_shard_waiter_store(rt_shard* shard) {
    return shard != NULL ? &shard->waiter_store : NULL;
}

const rt_waiter_store* rt_shard_waiter_store_const(const rt_shard* shard) {
    return shard != NULL ? &shard->waiter_store : NULL;
}

rt_waiter_store* rt_executor_waiter_store(rt_executor* ex) {
    return rt_executor_waiter_store_for_shard(ex, 0);
}

const rt_waiter_store* rt_executor_waiter_store_const(const rt_executor* ex) {
    return rt_executor_waiter_store_const_for_shard(ex, 0);
}

rt_waiter_store* rt_executor_waiter_store_for_shard(rt_executor* ex, size_t shard_index) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_index);
    return rt_shard_waiter_store(shard);
}

const rt_waiter_store* rt_executor_waiter_store_const_for_shard(const rt_executor* ex,
                                                                size_t shard_index) {
    return rt_shard_waiter_store_const(
        rt_runtime_shard_const(ex != NULL ? ex->runtime : NULL, shard_index));
}

rt_fd_registry* rt_shard_fd_registry(rt_shard* shard) {
    return shard != NULL ? &shard->fd_registry : NULL;
}

const rt_fd_registry* rt_shard_fd_registry_const(const rt_shard* shard) {
    return shard != NULL ? &shard->fd_registry : NULL;
}

rt_fd_registry* rt_executor_fd_registry(rt_executor* ex) {
    return rt_executor_fd_registry_for_shard(ex, 0);
}

rt_fd_registry* rt_executor_fd_registry_for_shard(rt_executor* ex, size_t shard_index) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), shard_index);
    return rt_shard_fd_registry(shard);
}

const rt_fd_registry* rt_executor_fd_registry_const(const rt_executor* ex) {
    return rt_executor_fd_registry_const_for_shard(ex, 0);
}

const rt_fd_registry* rt_executor_fd_registry_const_for_shard(const rt_executor* ex,
                                                              size_t shard_index) {
    return rt_shard_fd_registry_const(
        rt_runtime_shard_const(ex != NULL ? ex->runtime : NULL, shard_index));
}

rt_runtime_status rt_shard_scheduler_init(rt_shard* shard,
                                          uint32_t worker_count,
                                          uint8_t sched_mode_value,
                                          uint64_t sched_seed) {
    rt_scheduler* scheduler = rt_shard_scheduler(shard);
    if (scheduler == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    memset(scheduler, 0, sizeof(*scheduler));
    scheduler->worker_count = worker_count;
    scheduler->sched_mode = sched_mode_value;
    scheduler->sched_seed = sched_seed;
    if (worker_count == 0) {
        return RT_RUNTIME_STATUS_OK;
    }
    scheduler->local_queues = (rt_deque*)rt_alloc(
        (uint64_t)worker_count * (uint64_t)sizeof(rt_deque), _Alignof(rt_deque));
    if (scheduler->local_queues == NULL) {
        return RT_RUNTIME_STATUS_ALLOCATION_FAILED;
    }
    memset(scheduler->local_queues, 0, (size_t)worker_count * sizeof(rt_deque));
    return RT_RUNTIME_STATUS_OK;
}

uint32_t rt_runtime_default_worker_count(void) {
    uint32_t cpus = rt_runtime_detect_cpu_count();
    if (cpus < 2) {
        return 2;
    }
    return cpus;
}

uint32_t rt_runtime_default_blocking_count(uint32_t workers) {
    if (workers < 1) {
        workers = 1;
    }
    return workers;
}
