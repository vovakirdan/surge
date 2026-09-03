#include "rt_remote_spawn_internal.h"
#include "rt_remote_task.h"
#include "rt_sync_point.h"
#include "rt_value_cell.h"

#include <limits.h>
#include <stdatomic.h>

static rt_remote_spawn_status remote_spawn_transport_status(rt_transport_status status) {
    if (status == RT_TRANSPORT_STATUS_OK) {
        return RT_REMOTE_SPAWN_STATUS_OK;
    }
    if (status == RT_TRANSPORT_STATUS_QUEUE_FULL) {
        return RT_REMOTE_SPAWN_STATUS_QUEUE_FULL;
    }
    return RT_REMOTE_SPAWN_STATUS_REFUSED;
}

static rt_remote_spawn_status remote_spawn_placement_status(rt_placement_status status) {
    switch (status) {
        case RT_PLACEMENT_STATUS_OK:
            return RT_REMOTE_SPAWN_STATUS_OK;
        case RT_PLACEMENT_STATUS_UNSUPPORTED:
            return RT_REMOTE_SPAWN_STATUS_UNSUPPORTED_PLACEMENT;
        case RT_PLACEMENT_STATUS_INVALID_SHARD:
        case RT_PLACEMENT_STATUS_INVALID_ARGUMENT:
        default:
            return RT_REMOTE_SPAWN_STATUS_INVALID_PLACEMENT;
    }
}

static void remote_spawn_release_msg_payload(const rt_transport_msg* msg) {
    if (msg == NULL || msg->payload == NULL) {
        return;
    }
    if (msg->kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST ||
        msg->kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK) {
        remote_spawn_pending_release((rt_remote_spawn_pending*)msg->payload);
    }
}

static uint32_t remote_spawn_current_source_shard(const rt_task* current) {
    if (current != NULL && current->owner_shard_valid != 0) {
        return current->owner_shard_id;
    }
    uint32_t worker_shard = rt_debug_current_worker_shard_id();
    return worker_shard != UINT32_MAX ? worker_shard : 0;
}

static void remote_spawn_clear_reply_wait(rt_executor* ex,
                                          rt_task* current,
                                          uint64_t request_id,
                                          uint32_t source_shard_id);

static int remote_spawn_prepare_reply_wait(rt_executor* ex,
                                           rt_task* current,
                                           rt_remote_spawn_pending* pending) {
    uint64_t request_id = pending != NULL ? pending->request_id : 0;
    uint32_t source_shard_id = pending != NULL ? pending->source_shard_id : 0;
    waker_key key = remote_spawn_reply_key(request_id, source_shard_id);
    (void)rt_transport_reply_wait_before_task_suspend();
    prepare_park(ex, current, key, 0);
    pending_key = key;
    if (remote_spawn_pending_snapshot(pending, NULL) == RT_REMOTE_SPAWN_STATUS_PENDING) {
        return 0;
    }
    remote_spawn_clear_reply_wait(ex, current, request_id, source_shard_id);
    return 1;
}

static void remote_spawn_clear_reply_wait(rt_executor* ex,
                                          rt_task* current,
                                          uint64_t request_id,
                                          uint32_t source_shard_id) {
    if (ex == NULL || current == NULL) {
        return;
    }
    waker_key key = remote_spawn_reply_key(request_id, source_shard_id);
    remove_waiter(ex, key, current->id);
    if (current->park_key.kind == key.kind && current->park_key.id == key.id) {
        current->park_key = waker_none();
    }
    current->park_prepared = 0;
    if (pending_key.kind == key.kind && pending_key.id == key.id) {
        pending_key = waker_none();
    }
}

// A refusal before any pending exists still owns the moved state: the
// caller-side drop keeps the exactly-once obligation on synchronous
// failure edges (id 0 = nothing to drop, today's shape).
static void remote_spawn_drop_unshipped_state(uint64_t state_type_id, void* state) {
    if (state_type_id != 0 && state != NULL) {
        rt_value_release_owned_block(rt_channel_element_ops_for(state_type_id), state);
    }
}

