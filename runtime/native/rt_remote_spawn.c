#include "rt_remote_spawn.h"
#include "rt_async_internal.h"

#include <limits.h>
#include <stdatomic.h>

struct rt_remote_spawn_pending {
    uint64_t request_id;
    uint64_t poll_fn_id;
    void* state;
    uint64_t caller_task_id;
    uint32_t source_shard_id;
    uint32_t target_shard_id;
    rt_remote_spawn_status status;
    rt_far_task_handle handle;
    _Atomic uint32_t refs;
    uint8_t listed;
    struct rt_remote_spawn_pending* next;
};

static pthread_mutex_t remote_spawn_lock = PTHREAD_MUTEX_INITIALIZER;
static _Atomic uint64_t remote_spawn_next_request_id = 1;
static rt_remote_spawn_pending* remote_spawn_pending_head;

static void remote_spawn_pending_add_ref(rt_remote_spawn_pending* pending) {
    if (pending != NULL) {
        (void)atomic_fetch_add_explicit(&pending->refs, 1, memory_order_relaxed);
    }
}

static void remote_spawn_pending_release(rt_remote_spawn_pending* pending) {
    if (pending == NULL) {
        return;
    }
    uint32_t refs = atomic_fetch_sub_explicit(&pending->refs, 1, memory_order_acq_rel);
    if (refs == 1) {
        rt_free((uint8_t*)pending, sizeof(*pending), _Alignof(rt_remote_spawn_pending));
    }
}

static void remote_spawn_pending_link_locked(rt_remote_spawn_pending* pending) {
    pending->next = remote_spawn_pending_head;
    pending->listed = 1;
    remote_spawn_pending_head = pending;
}

static void remote_spawn_pending_unlink_locked(rt_remote_spawn_pending* pending) {
    rt_remote_spawn_pending** cursor = &remote_spawn_pending_head;
    while (*cursor != NULL) {
        if (*cursor == pending) {
            *cursor = pending->next;
            pending->next = NULL;
            pending->listed = 0;
            return;
        }
        cursor = &(*cursor)->next;
    }
}

static void remote_spawn_pending_finish(rt_executor* ex,
                                        rt_remote_spawn_pending* pending,
                                        rt_remote_spawn_status status,
                                        const rt_far_task_handle* handle) {
    int should_wake = 0;
    pthread_mutex_lock(&remote_spawn_lock);
    if (pending != NULL && pending->status == RT_REMOTE_SPAWN_STATUS_PENDING) {
        pending->status = status;
        if (handle != NULL) {
            pending->handle = *handle;
        }
        should_wake = 1;
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    if (should_wake) {
        wake_key_all_with_policy(ex, remote_spawn_reply_key(pending->request_id), 0);
    }
}

static rt_remote_spawn_status remote_spawn_pending_snapshot(const rt_remote_spawn_pending* pending,
                                                            rt_far_task_handle* out) {
    rt_remote_spawn_status status = RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    pthread_mutex_lock(&remote_spawn_lock);
    if (pending != NULL) {
        status = pending->status;
        if (out != NULL) {
            *out = pending->handle;
        }
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    return status;
}

static void remote_spawn_pending_consume(rt_remote_spawn_pending* pending) {
    pthread_mutex_lock(&remote_spawn_lock);
    if (pending != NULL && pending->listed != 0) {
        remote_spawn_pending_unlink_locked(pending);
    }
    pthread_mutex_unlock(&remote_spawn_lock);
    remote_spawn_pending_release(pending);
}

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

static void
remote_spawn_prepare_reply_wait(rt_executor* ex, rt_task* current, uint64_t request_id) {
    waker_key key = remote_spawn_reply_key(request_id);
    (void)rt_transport_reply_wait_before_task_suspend();
    prepare_park(ex, current, key, 0);
    pending_key = key;
}

rt_remote_spawn_status rt_remote_spawn_publish(uint32_t dst_shard_id,
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
        remote_spawn_prepare_reply_wait(ex, current, (*pending)->request_id);
        return RT_REMOTE_SPAWN_STATUS_PENDING;
    }

    rt_shard* dst = rt_runtime_shard(runtime, dst_shard_id);
    if (dst == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&dst->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        return RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN;
    }

    rt_remote_spawn_pending* req =
        (rt_remote_spawn_pending*)rt_alloc(sizeof(*req), _Alignof(rt_remote_spawn_pending));
    if (req == NULL) {
        return RT_REMOTE_SPAWN_STATUS_REFUSED;
    }
    memset(req, 0, sizeof(*req));
    req->request_id =
        atomic_fetch_add_explicit(&remote_spawn_next_request_id, 1, memory_order_relaxed);
    req->poll_fn_id = (uint64_t)poll_fn_id;
    req->state = state;
    req->caller_task_id = current->id;
    req->source_shard_id = remote_spawn_current_source_shard(current);
    req->target_shard_id = dst_shard_id;
    req->status = RT_REMOTE_SPAWN_STATUS_PENDING;
    atomic_store_explicit(&req->refs, 1, memory_order_relaxed);

    pthread_mutex_lock(&remote_spawn_lock);
    remote_spawn_pending_link_locked(req);
    pthread_mutex_unlock(&remote_spawn_lock);

    remote_spawn_pending_add_ref(req);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST,
        .source_shard_id = req->source_shard_id,
        .target_shard_id = req->target_shard_id,
        .route_id = req->request_id,
        .generation = 0,
        .payload = req,
        .payload_len = 0,
    };
    rt_remote_spawn_status status = remote_spawn_transport_status(rt_transport_enqueue(dst, &msg));
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        remote_spawn_pending_consume(req);
        remote_spawn_pending_release(req);
        return status;
    }

    *pending = req;
    remote_spawn_prepare_reply_wait(ex, current, req->request_id);
    return RT_REMOTE_SPAWN_STATUS_PENDING;
}

