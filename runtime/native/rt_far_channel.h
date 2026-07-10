#ifndef SURGE_RUNTIME_NATIVE_RT_FAR_CHANNEL_H
#define SURGE_RUNTIME_NATIVE_RT_FAR_CHANNEL_H

#include "rt_remote_task.h"

typedef struct rt_far_channel_state rt_far_channel_state;

rt_far_channel_state* rt_far_channel_state_get(rt_executor* ex);
// Init-rollback pair; mirrors the remote-task state convention.
rt_runtime_status rt_far_channel_state_init(rt_executor* ex);
rt_runtime_status rt_far_channel_state_destroy(rt_executor* ex);

rt_remote_task_status rt_far_channel_mint(rt_executor* ex,
                                          void* channel,
                                          uint32_t owner_shard_id,
                                          rt_far_task_handle* out);
void* rt_far_channel_resolve(rt_executor* ex, const rt_far_task_handle* handle);
rt_remote_task_status rt_far_channel_release(rt_executor* ex, const rt_far_task_handle* handle);
void rt_far_channel_release_all(rt_executor* ex);

rt_remote_task_status rt_far_channel_create(uint64_t placement,
                                            uint64_t capacity,
                                            rt_remote_task_pending** pending,
                                            rt_far_task_handle* out_handle,
                                            uint8_t* out_kind,
                                            uint64_t* out_bits);
void rt_far_channel_dispatch_create(rt_executor* ex, const rt_transport_msg* msg);

#endif