rt_remote_spawn_status rt_remote_spawn_publish(uint32_t dst_shard_id,
                                               uint64_t state_type_id,
                                               uint64_t result_type_id,
                                               int64_t poll_fn_id,
                                               void* state,
                                               rt_remote_spawn_pending** pending,
                                               rt_far_task_handle* out_handle) {
    rt_executor* ex = ensure_exec();
    if (ex == NULL || pending == NULL || out_handle == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_task* current = rt_current_task();
    if (runtime == NULL || current == NULL || rt_current_task_id() == 0) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        rt_remote_spawn_status status = remote_spawn_pending_snapshot(*pending, out_handle);
        if (status != RT_REMOTE_SPAWN_STATUS_PENDING) {
            remote_spawn_pending_consume(*pending);
            *pending = NULL;
            return status;
        }
        if (atomic_load_explicit(&(*pending)->admission.parked, memory_order_acquire) != 0) {
            // Parked on the target's data lane: try again now that something
            // woke this task, and park again if the lane still refuses. A
            // hard refusal resolves the request here.
            rt_transport_status admitted = rt_remote_admit(ex, current, &(*pending)->admission);
            if (admitted == RT_TRANSPORT_STATUS_UNAVAILABLE) {
                return RT_REMOTE_SPAWN_STATUS_PENDING;
            }
            if (admitted != RT_TRANSPORT_STATUS_OK) {
                remote_spawn_pending_finish(
                    ex,
                    *pending,
                    rt_remote_spawn_admission_refusal(&(*pending)->admission, admitted),
                    NULL);
                remote_spawn_pending_release(*pending);
                status = remote_spawn_pending_snapshot(*pending, out_handle);
                remote_spawn_pending_consume(*pending);
                *pending = NULL;
                return status;
            }
        }
        if (remote_spawn_prepare_reply_wait(ex, current, *pending) != 0) {
            status = remote_spawn_pending_snapshot(*pending, out_handle);
            remote_spawn_pending_consume(*pending);
            *pending = NULL;
            return status;
        }
        return RT_REMOTE_SPAWN_STATUS_PENDING;
    }

    rt_shard* dst = rt_runtime_shard(runtime, dst_shard_id);
    if (dst == NULL) {
        remote_spawn_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&dst->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        remote_spawn_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN;
    }

    rt_remote_spawn_pending* req =
        (rt_remote_spawn_pending*)rt_alloc(sizeof(*req), _Alignof(rt_remote_spawn_pending));
    if (req == NULL) {
        remote_spawn_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_SPAWN_STATUS_REFUSED;
    }
    memset(req, 0, sizeof(*req));
    req->executor = ex;
    req->poll_fn_id = (uint64_t)poll_fn_id;
    req->state = state;
    req->state_type_id = state_type_id;
    req->state_owned = state_type_id != 0;
    req->result_type_id = result_type_id;
    req->caller_task_id = current->id;
    req->source_shard_id = remote_spawn_current_source_shard(current);
    req->target_shard_id = dst_shard_id;
    req->status = RT_REMOTE_SPAWN_STATUS_PENDING;
    req->out_handle = out_handle;
    atomic_store_explicit(&req->refs, 1, memory_order_relaxed);

    remote_spawn_pending_link(req);

    *pending = req;
    remote_spawn_pending_add_ref(req);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = req->source_shard_id,
        .target_shard_id = req->target_shard_id,
        .route_id = req->request_id,
        .generation = 0,
        .payload = req,
    };
    // The spawn's answer is a control ACK: the target's data lane is the one
    // resource, and its exhaustion parks the caller rather than refusing it.
    rt_remote_admission_init(&req->admission, &msg, 0);
    rt_transport_status admitted = rt_remote_admit(ex, current, &req->admission);
    if (admitted == RT_TRANSPORT_STATUS_OK) {
        (void)remote_spawn_prepare_reply_wait(ex, current, req);
        return RT_REMOTE_SPAWN_STATUS_PENDING;
    }
    if (admitted == RT_TRANSPORT_STATUS_UNAVAILABLE) {
        return RT_REMOTE_SPAWN_STATUS_PENDING;
    }
    rt_remote_spawn_status status = remote_spawn_transport_status(admitted);
    remote_spawn_clear_reply_wait(ex, current, req->request_id, req->source_shard_id);
    remote_spawn_pending_consume(req);
    remote_spawn_pending_release(req);
    *pending = NULL;
    return status;
}

