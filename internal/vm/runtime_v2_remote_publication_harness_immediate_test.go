//go:build runtime_v2_pending

package vm_test

// Modes for immediate-on and anchored-on: refusal, cancellation against a
// window, redelivery, and the anchored happy path.
const remotePublicationHarnessImmediate = `
static int run_immediate_refusal_drop(int shutdown_first) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.placement = rt_placement_shard(0);
    st.fill_queue = shutdown_first ? 0 : 1;
    st.shutdown_first = shutdown_first ? 1 : 0;
    st.droppable = 1;
    drop_expected_state = &child;
    if (!await_immediate(&st)) return fail("immediate refusal await failed");
    rt_remote_task_status want = shutdown_first ? RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN
                                                : RT_REMOTE_TASK_STATUS_QUEUE_FULL;
    if (st.status != want) return fail("immediate refusal status mismatch");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 1) {
        return fail("refused immediate execute must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("refused immediate execute must not run a body");
    }
    if (!shutdown_first) {
        (void)rt_executor_request_shutdown(ex);
    }
    return 0;
}

// Immediate-on caller-teardown split. Cancelling the caller while its
// execute request is UNBOUND (held at dispatch entry) must resolve the
// pending through the teardown sweep so the late dispatch refuses to
// create a body — the pending stays the state's sole owner and drops it
// exactly once. Cancelling while the request is BOUND (held between the
// body bind and its publication) routes exactly one cancel; the state
// handed off with the publication, so it must never drop through the
// pending, and the reply edge resolves once with no caller to wake.
static int run_immediate_cancel(rt_sync_point_id window, int expect_body) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.placement = rt_placement_shard(pin_shard(ex, 1));
    st.droppable = 1;
    drop_expected_state = &child;
    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st, rt_channel_opaque_word_ops());
    if (caller == NULL) return fail("immediate caller create failed");
    if (!wait_reached(window, 5000)) return fail("immediate window was never reached");
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled immediate caller must not resolve successfully");
    rt_sync_point_open();
    if (expect_body) {
        if (!wait_child(&child, 5000)) return fail("bound execute body did not run");
        // The reply edge resolves behind the body completion; give the
        // release chain a settle window before pinning the no-drop census.
        sleep_us(50000);
        if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
            return fail("handed-off state must not drop after bind");
        }
    } else {
        if (!wait_drops(1, 5000)) {
            return fail("unbound cancel must drop the state exactly once through the pending");
        }
        if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
            return fail("unbound cancel must refuse body creation");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Immediate-on redelivery after resolution. A duplicate of the ORIGINAL
// execute request still carries the request-scoped token, while the
// pending's handle was rebound to the body task's generation at the
// bind — so the duplicate must fail the token match, count one stale
// drop, answer stale-token into the (already resolved, hence no-op)
// reply edge, and release only its own message reference. A redelivered
// REPLY matches the resolved pending and must equally release only its
// reference. Neither may drop the body-owned state or create a second
// body.
static int run_immediate_redelivery(int redeliver_reply) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    uint32_t dst = pin_shard(ex, 1);
    st.placement = rt_placement_shard(dst);
    st.droppable = 1;
    drop_expected_state = &child;
    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st, rt_channel_opaque_word_ops());
    if (caller == NULL) return fail("immediate caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH, 5000)) {
        return fail("immediate publish window was never reached");
    }
    rt_remote_task_pending* req = wait_task_pending_shared(&st, 5000);
    if (req == NULL) return fail("pending missing in publish window");
    rt_remote_task_pending_add_ref(req);
    rt_remote_task_pending_add_ref(req);
    uint64_t request_id = req->request_id;
    uint32_t source_shard = req->source_shard_id;
    rt_shard* dst_shard = rt_runtime_shard(rt_executor_runtime(ex), dst);
    uint64_t stale_before = rt_transport_debug_snapshot(dst_shard).remote_task_stale_drops;
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 ||
        st.out_bits != 77) {
        return fail("immediate execute must resolve OK before the redelivery");
    }
    if (!wait_child(&child, 5000)) return fail("body did not run before the redelivery");
    rt_transport_msg dup = {0};
    if (redeliver_reply) {
        dup.kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY;
        dup.source_shard_id = req->handle.owner_shard_id;
        dup.target_shard_id = source_shard;
        dup.route_id = request_id;
        dup.generation = req->handle.generation;
    } else {
        dup.kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST;
        dup.source_shard_id = source_shard;
        dup.target_shard_id = dst;
        dup.route_id = request_id;
        // The original request's token: request-scoped generation, minted
        // before the bind rebound the handle to the body task.
        dup.generation = request_id;
    }
    rt_shard* target_shard =
        rt_runtime_shard(rt_executor_runtime(ex), dup.target_shard_id);
    dup.payload = req;
    if (rt_transport_enqueue(target_shard, &dup) != RT_TRANSPORT_STATUS_OK) {
        return fail("redelivery enqueue failed");
    }
    if (!wait_task_refs(req, 1, 5000)) {
        return fail("redelivered message was never fully released");
    }
    if (!redeliver_reply) {
        uint64_t stale_after = rt_transport_debug_snapshot(dst_shard).remote_task_stale_drops;
        if (stale_after != stale_before + 1) {
            return fail("duplicate request must count exactly one stale drop");
        }
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("redelivery must not drop the body-owned state");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 1) {
        return fail("redelivery must not create a second body");
    }
    rt_remote_task_pending_release(req);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Anchored immediate-on rows. The dispatch entry (rt_immediate_on_dispatch_execute)
// pins the anchor before a body exists (rt_immediate_on.c); a stale generation
// answers STALE_TOKEN without ever incrementing the entry's in-flight pin
// count, and every later exit (create-fail, teardown re-check, publish-fail,
// the owner-done reply edge) unpins exactly once. There is no counter the
// harness can read directly, so pin balance is proven through the registry's
// own reclaim rule (rt_far_channel.c: an entry with no active leases and no
// in-flight pins is freed): the driver mints the anchor (one lease, live
// count 1), lets the scenario run, then releases its own lease last. If a
// dispatch-side pin were still outstanding, the entry's in-flight count
// would be nonzero and the release would NOT bring the live count to zero;
// if the anchored path leaked no pin, the release is the final one and the
// entry reclaims immediately.
static int mint_anchor(rt_executor* ex, uint32_t owner_shard_id, rt_far_task_handle* out) {
    void* channel = rt_channel_new(0, rt_channel_opaque_word_ops(), 0);
    if (channel == NULL) {
        return 0;
    }
    rt_channel_bind_owner_shard(channel, owner_shard_id);
    return rt_far_channel_mint(ex, channel, owner_shard_id, out) == RT_REMOTE_TASK_STATUS_OK;
}

// Select-row variant: returns the local channel pointer too (the select
// rows drive/observe the channel directly from the driver thread to prove
// exactly-once delivery) and takes a capacity so a SEND arm can commit
// deterministically against a buffered channel with room.
static void* mint_channel_anchor(rt_executor* ex,
                                 uint32_t owner_shard_id,
                                 uint64_t capacity,
                                 rt_far_task_handle* out) {
    void* channel = rt_channel_new(capacity, rt_channel_opaque_word_ops(), 0);
    if (channel == NULL) {
        return NULL;
    }
    rt_channel_bind_owner_shard(channel, owner_shard_id);
    if (rt_far_channel_mint(ex, channel, owner_shard_id, out) != RT_REMOTE_TASK_STATUS_OK) {
        return NULL;
    }
    return channel;
}

// Row A: a stale-generation anchor refuses the execute before any body
// exists. The pending is the sole owner of the shipped state (dispatch bails
// at the pin check, well before the publication-accepted handoff), so it
// drops exactly once; the failed pin attempt never touches the entry's
// in-flight count, so releasing the original (still-active) lease reclaims
// the entry immediately.
static int run_anchored_stale_anchor(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, 0, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;
    st.anchor.generation++;  // corrupt a COPY; the original lease stays valid below

    if (!await_immediate(&st)) return fail("stale anchor await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return fail("stale anchor execute must answer STALE_TOKEN");
    }
    if (!wait_drops(1, 5000)) {
        return fail("stale anchor path must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("stale anchor must not run a body");
    }
    if (rt_far_channel_debug_live_count(ex) != 1) return fail("mint left no live entry to check");
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("original anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("stale-anchor dispatch attempt leaked a pin on the entry");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row B: the happy path. Execute against a live anchor, let the body run
// (it does not touch the channel), and prove the dispatch-time pin was
// already released at the reply edge (rt_remote_task_reply_owner_done unpins
// before answering OK) by releasing the driver's own lease afterward and
// checking the entry reclaims immediately.
static int run_anchored_happy_path(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, dst, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;

    if (!await_immediate(&st)) return fail("anchored happy-path await failed");
    if (st.status != RT_REMOTE_TASK_STATUS_OK || st.out_kind != 1 || st.out_bits != 77) {
        return fail("anchored happy path did not resolve OK");
    }
    if (!wait_child(&child, 5000)) return fail("anchored body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop on the anchored happy path");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("anchored happy path leaked the dispatch-time pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row C: caller cancelled while the anchored execute is BOUND (anchor
// pinned, body created, held at SP_IMMEDIATE_ON_BEFORE_PUBLISH before its
// publication). Mirrors the placement family's cancel-bound row: the body
// still runs (no suspension points; both completions are legal per the
// cancel-route contract), the reply edge resolves with no caller to wake,
// the handed-off state never drops through the pending, and the
// dispatch-time pin is released exactly once at that same reply edge.
static int run_anchored_cancel_bound(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, dst, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;

    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st, rt_channel_opaque_word_ops());
    if (caller == NULL) return fail("anchored caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH, 5000)) {
        return fail("anchored publish window was never reached");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled anchored caller must not resolve successfully");
    rt_sync_point_open();
    if (!wait_child(&child, 5000)) return fail("bound anchored body did not run");
    // The reply edge (and its unpin) resolves behind the body completion;
    // give the release chain a settle window before pinning the census.
    sleep_us(50000);
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop after the anchored bind");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("anchored cancel-bound path leaked the dispatch-time pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// The anchored twin of the unbound caller-teardown row: the sweep must
// resolve an UNBOUND anchored execute exactly like a placement one, so
// the late dispatch refuses at its snapshot check BEFORE the anchor pin
// — no body, no pin, the sole-owner pending drops the state once.
static int run_anchored_cancel_unbound(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    immediate_exec_state st;
    memset(&st, 0, sizeof(st));
    st.child = &child;
    st.droppable = 1;
    st.anchored = 1;
    drop_expected_state = &child;

    uint32_t dst = pin_shard(ex, 1);
    rt_far_task_handle anchor = {0};
    if (!mint_anchor(ex, dst, &anchor)) return fail("anchor mint failed");
    st.anchor = anchor;

    void* caller = __task_create(POLL_IMMEDIATE_CALLER, &st, rt_channel_opaque_word_ops());
    if (caller == NULL) return fail("anchored caller create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_DISPATCH, 5000)) {
        return fail("anchored dispatch window was never reached");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind == 1) return fail("cancelled anchored caller must not resolve successfully");
    rt_sync_point_open();
    if (!wait_drops(1, 5000)) {
        return fail("unbound anchored cancel must drop the state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("unbound anchored cancel must refuse body creation");
    }
    if (rt_far_channel_release(ex, &anchor) != RT_REMOTE_TASK_STATUS_OK) {
        return fail("anchor lease release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return fail("refused anchored dispatch must not touch the pin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Remote select rows (Epic 20 Task 7 rows 2-5): deterministic runtime races
// over Copy payloads on the same execute/reply pending discipline the
// anchored rows already prove. Every row uses a SEND arm into a freshly
// minted, buffered (capacity-1) far channel: rt_select_poll's SEND case
// pushes the value into the channel AS PART of committing the winner, so
// the commit is deterministic on the body's first poll (no parking), and
// the driver can verify exactly-once delivery afterward straight off the
// local channel object (mint_channel_anchor hands back both the far handle
// and the local pointer).

// Row 2: cancel-vs-commit race. The body is held at the new
// SP_FAR_SELECT_AFTER_COMMIT_BEFORE_REPLY window with the winner already
// committed (the send already landed in the channel). Cancelling the
// caller from there races a caller-side cancel against the still-unsent
// reply. The caller resolves as Cancelled immediately (rt_async_yield's
// own cancelled check short-circuits its retry poll -- the pending stays
// alive, still owner-registered, so the eventual reply is unaffected) --
// so the row tracks the PENDING directly (an extra driver-held reference)
// rather than trusting the caller's own outcome.`
