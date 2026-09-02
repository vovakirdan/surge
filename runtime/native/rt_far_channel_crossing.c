#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task_internal.h"

// The far channel's two crossings -- create and share -- split out of
// rt_far_channel.c with no change in behaviour: the registry, its leases and
// pins stay there; the execute/reply discipline that reaches the registry
// through the transport lives here. Nothing private crosses the seam: both
// dispatchers mint through the registry's public entry points.

// Caller-side create: execute/reply discipline with a caller-allocated
// handle out-param, mirroring `spawn on` publication. The destination
// creates the channel with owner-side heap accounting, mints the registry
// entry, binds the token into the shared pending, and replies exactly once.
rt_remote_task_status rt_far_channel_create(uint64_t placement,
                                            uint64_t capacity,
                                            uint64_t payload_drop_fn_id,
                                            rt_remote_task_pending** pending,
                                            rt_far_task_handle* out_handle,
                                            uint8_t* out_kind,
                                            uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (ex == NULL || pending == NULL || out_handle == NULL || current == NULL ||
        rt_current_task_id() == 0) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        rt_remote_task_status status =
            rt_remote_task_pending_snapshot(*pending, out_kind, out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            if (rt_remote_task_prepare_reply_wait(ex, current, *pending) == 0) {
                return RT_REMOTE_TASK_STATUS_PENDING;
            }
            status = rt_remote_task_pending_snapshot(*pending, out_kind, out_bits);
        }
        if (status == RT_REMOTE_TASK_STATUS_OK) {
            *out_handle = (*pending)->handle;
        }
        rt_remote_task_pending_consume(*pending);
        *pending = NULL;
        return status;
    }

    rt_runtime* runtime = rt_executor_runtime(ex);
    uint32_t source_shard = current->owner_shard_valid != 0 ? current->owner_shard_id : 0;
    rt_placement_resolution resolved = rt_placement_resolve(runtime, placement, source_shard);
    if (resolved.status == RT_PLACEMENT_STATUS_UNSUPPORTED) {
        return RT_REMOTE_TASK_STATUS_UNSUPPORTED_PLACEMENT;
    }
    if (resolved.status != RT_PLACEMENT_STATUS_OK) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_shard* destination = rt_runtime_shard(runtime, resolved.shard_id);
    if (destination == NULL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&destination->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }

    rt_far_task_handle route = {.task_id = 0,
                                .generation = 0,
                                .owner_shard_id = resolved.shard_id,
                                .kind = RT_FAR_HANDLE_KIND_CHANNEL};
    rt_remote_task_pending* request =
        rt_remote_task_pending_new(ex, &route, source_shard, RT_REMOTE_TASK_OP_CHANNEL_CREATE, 1);
    if (request == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    request->handle.generation = request->request_id;
    request->caller_task_id = current->id;
    request->body_poll_fn_id = capacity;
    request->payload_drop_fn_id = payload_drop_fn_id;
    *pending = request;
    (void)rt_remote_task_prepare_reply_wait(ex, current, request);
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = resolved.shard_id,
        .route_id = request->request_id,
        .generation = request->handle.generation,
        .payload = request,
    };
    rt_remote_task_status status =
        rt_remote_task_transport_status(rt_transport_enqueue(destination, &msg));
    if (status == RT_REMOTE_TASK_STATUS_OK) {
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    rt_remote_task_clear_reply_wait(ex, current, request);
    rt_remote_task_pending_consume(request);
    rt_remote_task_pending_release(request);
    *pending = NULL;
    return status;
}

static int create_request_matches(const rt_transport_msg* msg,
                                  const rt_remote_task_pending* pending) {
    return msg != NULL && pending != NULL && msg->route_id == pending->request_id &&
           msg->generation == pending->handle.generation &&
           msg->source_shard_id == pending->source_shard_id &&
           msg->target_shard_id == pending->handle.owner_shard_id;
}

void rt_far_channel_dispatch_create(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    rt_remote_task_state* tokens = rt_remote_task_state_get(ex);
    if (tokens == NULL || pending == NULL) {
        rt_remote_task_pending_release(pending);
        return;
    }
    if (!create_request_matches(msg, pending)) {
        rt_transport_record_remote_task_stale(
            rt_runtime_shard(rt_executor_runtime(ex), msg->target_shard_id));
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_STALE_TOKEN,
                                       2,
                                       0,
                                       RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY);
        return;
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) != RT_REMOTE_TASK_STATUS_PENDING) {
        rt_remote_task_pending_release(pending);
        return;
    }
    // The payload type arrived as a number, which is the only form that
    // survives the boundary; the descriptor is looked up on this side.
    const rt_value_ops* ops = rt_channel_element_ops_for(pending->payload_drop_fn_id);
    void* channel = rt_channel_new(pending->body_poll_fn_id, ops, pending->payload_drop_fn_id);
    if (channel != NULL) {
        rt_channel_bind_owner_shard(channel, msg->target_shard_id);
    }
    if (channel == NULL) {
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_REFUSED,
                                       2,
                                       0,
                                       RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY);
        return;
    }
    rt_far_task_handle minted = {0};
    if (rt_far_channel_mint(ex, channel, msg->target_shard_id, &minted) !=
        RT_REMOTE_TASK_STATUS_OK) {
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_REFUSED,
                                       2,
                                       0,
                                       RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY);
        return;
    }
    // Bind the minted token into the shared pending under the token lock so
    // the caller-side copy after the terminal snapshot reads a full token.
    pthread_mutex_lock(&tokens->lock);
    pending->handle = minted;
    pthread_mutex_unlock(&tokens->lock);
    rt_remote_task_reply_or_finish(ex,
                                   pending,
                                   RT_REMOTE_TASK_STATUS_OK,
                                   1,
                                   minted.task_id,
                                   RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY);
}

