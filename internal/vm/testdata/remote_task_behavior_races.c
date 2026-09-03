#include "remote_task_behavior.h"

#include <pthread.h>
#include <string.h>

static int wait_pending(rt_executor* ex,
                        rt_remote_task_pending* pending,
                        rt_remote_task_status* status,
                        uint8_t* kind,
                        uint64_t* bits) {
    // A reply carries no word any more (Wave E): the kind is the answer, and a
    // value travels through the producer's typed slot.
    if (bits != NULL) {
        *bits = 0;
    }
    for (uint32_t i = 0; i < 5000; i++) {
        *status = rt_remote_task_pending_snapshot(pending, kind);
        if (*status != RT_REMOTE_TASK_STATUS_PENDING) {
            return 1;
        }
        size_t count = rt_runtime_shard_count(rt_executor_runtime(ex));
        for (size_t shard = 0; shard < count; shard++) {
            rtb_drain(ex, (uint32_t)shard);
        }
        rtb_sleep_us(1000);
    }
    return 0;
}

int rtb_mode_already_done(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    atomic_store_explicit(&child.gate, 1, memory_order_relaxed);
    rt_far_task_handle* handle = rtb_publish_handle(&child, 1);
    if (handle == NULL)
        return rtb_fail("already-DONE publication failed");
    if (!rtb_wait_task_done(ex, handle->task_id, 5000)) {
        return rtb_fail("remote child did not reach DONE before await");
    }
    rtb_lifecycle_state state;
    void* task = rtb_start_lifecycle(&state, handle, 0);
    uint8_t kind = 0;
    uint64_t bits = 0;
    if (!rtb_await(task, &kind, &bits))
        return rtb_fail("already-DONE caller failed");
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 91) {
        return rtb_fail("already-DONE await result mismatch");
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), 0);
    if (rt_transport_debug_snapshot(source).remote_task_completion_replies != 1) {
        return rtb_fail("already-DONE completion counter mismatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int rtb_mode_stale(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rt_far_task_handle* handle = rtb_publish_handle(&child, 1);
    if (handle == NULL)
        return rtb_fail("stale publication failed");
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), 0);
    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), 1);
    uint64_t source_stale_before = rt_transport_debug_snapshot(source).remote_task_stale_drops;
    uint64_t owner_stale_before = rt_transport_debug_snapshot(owner).remote_task_stale_drops;

    rt_remote_task_pending* bad =
        rt_remote_task_pending_new(ex, handle, 0, RT_REMOTE_TASK_OP_AWAIT, 1);
    rt_remote_task_pending_add_ref(bad);
    rt_transport_msg bad_request = {
        .kind = RT_TRANSPORT_MSG_REMOTE_TASK_AWAIT_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = handle->owner_shard_id,
        .route_id = handle->task_id,
        .generation = handle->generation + 1,
        .payload = bad,
    };
    (void)rt_remote_task_dispatch_message(ex, &bad_request);
    if (rt_remote_task_pending_snapshot(bad, NULL) != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("stale request was not rejected");
    }
    rt_remote_task_pending_consume(bad);

    rtb_lifecycle_state state;
    void* task = rtb_start_lifecycle(&state, handle, 0);
    rt_remote_task_pending* pending = NULL;
    for (uint32_t i = 0; i < 5000 && pending == NULL; i++) {
        pending = atomic_load_explicit(&state.visible_pending, memory_order_acquire);
        rtb_sleep_us(1000);
    }
    if (pending == NULL)
        return rtb_fail("valid await did not become pending");
    rt_remote_task_pending_add_ref(pending);
    rt_transport_msg bad_reply = {
        .kind = RT_TRANSPORT_MSG_REMOTE_TASK_COMPLETION,
        .source_shard_id = pending->handle.owner_shard_id,
        .target_shard_id = pending->source_shard_id,
        .route_id = pending->request_id,
        .generation = pending->handle.generation + 1,
        .payload = pending,
    };
    (void)rt_remote_task_dispatch_message(ex, &bad_reply);
    if (rt_remote_task_pending_snapshot(pending, NULL) != RT_REMOTE_TASK_STATUS_PENDING) {
        return rtb_fail("stale reply completed the valid pending request");
    }
    uint64_t source_stale_after = rt_transport_debug_snapshot(source).remote_task_stale_drops;
    uint64_t owner_stale_after = rt_transport_debug_snapshot(owner).remote_task_stale_drops;
    if (source_stale_after != source_stale_before + 1 ||
        owner_stale_after != owner_stale_before + 1) {
        return rtb_fail("stale drop counters did not attribute request and reply targets");
    }

    atomic_store_explicit(&child.gate, 1, memory_order_release);
    rtb_wake(ex, pending->handle.task_id);
    uint8_t kind = 0;
    uint64_t bits = 0;
    if (!rtb_await(task, &kind, &bits))
        return rtb_fail("valid await caller failed");
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 91) {
        return rtb_fail("valid reply after stale reply did not succeed");
    }
    if (rt_transport_debug_snapshot(source).remote_task_completion_replies != 1) {
        return rtb_fail("valid completion reply counter mismatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

typedef struct race_dispatch {
    rt_executor* executor;
    rt_transport_msg message;
} race_dispatch;

static void* dispatch_race(void* raw) {
    race_dispatch* race = (race_dispatch*)raw;
    (void)rt_remote_task_dispatch_message(race->executor, &race->message);
    return NULL;
}

int rtb_mode_registration_race(rt_sync_point_id point) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rt_far_task_handle* handle = rtb_publish_handle(&child, 0);
    if (handle == NULL)
        return rtb_fail("race publication failed");
    if (rt_far_task_lease_consume(handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("race lease consume failed");
    }
    rt_remote_task_pending* pending =
        rt_remote_task_pending_new(ex, handle, 0, RT_REMOTE_TASK_OP_CANCEL, 1);
    rt_remote_task_pending_add_ref(pending);
    race_dispatch race = {
        .executor = ex,
        .message = {.kind = RT_TRANSPORT_MSG_REMOTE_TASK_CANCEL_REQUEST,
                    .source_shard_id = 0,
                    .target_shard_id = 0,
                    .route_id = handle->task_id,
                    .generation = handle->generation,
                    .payload = pending},
    };
    pthread_t thread;
    if (pthread_create(&thread, NULL, dispatch_race, &race) != 0) {
        return rtb_fail("race dispatcher start failed");
    }
    for (uint32_t i = 0; i < 5000 && rt_sync_point_reached_count(point) == 0; i++) {
        rtb_sleep_us(1000);
    }
    if (rt_sync_point_reached_count(point) != 1)
        return rtb_fail("race window not reached");
    atomic_store_explicit(&child.gate, 1, memory_order_release);
    rtb_wake(ex, handle->task_id);
    if (!rtb_wait_task_done(ex, handle->task_id, 5000)) {
        return rtb_fail("child did not complete inside registration window");
    }
    rt_sync_point_open();
    (void)pthread_join(thread, NULL);
    rt_remote_task_status status = RT_REMOTE_TASK_STATUS_PENDING;
    uint8_t kind = 0;
    uint64_t bits = 0;
    if (!wait_pending(ex, pending, &status, &kind, &bits)) {
        return rtb_fail("registration race stranded pending reply");
    }
    if (status != RT_REMOTE_TASK_STATUS_OK || kind != 1) {
        return rtb_fail("registration race result mismatch");
    }
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
    if (snapshot.remote_task_cancel_replies != 1 || snapshot.remote_task_stale_drops != 0) {
        return rtb_fail("registration race consumed more than one reply edge");
    }
    rt_remote_task_pending_consume(pending);
    rt_far_task_handle_free(handle);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
