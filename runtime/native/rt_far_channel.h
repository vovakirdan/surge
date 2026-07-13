#ifndef SURGE_RUNTIME_NATIVE_RT_FAR_CHANNEL_H
#define SURGE_RUNTIME_NATIVE_RT_FAR_CHANNEL_H

#include "rt_remote_task.h"

typedef struct rt_far_channel_state rt_far_channel_state;

rt_far_channel_state* rt_far_channel_state_get(rt_executor* ex);
// Test-support census of live registry entries.
size_t rt_far_channel_debug_live_count(rt_executor* ex);
// Test-support census of lease rows across live entries.
size_t rt_far_channel_debug_lease_count(rt_executor* ex);
// Active leases on the entry a token addresses (deadlock diagnostics).
size_t rt_far_channel_active_lease_count(rt_executor* ex, const rt_far_task_handle* handle);
// Owner-side sibling mint: validates the source lease and issues a fresh
// lease token on the same entry.
rt_remote_task_status rt_far_channel_mint_sibling(rt_executor* ex,
                                                  const rt_far_task_handle* source,
                                                  rt_far_task_handle* out);
// Caller-side share (execute/reply discipline; destination = the source
// token's owner shard) and its owner-side dispatch.
rt_remote_task_status rt_far_channel_share(const rt_far_task_handle* source,
                                           rt_remote_task_pending** pending,
                                           rt_far_task_handle* out_handle,
                                           uint8_t* out_kind,
                                           uint64_t* out_bits);
void rt_far_channel_dispatch_share(rt_executor* ex, const rt_transport_msg* msg);
// Caller-side remote select (execute/reply discipline; destination = the
// arms' shared owner shard; reply bits = the winner index) and its
// owner-side dispatch.
rt_remote_task_status rt_far_channel_select(const rt_far_task_handle* const* anchors,
                                            const uint8_t* kinds,
                                            const uint64_t* send_bits,
                                            uint64_t count,
                                            int64_t poll_fn_id,
                                            void* state,
                                            rt_remote_task_pending** pending,
                                            uint8_t* out_kind,
                                            uint64_t* out_bits);
void rt_far_channel_dispatch_select(rt_executor* ex, const rt_transport_msg* msg);
// Init-rollback pair; mirrors the remote-task state convention.
rt_runtime_status rt_far_channel_state_init(rt_executor* ex);
rt_runtime_status rt_far_channel_state_destroy(rt_executor* ex);

rt_remote_task_status rt_far_channel_mint(rt_executor* ex,
                                          void* channel,
                                          uint32_t owner_shard_id,
                                          rt_far_task_handle* out);
void* rt_far_channel_resolve(rt_executor* ex, const rt_far_task_handle* handle);
int rt_far_channel_pin(rt_executor* ex, const rt_far_task_handle* handle, void** out_channel);
void rt_far_channel_unpin(rt_executor* ex, const rt_far_task_handle* handle);
rt_remote_task_status rt_far_channel_release(rt_executor* ex, const rt_far_task_handle* handle);
void rt_far_channel_release_all(rt_executor* ex);

rt_remote_task_status rt_far_channel_handle_alloc(rt_far_task_handle** out);
void rt_far_channel_handle_free(const rt_far_task_handle* handle);
void rt_far_channel_handle_drop(const rt_far_task_handle* handle);
rt_remote_task_status rt_far_channel_create(uint64_t placement,
                                            uint64_t capacity,
                                            rt_remote_task_pending** pending,
                                            rt_far_task_handle* out_handle,
                                            uint8_t* out_kind,
                                            uint64_t* out_bits);
void rt_far_channel_dispatch_create(rt_executor* ex, const rt_transport_msg* msg);

#endif
