#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_TASK_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_TASK_H

#include <stdint.h>

#include "rt_remote_spawn.h"
#include "rt_transport.h"

typedef struct rt_remote_task_pending rt_remote_task_pending;
typedef struct rt_task rt_task;

typedef enum rt_remote_task_status {
    RT_REMOTE_TASK_STATUS_OK = 0,
    RT_REMOTE_TASK_STATUS_PENDING = 1,
    RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT = 2,
    RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN = 3,
    RT_REMOTE_TASK_STATUS_QUEUE_FULL = 4,
    RT_REMOTE_TASK_STATUS_REFUSED = 5,
    RT_REMOTE_TASK_STATUS_STALE_TOKEN = 6,
    RT_REMOTE_TASK_STATUS_CONSUMED = 7,
    RT_REMOTE_TASK_STATUS_UNSUPPORTED_PLACEMENT = 8,
} rt_remote_task_status;

rt_remote_task_status rt_far_task_await(const rt_far_task_handle* handle,
                                        rt_remote_task_pending** pending,
                                        uint8_t* out_kind,
                                        uint64_t* out_bits);
rt_remote_task_status rt_far_task_cancel(const rt_far_task_handle* handle,
                                         rt_remote_task_pending** pending,
                                         uint8_t* out_kind,
                                         uint64_t* out_bits);
rt_remote_task_status rt_far_task_release(const rt_far_task_handle* handle);
// Immediate `on placement` execute/reply: one request, one reply, one
// request-scoped cancellation token, no publicly observable far Task handle.
rt_remote_task_status rt_immediate_on_execute_anchored(const rt_far_task_handle* anchor,
                                                       int64_t poll_fn_id,
                                                       void* state,
                                                       rt_remote_task_pending** pending,
                                                       uint8_t* out_kind,
                                                       uint64_t* out_bits);
rt_remote_task_status rt_immediate_on_execute(uint64_t placement,
                                              int64_t poll_fn_id,
                                              void* state,
                                              rt_remote_task_pending** pending,
                                              uint8_t* out_kind,
                                              uint64_t* out_bits);
rt_remote_spawn_status rt_far_task_handle_alloc(rt_far_task_handle** out_handle);
void rt_far_task_handle_free(const rt_far_task_handle* handle);
void rt_far_task_begin_transfer(const rt_far_task_handle* handle);
void rt_far_task_finish_transfer(const rt_far_task_handle* handle, void* child_task);
void rt_far_task_prepare_return(const rt_far_task_handle* handle);
void rt_far_task_release_owned(rt_executor* ex, const rt_task* holder);
void rt_far_task_release_all(rt_executor* ex);
uint8_t rt_far_task_take_result(rt_task* producer, rt_task* holder, uint64_t* out_bits);
void rt_far_task_release_result(rt_executor* ex, rt_task* producer);

rt_runtime_status rt_remote_task_state_init(rt_executor* ex);
// Init-rollback pair. The global executor itself is process-lifetime; normal
// shutdown quiesces it but does not destroy executor-owned locks/state.
rt_runtime_status rt_remote_task_state_destroy(rt_executor* ex);

int rt_remote_task_dispatch_message(rt_executor* ex, const rt_transport_msg* msg);
void rt_remote_task_release_msg_payload(const rt_transport_msg* msg);
void rt_remote_task_on_owner_done(rt_executor* ex, rt_task* task);
void rt_immediate_on_release_owned(rt_executor* ex, const rt_task* caller);
void rt_remote_task_fail_all_pending(rt_executor* ex, rt_remote_task_status status);

#endif