rt_remote_spawn_status rt_remote_spawn_publish_placement(rt_placement placement,
                                                         uint64_t state_type_id,
                                                         uint64_t result_type_id,
                                                         int64_t poll_fn_id,
                                                         void* state,
                                                         rt_remote_spawn_pending** pending,
                                                         rt_far_task_handle* out_handle) {
    if (pending == NULL || out_handle == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        return rt_remote_spawn_publish(
            0, state_type_id, result_type_id, poll_fn_id, state, pending, out_handle);
    }

    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    const rt_task* current = rt_current_task();
    if (runtime == NULL || current == NULL || rt_current_task_id() == 0) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }

    rt_placement_resolution resolved =
        rt_placement_resolve(runtime, placement, remote_spawn_current_source_shard(current));
    if (resolved.status != RT_PLACEMENT_STATUS_OK) {
        remote_spawn_drop_unshipped_state(state_type_id, state);
        return remote_spawn_placement_status(resolved.status);
    }
    return rt_remote_spawn_publish(
        resolved.shard_id, state_type_id, result_type_id, poll_fn_id, state, pending, out_handle);
}

rt_remote_spawn_status rt_remote_spawn_create_body_task(rt_executor* ex,
                                                        uint64_t poll_fn_id,
                                                        void* state,
                                                        uint32_t target_shard_id,
                                                        uint64_t result_type_id,
                                                        rt_task** out_task) {
    if (out_task == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    *out_task = NULL;
    uint64_t id = atomic_fetch_add_explicit(&ex->next_id, 1, memory_order_relaxed);
    if (rt_task_table_segment_missing(ex, id)) {
        int need_control = !rt_lane_holds_control();
        if (need_control) {
            rt_control_lock(ex);
        }
        rt_trace_control_lock_site(RT_CTRL_SITE_CREATE);
        ensure_task_cap(ex, id);
        if (need_control) {
            rt_control_unlock(ex);
        }
    }
    rt_task* task = (rt_task*)rt_alloc(sizeof(rt_task), _Alignof(rt_task));
    if (task == NULL) {
        return RT_REMOTE_SPAWN_STATUS_REFUSED;
    }
    memset(task, 0, sizeof(*task));
    // The body's result lives in its own slot, and the type it holds arrived as
    // a number because that is the only form that survives a crossing. The
    // descriptor is resolved on this side, the way a far channel's element is.
    // Zero, or a type this process compiled no descriptor for, resolves to the
    // opaque machine word -- the same rule a far channel's element follows. A
    // body that produces no value publishes none, so an empty slot describing a
    // word costs nothing and keeps one shape for every body.
    const rt_value_ops* result_ops = rt_channel_element_ops_for(result_type_id);
    if (rt_value_cell_bind(&task->result, result_ops) != RT_SLOT_CONTROL_OK) {
        rt_free((uint8_t*)task, sizeof(*task), _Alignof(rt_task));
        return RT_REMOTE_SPAWN_STATUS_REFUSED;
    }
    task->id = id;
    task->generation = id;
    task->poll_fn_id = (int64_t)poll_fn_id;
    task->state = state;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_READY);
    task_cancel_gate_init(task);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->remote_handle_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_state, 0, memory_order_relaxed);
    atomic_store_explicit(&task->far_task_result_lease, NULL, memory_order_relaxed);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_entitlements_init(&task->entitlements);
    rt_task_set_placement(task, target_shard_id, TASK_PLACEMENT_CONNECTION);

    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), target_shard_id);
    if (owner == NULL) {
        rt_free((uint8_t*)task, sizeof(*task), _Alignof(rt_task));
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(owner);
    rt_task_slot_store(ex, id, task);
    rt_shard_unlock(owner);
    *out_task = task;
    return RT_REMOTE_SPAWN_STATUS_OK;
}

void rt_remote_spawn_free_unpublished_task(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return;
    }
    int need_control = !rt_lane_holds_control();
    if (need_control) {
        rt_control_lock(ex);
    }
    rt_task_slot_store(ex, task->id, NULL);
    if (need_control) {
        rt_control_unlock(ex);
    }
    rt_free((uint8_t*)task, sizeof(*task), _Alignof(rt_task));
}

