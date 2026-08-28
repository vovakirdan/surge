#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"
#include "rt_value_cell.h"
// Immediate `on placement` execute/reply: one request, one reply, one
// request-scoped cancellation token, no far Task handle exposed. The
// pending handle starts as {task_id=0, generation=request_id, owner_shard};
// the destination fills
// task_id/generation from the created body task before registering the reply
// route, so the owner-done completion hook and the reply envelope share the
// same token discipline as far-task await.

// One reply edge, stated once: every dispatch-side failure answers the
// caller with a status through the shared pending and never twice.
static void immediate_on_answer(rt_executor* ex,
                                rt_remote_task_pending* pending,
                                rt_remote_task_status status) {
    rt_remote_task_reply_or_finish(ex, pending, status, 2, 0, RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY);
}

uint32_t rt_immediate_on_source_shard(const rt_task* current) {
    if (current != NULL && current->owner_shard_valid != 0) {
        return current->owner_shard_id;
    }
    uint32_t worker_shard = rt_debug_current_worker_shard_id();
    return worker_shard != UINT32_MAX ? worker_shard : 0;
}

rt_remote_task_status
rt_immediate_on_finish_retry(rt_remote_task_pending** slot, uint8_t* out_kind, void* out_dst) {
    rt_remote_task_status status = rt_remote_task_pending_snapshot(*slot, out_kind, NULL);
    if (status != RT_REMOTE_TASK_STATUS_PENDING) {
        // Terminal call only: out_dst is live storage on this poll and nothing
        // may keep its address across one. See finish_retry in
        // rt_remote_task_api.c, which this mirrors.
        rt_result_source source = rt_remote_task_pending_result_source(*slot);
        if (source.task_id != 0) {
            if (!rt_remote_task_take_result_source(ensure_exec(), &source, out_dst) &&
                out_kind != NULL) {
                // The capability named a result that is no longer there. The
                // reply says Success, but the caller's storage holds nothing,
                // and telling it Success would hand it uninitialized bytes to
                // read as a value. "Nothing here" is the only honest answer.
                *out_kind = 2;
            }
            rt_remote_task_pending_clear_result_source(*slot);
        }
        rt_remote_task_pending_consume(*slot);
        *slot = NULL;
    }
    return status;
}

// Caller-cancel routing: a cancelled caller polling an in-flight execute
// routes exactly one token-validated cancel request to the destination. The
// execute reply edge stays the single resume path; the body observes
// cancellation and completes as Cancelled (or completes first — both legal).
void rt_immediate_on_cancel_inflight(rt_executor* ex, rt_remote_task_pending* pending) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || pending == NULL) {
        return;
    }
    rt_far_task_handle handle;
    pthread_mutex_lock(&state->lock);
    int route = pending->cancel_routed == 0 && pending->status == RT_REMOTE_TASK_STATUS_PENDING &&
                pending->handle.task_id != 0;
    if (route) {
        pending->cancel_routed = 1;
    }
    handle = pending->handle;
    pthread_mutex_unlock(&state->lock);
    if (!route) {
        return;
    }
    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), handle.owner_shard_id);
    if (owner == NULL) {
        return;
    }
    rt_remote_task_pending_add_ref(pending);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST,
        .source_shard_id = pending->source_shard_id,
        .target_shard_id = handle.owner_shard_id,
        .route_id = handle.task_id,
        .generation = handle.generation,
        .payload = pending,
    };
    if (rt_remote_task_transport_status(rt_remote_spawn_enqueue_with_drain(ex, owner, &msg)) !=
        RT_REMOTE_TASK_STATUS_OK) {
        pthread_mutex_lock(&state->lock);
        pending->cancel_routed = 0;
        pthread_mutex_unlock(&state->lock);
        rt_remote_task_pending_release(pending);
    }
}

// A refusal before any pending exists still owns the moved state (id 0 =
// nothing to drop, today's shape).
static void immediate_on_drop_unshipped_state(uint64_t state_type_id, void* state) {
    if (state_type_id != 0 && state != NULL) {
        rt_value_release_owned_block(rt_channel_element_ops_for(state_type_id), state);
    }
}

