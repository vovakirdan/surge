#include "rt_async_internal.h"

#include <limits.h>
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

static rt_runtime_status rt_runtime_init_n1(rt_runtime* runtime, rt_executor* ex) {
    if (runtime == NULL || ex == NULL) {
        return RT_RUNTIME_STATUS_INVALID_ARGUMENT;
    }
    memset(runtime, 0, sizeof(*runtime));
    runtime->shard_count = RT_RUNTIME_SHARD_COUNT;
    runtime->shards[0].runtime = runtime;
    runtime->shards[0].executor = ex;
    runtime->shards[0].shard_id = 0;
    ex->runtime = runtime;
    return RT_RUNTIME_STATUS_OK;
}

rt_runtime_status rt_runtime_init_global_n1(rt_executor* ex) {
    return rt_runtime_init_n1(&runtime_state, ex);
}

rt_runtime* rt_executor_runtime(rt_executor* ex) {
    return ex != NULL ? ex->runtime : NULL;
}

rt_shard* rt_runtime_shard0(rt_runtime* runtime) {
    if (runtime == NULL || runtime->shard_count != RT_RUNTIME_SHARD_COUNT) {
        return NULL;
    }
    return &runtime->shards[0];
}

size_t rt_runtime_shard_count(const rt_runtime* runtime) {
    return runtime != NULL ? runtime->shard_count : 0;
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
