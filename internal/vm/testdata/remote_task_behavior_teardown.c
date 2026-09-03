#include "remote_task_behavior.h"

#include <string.h>

static int lease_registry_empty(rt_executor* ex) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return 0;
    }
    pthread_mutex_lock(&state->lock);
    int empty = state->lease_head == NULL;
    pthread_mutex_unlock(&state->lock);
    return empty;
}

static int wait_release(rt_executor* ex, rtb_child_state* child, uint32_t owner, uint64_t task_id) {
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), owner);
    for (uint32_t i = 0; i < 5000; i++) {
        struct rt_transport_debug_snapshot snapshot = rt_transport_debug_snapshot(shard);
        if (snapshot.remote_task_release_requests == 1 &&
            atomic_load_explicit(&child->done, memory_order_acquire) != 0 &&
            get_task(ex, task_id) == NULL && lease_registry_empty(ex)) {
            return 1;
        }
        rtb_drain(ex, owner);
        (void)run_ready_one(ex);
        rtb_sleep_us(1000);
    }
    return 0;
}

int rtb_mode_teardown(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rtb_publish_state state;
    memset(&state, 0, sizeof(state));
    state.task_state = &child;
    state.poll_id = POLL_RTB_CHILD;
    state.destination = 1;
    void* publisher = __task_create(POLL_RTB_PUBLISHER, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    if (!rtb_await(publisher, &kind, &bits) || state.status != RT_REMOTE_SPAWN_STATUS_OK ||
        state.published_task_id == 0) {
        return rtb_fail("unconsumed-handle publisher failed");
    }
    if (!wait_release(ex, &child, 1, state.published_task_id)) {
        return rtb_fail("unconsumed handle did not owner-route one release without orphan");
    }
    if (atomic_load_explicit(&child.cancelled, memory_order_acquire) == 0) {
        return rtb_fail("unconsumed remote child was not cancelled");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int rtb_mode_pre_ack_cancel(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rtb_publish_state state;
    memset(&state, 0, sizeof(state));
    state.task_state = &child;
    state.poll_id = POLL_RTB_CHILD;
    state.destination = 1;
    void* publisher = __task_create(POLL_RTB_PUBLISHER, &state, rt_channel_opaque_word_ops());
    for (uint32_t i = 0;
         i < 5000 && rt_sync_point_reached_count(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK) == 0;
         i++) {
        rtb_sleep_us(1000);
    }
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK) != 1 ||
        state.pending == NULL || state.pending->handle.task_id == 0) {
        return rtb_fail("pre-ACK window was not reached after child publication");
    }
    uint64_t child_task_id = state.pending->handle.task_id;
    rt_control_lock(ex);
    cancel_task(ex, task_from_handle(publisher)->id);
    rt_control_unlock(ex);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 2 || !lease_registry_empty(ex)) {
        return rtb_fail("cancelled publisher retained its pre-ACK lease");
    }
    rt_sync_point_open();
    if (!wait_release(ex, &child, 1, child_task_id)) {
        return rtb_fail("abandoned ACK did not release the published child");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A saturated data budget PARKS the awaiting caller rather than refusing
// it: the request is unsent, the caller is PENDING on the owner's slot key
// with the lease it consumed still its own and its reply slot reserved, and
// shutdown is what resolves it -- the sweep answers the listed request
// DESTINATION_SHUTDOWN, gives the reserved slot back, and teardown releases
// the child with every other far task. Read from the pending and the
// registry, not from the caller's poll: the caller's carrier may have
// exited before the shutdown wake reached it.
int rtb_mode_queue_failure(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rt_far_task_handle* handle = rtb_publish_handle(&child, 0);
    if (handle == NULL)
        return rtb_fail("queue-failure publication failed");
    uint64_t child_task_id = handle->task_id;
    rtb_lifecycle_state state;
    memset(&state, 0, sizeof(state));
    state.handle = handle;
    state.fill_inbound = 1;
    rt_far_task_begin_transfer(handle);
    void* caller = __task_create(POLL_RTB_LIFECYCLE, &state, rt_channel_opaque_word_ops());
    rt_far_task_finish_transfer(handle, caller);
    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), handle->owner_shard_id);
    if (!rtb_wait_admission_parks(owner, 1, 5000)) {
        return rtb_fail("saturated data budget did not park the awaiting caller");
    }
    if (state.status != RT_REMOTE_TASK_STATUS_PENDING) {
        return rtb_fail("parked caller answered something other than PENDING");
    }
    rt_remote_task_pending* parked =
        atomic_load_explicit(&state.visible_pending, memory_order_acquire);
    if (parked == NULL) {
        return rtb_fail("parked caller published no pending");
    }
    (void)caller;
    (void)rt_executor_request_shutdown(ex);
    int resolved = 0;
    for (uint32_t i = 0; i < 5000 && !resolved; i++) {
        resolved = rt_remote_task_pending_snapshot(parked, NULL) ==
                   RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
        if (!resolved) {
            rtb_sleep_us(1000);
        }
    }
    if (!resolved) {
        return rtb_fail("shutdown did not resolve the parked await as destination-shutdown");
    }
    if (rt_transport_debug_snapshot(owner).reply_reserved != 0 ||
        rt_remote_admission_orphan_count() != 0) {
        return rtb_fail("the parked await's reply slot was not given back at shutdown");
    }
    int leases_gone = 0;
    for (uint32_t i = 0; i < 5000 && !leases_gone; i++) {
        leases_gone = lease_registry_empty(ex);
        if (!leases_gone) {
            rtb_drain(ex, 0);
            (void)run_ready_one(ex);
            rtb_sleep_us(1000);
        }
    }
    if (!leases_gone) {
        return rtb_fail("teardown did not release the lease the parked await consumed");
    }
    (void)child_task_id;
    return 0;
}

