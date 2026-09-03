#include "rt_far_channel.h"
#include "rt_remote_spawn_internal.h"
#include "rt_remote_task_internal.h"
#include "rt_resident_bytes.h"
#include "rt_sync_point.h"
#include "rt_value_ops.h"

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
        ex, pending, status, 2, RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY);
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
static void select_drop_unshipped_state(uint64_t state_type_id, void* state) {
    if (state_type_id != 0 && state != NULL) {
        rt_value_release_owned_block(rt_channel_element_ops_for(state_type_id), state);
    }
}

// A winner reply hands each losing SEND payload BACK to compiled code: the
// value MOVES out of the arm's cell into the caller's storage for that arm, and
// the cell is left MOVED so nothing destroys it twice. The backend restores it
// into the explicit MIR ReturnPlace before entering the winning arm, where
// ordinary ownership drop synthesis reclaims it. The committed arm is skipped:
// the destination's select already moved its value into the channel.
//
// This is where select DIVERGES from the result handover in
// rt_remote_task_api.c's finish_retry, which runs unconditionally. That is safe
// there because a non-success reply names no result, so there is nothing to
// take. An arm payload is the opposite: it stays fully alive on a Cancelled
// reply, and the compiled cancelled path re-enters the pend edge instead of
// running any arm block (internal/backend/llvm/emit_crossing_select.go), so
// nothing caller-side would ever reclaim it. The handback is therefore gated on
// a genuine winner reply; every other outcome -- cancelled, failed, or a
// teardown that never reaches the caller at all -- leaves the value in its cell
// for the pending's cleanup to destroy.
//
// `out_values` is the caller's live storage on THIS poll and is read nowhere
// else: the pending does not keep it, because a park between polls would leave
// it holding an address of a frame that is gone.
//
// The committed index is read under the state lock, symmetric with
// record_select_commit which writes it; the moves themselves run with the lock
// released, because a move is generated code (§8 P2). The caller still holds
// its own pending ref, so the free path cannot be running against these cells
// concurrently.
static int
select_return_arms(rt_remote_task_pending* pending, void* const* out_values, uint64_t out_count) {
    rt_remote_task_state* state =
        pending != NULL ? rt_remote_task_state_get(pending->executor) : NULL;
    if (pending == NULL || pending->select_arms == NULL || state == NULL || out_values == NULL ||
        out_count != pending->select_count) {
        return 0;
    }
    pthread_mutex_lock(&state->lock);
    uint64_t committed = pending->select_committed_index;
    pthread_mutex_unlock(&state->lock);
    for (uint64_t i = 0; i < pending->select_count; i++) {
        rt_far_channel_select_arm* arm = &pending->select_arms[i];
        if (arm->kind != SELECT_CHAN_SEND || i == committed) {
            continue;
        }
        void* destination = out_values[i];
        void* value = rt_value_cell_value(&arm->payload);
        if (destination == NULL || value == NULL) {
            continue;
        }
        rt_value_move_init_detached(arm->payload.operations, destination, value);
        (void)rt_value_cell_commit_move(&arm->payload);
    }
    return 1;
}

static rt_remote_task_status select_finish_retry(rt_remote_task_pending** slot,
                                                 void* const* out_values,
                                                 uint64_t out_count,
                                                 uint8_t* out_kind,
                                                 uint64_t* out_winner) {
    uint8_t kind = 0;
    if (*slot != NULL &&
        rt_remote_task_pending_snapshot(*slot, &kind) == RT_REMOTE_TASK_STATUS_OK &&
        kind == RT_REMOTE_TASK_REPLY_KIND_SUCCESS) {
        if (!select_return_arms(*slot, out_values, out_count)) {
            // A success without storage for the returned losing payloads
            // cannot be exposed to compiled arm dispatch. Leave every value in
            // its cell for the cleanup to destroy, consume the pending, and
            // fail closed.
            (void)rt_immediate_on_finish_retry(slot, out_kind, out_winner);
            return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
        }
    }
    return rt_immediate_on_finish_retry(slot, out_kind, out_winner);
}