rt_remote_task_status rt_immediate_on_execute(uint64_t placement,
                                              uint64_t state_type_id,
                                              uint64_t result_type_id,
                                              int64_t poll_fn_id,
                                              void* state,
                                              rt_remote_task_pending** pending,
                                              uint8_t* out_kind,
                                              void* out_dst) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (ex == NULL || pending == NULL || current == NULL || rt_current_task_id() == 0) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        rt_remote_task_status status = rt_remote_task_pending_snapshot(*pending, out_kind, NULL);
        if (status != RT_REMOTE_TASK_STATUS_PENDING) {
            return rt_immediate_on_finish_retry(pending, out_kind, out_dst);
        }
        if (task_cancelled_load(current) != 0) {
            rt_immediate_on_cancel_inflight(ex, *pending);
        }
        if (rt_remote_task_prepare_reply_wait(ex, current, *pending) != 0) {
            return rt_immediate_on_finish_retry(pending, out_kind, out_dst);
        }
        return RT_REMOTE_TASK_STATUS_PENDING;
    }

    rt_runtime* runtime = rt_executor_runtime(ex);
    uint32_t source_shard = rt_immediate_on_source_shard(current);
    rt_placement_resolution resolved = rt_placement_resolve(runtime, placement, source_shard);
    if (resolved.status == RT_PLACEMENT_STATUS_UNSUPPORTED) {
        immediate_on_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_UNSUPPORTED_PLACEMENT;
    }
    if (resolved.status != RT_PLACEMENT_STATUS_OK) {
        immediate_on_drop_unshipped_state(state_type_id, state);
        // Out-of-range shard(id): deterministic non-executing placement error
        // path — the immediate crossing resumes as Cancelled, the body does
        // not run, and the resolver's invalid-placement counter records it.
        if (out_kind != NULL) {
            *out_kind = 2;
        }

        return RT_REMOTE_TASK_STATUS_OK;
    }
    rt_shard* destination = rt_runtime_shard(runtime, resolved.shard_id);
    if (destination == NULL) {
        immediate_on_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&destination->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        immediate_on_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }

    rt_far_task_handle route = {.task_id = 0,
                                .generation = 0,
                                .owner_shard_id = resolved.shard_id,
                                .kind = RT_FAR_HANDLE_KIND_TASK};
    rt_remote_task_pending* request =
        rt_remote_task_pending_new(ex, &route, source_shard, RT_REMOTE_TASK_OP_EXECUTE, 1);
    if (request == NULL) {
        immediate_on_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    // Request-scoped token until the destination binds the body task.
    request->handle.generation = request->request_id;
    request->caller_task_id = current->id;
    request->body_poll_fn_id = (uint64_t)poll_fn_id;
    request->body_state = state;
    request->state_type_id = state_type_id;
    // The body's result type, as a number: the descriptor is resolved on the
    // destination shard, which is the only side that can name it.
    request->result_type_id = result_type_id;
    request->state_owned = state_type_id != 0;
    *pending = request;
    (void)rt_remote_task_prepare_reply_wait(ex, current, request);
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
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

static int immediate_on_request_matches(const rt_transport_msg* msg,
                                        const rt_remote_task_pending* pending) {
    return msg != NULL && pending != NULL && msg->route_id == pending->request_id &&
           msg->generation == pending->handle.generation &&
           msg->source_shard_id == pending->source_shard_id &&
           msg->target_shard_id == pending->handle.owner_shard_id;
}

void rt_immediate_on_dispatch_execute(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || pending == NULL) {
        rt_remote_task_pending_release(pending);
        return;
    }
    RT_SYNC_POINT(SP_IMMEDIATE_ON_BEFORE_DISPATCH);
    if (!immediate_on_request_matches(msg, pending)) {
        rt_runtime* runtime = rt_executor_runtime(ex);
        rt_transport_record_remote_task_stale(
            msg != NULL ? rt_runtime_shard(runtime, msg->target_shard_id) : NULL);
        immediate_on_answer(ex, pending, RT_REMOTE_TASK_STATUS_STALE_TOKEN);
        return;
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) != RT_REMOTE_TASK_STATUS_PENDING) {
        rt_remote_task_pending_release(pending);
        return;
    }
    if (pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
        // Validate and pin the anchor before any body exists: a stale anchor
        // answers without running anything, and a live one cannot be
        // reclaimed under the block's feet until the reply edge resolves.
        if (!rt_far_channel_pin(ex, &pending->anchor, &pending->anchored_channel)) {
            immediate_on_answer(ex, pending, RT_REMOTE_TASK_STATUS_STALE_TOKEN);
            return;
        }
    }
    rt_task* task = NULL;
    rt_remote_spawn_status created = rt_remote_spawn_create_body_task(ex,
                                                                      pending->body_poll_fn_id,
                                                                      pending->body_state,
                                                                      msg->target_shard_id,
                                                                      pending->result_type_id,
                                                                      &task);
    if (created != RT_REMOTE_SPAWN_STATUS_OK) {
        if (pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
            rt_far_channel_unpin(ex, &pending->anchor);
        }
        immediate_on_answer(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED);
        return;
    }
    // Bind the body task into the token and register the reply route before
    // the body can run; the owner-done hook then answers exactly once. The
    // status re-check under the same lock closes the race against a caller
    // teardown that resolved the pending between the snapshot and the bind.
    pthread_mutex_lock(&state->lock);
    if (pending->status != RT_REMOTE_TASK_STATUS_PENDING) {
        pthread_mutex_unlock(&state->lock);
        if (pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
            rt_far_channel_unpin(ex, &pending->anchor);
        }
        rt_remote_spawn_free_unpublished_task(ex, task);
        rt_remote_task_pending_release(pending);
        return;
    }
    pending->handle.task_id = task->id;
    pending->handle.generation = task->generation;
    pthread_mutex_unlock(&state->lock);
    task_add_ref(task);
    rt_remote_task_pending_set_owner_registered(pending, 1);
    // Hold the pending across publication. The handoff contract
    // (rt_remote_spawn_internal.h) relies on the dispatch lane still owning
    // a reference when it clears state_owned: the plain store is ordered
    // before that reference's acq_rel drop, so whichever release ends up
    // final observes the cleared flag. The spawn family gets that for free
    // because its reference only leaves with the ACK, enqueued after the
    // store. Here the in-flight reference has already been handed to the
    // owner registration, and publishing makes the body runnable — another
    // thread can complete it, consume that registration, and free the
    // pending before the store below executes.
    rt_remote_task_pending_add_ref(pending);
    RT_SYNC_POINT(SP_IMMEDIATE_ON_BEFORE_PUBLISH);
    rt_remote_spawn_status published = rt_remote_spawn_publish_body_task(ex, task);
    if (published != RT_REMOTE_SPAWN_STATUS_OK) {
        if (pending->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED) {
            rt_far_channel_unpin(ex, &pending->anchor);
        }
        rt_remote_task_pending_set_owner_registered(pending, 0);
        task_release_lane_aware(ex, task);
        rt_remote_spawn_free_unpublished_task(ex, task);
        immediate_on_answer(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED);
        rt_remote_task_pending_release(pending);
        return;
    }
    RT_SYNC_POINT(SP_IMMEDIATE_ON_AFTER_PUBLISH);
    // PUBLICATION-ACCEPTED HANDOFF (contract: rt_remote_spawn_internal.h);
    // anchored bodies hand off here too — this dispatch is shared.
    pending->state_owned = 0;
    rt_remote_task_pending_release(pending);
    // Drop the creation reference: no far handle exists for an immediate
    // execute, so the owner registration (released by the owner-done reply)
    // is the only remaining task reference held for this request.
    task_release_lane_aware(ex, task);
    // The message reference is now owned by the owner registration and flows
    // into the reply envelope when the body completes.
}

