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

// `out_dst` is the caller's storage for the result, sized from the result's own
// type, and it is READ ONLY on the terminal call: every earlier call answers
// PENDING and must not be given an address that a park would outlive. The value
// itself never becomes a machine word -- the reply names the producer's slot
// and this moves it out.
rt_remote_task_status rt_far_task_await(const rt_far_task_handle* handle,
                                        uint64_t result_type_id,
                                        rt_remote_task_pending** pending,
                                        uint8_t* out_kind,
                                        void* out_dst);
rt_remote_task_status rt_far_task_cancel(const rt_far_task_handle* handle,
                                         uint64_t result_type_id,
                                         rt_remote_task_pending** pending,
                                         uint8_t* out_kind,
                                         void* out_dst);
rt_remote_task_status rt_far_task_release(const rt_far_task_handle* handle);
// Immediate `on placement` execute/reply: one request, one reply, one
// request-scoped cancellation token, no publicly observable far Task handle.
rt_remote_task_status rt_immediate_on_execute_anchored(const rt_far_task_handle* anchor,
                                                       uint64_t state_type_id,
                                                       uint64_t result_type_id,
                                                       int64_t poll_fn_id,
                                                       void* state,
                                                       rt_remote_task_pending** pending,
                                                       uint8_t* out_kind,
                                                       void* out_dst);
rt_remote_task_status rt_immediate_on_execute(uint64_t placement,
                                              uint64_t state_type_id,
                                              uint64_t result_type_id,
                                              int64_t poll_fn_id,
                                              void* state,
                                              rt_remote_task_pending** pending,
                                              uint8_t* out_kind,
                                              void* out_dst);
rt_remote_spawn_status rt_far_task_handle_alloc(rt_far_task_handle** out_handle);
void rt_far_task_handle_free(const rt_far_task_handle* handle);
void rt_far_task_begin_transfer(const rt_far_task_handle* handle);
void rt_far_task_finish_transfer(const rt_far_task_handle* handle, void* child_task);
void rt_far_task_prepare_return(const rt_far_task_handle* handle);
void rt_far_task_release_owned(rt_executor* ex, const rt_task* holder);
void rt_far_task_release_all(rt_executor* ex);
// Serves this task's result to one asker: an independent value when more than
// one handle can still ask, and a move when this is the only one. The value is
// written into `out_dst`, which the caller sized from the result's own type.
uint8_t rt_far_task_take_result(rt_task* producer, rt_task* holder, void* out_dst);
void rt_far_task_release_result(rt_executor* ex, rt_task* producer);

rt_runtime_status rt_remote_task_state_init(rt_executor* ex);
// Init-rollback pair. The global executor itself is process-lifetime; normal
// shutdown quiesces it but does not destroy executor-owned locks/state.
rt_runtime_status rt_remote_task_state_destroy(rt_executor* ex);

int rt_remote_task_dispatch_message(rt_executor* ex, const rt_transport_msg* msg);
void rt_remote_task_release_msg_payload(const rt_transport_msg* msg);
void rt_remote_task_on_owner_done(rt_executor* ex, rt_task* task);
void rt_immediate_on_release_owned(rt_executor* ex, const rt_task* caller);
void rt_remote_task_release_owned(rt_executor* ex, const rt_task* caller);
void rt_remote_task_fail_all_pending(rt_executor* ex, rt_remote_task_status status);
// Idle-park-edge self-deadlock detection for suspended execute blocks.
void rt_remote_task_deadlock_check(rt_executor* ex);
// The dispatch-cached local channel of the calling anchored body, or NULL.
void* rt_remote_task_anchored_channel_current(void);
// Both halves of the calling anchored body's binding: the dispatch-cached
// channel and the shipped poll state. Returns 0 outside a bound body.
int rt_remote_task_anchored_binding_current(void** out_channel, void** out_state);
// Compiled anchored-body channel operations over the dispatch-cached
// channel; parked send/recv yields inside (re-entry restarts the body), so
// returning from a helper means the operation completed.
void rt_anchored_channel_send(void* src);
uint8_t rt_anchored_channel_recv(void* dst);
void rt_anchored_channel_close(void);
// The remote-select body's single operation: local select over the bound
// arm channels; parked selection yields inside (re-entry restarts the
// body), so returning means a winner was decided. Returns the winner index.
uint64_t rt_anchored_channel_select(void);

#endif