static rt_remote_spawn_status
remote_spawn_enqueue_ack(rt_executor* ex, rt_shard* source, const rt_transport_msg* ack) {
    return remote_spawn_transport_status(rt_remote_spawn_enqueue_with_drain(ex, source, ack));
}

rt_remote_spawn_status rt_remote_spawn_publish_body_task(rt_executor* ex, rt_task* task) {
    if (ex == NULL || task == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_shard* owner = rt_task_owner_shard(ex, task);
    if (owner == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(owner);
    int pushed = ready_push_task_locked(ex, owner, task, 1, 0, 1);
    rt_shard_unlock(owner);
    return pushed ? RT_REMOTE_SPAWN_STATUS_OK : RT_REMOTE_SPAWN_STATUS_REFUSED;
}

static void remote_spawn_dispatch_request(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_spawn_pending* req = (rt_remote_spawn_pending*)msg->payload;
    if (ex == NULL || req == NULL) {
        return;
    }
    RT_SYNC_POINT(SP_REMOTE_SPAWN_BEFORE_DISPATCH);
    if (remote_spawn_pending_snapshot(req, NULL) != RT_REMOTE_SPAWN_STATUS_PENDING) {
        remote_spawn_pending_release(req);
        return;
    }

    rt_far_task_handle handle = {0};
    rt_task* task = NULL;
    rt_remote_spawn_status status = rt_remote_spawn_create_body_task(
        ex, req->poll_fn_id, req->state, req->target_shard_id, req->result_type_id, &task);
    if (status == RT_REMOTE_SPAWN_STATUS_OK) {
        // Publication-accepted handoff. The body task owns its result the way
        // every task does now -- in its own slot, destroyed by its own dispose
        // if nobody fetches it -- so there is no reply-edge obligation left to
        // thread here (RV2-DEBT-053a).
        handle = (rt_far_task_handle){.task_id = task->id,
                                      .generation = task->generation,
                                      .owner_shard_id = req->target_shard_id,
                                      .kind = RT_FAR_HANDLE_KIND_TASK};
    }
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        remote_spawn_pending_finish(ex, req, status, NULL);
        remote_spawn_pending_release(req);
        return;
    }

    req->handle = handle;

    RT_SYNC_POINT(SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH);
    status = rt_remote_spawn_publish_body_task(ex, task);
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        rt_remote_spawn_free_unpublished_task(ex, task);
        remote_spawn_pending_finish(ex, req, status, NULL);
        remote_spawn_pending_release(req);
        return;
    }
    // PUBLICATION-ACCEPTED HANDOFF (contract: rt_remote_spawn_internal.h):
    // from here the body owns the shipped state; the pending's final
    // release must no longer drop it.
    req->state_owned = 0;

    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), req->source_shard_id);
    rt_transport_msg ack = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK,
        .source_shard_id = req->target_shard_id,
        .target_shard_id = req->source_shard_id,
        .route_id = req->request_id,
        .generation = handle.generation,
        .payload = req,
    };
    RT_SYNC_POINT(SP_REMOTE_SPAWN_BEFORE_ACK);
    status = remote_spawn_enqueue_ack(ex, source, &ack);
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        (void)rt_far_task_release(&handle);
        remote_spawn_pending_finish(ex, req, status, NULL);
        remote_spawn_pending_release(req);
    }
}

static void remote_spawn_dispatch_ack(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_spawn_pending* req = (rt_remote_spawn_pending*)msg->payload;
    if (req != NULL) {
        remote_spawn_pending_finish(ex, req, RT_REMOTE_SPAWN_STATUS_OK, &req->handle);
        remote_spawn_pending_release(req);
    }
}

rt_transport_status
rt_remote_spawn_enqueue_with_drain(rt_executor* ex, rt_shard* shard, const rt_transport_msg* msg) {
    rt_transport_status status = rt_transport_enqueue(shard, msg);
    if (status != RT_TRANSPORT_STATUS_QUEUE_FULL || shard == NULL ||
        rt_lane_holds_shard(shard->shard_id)) {
        return status;
    }
    rt_shard_lock(shard);
    (void)rt_remote_spawn_drain_inbound_locked(ex, shard, RT_TRANSPORT_DRAIN_TURN_LIMIT);
    rt_shard_unlock(shard);
    return rt_transport_enqueue(shard, msg);
}

