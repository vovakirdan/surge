#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_SPAWN_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_SPAWN_INTERNAL_H

#include "rt_async_internal.h"
#include "rt_remote_spawn.h"

struct rt_remote_spawn_pending {
    uint64_t request_id;
    uint64_t poll_fn_id;
    void* state;
    uint64_t caller_task_id;
    uint32_t source_shard_id;
    uint32_t target_shard_id;
    rt_remote_spawn_status status;
    rt_far_task_handle handle;
    rt_far_task_handle* out_handle;
    _Atomic uint32_t refs;
    uint8_t listed;
    uint8_t abandoned;
    struct rt_remote_spawn_pending* next;
};

void remote_spawn_pending_add_ref(rt_remote_spawn_pending* pending);
void remote_spawn_pending_release(rt_remote_spawn_pending* pending);
void remote_spawn_pending_link(rt_remote_spawn_pending* pending);
void remote_spawn_pending_finish(rt_executor* ex,
                                 rt_remote_spawn_pending* pending,
                                 rt_remote_spawn_status status,
                                 const rt_far_task_handle* handle);
rt_remote_spawn_status remote_spawn_pending_snapshot(const rt_remote_spawn_pending* pending,
                                                     rt_far_task_handle* out);
void remote_spawn_pending_consume(rt_remote_spawn_pending* pending);
void remote_spawn_pending_fail_all(rt_executor* ex, rt_remote_spawn_status status);

#endif