// Caller-side share: the execute/reply discipline of the create path with
// the anchor's owner shard as the destination. The reply carries the
// sibling token through the shared pending exactly like a fresh mint.
rt_remote_task_status rt_far_channel_share(const rt_far_task_handle* source,
                                           rt_remote_task_pending** pending,
                                           rt_far_task_handle* out_handle,
                                           uint8_t* out_kind,
                                           uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (ex == NULL || pending == NULL || out_handle == NULL || current == NULL ||
        rt_current_task_id() == 0) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        rt_remote_task_status status =
            rt_remote_task_pending_snapshot(*pending, out_kind, out_bits);
        if (status == RT_REMOTE_TASK_STATUS_PENDING) {
            if (rt_remote_task_prepare_reply_wait(ex, current, *pending) == 0) {
                return RT_REMOTE_TASK_STATUS_PENDING;
            }
            status = rt_remote_task_pending_snapshot(*pending, out_kind, out_bits);
        }
        if (status == RT_REMOTE_TASK_STATUS_OK) {
            *out_handle = (*pending)->handle;
        }
        rt_remote_task_pending_consume(*pending);
        *pending = NULL;
        return status;
    }
    if (source == NULL || source->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* destination = rt_runtime_shard(runtime, source->owner_shard_id);
    if (destination == NULL) {
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&destination->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }
    uint32_t source_shard = current->owner_shard_valid != 0 ? current->owner_shard_id : 0;
    rt_far_task_handle route = {.task_id = 0,
                                .generation = 0,
                                .owner_shard_id = source->owner_shard_id,
                                .kind = RT_FAR_HANDLE_KIND_CHANNEL};
    rt_remote_task_pending* request =
        rt_remote_task_pending_new(ex, &route, source_shard, RT_REMOTE_TASK_OP_CHANNEL_SHARE, 1);
    if (request == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    request->handle.generation = request->request_id;
    request->caller_task_id = current->id;
    // The source token rides the pending's anchor slot: the dispatch side
    // validates the lease it names before minting anything.
    request->anchor = *source;
    *pending = request;
    (void)rt_remote_task_prepare_reply_wait(ex, current, request);
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = source->owner_shard_id,
        .route_id = request->request_id,
        .generation = request->handle.generation,
        .payload = request,
    };
    rt_remote_task_status status =
        rt_remote_task_transport_status(rt_transport_enqueue(destination, &msg));
    if (status == RT_REMOTE_TASK_STATUS_OK) {
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    rt_remote_task_clear_reply_wait(ex, current, request);
    rt_remote_task_pending_consume(request);
    rt_remote_task_pending_release(request);
    *pending = NULL;
    return status;
}

void rt_far_channel_dispatch_share(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    rt_remote_task_state* tokens = rt_remote_task_state_get(ex);
    if (tokens == NULL || pending == NULL) {
        rt_remote_task_pending_release(pending);
        return;
    }
    if (!create_request_matches(msg, pending)) {
        rt_transport_record_remote_task_stale(
            rt_runtime_shard(rt_executor_runtime(ex), msg->target_shard_id));
        rt_remote_task_reply_or_finish(ex,
                                       pending,
                                       RT_REMOTE_TASK_STATUS_STALE_TOKEN,
                                       2,
                                       0,
                                       RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY);
        return;
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) != RT_REMOTE_TASK_STATUS_PENDING) {
        rt_remote_task_pending_release(pending);
        return;
    }
    rt_far_task_handle sibling = {0};
    rt_remote_task_status minted = rt_far_channel_mint_sibling(ex, &pending->anchor, &sibling);
    if (minted != RT_REMOTE_TASK_STATUS_OK) {
        rt_remote_task_reply_or_finish(
            ex, pending, minted, 2, 0, RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY);
        return;
    }
    pthread_mutex_lock(&tokens->lock);
    pending->handle = sibling;
    pthread_mutex_unlock(&tokens->lock);
    rt_remote_task_reply_or_finish(ex,
                                   pending,
                                   RT_REMOTE_TASK_STATUS_OK,
                                   1,
                                   sibling.task_id,
                                   RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY);
}
