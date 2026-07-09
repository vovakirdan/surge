#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_SPAWN_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_SPAWN_H

#include <stddef.h>
#include <stdint.h>

typedef struct rt_executor rt_executor;
typedef struct rt_shard rt_shard;
typedef struct rt_remote_spawn_pending rt_remote_spawn_pending;

typedef enum rt_remote_spawn_status {
    RT_REMOTE_SPAWN_STATUS_OK = 0,
    RT_REMOTE_SPAWN_STATUS_PENDING = 1,
    RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT = 2,
    RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN = 3,
    RT_REMOTE_SPAWN_STATUS_QUEUE_FULL = 4,
    RT_REMOTE_SPAWN_STATUS_REFUSED = 5,
    RT_REMOTE_SPAWN_STATUS_STALE_TOKEN = 6,
} rt_remote_spawn_status;

typedef struct rt_far_task_handle {
    uint64_t task_id;
    uint64_t generation;
    uint32_t owner_shard_id;
    uint32_t _pad;
} rt_far_task_handle;

rt_remote_spawn_status rt_remote_spawn_publish(uint32_t dst_shard_id,
                                               int64_t poll_fn_id,
                                               void* state,
                                               rt_remote_spawn_pending** pending,
                                               rt_far_task_handle* out_handle);
rt_remote_spawn_status rt_remote_spawn_handle_validate(rt_executor* ex,
                                                       const rt_far_task_handle* handle);
uint64_t rt_remote_spawn_pending_request_id(const rt_remote_spawn_pending* pending);
size_t rt_remote_spawn_drain_inbound_locked(rt_executor* ex, rt_shard* shard, size_t limit);
void rt_remote_spawn_fail_all_pending(rt_executor* ex, rt_remote_spawn_status status);

#endif
