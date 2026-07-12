#include "rt_far_channel.h"
#include "rt_remote_task_internal.h"

// The anchored immediate execute: the destination is the anchor's owner
// shard rather than a resolved placement; everything else (pending, reply
// wait, cancel routing, owner-done reply) is the shared execute/reply
// discipline. The helpers referenced here live in rt_immediate_on.c and the
// shared internal header.

// Anchored variant: the destination is the anchor's owner shard, and the
// dispatch side validates the anchor against the channel registry before a
// body exists. The retry/cancel path is the placement variant's.
rt_remote_task_status rt_immediate_on_execute_anchored(const rt_far_task_handle* anchor,
                                                       int64_t poll_fn_id,
                                                       void* state,
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
            return rt_immediate_on_finish_retry(pending, out_kind, out_bits);
        }
        if (task_cancelled_load(current) != 0) {
            rt_immediate_on_cancel_inflight(ex, *pending);
        }
        if (rt_remote_task_prepare_reply_wait(ex, current, *pending) != 0) {
            return rt_immediate_on_finish_retry(pending, out_kind, out_bits);
        }
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    if (anchor == NULL || anchor->kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* destination = rt_runtime_shard(runtime, anchor->owner_shard_id);
    if (destination == NULL) {
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&destination->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }
    rt_far_task_handle route = {.task_id = 0,
                                .generation = 0,
                                .owner_shard_id = anchor->owner_shard_id,
                                .kind = RT_FAR_HANDLE_KIND_TASK};
    rt_remote_task_pending* request = rt_remote_task_pending_new(
        ex, &route, rt_immediate_on_source_shard(current), RT_REMOTE_TASK_OP_EXECUTE_ANCHORED, 1);
    if (request == NULL) {
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    request->handle.generation = request->request_id;
    request->caller_task_id = current->id;
    request->body_poll_fn_id = (uint64_t)poll_fn_id;
    request->body_state = state;
    request->anchor = *anchor;
    *pending = request;
    (void)rt_remote_task_prepare_reply_wait(ex, current, request);
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = anchor->owner_shard_id,
        .route_id = request->request_id,
        .generation = request->handle.generation,
        .payload = request,
        .payload_len = 0,
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

// Compiled anchored-body channel operations. The park protocol is the local
// channel machinery unchanged: a parked send/recv registers the local
// waiter and yields; the wake re-enters the body FROM THE TOP, which stays
// sound because the language restricts the first vertical to bodies whose
// anchored operation is the first statement (an empty prefix replays for
// free, and the re-entered operation consumes the handoff ack). Returning
// from a helper therefore means the operation completed.

static void anchored_binding_or_die(void** out_channel, void** out_state) {
    if (!rt_remote_task_anchored_binding_current(out_channel, out_state)) {
        panic_msg("anchored channel operation outside an anchored block body");
    }
}

void rt_anchored_channel_send(uint64_t value_bits) {
    rt_executor* ex = ensure_exec();
    void* channel = NULL;
    void* state = NULL;
    anchored_binding_or_die(&channel, &state);
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    if (!rt_channel_send(channel, value_bits)) {
        rt_async_yield(state);
    }
}

// Returns the local recv outcome: 1 delivers a value through out_bits, 2 is
// the closed outcome. The parked case never returns (yield re-enters).
uint8_t rt_anchored_channel_recv(uint64_t* out_bits) {
    rt_executor* ex = ensure_exec();
    void* channel = NULL;
    void* state = NULL;
    anchored_binding_or_die(&channel, &state);
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    uint8_t status = rt_channel_recv(channel, out_bits);
    if (status == 0) {
        rt_async_yield(state);
    }
    return status;
}

void rt_anchored_channel_close(void) {
    void* channel = NULL;
    anchored_binding_or_die(&channel, NULL);
    rt_channel_close(channel);
}