rt_remote_spawn_status rt_remote_spawn_publish_placement(rt_placement placement,
                                                         int64_t poll_fn_id,
                                                         void* state,
                                                         rt_remote_spawn_pending** pending,
                                                         rt_far_task_handle* out_handle) {
    if (pending == NULL || out_handle == NULL) {
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        return rt_remote_spawn_publish(0, poll_fn_id, state, pending, out_handle);
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
        return remote_spawn_placement_status(resolved.status);
    }
    return rt_remote_spawn_publish(resolved.shard_id, poll_fn_id, state, pending, out_handle);
}

static rt_remote_spawn_status
remote_spawn_create_destination_task(rt_executor* ex,
                                     const rt_remote_spawn_pending* req,
                                     rt_task** out_task,
                                     rt_far_task_handle* out_handle) {
    if (out_task == NULL || out_handle == NULL) {
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
    task->id = id;
    task->generation = id;
    task->poll_fn_id = (int64_t)req->poll_fn_id;
    task->state = req->state;
    task->kind = TASK_KIND_USER;
    task_status_store(task, TASK_READY);
    task_cancelled_store(task, 0);
    task_enqueued_store(task, 0);
    (void)task_wake_token_exchange(task, 0);
    atomic_store_explicit(&task->handle_refs, 1, memory_order_relaxed);
    rt_task_set_placement(task, req->target_shard_id, TASK_PLACEMENT_CONNECTION);

    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), req->target_shard_id);
    if (owner == NULL) {
        rt_free((uint8_t*)task, sizeof(*task), _Alignof(rt_task));
        return RT_REMOTE_SPAWN_STATUS_INVALID_ARGUMENT;
    }
    rt_shard_lock(owner);
    rt_task_slot_store(ex, id, task);
    rt_shard_unlock(owner);
    *out_handle = (rt_far_task_handle){.task_id = id,
                                       .generation = task->generation,
                                       .owner_shard_id = req->target_shard_id,
                                       ._pad = 0};
    *out_task = task;
    return RT_REMOTE_SPAWN_STATUS_OK;
}

