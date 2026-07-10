#ifndef SURGE_RUNTIME_NATIVE_RT_REMOTE_TASK_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_REMOTE_TASK_INTERNAL_H

#include "rt_async_internal.h"
#include "rt_remote_task.h"

typedef struct rt_far_task_lease rt_far_task_lease;
typedef struct rt_remote_task_state rt_remote_task_state;

typedef enum rt_remote_task_op {
    RT_REMOTE_TASK_OP_AWAIT = 1,
    RT_REMOTE_TASK_OP_CANCEL = 2,
    RT_REMOTE_TASK_OP_RELEASE = 3,
    RT_REMOTE_TASK_OP_EXECUTE = 4,
    RT_REMOTE_TASK_OP_CHANNEL_CREATE = 5,
} rt_remote_task_op;

enum {
    RT_REMOTE_TASK_HANDLE_OPEN = 0,
    RT_REMOTE_TASK_HANDLE_AWAIT = 1,
    RT_REMOTE_TASK_HANDLE_CANCEL = 2,
    RT_REMOTE_TASK_HANDLE_RELEASE = 3,
};

typedef enum rt_far_task_lease_state {
    RT_FAR_TASK_LEASE_OPEN = 0,
    RT_FAR_TASK_LEASE_TRANSFERRING = 1,
    RT_FAR_TASK_LEASE_RETURNING = 2,
    RT_FAR_TASK_LEASE_CONSUMED = 3,
    RT_FAR_TASK_LEASE_RELEASING = 4,
} rt_far_task_lease_state;

struct rt_far_task_lease {
    rt_far_task_handle handle;
    rt_executor* executor;
    rt_task* holder;
    rt_task* result_owner;
    _Atomic uint8_t state;
    _Atomic uint32_t refs;
    struct rt_far_task_lease* next;
};

struct rt_remote_task_pending {
    rt_executor* executor;
    uint64_t request_id;
    rt_far_task_handle handle;
    uint32_t source_shard_id;
    uint8_t op;
    uint8_t status;
    uint8_t reply_status;
    uint8_t result_kind;
    uint8_t owner_registered;
    uint8_t cancel_routed;
    uint64_t caller_task_id;
    uint64_t body_poll_fn_id;
    void* body_state;
    uint64_t result_bits;
    _Atomic uint32_t refs;
    uint8_t listed;
    struct rt_remote_task_pending* next;
};

struct rt_remote_task_state {
    rt_executor* executor;
    pthread_mutex_t lock;
    _Atomic uint64_t next_request_id;
    rt_remote_task_pending* pending_head;
    rt_far_task_lease* lease_head;
};

// Result-kind byte for TaskResult<T> replies: 2 = Cancelled, 1 = Success.
static inline uint8_t rt_remote_task_result_kind(const rt_task* task) {
    return task != NULL && task->result_kind == TASK_RESULT_CANCELLED ? 2 : 1;
}

rt_remote_task_state* rt_remote_task_state_get(rt_executor* ex);
rt_far_task_lease* rt_far_task_lease_find_locked(rt_remote_task_state* state,
                                                 const rt_far_task_handle* handle);
rt_remote_task_pending* rt_remote_task_pending_new(rt_executor* ex,
                                                   const rt_far_task_handle* handle,
                                                   uint32_t source_shard_id,
                                                   rt_remote_task_op op,
                                                   int listed);
void rt_remote_task_pending_add_ref(rt_remote_task_pending* pending);
void rt_remote_task_pending_release(rt_remote_task_pending* pending);
void rt_remote_task_pending_consume(rt_remote_task_pending* pending);
rt_remote_task_status rt_remote_task_pending_snapshot(const rt_remote_task_pending* pending,
                                                      uint8_t* out_kind,
                                                      uint64_t* out_bits);
void rt_remote_task_pending_set_reply(rt_remote_task_pending* pending,
                                      rt_remote_task_status status,
                                      uint8_t result_kind,
                                      uint64_t result_bits);
void rt_remote_task_pending_finish(rt_executor* ex,
                                   rt_remote_task_pending* pending,
                                   rt_remote_task_status status,
                                   uint8_t result_kind,
                                   uint64_t result_bits);
void rt_remote_task_pending_set_owner_registered(rt_remote_task_pending* pending, int value);
rt_remote_task_pending* rt_remote_task_pending_take_owner(const rt_task* task);

rt_remote_task_status rt_far_task_lease_consume(const rt_far_task_handle* handle);
void rt_far_task_lease_restore(const rt_far_task_handle* handle);
void rt_far_task_lease_drop_ref(rt_far_task_lease* lease);
void rt_far_task_lease_release_route(rt_far_task_lease* lease);
int rt_far_task_adopt_result(rt_task* producer, rt_task* holder);
void rt_far_task_release_result(rt_executor* ex, rt_task* producer);

waker_key rt_remote_task_reply_key(uint64_t request_id, uint32_t source_shard_id);
int rt_remote_task_prepare_reply_wait(rt_executor* ex,
                                      rt_task* current,
                                      rt_remote_task_pending* pending);
void rt_remote_task_clear_reply_wait(rt_executor* ex,
                                     rt_task* current,
                                     const rt_remote_task_pending* pending);

rt_remote_task_status rt_remote_task_transport_status(rt_transport_status status);
void rt_remote_task_reply_owner_done(rt_executor* ex,
                                     rt_task* task,
                                     rt_remote_task_pending* pending);
void rt_immediate_on_dispatch_execute(rt_executor* ex, const rt_transport_msg* msg);
void rt_immediate_on_cancel_inflight(rt_executor* ex, rt_remote_task_pending* pending);
void rt_remote_task_reply_or_finish(rt_executor* ex,
                                    rt_remote_task_pending* pending,
                                    rt_remote_task_status status,
                                    uint8_t result_kind,
                                    uint64_t result_bits,
                                    rt_transport_msg_kind reply_kind);

#endif
