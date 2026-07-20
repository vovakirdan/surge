#include "rt_far_channel.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task_internal.h"
#include "rt_sync_point.h"

#include <string.h>

// Remote select: the owner-side proxy selector. The whole select ships as
// ONE anchored execute to the arms' shared owner shard; the body runs the
// LOCAL select over the co-located channels (rt_select_poll, unchanged
// protocol: parked re-entry from the top, clear_wait_keys makes the re-poll
// idempotent) and replies with the winner index — the same value local
// select's lowering stores into its select_index destination. Everything
// else is the proven execute/reply discipline: single pending retry,
// cancel-inflight with exactly one reply edge, orphaned-reply consumption.
// The winner is decided on the owner lane; no caller-lane ordering is
// promised across the shard boundary (contract clause, RUNTIME_V2.md).

// One reply edge for dispatch-side failures.
static void
select_answer(rt_executor* ex, rt_remote_task_pending* pending, rt_remote_task_status status) {
    rt_remote_task_reply_or_finish(
        ex, pending, status, 2, 0, RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY);
}

static int select_request_matches(const rt_transport_msg* msg,
                                  const rt_remote_task_pending* pending) {
    return msg != NULL && pending != NULL && msg->route_id == pending->request_id &&
           msg->generation == pending->handle.generation &&
           msg->source_shard_id == pending->source_shard_id &&
           msg->target_shard_id == pending->handle.owner_shard_id;
}

// Caller side. All arms must be channel leases sharing ONE owner shard
// (sema owns the kind diagnostic; this is the runtime's defense in depth),
// and every arm kind must be a channel operation — timeout and default arms
// stay on the caller in every lowering.
static void select_drop_unshipped_state(uint64_t state_drop_fn_id, void* state) {
    if (state_drop_fn_id != 0 && state != NULL) {
        __surge_drop_call(state_drop_fn_id, state);
    }
}

