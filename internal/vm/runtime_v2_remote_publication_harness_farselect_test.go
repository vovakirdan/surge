//go:build runtime_v2_pending

package vm_test

// Modes for far select, plus main() -- the switch that names every mode, so a
// mode added without wiring it here fails to build rather than silently never
// running.
const remotePublicationHarnessFarSelect = `
static int run_far_select_cancel_vs_commit(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 42;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    void* caller = __task_create(POLL_SELECT_CALLER, &st);
    if (caller == NULL) return fail("select caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY, 5000)) {
        return fail("commit window was never reached");
    }
    rt_remote_task_pending* req = wait_select_pending_shared(&st, 5000);
    if (req == NULL) return fail("select pending missing at commit window");
    rt_remote_task_pending_add_ref(req);

    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled select caller must not resolve successfully");
    rt_sync_point_open();

    if (!wait_task_refs(req, 1, 5000)) return fail("select reply never resolved");
    uint8_t result_kind = 0;
    uint64_t result_bits = 0;
    rt_remote_task_status status = rt_remote_task_pending_snapshot(req, &result_kind, &result_bits);
    if (status != RT_REMOTE_TASK_STATUS_OK || result_kind != 1 || result_bits != 0) {
        return fail("commit-vs-cancel race must still resolve kind=1 with the committed winner");
    }
    if (!channel_recv_once(ex, channel, 42)) {
        return fail("committed send must land in the channel exactly once");
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off select state must not drop across the commit race");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("committed select payload must stay with its channel");
    }
    rt_remote_task_pending_release(req);
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("commit-vs-cancel race leaked a pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 3: cancel-before-dispatch. The select request is cancelled while
// still UNBOUND, held at the new SP_FAR_SELECT_BEFORE_DISPATCH entry
// window (the select twin of SP_IMMEDIATE_ON_BEFORE_DISPATCH). The
// cancelled caller's own completion resolves the pending through the
// teardown sweep (rt_immediate_on_release_owned, which lists
// RT_REMOTE_TASK_OP_CHANNEL_SELECT) before the late dispatch ever reaches
// the pin loop -- no arm is touched.
static int run_far_select_cancel_before_dispatch(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 55;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    void* caller = __task_create(POLL_SELECT_CALLER, &st);
    if (caller == NULL) return fail("select caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_FAR_SELECT_BEFORE_DISPATCH, 5000)) {
        return fail("dispatch window was never reached");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled select caller must not resolve successfully");
    rt_sync_point_open();

    if (!wait_drops(1, 5000)) {
        return fail("unbound select cancel must drop the state exactly once");
    }
    if (!wait_payload_drops(1, 5000)) {
        return fail("unbound select cancel must drop the pending payload exactly once");
    }
    if (!channel_is_empty(ex, channel)) {
        return fail("unbound select cancel must never touch the arm");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("refused select dispatch must not touch the pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 4: double-cancel idempotency. The first cancel route runs through
// rt_task_cancel: the caller's own retry poll routes it, and (since the
// caller resolves as Cancelled through the same yield short-circuit as
// row 2) its own teardown immediately re-attempts the same route --
// cancel_routed already absorbs that inner duplicate. This row adds a
// THIRD, fully independent route: the driver, holding its own reference
// on the same pending, calls rt_immediate_on_cancel_inflight directly.
// The owner shard's single worker is still parked at the armed window
// (the sync point is not opened until after this check), so the first
// route's in-flight cancel-request message cannot possibly be drained
// out from under the before/after snapshot below -- nothing else can be
// touching refs at that instant, a clean proof that the third route is a
// pure no-op once cancel_routed is set.
static int run_far_select_double_cancel(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 91;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    void* caller = __task_create(POLL_SELECT_CALLER, &st);
    if (caller == NULL) return fail("select caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY, 5000)) {
        return fail("commit window was never reached");
    }
    rt_remote_task_pending* req = wait_select_pending_shared(&st, 5000);
    if (req == NULL) return fail("select pending missing at commit window");
    rt_remote_task_pending_add_ref(req);

    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled select caller must not resolve successfully");

    uint32_t refs_before = atomic_load_explicit(&req->refs, memory_order_acquire);
    rt_immediate_on_cancel_inflight(ex, req);
    uint32_t refs_after = atomic_load_explicit(&req->refs, memory_order_acquire);
    if (refs_after != refs_before) {
        return fail("a second cancel route must not land once cancel_routed is set");
    }

    rt_sync_point_open();

    if (!wait_task_refs(req, 1, 5000)) return fail("select reply never resolved");
    uint8_t result_kind = 0;
    uint64_t result_bits = 0;
    rt_remote_task_status status = rt_remote_task_pending_snapshot(req, &result_kind, &result_bits);
    if (status != RT_REMOTE_TASK_STATUS_OK || result_kind != 1 || result_bits != 0) {
        return fail("double-cancel must still resolve exactly once with the committed winner");
    }
    if (!channel_recv_once(ex, channel, 91)) {
        return fail("committed send must land exactly once under double-cancel");
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off select state must not drop under double-cancel");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("committed select payload must not drop under double-cancel");
    }
    rt_remote_task_pending_release(req);
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("double-cancel leaked a pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 5: refusal-after-shipped regression guard. Two arms share the same
// owner shard; arm 0 pins cleanly, arm 1 carries a corrupted (stale)
// anchor generation COPY -- its original lease stays valid, so the
// mint-side registry entry is untouched. The dispatch-time pin loop pins
// arm 0, fails on arm 1, unpins the already-pinned prefix (arm 0), and
// answers STALE_TOKEN before any body exists: the request never reaches
// rt_select_poll, so neither channel is ever touched despite both arms
// carrying live SEND payloads, and the sole-owner pending drops the
// shipped poll state exactly once.
static int run_far_select_refusal_after_shipped(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor0 = {0};
    void* channel0 = mint_channel_anchor(ex, dst, 1, &anchor0);
    if (channel0 == NULL) return fail("select channel 0 mint failed");
    rt_far_task_handle anchor1 = {0};
    void* channel1 = mint_channel_anchor(ex, dst, 1, &anchor1);
    if (channel1 == NULL) return fail("select channel 1 mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor0;
    st.anchors[1] = anchor1;
    st.anchors[1].generation++;  // corrupt a COPY; the original lease stays valid
    st.anchor_ptrs[0] = &st.anchors[0];
    st.anchor_ptrs[1] = &st.anchors[1];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.kinds[1] = SELECT_CHAN_SEND;
    st.send_bits[0] = 12;
    st.send_bits[1] = 34;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.send_drop_ids[1] = DROP_SELECT_PAYLOAD;
    st.count = 2;
    st.droppable = 1;
    int state_marker = 0;
    st.body_state = &state_marker;
    drop_expected_state = &state_marker;

    if (!await_select(&st)) return fail("refusal-after-shipped await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return fail("stale mid-pin arm must answer STALE_TOKEN");
    }
    if (!wait_drops(1, 5000)) {
        return fail("refused select must drop the shipped poll state exactly once");
    }
    if (!wait_payload_drops(2, 5000)) {
        return fail("refused select must drop each shipped payload exactly once");
    }
    if (!channel_is_empty(ex, channel0) || !channel_is_empty(ex, channel1)) {
        return fail("refused select must never touch send_bits on either arm");
    }
    if (rt_far_channel_release(ex, &anchor0) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &anchor1) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("select anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("refusal-after-shipped leaked a pin on one of the arms");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A reachable synchronous failure before a pending exists: compiler-built
// arms are well formed, but their far leases name different owners. The
// conditional-transfer ABI has already consumed both SEND operands when the
// call starts, so this status must drop each exactly once instead of leaving
// ownership in caller slots that no longer exist in the async state.
static int run_far_select_initial_owner_mismatch(void) {
    rt_executor* ex = ensure_exec();
    if (rt_runtime_shard_count(rt_executor_runtime(ex)) < 2) {
        return fail("owner-mismatch row requires two shards");
    }
    rt_far_task_handle anchor0 = {0};
    rt_far_task_handle anchor1 = {0};
    void* channel0 = mint_channel_anchor(ex, 0, 1, &anchor0);
    void* channel1 = mint_channel_anchor(ex, 1, 1, &anchor1);
    if (channel0 == NULL || channel1 == NULL) return fail("owner-mismatch mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor0;
    st.anchors[1] = anchor1;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.anchor_ptrs[1] = &st.anchors[1];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.kinds[1] = SELECT_CHAN_SEND;
    st.send_bits[0] = 101;
    st.send_bits[1] = 202;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.send_drop_ids[1] = DROP_SELECT_PAYLOAD;
    st.count = 2;

    if (!await_select(&st)) return fail("owner-mismatch await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
        return fail("owner-mismatch select must fail synchronously");
    }
    if (!wait_payload_drops(2, 5000)) {
        return fail("owner-mismatch failure must consume both payloads exactly once");
    }
    if (!channel_is_empty(ex, channel0) || !channel_is_empty(ex, channel1)) {
        return fail("owner-mismatch failure must not touch either channel");
    }
    if (rt_far_channel_release(ex, &anchor0) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &anchor1) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("owner-mismatch lease release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// The API has consumed the owned SEND operand before it validates the anchor
// table. Even a wholly missing table must therefore reclaim every payload it
// can describe instead of leaking the caller's now-unreachable owner.
static int run_far_select_initial_null_anchors(void) {
	select_exec_state st;
	memset(&st, 0, sizeof(st));
	st.null_anchor_array = 1;
	st.kinds[0] = SELECT_CHAN_SEND;
	st.send_bits[0] = 303;
	st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
	st.count = 1;

	if (!await_select(&st)) return fail("null-anchor select await failed");
	if (st.status != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
		return fail("null-anchor select must fail synchronously");
	}
	if (st.pending != NULL) return fail("null-anchor select must not create a pending");
	if (!wait_payload_drops(1, 5000)) {
		return fail("null-anchor failure must consume its payload exactly once");
	}
	(void)rt_executor_request_shutdown(ensure_exec());
	return 0;
}

// The arm table and pending both exist before the initial transport enqueue.
// A saturated destination must therefore release through the pending exactly
// once; calling the earlier input-payload cleanup as well would double-drop.
static int run_far_select_enqueue_refusal(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 0);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor(ex, dst, 1, &anchor);
    if (channel == NULL) return fail("enqueue-refusal mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_bits[0] = 505;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 1;
    st.fill_queue = 1;

    if (!await_select(&st)) return fail("enqueue-refusal await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_QUEUE_FULL) {
        return fail("enqueue-refusal select must report QUEUE_FULL");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 1) {
        return fail("enqueue-refusal pending must consume the payload exactly once");
    }
    if (!channel_is_empty(ex, channel)) {
        return fail("enqueue-refusal must not touch the channel");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("enqueue-refusal lease release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A normal RECV winner returns the losing SEND payload to compiled code
// before the pending disowns/frees its arm table. The harness observes the
// raw handback buffer, then performs the one compiled losing-arm drop itself.
static int run_far_select_recv_winner_handback(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle send_anchor = {0};
    rt_far_task_handle recv_anchor = {0};
    void* send_channel = mint_channel_anchor(ex, dst, 0, &send_anchor);
    void* recv_channel = mint_channel_anchor(ex, dst, 1, &recv_anchor);
    if (send_channel == NULL || recv_channel == NULL) return fail("handback mint failed");
    rt_channel_send_blocking(recv_channel, &(uint64_t){303});

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = send_anchor;
    st.anchors[1] = recv_anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.anchor_ptrs[1] = &st.anchors[1];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.kinds[1] = SELECT_CHAN_RECV;
    st.send_bits[0] = 404;
    st.send_drop_ids[0] = DROP_SELECT_PAYLOAD;
    st.count = 2;

    if (!await_select(&st)) return fail("handback await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 || st.out_bits != 1) {
        return fail("handback row must choose the ready RECV arm");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("pending must disown, not drop, a returned losing payload");
    }
    if (st.send_bits[0] != 404) {
        return fail("losing SEND payload was not returned to caller buffer");
    }
    __surge_drop_result_call(DROP_SELECT_PAYLOAD, (void*)st.send_bits[0]);
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 1) {
        return fail("compiled losing-arm drop must consume returned payload once");
    }
    if (!channel_is_empty(ex, send_channel) || !channel_is_empty(ex, recv_channel)) {
        return fail("handback row left unexpected channel data");
    }
    if (rt_far_channel_release(ex, &send_anchor) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &recv_anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("handback lease release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

int main(int argc, char** argv) {
    if (argc != 2) return fail("usage: remote_publication_harness <mode>");
    if (strcmp(argv[1], "publish-other") == 0) return run_publish(1, 0);
    if (strcmp(argv[1], "self-crossing") == 0) return run_publish(0, 0);
    if (strcmp(argv[1], "stale-token") == 0) return run_publish(1, 1);
    if (strcmp(argv[1], "queue-full") == 0) return run_queue_full();
    if (strcmp(argv[1], "shutdown") == 0) return run_shutdown();
    if (strcmp(argv[1], "shutdown-queued-kinds") == 0) return run_shutdown_queued_kinds();
    if (strcmp(argv[1], "refusal-drop-queue-full") == 0) return run_refusal_drop(0);
    if (strcmp(argv[1], "refusal-drop-shutdown") == 0) return run_refusal_drop(1);
    if (strcmp(argv[1], "abandon-before-dispatch") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH);
    }
    if (strcmp(argv[1], "abandon-before-body-publish") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_BODY_PUBLISH);
    }
    if (strcmp(argv[1], "abandon-before-ack") == 0) {
        return run_abandon_window(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK);
    }
    if (strcmp(argv[1], "ack-rescue-drain") == 0) return run_ack_rescue_drain();
    if (strcmp(argv[1], "stale-request-before-body") == 0) return run_stale_request_before_body();
    if (strcmp(argv[1], "duplicate-request-after-handoff") == 0) return run_stale_redelivery(0);
    if (strcmp(argv[1], "stale-ack-after-resolution") == 0) return run_stale_redelivery(1);
    if (strcmp(argv[1], "immediate-refusal-queue-full") == 0) return run_immediate_refusal_drop(0);
    if (strcmp(argv[1], "immediate-refusal-shutdown") == 0) return run_immediate_refusal_drop(1);
    if (strcmp(argv[1], "immediate-cancel-unbound") == 0) {
        return run_immediate_cancel(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH, 0);
    }
    if (strcmp(argv[1], "immediate-cancel-bound") == 0) {
        return run_immediate_cancel(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH, 1);
    }
    if (strcmp(argv[1], "immediate-duplicate-request") == 0) return run_immediate_redelivery(0);
    if (strcmp(argv[1], "immediate-stale-reply") == 0) return run_immediate_redelivery(1);
    if (strcmp(argv[1], "anchored-stale-anchor") == 0) return run_anchored_stale_anchor();
    if (strcmp(argv[1], "anchored-happy-path") == 0) return run_anchored_happy_path();
    if (strcmp(argv[1], "anchored-cancel-bound") == 0) return run_anchored_cancel_bound();
    if (strcmp(argv[1], "anchored-cancel-unbound") == 0) return run_anchored_cancel_unbound();
    if (strcmp(argv[1], "far-select-cancel-vs-commit") == 0) {
        return run_far_select_cancel_vs_commit();
    }
    if (strcmp(argv[1], "far-select-cancel-before-dispatch") == 0) {
        return run_far_select_cancel_before_dispatch();
    }
    if (strcmp(argv[1], "far-select-double-cancel") == 0) return run_far_select_double_cancel();
    if (strcmp(argv[1], "far-select-refusal-after-shipped") == 0) {
        return run_far_select_refusal_after_shipped();
    }
    if (strcmp(argv[1], "far-select-initial-owner-mismatch") == 0) {
        return run_far_select_initial_owner_mismatch();
    }
	if (strcmp(argv[1], "far-select-initial-null-anchors") == 0) {
		return run_far_select_initial_null_anchors();
	}
    if (strcmp(argv[1], "far-select-enqueue-refusal") == 0) {
        return run_far_select_enqueue_refusal();
    }
    if (strcmp(argv[1], "far-select-recv-winner-handback") == 0) {
        return run_far_select_recv_winner_handback();
    }
    return fail("unknown mode");
}
`