size_t rt_remote_spawn_drain_inbound_locked(rt_executor* ex, rt_shard* shard, size_t limit) {
    if (ex == NULL || shard == NULL || !rt_lane_holds_shard(shard->shard_id)) {
        return 0;
    }
    size_t drained = 0;
    while (limit == 0 || drained < limit) {
        rt_transport_msg msg = {0};
        if (rt_transport_try_drain_one(shard, &msg) != RT_TRANSPORT_STATUS_OK) {
            break;
        }
        drained++;
        rt_shard_unlock(shard);
        // The pop freed a slot in the message's lane. A producer parked on an
        // exhausted DATA lane is woken here, with no shard lock held, before
        // the message is even dispatched: the slot is free now, and the
        // dispatch below may itself block on another lane.
        if (rt_transport_msg_is_data(msg.kind)) {
            rt_transport_wake_slot_waiters(ex, shard);
        }
        if (msg.kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST) {
            remote_spawn_dispatch_request(ex, &msg);
        } else if (msg.kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK) {
            remote_spawn_dispatch_ack(ex, &msg);
        } else if (rt_remote_task_dispatch_message(ex, &msg)) {
            // Handled by the remote task lifecycle dispatcher.
        } else if (msg.kind != RT_TRANSPORT_MSG_NONE) {
            panic_msg("remote spawn: unsupported transport message kind");
        }
        rt_shard_lock(shard);
    }
    return drained;
}

void rt_remote_spawn_fail_all_pending(rt_executor* ex, rt_remote_spawn_status status) {
    rt_remote_task_fail_all_pending(ex, RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN);
    remote_spawn_pending_fail_all(ex, status);
    rt_runtime* runtime = rt_executor_runtime(ex);
    size_t shard_count = rt_runtime_shard_count(runtime);
    for (size_t i = 0; i < shard_count; i++) {
        rt_shard* shard = rt_runtime_shard(runtime, i);
        if (shard == NULL) {
            continue;
        }
        rt_shard_lock(shard);
        for (;;) {
            rt_transport_msg msg = {0};
            if (rt_transport_try_drain_one(shard, &msg) != RT_TRANSPORT_STATUS_OK) {
                break;
            }
            // Every kind production can enqueue must RELEASE here, never
            // panic: a message parked between the last steady-state drain
            // and shutdown is valid traffic. The switch is the static
            // exhaustiveness guard — a new kind fails the -Wswitch build
            // until it takes a release stance.
            switch (msg.kind) {
                case RT_TRANSPORT_MSG_NONE:
                case RT_TRANSPORT_MSG_SHUTDOWN_WAKE:
                case RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST:
                case RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK:
                case RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST:
                case RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION:
                case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST:
                case RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_ACK:
                case RT_TRANSPORT_MSG_REMOTE_TASK_RELEASE_REQUEST:
                case RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST:
                case RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY:
                case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST:
                case RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REPLY:
                case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST:
                case RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REPLY:
                case RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST:
                case RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY:
                    break;
                default:
                    panic_msg("remote spawn: unknown transport message kind during shutdown");
            }
            remote_spawn_release_msg_payload(&msg);
            rt_remote_task_release_msg_payload(&msg);
        }
        rt_shard_unlock(shard);
    }
    // A producer parked on a lane wakes to find the executor shut down and
    // fails its request itself; nothing else would ever wake it now.
    rt_remote_admission_wake_all_parked(ex);
}

rt_remote_spawn_status rt_remote_spawn_handle_validate(rt_executor* ex,
                                                       const rt_far_task_handle* handle) {
    if (ex == NULL || handle == NULL || handle->task_id == 0 || handle->generation == 0) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    const rt_task* task = get_task(ex, handle->task_id);
    if (task == NULL || task->generation != handle->generation ||
        task->owner_shard_id != handle->owner_shard_id) {
        return RT_REMOTE_SPAWN_STATUS_STALE_TOKEN;
    }
    return RT_REMOTE_SPAWN_STATUS_OK;
}
