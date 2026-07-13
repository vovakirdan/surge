#include "rt_far_channel.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"

static rt_remote_task_status
enqueue_reply(rt_executor* ex, rt_remote_task_pending* pending, rt_transport_msg_kind kind) {
    rt_runtime* runtime = ex != NULL ? rt_executor_runtime(ex) : NULL;
    rt_shard* source = pending != NULL ? rt_runtime_shard(runtime, pending->source_shard_id) : NULL;
    if (source == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_transport_msg reply = {
        .kind = kind,
        .source_shard_id = pending->handle.owner_shard_id,
        .target_shard_id = pending->source_shard_id,
        .route_id = pending->request_id,
        .generation = pending->handle.generation,
        .payload = pending,
        .payload_len = 0,
    };
    return rt_remote_task_transport_status(rt_remote_spawn_enqueue_with_drain(ex, source, &reply));
}

void rt_remote_task_reply_or_finish(rt_executor* ex,
                                    rt_remote_task_pending* pending,
                                    rt_remote_task_status status,
                                    uint8_t result_kind,
                                    uint64_t result_bits,
                                    rt_transport_msg_kind reply_kind) {
    if (pending == NULL) {
        return;
    }
    rt_remote_task_pending_set_reply(pending, status, result_kind, result_bits);
    if (status == RT_REMOTE_TASK_STATUS_OK) {
        rt_remote_task_status enqueue_status = enqueue_reply(ex, pending, reply_kind);
        if (enqueue_status == RT_REMOTE_TASK_STATUS_OK) {
            return;
        }
        status = enqueue_status;
    }
    rt_remote_task_pending_finish(ex, pending, status, result_kind, result_bits);
    rt_remote_task_pending_release(pending);
}

static int request_matches(const rt_transport_msg* msg, const rt_remote_task_pending* pending) {
    return msg != NULL && pending != NULL && msg->route_id == pending->handle.task_id &&
           msg->generation == pending->handle.generation &&
           msg->source_shard_id == pending->source_shard_id &&
           msg->target_shard_id == pending->handle.owner_shard_id;
}

static int reply_matches(const rt_transport_msg* msg, const rt_remote_task_pending* pending) {
    return msg != NULL && pending != NULL && msg->route_id == pending->request_id &&
           msg->generation == pending->handle.generation &&
           msg->source_shard_id == pending->handle.owner_shard_id &&
           msg->target_shard_id == pending->source_shard_id;
}

static void record_stale_drop(rt_executor* ex, const rt_transport_msg* msg) {
    rt_runtime* runtime = ex != NULL ? rt_executor_runtime(ex) : NULL;
    rt_shard* shard = msg != NULL ? rt_runtime_shard(runtime, msg->target_shard_id) : NULL;
    rt_transport_record_remote_task_stale(shard);
}

static int
owner_matches(const rt_far_task_handle* handle, const rt_task* task, uint32_t owner_shard_id) {
    return handle != NULL && task != NULL && handle->kind == RT_FAR_HANDLE_KIND_TASK &&
           handle->task_id == task->id && task->generation == handle->generation &&
           task->owner_shard_valid != 0 && task->owner_shard_id == owner_shard_id &&
           task->owner_shard_id == handle->owner_shard_id;
}

static rt_remote_task_status consume_handle(rt_task* task, uint8_t next_state) {
    if (task == NULL) {
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    uint8_t expected = RT_REMOTE_TASK_HANDLE_OPEN;
    if (!atomic_compare_exchange_strong_explicit(&task->remote_handle_state,
                                                 &expected,
                                                 next_state,
                                                 memory_order_acq_rel,
                                                 memory_order_acquire)) {
        return RT_REMOTE_TASK_STATUS_CONSUMED;
    }
    return RT_REMOTE_TASK_STATUS_OK;
}

// Validates route/generation for an inbound owner-side request; on any
// mismatch records the stale drop and answers STALE_TOKEN on reply_kind.
static rt_task* take_valid_request(rt_executor* ex,
                                   const rt_transport_msg* msg,
                                   rt_remote_task_pending* pending,
                                   rt_transport_msg_kind reply_kind) {
    if (!request_matches(msg, pending)) {
        record_stale_drop(ex, msg);
        rt_remote_task_reply_or_finish(
            ex, pending, RT_REMOTE_TASK_STATUS_STALE_TOKEN, 2, 0, reply_kind);
        return NULL;
    }
    rt_task* task = get_task(ex, pending->handle.task_id);
    if (!owner_matches(&pending->handle, task, msg->target_shard_id)) {
        record_stale_drop(ex, msg);
        rt_remote_task_reply_or_finish(
            ex, pending, RT_REMOTE_TASK_STATUS_STALE_TOKEN, 2, 0, reply_kind);
        return NULL;
    }
    return task;
}

static void cancel_owner_task(rt_executor* ex, uint64_t task_id) {
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
        rt_trace_control_lock_site(RT_CTRL_SITE_HANDLE);
        rt_trace_control_lock_handle_site(RT_CTRL_HANDLE_CANCEL);
    }
    cancel_task(ex, task_id);
    if (need_control) {
        rt_control_unlock(ex);
    }
}

static int finish_registered_owner(rt_executor* ex, rt_task* task) {
    if (task_status_load(task) != TASK_DONE) {
        return 0;
    }
    rt_remote_task_pending* pending = rt_remote_task_pending_take_owner(task);
    if (pending == NULL) {
        return 1;
    }
    rt_remote_task_reply_owner_done(ex, task, pending);
    return 1;
}

// Cancel of an in-flight immediate execute: token-validated, no handle
// consumption, no second owner registration, never touches the reply edge.
static void dispatch_execute_cancel(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    if (!request_matches(msg, pending)) {
        record_stale_drop(ex, msg);
        rt_remote_task_pending_release(pending);
        return;
    }
    const rt_task* task = get_task(ex, pending->handle.task_id);
    if (owner_matches(&pending->handle, task, msg->target_shard_id)) {
        if (task_status_load(task) != TASK_DONE) {
            cancel_owner_task(ex, task->id);
        }
    } else {
        record_stale_drop(ex, msg);
    }
    rt_remote_task_pending_release(pending);
}

static void dispatch_request(rt_executor* ex, const rt_transport_msg* msg, rt_remote_task_op op) {
    rt_transport_msg_kind reply_kind = op == RT_REMOTE_TASK_OP_CANCEL
                                           ? RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK
                                           : RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION;
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    if (op == RT_REMOTE_TASK_OP_CANCEL && pending != NULL &&
        (pending->op == RT_REMOTE_TASK_OP_EXECUTE ||
         pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED)) {
        dispatch_execute_cancel(ex, msg);
        return;
    }
    rt_task* task = take_valid_request(ex, msg, pending, reply_kind);
    if (task == NULL) {
        return;
    }
    uint8_t next_state =
        op == RT_REMOTE_TASK_OP_CANCEL ? RT_REMOTE_TASK_HANDLE_CANCEL : RT_REMOTE_TASK_HANDLE_AWAIT;
    rt_remote_task_status consumed = consume_handle(task, next_state);
    if (consumed != RT_REMOTE_TASK_STATUS_OK) {
        rt_remote_task_reply_or_finish(ex, pending, consumed, 2, 0, reply_kind);
        return;
    }
    if (task_status_load(task) == TASK_DONE) {
        uint64_t bits = op == RT_REMOTE_TASK_OP_AWAIT ? task->result_bits : 0;
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_OK,
                                       rt_remote_task_result_kind(task),
                                       bits,
                                       reply_kind);
        task_release_lane_aware(ex, task);
        return;
    }
    RT_SYNC_POINT(SP_REMOTE_TASK_BEFORE_OWNER_REGISTER);
    task_add_ref(task);
    rt_remote_task_pending_set_owner_registered(pending, 1);
    RT_SYNC_POINT(SP_REMOTE_TASK_AFTER_OWNER_REGISTER);
    if (finish_registered_owner(ex, task)) {
        task_release_lane_aware(ex, task);
        return;
    }
    if (op == RT_REMOTE_TASK_OP_CANCEL) {
        cancel_owner_task(ex, pending->handle.task_id);
    } else {
        wake_task(ex, pending->handle.task_id, 1);
    }
    task_release_lane_aware(ex, task);
}

