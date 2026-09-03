//go:build runtime_v2_pending

package vm_test

// The far-select rows about a PAYLOAD's own ownership: what happens to the
// value an arm carries when the arm wins, when it loses, and when it is wider
// than the machine word the arm table used to be.
const remotePublicationHarnessFarSelectPayload = `
// A SEND arm whose payload is WIDER than a machine word: two words, moved
// through the select and out the other side intact.
//
// This row exists because of what the arm table used to be. A payload rode in
// a uint64_t beside a numeric drop id, so a value this wide could not travel
// at all without being boxed first -- and the box is the representation this
// step removes. The arm owns a typed cell now, sized by the element's own
// descriptor, so the value moves once on the way in and once on the way out
// and is never copied into a pointer-shaped hole.
//
// Both halves are asserted, because a cell that can be written and not read
// passes every test that does not round-trip: the words arrive in the
// channel EXACTLY as sent, and the payload is not destroyed on the way (the
// winner's value became the channel's, and destroying it would be the double
// ownership this whole layer exists to prevent).
static int run_far_select_wide_payload(void) {
    rt_executor* ex = ensure_exec();
    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    void* channel = mint_channel_anchor_typed(ex, dst, 1, WIDE_SELECT_PAYLOAD, &anchor);
    if (channel == NULL) return fail("wide select channel mint failed");

    select_exec_state st;
    memset(&st, 0, sizeof(st));
    st.anchors[0] = anchor;
    st.anchor_ptrs[0] = &st.anchors[0];
    st.kinds[0] = SELECT_CHAN_SEND;
    st.send_type_ids[0] = WIDE_SELECT_PAYLOAD;
    st.wide_send[0] = 0x1122334455667788ULL;
    st.wide_send[1] = 0x99AABBCCDDEEFF00ULL;
    st.wide_arm = 1;
    st.count = 1;

    if (!await_select(&st)) return fail("wide payload await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 || st.out_bits != 0) {
        return fail("wide payload row must commit its only SEND arm");
    }
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 0) {
        return fail("a committed wide payload belongs to its channel, not to a drop");
    }
    if (st.wide_send[0] != 0 || st.wide_send[1] != 0) {
        return fail("staging must EMPTY the caller's storage, not copy out of it");
    }
    uint64_t received[2] = {0, 0};
    if (!rt_channel_try_recv(channel, received)) {
        return fail("wide payload never reached the channel");
    }
    if (received[0] != 0x1122334455667788ULL || received[1] != 0x99AABBCCDDEEFF00ULL) {
        return fail("wide payload did not arrive intact");
    }
    rt_value_drop_in_place_detached(__surge_value_ops_for(WIDE_SELECT_PAYLOAD), received);
    if (atomic_load_explicit(&payload_drop_calls, memory_order_acquire) != 1) {
        return fail("the received wide payload must be destroyed exactly once");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("wide payload lease release failed");
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
    st.send_type_ids[0] = DROP_SELECT_PAYLOAD;
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
    // What compiled code does with a returned loser: destroy it through the
    // descriptor its type names, in the storage it was returned to.
    rt_value_drop_in_place_detached(__surge_value_ops_for(DROP_SELECT_PAYLOAD), &st.send_bits[0]);
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

static int run_mode(int argc, char** argv) {
    if (argc != 2) return fail("usage: remote_publication_harness <mode>");
    if (strcmp(argv[1], "publish-other") == 0) return run_publish(1, 0);
    if (strcmp(argv[1], "self-crossing") == 0) return run_publish(0, 0);
    if (strcmp(argv[1], "stale-token") == 0) return run_publish(1, 1);
    if (strcmp(argv[1], "queue-full") == 0) return run_queue_full();
    if (strcmp(argv[1], "shutdown") == 0) return run_shutdown();
    if (strcmp(argv[1], "shutdown-queued-kinds") == 0) return run_shutdown_queued_kinds();
    if (strcmp(argv[1], "refusal-drop-shutdown") == 0) return run_refusal_drop_shutdown();
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
    if (strcmp(argv[1], "far-select-commit-vs-shutdown-sweep") == 0) {
        return run_far_select_commit_vs_shutdown_sweep();
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
    if (strcmp(argv[1], "far-select-wide-payload") == 0) {
        return run_far_select_wide_payload();
    }
    return fail("unknown mode");
}

// Every mode that passes also leaves nothing resident on the source side:
// the resident-byte balance is the byte half of the exact-return contract,
// and one check after every mode is how every edge the harness takes gets
// it, rather than the edges somebody remembered to assert.
int main(int argc, char** argv) {
    int rc = run_mode(argc, argv);
    if (rc != 0) {
        return rc;
    }
    return resident_quiescent_after(argv[1]);
}
`