// Caller teardown: a caller that unwinds (cancel/failfast) while an immediate
// execute or a remote select is in flight leaves the pending's caller-owned
// reference behind. Bound requests get a routed cancel so the destination
// body is not orphaned; unbound requests (still queued) are resolved so a
// late dispatch refuses to create a body (and, for select, never pins the
// arms). The reply edge still resolves exactly once.
void rt_immediate_on_release_owned(rt_executor* ex, const rt_task* caller) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || caller == NULL) {
        return;
    }
    for (;;) {
        rt_remote_task_pending* pending = NULL;
        int bound = 0;
        pthread_mutex_lock(&state->lock);
        for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
            if ((it->op == RT_REMOTE_TASK_OP_EXECUTE ||
                 it->op == RT_REMOTE_TASK_OP_EXECUTE_ANCHORED ||
                 it->op == RT_REMOTE_TASK_OP_CHANNEL_SELECT) &&
                it->caller_task_id == caller->id) {
                pending = it;
                bound = it->handle.task_id != 0;
                it->caller_task_id = 0;
                break;
            }
        }
        pthread_mutex_unlock(&state->lock);
        if (pending == NULL) {
            return;
        }
        if (bound) {
            // Keep the pending listed: the destination owner registration
            // still routes the orphaned reply through it exactly once.
            rt_immediate_on_cancel_inflight(ex, pending);
            rt_remote_task_pending_release(pending);
        } else {
            rt_remote_task_pending_finish(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED, 2, 0, NULL);
            rt_remote_task_pending_consume(pending);
        }
    }
}