static void remote_spawn_free_unpublished_task(rt_executor* ex, rt_task* task) {
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
    rt_remote_spawn_status status =
        remote_spawn_transport_status(rt_transport_enqueue(source, ack));
    if (status != RT_REMOTE_SPAWN_STATUS_QUEUE_FULL) {
        return status;
    }
    if (source == NULL) {
        return status;
    }
    rt_shard_lock(source);
    (void)rt_remote_spawn_drain_inbound_locked(ex, source, RT_TRANSPORT_DRAIN_TURN_LIMIT);
    rt_shard_unlock(source);
    return remote_spawn_transport_status(rt_transport_enqueue(source, ack));
}

static rt_remote_spawn_status remote_spawn_publish_destination_task(rt_executor* ex,
                                                                    rt_task* task) {
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
    if (remote_spawn_pending_snapshot(req, NULL) != RT_REMOTE_SPAWN_STATUS_PENDING) {
        remote_spawn_pending_release(req);
        return;
    }

    rt_far_task_handle handle = {0};
    rt_task* task = NULL;
    rt_remote_spawn_status status = remote_spawn_create_destination_task(ex, req, &task, &handle);
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        remote_spawn_pending_finish(ex, req, status, NULL);
        remote_spawn_pending_release(req);
        return;
    }

    pthread_mutex_lock(&remote_spawn_lock);
    req->handle = handle;
    pthread_mutex_unlock(&remote_spawn_lock);

    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), req->source_shard_id);
    rt_transport_msg ack = {
        .kind = RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK,
        .source_shard_id = req->target_shard_id,
        .target_shard_id = req->source_shard_id,
        .route_id = req->request_id,
        .generation = handle.generation,
        .payload = req,
        .payload_len = 0,
    };
    status = remote_spawn_enqueue_ack(ex, source, &ack);
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        remote_spawn_free_unpublished_task(ex, task);
        remote_spawn_pending_finish(ex, req, status, NULL);
        remote_spawn_pending_release(req);
        return;
    }
    status = remote_spawn_publish_destination_task(ex, task);
    if (status != RT_REMOTE_SPAWN_STATUS_OK) {
        remote_spawn_free_unpublished_task(ex, task);
        remote_spawn_pending_finish(ex, req, status, NULL);
    }
}

static void remote_spawn_dispatch_ack(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_spawn_pending* req = (rt_remote_spawn_pending*)msg->payload;
    if (req != NULL) {
        remote_spawn_pending_finish(ex, req, RT_REMOTE_SPAWN_STATUS_OK, &req->handle);
        remote_spawn_pending_release(req);
    }
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
        if (msg.kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST) {
            remote_spawn_dispatch_request(ex, &msg);
        } else if (msg.kind == RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK) {
            remote_spawn_dispatch_ack(ex, &msg);
        } else if (msg.kind != RT_TRANSPORT_MSG_NONE) {
            panic_msg("remote spawn: unsupported transport message kind");
        }
        rt_shard_lock(shard);
    }
    return drained;
}

void rt_remote_spawn_fail_all_pending(rt_executor* ex, rt_remote_spawn_status status) {
    pthread_mutex_lock(&remote_spawn_lock);
    for (rt_remote_spawn_pending* it = remote_spawn_pending_head; it != NULL; it = it->next) {
        if (it->status == RT_REMOTE_SPAWN_STATUS_PENDING) {
            it->status = status;
            wake_key_all_with_policy(ex, remote_spawn_reply_key(it->request_id), 0);
        }
    }
    pthread_mutex_unlock(&remote_spawn_lock);
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
            if (msg.kind != RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST &&
                msg.kind != RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK &&
                msg.kind != RT_TRANSPORT_MSG_NONE) {
                panic_msg("remote spawn: unsupported transport message kind during shutdown");
            }
            remote_spawn_release_msg_payload(&msg);
        }
        rt_shard_unlock(shard);
    }
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

uint64_t rt_remote_spawn_pending_request_id(const rt_remote_spawn_pending* pending) {
    return pending != NULL ? pending->request_id : 0;
}