static void dispatch_release(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    if (!request_matches(msg, pending)) {
        record_stale_drop(ex, msg);
        rt_remote_task_pending_release(pending);
        return;
    }
    rt_task* task = get_task(ex, pending->handle.task_id);
    if (owner_matches(&pending->handle, task, msg->target_shard_id) &&
        consume_handle(task, RT_REMOTE_TASK_HANDLE_RELEASE) == RT_REMOTE_TASK_STATUS_OK) {
        if (task_status_load(task) != TASK_DONE) {
            cancel_owner_task(ex, task->id);
        }
        task_release_lane_aware(ex, task);
    }
    rt_remote_task_pending_release(pending);
}

static void dispatch_reply(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    if (!reply_matches(msg, pending)) {
        record_stale_drop(ex, msg);
        rt_remote_task_pending_release(pending);
        return;
    }
    rt_remote_task_pending_finish(ex,
                                  pending,
                                  (rt_remote_task_status)pending->reply_status,
                                  pending->result_kind,
                                  pending->result_bits);
    // Consume (unlink + release): an orphaned reply — the caller already
    // resumed through the cancel path and dropped its reference — must not
    // leave a freed pending linked in the list.
    rt_remote_task_pending_consume(pending);
}

int rt_remote_task_dispatch_message(rt_executor* ex, const rt_transport_msg* msg) {
    if (msg == NULL) {
        return 0;
    }
    switch (msg->kind) {
        case RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST:
            dispatch_request(ex, msg, RT_REMOTE_TASK_OP_AWAIT);
            return 1;
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST:
            dispatch_request(ex, msg, RT_REMOTE_TASK_OP_CANCEL);
            return 1;
        case RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST:
            dispatch_release(ex, msg);
            return 1;
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST:
            rt_immediate_on_dispatch_execute(ex, msg);
            return 1;
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST:
            rt_far_channel_dispatch_create(ex, msg);
            return 1;
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST:
            rt_far_channel_dispatch_share(ex, msg);
            return 1;
        case RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION:
        case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK:
        case RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY:
        case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY:
            dispatch_reply(ex, msg);
            return 1;
        default:
            return 0;
    }
}