int rtb_mode_shutdown_waiters(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state children[2];
    rtb_lifecycle_state callers[2];
    rt_far_task_handle* caller_handles[2] = {NULL, NULL};
    memset(children, 0, sizeof(children));
    memset(callers, 0, sizeof(callers));
    for (uint32_t shard = 0; shard < 2; shard++) {
        rt_far_task_handle* child_handle = rtb_publish_handle(&children[shard], 1 - shard);
        if (child_handle == NULL)
            return rtb_fail("shutdown child publication failed");
        callers[shard].handle = child_handle;
        rt_far_task_begin_transfer(child_handle);
        caller_handles[shard] = rtb_publish_poll(POLL_RTB_LIFECYCLE, &callers[shard], shard);
        if (caller_handles[shard] == NULL) {
            return rtb_fail("shutdown caller publication failed");
        }
        rt_task* caller_task = get_task(ex, caller_handles[shard]->task_id);
        rt_far_task_finish_transfer(child_handle, caller_task);
    }
    rt_remote_task_pending* pending[2] = {NULL, NULL};
    struct rt_transport_debug_snapshot before[2];
    for (uint32_t shard = 0; shard < 2; shard++) {
        for (uint32_t i = 0; i < 5000; i++) {
            pending[shard] =
                atomic_load_explicit(&callers[shard].visible_pending, memory_order_acquire);
            const rt_task* task = get_task(ex, caller_handles[shard]->task_id);
            if (pending[shard] != NULL && task != NULL && task_status_load(task) == TASK_WAITING &&
                task->park_key.kind == WAKER_REMOTE_TASK_REPLY) {
                break;
            }
            rtb_sleep_us(1000);
        }
        if (pending[shard] == NULL || pending[shard]->source_shard_id != shard) {
            return rtb_fail("remote await caller did not park on its source shard");
        }
        const rt_task* task = get_task(ex, caller_handles[shard]->task_id);
        if (task == NULL || task_status_load(task) != TASK_WAITING ||
            task->park_key.kind != WAKER_REMOTE_TASK_REPLY) {
            return rtb_fail("remote await caller was not reply-parked before shutdown");
        }
        rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), shard);
        before[shard] = rt_transport_debug_snapshot(owner);
    }
    for (uint32_t shard = 0; shard < 2; shard++) {
        // visible_pending is only a borrowed observation. Shutdown can let
        // the caller consume its own reference before this thread inspects
        // the terminal status, so hold one explicit test reference across
        // the shutdown boundary.
        rt_remote_task_pending_add_ref(pending[shard]);
    }
    const char* failure = NULL;
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        failure = "executor shutdown failed";
    }
    for (uint32_t shard = 0; failure == NULL && shard < 2; shard++) {
        if (rt_remote_task_pending_snapshot(pending[shard], NULL) !=
            RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN) {
            failure = "shutdown did not fail remote reply waiter";
            break;
        }
        rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), shard);
        struct rt_transport_debug_snapshot after = rt_transport_debug_snapshot(owner);
        if (after.shutdown_wakes != before[shard].shutdown_wakes + 1 ||
            after.park_state != RT_TRANSPORT_SHARD_SHUTDOWN) {
            failure = "shutdown did not wake every caller shard";
        }
    }
    if (failure == NULL && !lease_registry_empty(ex)) {
        failure = "shutdown retained far-task leases";
    }
    for (uint32_t shard = 0; shard < 2; shard++) {
        rt_remote_task_pending_release(pending[shard]);
    }
    return failure != NULL ? rtb_fail(failure) : 0;
}
