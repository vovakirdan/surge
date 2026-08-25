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
// arms' shared owner shard; the reply names the winner index) and its
// owner-side dispatch.
//
// `send_values` is one address per arm, and it is the caller's LIVE storage on
// the current poll: the arming call MOVES each SEND payload out of it into the
// arm's cell, and the terminal retry call moves every losing payload back into
// it. No call in between reads it, and the runtime never keeps it -- a park
// would leave a stored address naming a frame that is gone.
//
// `payload_type_ids` names each SEND arm's element type, which is how the
// runtime finds the descriptor that moves and destroys that payload. A RECV
// arm's entries in both arrays are ignored.
rt_remote_task_status rt_far_channel_select(const rt_far_task_handle* const* anchors,
                                            const uint8_t* kinds,
                                            void* const* send_values,
                                            const uint64_t* payload_type_ids,
                                            uint64_t count,
                                            uint64_t state_type_id,
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
void rt_far_channel_handle_free(rt_far_task_handle* handle);
void rt_far_channel_handle_drop(rt_far_task_handle* handle);
rt_remote_task_status rt_far_channel_create(uint64_t placement,
                                            uint64_t capacity,
                                            uint64_t payload_drop_fn_id,
                                            rt_remote_task_pending** pending,
                                            rt_far_task_handle* out_handle,
                                            uint8_t* out_kind,
                                            uint64_t* out_bits);
void rt_far_channel_dispatch_create(rt_executor* ex, const rt_transport_msg* msg);

#endif
