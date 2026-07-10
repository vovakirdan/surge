#include "rt_remote_task_internal.h"

#include <limits.h>

rt_remote_task_status rt_remote_task_transport_status(rt_transport_status status) {
    if (status == RT_TRANSPORT_STATUS_OK) {
        return RT_REMOTE_TASK_STATUS_OK;
    }
    if (status == RT_TRANSPORT_STATUS_QUEUE_FULL) {
        return RT_REMOTE_TASK_STATUS_QUEUE_FULL;
    }
    return RT_REMOTE_TASK_STATUS_REFUSED;
}

static uint32_t current_source_shard(const rt_task* current) {
    if (current != NULL && current->owner_shard_valid != 0) {
        return current->owner_shard_id;
    }
    uint32_t worker_shard = rt_debug_current_worker_shard_id();
    return worker_shard != UINT32_MAX ? worker_shard : 0;
}

static rt_remote_task_status
finish_retry(rt_remote_task_pending** slot, uint8_t* out_kind, uint64_t* out_bits) {
    rt_remote_task_status status = rt_remote_task_pending_snapshot(*slot, out_kind, out_bits);
    if (status != RT_REMOTE_TASK_STATUS_PENDING) {
        rt_remote_task_pending_consume(*slot);
        *slot = NULL;
    }
    return status;
}

static rt_remote_task_status start_remote_task(rt_remote_task_op op,
                                               const rt_far_task_handle* handle,
                                               rt_remote_task_pending** pending,
                                               uint8_t* out_kind,
                                               uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (ex == NULL || pending == NULL || current == NULL || rt_current_task_id() == 0) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        rt_remote_task_status status =
            rt_remote_task_pending_snapshot(*pending, out_kind, out_bits);
        if (status != RT_REMOTE_TASK_STATUS_PENDING) {
            return finish_retry(pending, out_kind, out_bits);
        }
        if (rt_remote_task_prepare_reply_wait(ex, current, *pending) != 0) {
            return finish_retry(pending, out_kind, out_bits);
        }
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    if (handle == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_remote_task_status lease_status = rt_far_task_lease_consume(handle);
    if (lease_status != RT_REMOTE_TASK_STATUS_OK) {
        return lease_status;
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* owner = rt_runtime_shard(runtime, handle->owner_shard_id);
    if (owner == NULL) {
        rt_far_task_lease_restore(handle);
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&owner->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        rt_far_task_lease_restore(handle);
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }
    rt_remote_task_pending* request =
        rt_remote_task_pending_new(ex, handle, current_source_shard(current), op, 1);
    if (request == NULL) {
        rt_far_task_lease_restore(handle);
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    *pending = request;
    (void)rt_remote_task_prepare_reply_wait(ex, current, request);
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg_kind kind = op == RT_REMOTE_TASK_OP_AWAIT
                                     ? RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST
                                     : RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST;
    rt_transport_msg msg = {
        .kind = kind,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = handle->owner_shard_id,
        .route_id = handle->task_id,
        .generation = handle->generation,
        .payload = request,
        .payload_len = 0,
    };
    rt_remote_task_status status =
        rt_remote_task_transport_status(rt_transport_enqueue(owner, &msg));
    if (status == RT_REMOTE_TASK_STATUS_OK) {
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    rt_remote_task_clear_reply_wait(ex, current, request);
    rt_remote_task_pending_consume(request);
    rt_remote_task_pending_release(request);
    *pending = NULL;
    rt_far_task_lease_restore(handle);
    return status;
}

rt_remote_task_status rt_far_task_await(const rt_far_task_handle* handle,
                                        rt_remote_task_pending** pending,
                                        uint8_t* out_kind,
                                        uint64_t* out_bits) {
    return start_remote_task(RT_REMOTE_TASK_OP_AWAIT, handle, pending, out_kind, out_bits);
}

rt_remote_task_status rt_far_task_cancel(const rt_far_task_handle* handle,
                                         rt_remote_task_pending** pending,
                                         uint8_t* out_kind,
                                         uint64_t* out_bits) {
    return start_remote_task(RT_REMOTE_TASK_OP_CANCEL, handle, pending, out_kind, out_bits);
}

rt_remote_task_status rt_far_task_release(const rt_far_task_handle* handle) {
    rt_executor* ex = ensure_exec();
    rt_runtime* runtime = ex != NULL ? rt_executor_runtime(ex) : NULL;
    rt_shard* owner = handle != NULL ? rt_runtime_shard(runtime, handle->owner_shard_id) : NULL;
    if (ex == NULL || handle == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (owner == NULL) {
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    rt_remote_task_pending* request = rt_remote_task_pending_new(
        ex, handle, current_source_shard(rt_current_task()), RT_REMOTE_TASK_OP_RELEASE, 0);
    if (request == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = handle->owner_shard_id,
        .route_id = handle->task_id,
        .generation = handle->generation,
        .payload = request,
        .payload_len = 0,
    };
    rt_remote_task_status status =
        rt_remote_task_transport_status(rt_remote_spawn_enqueue_with_drain(ex, owner, &msg));
    if (status != RT_REMOTE_TASK_STATUS_OK) {
        rt_remote_task_pending_release(request);
    }
    return status;
}