rt_remote_task_status rt_far_channel_select(const rt_far_task_handle* const* anchors,
                                            const uint8_t* kinds,
                                            const uint64_t* send_bits,
                                            const uint64_t* send_drop_fn_ids,
                                            uint64_t count,
                                            uint64_t state_drop_fn_id,
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
    if (anchors == NULL || kinds == NULL || count == 0 || count > RT_FAR_CHANNEL_SELECT_MAX_ARMS) {
        select_drop_unshipped_state(state_drop_fn_id, state);
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    for (uint64_t i = 0; i < count; i++) {
        if (anchors[i] == NULL || anchors[i]->kind != RT_FAR_HANDLE_KIND_CHANNEL ||
            anchors[i]->owner_shard_id != anchors[0]->owner_shard_id) {
            select_drop_unshipped_state(state_drop_fn_id, state);
            return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
        }
        if (kinds[i] != SELECT_CHAN_RECV && kinds[i] != SELECT_CHAN_SEND) {
            select_drop_unshipped_state(state_drop_fn_id, state);
            return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
        }
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* destination = rt_runtime_shard(runtime, anchors[0]->owner_shard_id);
    if (destination == NULL) {
        select_drop_unshipped_state(state_drop_fn_id, state);
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&destination->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        select_drop_unshipped_state(state_drop_fn_id, state);
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }
    rt_far_channel_select_arm* arms = (rt_far_channel_select_arm*)rt_alloc(
        count * sizeof(rt_far_channel_select_arm), _Alignof(rt_far_channel_select_arm));
    if (arms == NULL) {
        select_drop_unshipped_state(state_drop_fn_id, state);
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    memset(arms, 0, count * sizeof(rt_far_channel_select_arm));
    for (uint64_t i = 0; i < count; i++) {
        arms[i].anchor = *anchors[i];
        arms[i].kind = kinds[i];
        arms[i].send_bits = send_bits != NULL ? send_bits[i] : 0;
        arms[i].payload_drop_fn_id = send_drop_fn_ids != NULL ? send_drop_fn_ids[i] : 0;
    }
    rt_far_task_handle route = {.task_id = 0,
                                .generation = 0,
                                .owner_shard_id = anchors[0]->owner_shard_id,
                                .kind = RT_FAR_HANDLE_KIND_TASK};
    rt_remote_task_pending* request = rt_remote_task_pending_new(
        ex, &route, rt_immediate_on_source_shard(current), RT_REMOTE_TASK_OP_CHANNEL_SELECT, 1);
    if (request == NULL) {
        // No pending was ever created, so nothing committed: every SEND
        // arm's payload (if heap-carried) is still fully owned right here.
        for (uint64_t i = 0; i < count; i++) {
            if (arms[i].payload_drop_fn_id != 0) {
                __surge_drop_result_call(arms[i].payload_drop_fn_id, (void*)arms[i].send_bits);
            }
        }
        rt_free((uint8_t*)arms,
                count * sizeof(rt_far_channel_select_arm),
                _Alignof(rt_far_channel_select_arm));
        select_drop_unshipped_state(state_drop_fn_id, state);
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    request->handle.generation = request->request_id;
    request->caller_task_id = current->id;
    request->body_poll_fn_id = (uint64_t)poll_fn_id;
    request->body_state = state;
    request->state_drop_fn_id = state_drop_fn_id;
    request->state_owned = state_drop_fn_id != 0;
    request->select_arms = arms;
    request->select_count = count;
    *pending = request;
    (void)rt_remote_task_prepare_reply_wait(ex, current, request);
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = anchors[0]->owner_shard_id,
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

// Unpins every arm the dispatch pinned; shared by the failure paths and the
// reply edge.
void rt_far_channel_select_unpin_arms(rt_executor* ex,
                                      const rt_remote_task_pending* pending,
                                      uint64_t pinned_count) {
    if (pending == NULL || pending->select_arms == NULL) {
        return;
    }
    for (uint64_t i = 0; i < pinned_count; i++) {
        rt_far_channel_unpin(ex, &pending->select_arms[i].anchor);
    }
}

// Owner-side dispatch: pin every arm's channel (all-or-nothing: one stale
// lease answers STALE_TOKEN and unpins the prefix — a released holder
// cannot select), then bind/register/publish the body task exactly like the
// anchored execute.
void rt_far_channel_dispatch_select(rt_executor* ex, const rt_transport_msg* msg) {
    rt_remote_task_pending* pending = msg != NULL ? msg->payload : NULL;
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL || pending == NULL) {
        rt_remote_task_pending_release(pending);
        return;
    }
    RT_SYNC_POINT(SP_FAR_SELECT_BEFORE_DISPATCH);
    if (!select_request_matches(msg, pending)) {
        rt_runtime* runtime = rt_executor_runtime(ex);
        rt_transport_record_remote_task_stale(
            msg != NULL ? rt_runtime_shard(runtime, msg->target_shard_id) : NULL);
        select_answer(ex, pending, RT_REMOTE_TASK_STATUS_STALE_TOKEN);
        return;
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) != RT_REMOTE_TASK_STATUS_PENDING) {
        rt_remote_task_pending_release(pending);
        return;
    }
    for (uint64_t i = 0; i < pending->select_count; i++) {
        rt_far_channel_select_arm* arm = &pending->select_arms[i];
        if (!rt_far_channel_pin(ex, &arm->anchor, &arm->channel)) {
            rt_far_channel_select_unpin_arms(ex, pending, i);
            select_answer(ex, pending, RT_REMOTE_TASK_STATUS_STALE_TOKEN);
            return;
        }
    }
    rt_task* task = NULL;
    rt_remote_spawn_status created = rt_remote_spawn_create_body_task(
        ex, pending->body_poll_fn_id, pending->body_state, msg->target_shard_id, &task);
    if (created != RT_REMOTE_SPAWN_STATUS_OK) {
        rt_far_channel_select_unpin_arms(ex, pending, pending->select_count);
        select_answer(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED);
        return;
    }
    pthread_mutex_lock(&state->lock);
    if (pending->status != RT_REMOTE_TASK_STATUS_PENDING) {
        pthread_mutex_unlock(&state->lock);
        rt_far_channel_select_unpin_arms(ex, pending, pending->select_count);
        rt_remote_spawn_free_unpublished_task(ex, task);
        rt_remote_task_pending_release(pending);
        return;
    }
    pending->handle.task_id = task->id;
    pending->handle.generation = task->generation;
    pthread_mutex_unlock(&state->lock);
    task_add_ref(task);
    rt_remote_task_pending_set_owner_registered(pending, 1);
    rt_remote_spawn_status published = rt_remote_spawn_publish_body_task(ex, task);
    if (published != RT_REMOTE_SPAWN_STATUS_OK) {
        rt_far_channel_select_unpin_arms(ex, pending, pending->select_count);
        rt_remote_task_pending_set_owner_registered(pending, 0);
        task_release_lane_aware(ex, task);
        rt_remote_spawn_free_unpublished_task(ex, task);
        select_answer(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED);
        return;
    }
    // PUBLICATION-ACCEPTED HANDOFF (contract: rt_remote_spawn_internal.h).
    pending->state_owned = 0;
    task_release_lane_aware(ex, task);
}

// The remote-select body's binding: the dispatch-pinned arm table and the
// shipped poll state, found through the pending that created this body (the
// same scan discipline as the anchored single-channel binding).
int rt_remote_task_select_binding_current(rt_far_channel_select_arm** out_arms,
                                          uint64_t* out_count,
                                          void** out_state) {
    rt_executor* ex = ensure_exec();
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    const rt_task* current = rt_current_task();
    if (state == NULL || current == NULL) {
        return 0;
    }
    int bound = 0;
    pthread_mutex_lock(&state->lock);
    for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
        if (it->op == RT_REMOTE_TASK_OP_CHANNEL_SELECT &&
            it->status == RT_REMOTE_TASK_STATUS_PENDING && it->handle.task_id == current->id &&
            it->handle.generation == current->generation &&
            it->handle.owner_shard_id == current->owner_shard_id) {
            if (out_arms != NULL) {
                *out_arms = it->select_arms;
            }
            if (out_count != NULL) {
                *out_count = it->select_count;
            }
            if (out_state != NULL) {
                *out_state = it->body_state;
            }
            bound = 1;
            break;
        }
    }
    pthread_mutex_unlock(&state->lock);
    return bound;
}

// Records which arm rt_select_poll committed, under the same lock the
// shutdown/cancel-inflight sweep (rt_remote_task_wait.c) uses to stomp
// result_kind/result_bits on a pending — that sweep never touches
// select_committed_index, but the write itself still needs the lock to
// avoid racing a concurrent unlink/free of the same pending. Called once,
// immediately after rt_select_poll's own critical section has already
// released (the window rt_anchored_channel_select's caller-facing comment
// already documents), so there is nothing left to race on the commit
// itself — only the registry lookup needs synchronizing.
static void record_select_commit(int64_t winner) {
    rt_executor* ex = ensure_exec();
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    const rt_task* current = rt_current_task();
    if (state == NULL || current == NULL) {
        return;
    }
    pthread_mutex_lock(&state->lock);
    for (rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
        if (it->op == RT_REMOTE_TASK_OP_CHANNEL_SELECT &&
            it->status == RT_REMOTE_TASK_STATUS_PENDING && it->handle.task_id == current->id &&
            it->handle.generation == current->generation &&
            it->handle.owner_shard_id == current->owner_shard_id) {
            it->select_committed_index = (uint64_t)winner;
            break;
        }
    }
    pthread_mutex_unlock(&state->lock);
}

// The selector body's single operation: runs the local select over the
// bound channels and returns the winner index. The parked path yields with
// rt_select_poll's registrations in place and the wake re-enters the body
// from the top — clear_wait_keys at the poll's entry makes the re-poll
// idempotent, byte-for-byte the compiled local select protocol.
uint64_t rt_anchored_channel_select(void) {
    rt_executor* ex = ensure_exec();
    rt_far_channel_select_arm* arms = NULL;
    uint64_t count = 0;
    void* state = NULL;
    if (!rt_remote_task_select_binding_current(&arms, &count, &state)) {
        panic_msg("anchored select outside a remote-select block body");
        return 0;
    }
    // See the matching note in rt_immediate_on_anchored.c: this state's drop
    // obligation already transferred onto the body task's ordinary state
    // lifecycle at the publication-accepted handoff; passing 0 here
    // deliberately leaves the abandoned-suspend-state stash untouched.
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state, 0);
    }
    uint8_t kinds[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    void* handles[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    uint64_t values[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    if (count > RT_FAR_CHANNEL_SELECT_MAX_ARMS) {
        panic_msg("anchored select arm table exceeds the arm cap");
        return 0;
    }
    for (uint64_t i = 0; i < count; i++) {
        kinds[i] = arms[i].kind;
        handles[i] = arms[i].channel;
        values[i] = arms[i].send_bits;
    }
    int64_t winner = rt_select_poll(count, kinds, handles, values, NULL, -1);
    if (winner < 0) {
        rt_async_yield(state, 0);
    }
    // rt_select_poll's control-lock critical section (the commit) has
    // already released by the time it returns a winner >= 0; this window
    // sits strictly after that commit and before the value reaches the
    // caller's async-return/reply (Epic 20 Task 7 row 2).
    RT_SYNC_POINT(SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY);
    // The commit is final the moment rt_select_poll returns a non-negative
    // winner (see above); record it so a teardown that reaches the pending
    // before this reply lands knows exactly which SEND arm's payload was
    // consumed and must be skipped, not dropped.
    record_select_commit(winner);
    return (uint64_t)winner;
}