// A select that never armed still owns every SEND payload the caller handed
// it, in the caller's own storage: nothing was staged, so each is destroyed
// where it stands, through its own descriptor.
static void select_drop_input_payloads(const uint8_t* kinds,
                                       void* const* send_values,
                                       const uint64_t* payload_type_ids,
                                       uint64_t count) {
    if (kinds == NULL || send_values == NULL || payload_type_ids == NULL) {
        return;
    }
    for (uint64_t i = 0; i < count; i++) {
        if (kinds[i] != SELECT_CHAN_SEND || send_values[i] == NULL) {
            continue;
        }
        rt_value_drop_in_place_detached(rt_channel_element_ops_for(payload_type_ids[i]),
                                        send_values[i]);
    }
}

rt_remote_task_status rt_far_channel_select(const rt_far_task_handle* const* anchors,
                                            const uint8_t* kinds,
                                            void* const* send_values,
                                            const uint64_t* payload_type_ids,
                                            uint64_t count,
                                            uint64_t state_type_id,
                                            int64_t poll_fn_id,
                                            void* state,
                                            rt_remote_task_pending** pending,
                                            uint8_t* out_kind,
                                            uint64_t* out_winner) {
    rt_executor* ex = ensure_exec();
    rt_task* current = rt_current_task();
    if (ex == NULL || pending == NULL || current == NULL || rt_current_task_id() == 0) {
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    if (*pending != NULL) {
        rt_remote_task_status status = rt_remote_task_pending_snapshot(*pending, out_kind);
        if (status != RT_REMOTE_TASK_STATUS_PENDING) {
            return select_finish_retry(pending, send_values, count, out_kind, out_winner);
        }
        switch (rt_remote_task_retry_admission(ex, current, *pending)) {
            case RT_REMOTE_ADMISSION_PARKED:
                return RT_REMOTE_TASK_STATUS_PENDING;
            case RT_REMOTE_ADMISSION_FINISHED:
                return select_finish_retry(pending, send_values, count, out_kind, out_winner);
            default:
                break;
        }
        if (task_cancelled_load(current) != 0) {
            rt_immediate_on_cancel_inflight(ex, *pending);
        }
        if (rt_remote_task_prepare_reply_wait(ex, current, *pending) != 0) {
            return select_finish_retry(pending, send_values, count, out_kind, out_winner);
        }
        return RT_REMOTE_TASK_STATUS_PENDING;
    }
    if (anchors == NULL || kinds == NULL || count == 0 || count > RT_FAR_CHANNEL_SELECT_MAX_ARMS) {
        // The call boundary already consumed every well-described SEND payload.
        // A missing anchor array is still enough information to reclaim them;
        // cap the walk at the ABI arm limit so a malformed count cannot make
        // failure cleanup read beyond the caller-provided arrays.
        if (count > 0 && count <= RT_FAR_CHANNEL_SELECT_MAX_ARMS) {
            select_drop_input_payloads(kinds, send_values, payload_type_ids, count);
        }
        select_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
    }
    for (uint64_t i = 0; i < count; i++) {
        if (anchors[i] == NULL || anchors[i]->kind != RT_FAR_HANDLE_KIND_CHANNEL ||
            anchors[i]->owner_shard_id != anchors[0]->owner_shard_id) {
            select_drop_input_payloads(kinds, send_values, payload_type_ids, count);
            select_drop_unshipped_state(state_type_id, state);
            return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
        }
        if (kinds[i] != SELECT_CHAN_RECV && kinds[i] != SELECT_CHAN_SEND) {
            select_drop_input_payloads(kinds, send_values, payload_type_ids, count);
            select_drop_unshipped_state(state_type_id, state);
            return RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT;
        }
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    rt_shard* destination = rt_runtime_shard(runtime, anchors[0]->owner_shard_id);
    if (destination == NULL) {
        select_drop_input_payloads(kinds, send_values, payload_type_ids, count);
        select_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_STALE_TOKEN;
    }
    if (atomic_load_explicit(&ex->shutdown, memory_order_acquire) != 0 ||
        atomic_load_explicit(&destination->transport.park_state, memory_order_acquire) ==
            RT_TRANSPORT_SHARD_SHUTDOWN) {
        select_drop_input_payloads(kinds, send_values, payload_type_ids, count);
        select_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN;
    }
    rt_far_channel_select_arm* arms = (rt_far_channel_select_arm*)rt_alloc(
        count * sizeof(rt_far_channel_select_arm), _Alignof(rt_far_channel_select_arm));
    if (arms == NULL) {
        select_drop_input_payloads(kinds, send_values, payload_type_ids, count);
        select_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    memset(arms, 0, count * sizeof(rt_far_channel_select_arm));
    rt_resident_bytes_acquire(RT_RESIDENT_SIDECAR, count * sizeof(rt_far_channel_select_arm));
    // Staging: each SEND payload MOVES out of the caller's storage into its
    // arm's cell, which is what makes the arm table the value's one owner from
    // here on. A staging that cannot reserve its storage leaves the value where
    // it is -- still the caller's, still reclaimed by the failure path below --
    // rather than half-taking it.
    uint64_t staged = 0;
    for (; staged < count; staged++) {
        arms[staged].anchor = *anchors[staged];
        arms[staged].kind = kinds[staged];
        if (kinds[staged] != SELECT_CHAN_SEND) {
            continue;
        }
        const uint64_t type_id = payload_type_ids != NULL ? payload_type_ids[staged] : 0;
        void* source = send_values != NULL ? send_values[staged] : NULL;
        void* cell_storage = NULL;
        if (rt_value_cell_bind(&arms[staged].payload, rt_channel_element_ops_for(type_id)) ==
            RT_SLOT_CONTROL_OK) {
            cell_storage = rt_value_cell_publish_storage(&arms[staged].payload);
        }
        if (cell_storage == NULL || source == NULL) {
            break;
        }
        rt_value_move_init_detached(arms[staged].payload.operations, cell_storage, source);
        (void)rt_value_cell_commit(&arms[staged].payload);
        if (arms[staged].payload.owns_block) {
            // A payload too wide for the arm's inline run: its block is
            // resident beside the table until the table is freed.
            rt_resident_payload_acquire(arms[staged].payload.operations);
        }
    }
    if (staged != count) {
        // One arm could not be staged. What was already staged lives in its
        // cell and is destroyed by the same loop the no-pending path below
        // runs; what was NOT staged is still the caller's, so it is destroyed
        // where it stands. Every payload is reclaimed exactly once no matter
        // where the walk stopped.
        select_drop_input_payloads(
            kinds + staged, send_values + staged, payload_type_ids + staged, count - staged);
    }
    rt_far_task_handle route = {.task_id = 0,
                                .generation = 0,
                                .owner_shard_id = anchors[0]->owner_shard_id,
                                .kind = RT_FAR_HANDLE_KIND_TASK};
    rt_remote_task_pending* request =
        staged != count ? NULL
                        : rt_remote_task_pending_new(ex,
                                                     &route,
                                                     rt_immediate_on_source_shard(current),
                                                     RT_REMOTE_TASK_OP_CHANNEL_SELECT,
                                                     1);
    if (request == NULL) {
        // Nothing was ever handed to a pending -- either staging stopped short
        // or the pending itself could not be allocated -- so every staged
        // payload is still fully owned right here, in its own cell.
        rt_remote_task_select_arms_free(arms, count);
        select_drop_unshipped_state(state_type_id, state);
        return RT_REMOTE_TASK_STATUS_REFUSED;
    }
    request->handle.generation = request->request_id;
    request->caller_task_id = current->id;
    request->body_poll_fn_id = (uint64_t)poll_fn_id;
    request->body_state = state;
    request->state_type_id = state_type_id;
    request->state_owned = state_type_id != 0;
    if (request->state_owned) {
        rt_resident_payload_acquire(rt_channel_element_ops_for(state_type_id));
    }
    request->select_arms = arms;
    request->select_count = count;
    *pending = request;
    rt_remote_task_pending_add_ref(request);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST,
        .source_shard_id = request->source_shard_id,
        .target_shard_id = anchors[0]->owner_shard_id,
        .route_id = request->request_id,
        .generation = request->handle.generation,
        .payload = request,
    };
    rt_remote_admission_init(&request->admission, &msg, 1);
    rt_remote_task_status status = rt_remote_task_submit(ex, current, request);
    if (status == RT_REMOTE_TASK_STATUS_PENDING) {
        return status;
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
    if (rt_remote_task_pending_snapshot(pending, NULL) != RT_REMOTE_TASK_STATUS_PENDING) {
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
    rt_remote_spawn_status created = rt_remote_spawn_create_body_task(ex,
                                                                      pending->body_poll_fn_id,
                                                                      pending->body_state,
                                                                      msg->target_shard_id,
                                                                      // D5 types this body's
                                                                      // result; until then it
                                                                      // answers with a word.
                                                                      0,
                                                                      &task);
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
    // Same publication window as the immediate-on dispatch: hold the pending
    // across it so the state_owned store below cannot land in memory a
    // completing body already freed (contract: rt_remote_spawn_internal.h).
    rt_remote_task_pending_add_ref(pending);
    rt_remote_spawn_status published = rt_remote_spawn_publish_body_task(ex, task);
    if (published != RT_REMOTE_SPAWN_STATUS_OK) {
        rt_far_channel_select_unpin_arms(ex, pending, pending->select_count);
        rt_remote_task_pending_set_owner_registered(pending, 0);
        task_release_lane_aware(ex, task);
        rt_remote_spawn_free_unpublished_task(ex, task);
        select_answer(ex, pending, RT_REMOTE_TASK_STATUS_REFUSED);
        rt_remote_task_pending_release(pending);
        return;
    }
    // PUBLICATION-ACCEPTED HANDOFF (contract: rt_remote_spawn_internal.h).
    if (pending->state_owned) {
        rt_resident_payload_release(rt_channel_element_ops_for(pending->state_type_id));
    }
    pending->state_owned = 0;
    rt_remote_task_pending_release(pending);
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
// result_kind on a pending — that sweep never touches
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
    // The local select takes each SEND arm's value BY ADDRESS and moves it into
    // the channel, so what it is given is the arm cell's own storage. No copy
    // of the value is made on the way in: the cell staged it once, on the
    // caller's side, and the winner's move out of that same storage is what
    // ends the arm's ownership of it.
    void* value_addrs[RT_FAR_CHANNEL_SELECT_MAX_ARMS];
    if (count > RT_FAR_CHANNEL_SELECT_MAX_ARMS) {
        panic_msg("anchored select arm table exceeds the arm cap");
        return 0;
    }
    for (uint64_t i = 0; i < count; i++) {
        kinds[i] = arms[i].kind;
        handles[i] = arms[i].channel;
        value_addrs[i] = rt_value_cell_value(&arms[i].payload);
    }
    int64_t winner = rt_select_poll(count, kinds, handles, value_addrs, NULL, -1);
    if (winner < 0) {
        rt_async_yield(state, 0);
    }
    // rt_select_poll's control-lock critical section (the commit) has
    // already released by the time it returns a winner >= 0; this window
    // sits strictly after that commit and before the value reaches the
    // caller's async-return/reply (Epic 20 Task 7 row 2).
    RT_SYNC_POINT(SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY);
    // The winner's value left its cell the moment the local select committed:
    // it was moved into the channel out of the cell's own storage. Marking the
    // cell MOVED is what tells every later reader -- the handback, the
    // pending's cleanup -- that this one is already somebody else's.
    if (arms[winner].kind == SELECT_CHAN_SEND) {
        (void)rt_value_cell_commit_move(&arms[winner].payload);
    }
    // The commit is final the moment rt_select_poll returns a non-negative
    // winner (see above); record it so a teardown that reaches the pending
    // before this reply lands knows exactly which SEND arm's payload was
    // consumed and must be skipped, not dropped.
    record_select_commit(winner);
    return (uint64_t)winner;
}
