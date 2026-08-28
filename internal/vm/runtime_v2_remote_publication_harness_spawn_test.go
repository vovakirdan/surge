//go:build runtime_v2_pending

package vm_test

// Modes that exercise remote SPAWN: publication, queue pressure, shutdown,
// refusal, the abandon window, and stale requests either side of the body.
const remotePublicationHarnessSpawn = `
static int run_publish(uint32_t wanted_dst, int stale) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = pin_shard(ex, wanted_dst);
    if (!await_parent(&st)) return fail("publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_OK) return fail("publish did not return OK");
    if (st.validate_status != RT_REMOTE_SPAWN_STATUS_OK) return fail("handle did not validate");
    if (st.handle.owner_shard_id != st.dst) return fail("ack returned wrong owner shard");
    if (st.handle.generation == 0) return fail("ack returned empty generation");
    if (st.children_after != 0) return fail("remote child was enrolled under caller children");
    if (st.saw_pending == 0 || st.request_id == 0) return fail("publisher did not suspend for ack");
    if (!wait_child(&child, 5000)) return fail("remote child did not run");
    if (atomic_load_explicit(&child.owner, memory_order_acquire) != st.dst) {
        return fail("remote child owner mismatch");
    }
    uint32_t worker = atomic_load_explicit(&child.worker, memory_order_acquire);
    if (worker != st.dst && !(st.dst == 0 && worker == UINT32_MAX)) {
        return fail("remote child ran on non-owner shard");
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(ex), 0);
    rt_shard* dest = rt_runtime_shard(rt_executor_runtime(ex), st.dst);
    struct rt_transport_debug_snapshot src = rt_transport_debug_snapshot(source);
    struct rt_transport_debug_snapshot dst = rt_transport_debug_snapshot(dest);
    if (dst.transport_spawn_requests == 0) return fail("destination request counter missing");
    if (src.transport_spawn_acks == 0) return fail("source ack counter missing");
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_TRANSPORT_REPLY_WAIT_BEFORE_TASK_SUSPEND) == 0) {
        return fail("reply wait did not use task-suspend seam");
    }
    if (stale) {
        rt_far_task_handle bad = st.handle;
        bad.generation++;
        if (rt_remote_spawn_handle_validate(ex, &bad) != RT_REMOTE_SPAWN_STATUS_STALE_TOKEN) {
            return fail("fabricated stale token validated");
        }
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int run_queue_full(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = 0;
    st.fill_queue = 1;
    if (!await_parent(&st)) return fail("queue-full publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_QUEUE_FULL) return fail("queue full status mismatch");
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (snap.control_len == 0 || snap.data_len != RT_TRANSPORT_DATA_SLOT_CREDITS) {
        return fail("control lane was blocked by full data lane");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// RV2-DEBT-047: messages parked between the last steady-state drain and
// shutdown are valid traffic for every production kind — the shutdown
// drain must release them, never panic.
static int run_shutdown_queued_kinds(void) {
    rt_executor* ex = ensure_exec();
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    const rt_transport_msg_kind kinds[] = {
        RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
        RT_TRANSPORT_MSG_IMMEDIATE_ON_REPLY,
        RT_TRANSPORT_MSG_FAR_CHANNEL_CREATE_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SHARE_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REQUEST,
        RT_TRANSPORT_MSG_FAR_CHANNEL_SELECT_REPLY,
    };
    for (size_t i = 0; i < sizeof(kinds) / sizeof(kinds[0]); i++) {
        rt_transport_msg msg = {0};
        msg.kind = kinds[i];
        msg.target_shard_id = 0;
        if (rt_transport_enqueue(shard, &msg) != RT_TRANSPORT_STATUS_OK) {
            return fail("queued-kind enqueue failed");
        }
    }
    rt_remote_spawn_fail_all_pending(ex, RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    if (snap.data_len != 0 || snap.control_len != 0) {
        return fail("shutdown drain left queued messages behind");
    }
    return 0;
}

static int run_shutdown(void) {
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = 0;
    st.shutdown_first = 1;
    if (!await_parent(&st)) return fail("shutdown publisher await failed");
    if (st.status != RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN) {
        return fail("shutdown status mismatch");
    }
    return 0;
}

// Refusal edges with a droppable shipped state: the publish is refused
// (queue full or destination shutdown), so the pending — or the pre-link
// path — is the sole owner and must drop the state exactly once, before
// the caller observes the refusal.
static int run_refusal_drop(int shutdown_first) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = 0;
    st.fill_queue = shutdown_first ? 0 : 1;
    st.shutdown_first = shutdown_first ? 1 : 0;
    st.droppable = 1;
    drop_expected_state = box;
    if (!await_parent(&st)) return fail("refusal publisher await failed");
    rt_remote_spawn_status want = shutdown_first ? RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN
                                                 : RT_REMOTE_SPAWN_STATUS_QUEUE_FULL;
    if (st.status != want) return fail("refusal status mismatch");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 1) {
        return fail("refused publish must drop the shipped state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("refused publish must not run a body");
    }
    if (!shutdown_first) {
        (void)rt_executor_request_shutdown(ex);
    }
    return 0;
}

// Abandon edges: the caller-owned handle is abandoned while the dispatch
// lane is held at an armed window (dispatch entry / created-but-unpublished
// / published-but-unacked). In every window the request is already in
// flight, so the body still runs and owns the shipped state — the pending
// must NOT drop it (drop count stays zero), and the resolved-while-abandoned
// ack turns into an owner-routed release instead of waking a caller.
static int run_abandon_window(rt_sync_point_id window) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    st.abandon_mode = 1;
    drop_expected_state = box;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st, rt_channel_opaque_word_ops());
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(window, 5000)) return fail("armed window was never reached");
    if (!rt_remote_spawn_abandon_handle(&st.handle)) {
        return fail("abandon did not find the listed pending");
    }
    rt_sync_point_open();
    if (!wait_child(&child, 5000)) return fail("abandoned spawn body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop through the abandoned pending");
    }
    if (rt_sync_point_reached_count(window) == 0) {
        return fail("window count vanished");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row-4 remainder, pinned as what the runtime actually guarantees: an
// ack facing a SATURATED control lane does not fail the publication —
// enqueue_with_drain rescue-drains (control-first) and the ack lands, so
// a full lane can never orphan the handle or the handed-off state. The
// failure branch below the rescue is reachable only through transport
// shutdown; its release ordering stays pinned by the static guards.
// Single-shard execution is driven by the main thread inside
// rt_task_await, so the lane fill runs on a helper thread while main is
// held at the armed window.
typedef struct ack_failure_driver {
    rt_executor* ex;
    _Atomic uint32_t failed;
} ack_failure_driver;

static void* ack_failure_driver_main(void* arg) {
    ack_failure_driver* drv = (ack_failure_driver*)arg;
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK, 5000)) {
        atomic_store_explicit(&drv->failed, 1, memory_order_release);
        rt_sync_point_open();
        return NULL;
    }
    rt_shard* source = rt_runtime_shard(rt_executor_runtime(drv->ex), 0);
    if (!fill_control_lane(source, 0)) {
        atomic_store_explicit(&drv->failed, 2, memory_order_release);
    }
    rt_sync_point_open();
    return NULL;
}

static int run_ack_rescue_drain(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = 0;
    st.droppable = 1;
    drop_expected_state = box;
    ack_failure_driver drv;
    memset(&drv, 0, sizeof(drv));
    drv.ex = ex;
    pthread_t driver;
    if (pthread_create(&driver, NULL, ack_failure_driver_main, &drv) != 0) {
        return fail("driver thread create failed");
    }
    remote_publish_state* stp = &st;
    if (!await_parent(stp)) {
        (void)pthread_join(driver, NULL);
        return fail("ack-failure publisher await failed");
    }
    (void)pthread_join(driver, NULL);
    if (atomic_load_explicit(&drv.failed, memory_order_acquire) != 0) {
        return fail(atomic_load_explicit(&drv.failed, memory_order_acquire) == 1
                        ? "ack window was never reached"
                        : "control lane did not saturate");
    }
    if (st.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return fail("saturated control lane must not fail the publication (rescue drain)");
    }
    if (!wait_child(&child, 5000)) return fail("published body did not run");
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("handed-off state must not drop across the ack rescue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Stale request before body creation: the pending resolves while its
// request message is still waiting at dispatch entry. The dispatch must
// step aside (no body), leaving the pending the sole owner — the state
// drops exactly once through the final pending release.
static int run_stale_request_before_body(void) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    drop_expected_state = box;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st, rt_channel_opaque_word_ops());
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_DISPATCH, 5000)) {
        return fail("dispatch window was never reached");
    }
    rt_remote_spawn_fail_all_pending(ex, RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN);
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_SPAWN_STATUS_DESTINATION_SHUTDOWN) {
        return fail("resolved-before-dispatch must surface the failure status");
    }
    if (!wait_drops(1, 5000)) {
        return fail("sole-owner pending must drop the state exactly once");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return fail("stale request must not create a body");
    }
    return 0;
}

// Duplicate/stale delivery after resolution: a second copy of an already
// resolved request (or its ack) must release only its own message
// reference — never drop the body-owned state, never create a second
// body. The extra pending reference taken in the ack window models the
// redelivered copy's payload reference.
static int run_stale_redelivery(int redeliver_ack) {
    rt_executor* ex = ensure_exec();
    remote_child_state child;
    memset(&child, 0, sizeof(child));
    remote_state_box* box = remote_child_box(&child);
    if (box == NULL) return fail("child state box alloc failed");
    remote_publish_state st;
    memset(&st, 0, sizeof(st));
    st.child = box;
    st.dst = pin_shard(ex, 1);
    st.droppable = 1;
    drop_expected_state = box;
    void* publisher = __task_create(POLL_REMOTE_PUBLISHER, &st, rt_channel_opaque_word_ops());
    if (publisher == NULL) return fail("publisher task create failed");
    if (!wait_reached(RT_SYNC_POINT_SP_REMOTE_SPAWN_BEFORE_ACK, 5000)) {
        return fail("ack window was never reached");
    }
    rt_remote_spawn_pending* req = wait_pending_shared(&st, 5000);
    if (req == NULL) return fail("pending missing in ack window");
    // Two references while the window pins the pending alive: one models
    // the redelivered copy's payload reference (consumed by its dispatch),
    // one keeps this driver's view of the refcount valid until the final
    // assertions are done.
    remote_spawn_pending_add_ref(req);
    remote_spawn_pending_add_ref(req);
    uint32_t source_shard = req->source_shard_id;
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(publisher, &kind, &bits);
    if (kind != 1 || st.status != RT_REMOTE_SPAWN_STATUS_OK) {
        return fail("publication must resolve OK before the redelivery");
    }
    if (!wait_child(&child, 5000)) return fail("body did not run before the redelivery");
    uint32_t target = redeliver_ack ? source_shard : st.dst;
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), target);
    rt_transport_msg dup = {0};
    dup.kind = redeliver_ack ? RT_TRANSPORT_MSG_REMOTE_SPAWN_ACK
                             : RT_TRANSPORT_MSG_REMOTE_SPAWN_REQUEST;
    dup.source_shard_id = source_shard;
    dup.target_shard_id = target;
    dup.route_id = rt_remote_spawn_pending_request_id(req);
    dup.payload = req;
    if (rt_transport_enqueue(shard, &dup) != RT_TRANSPORT_STATUS_OK) {
        return fail("redelivery enqueue failed");
    }
    // The redelivery's own pending_release is the last step of its
    // dispatch: refs falling back to the driver's single reference
    // happens-after everything that dispatch did with the pending.
    if (!wait_refs(req, 1, 5000)) {
        return fail("redelivered message was never dispatched");
    }
    if (atomic_load_explicit(&drop_calls, memory_order_acquire) != 0) {
        return fail("redelivery must not drop the body-owned state");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 1) {
        return fail("redelivery must not create a second body");
    }
    remote_spawn_pending_release(req);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

static int await_immediate(immediate_exec_state* st) {
    void* task = __task_create(POLL_IMMEDIATE_CALLER, st, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}

static int await_select(select_exec_state* st) {
    void* task = __task_create(POLL_SELECT_CALLER, st, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(task, &kind, &bits);
    if (kind != 1) {
        return 0;
    }
    return bits == (uint64_t)st->status;
}

// Immediate-on refusal edges: no far handle exists for the execute/reply
// category, so the pending (or the pre-link path) is the sole owner of the
// shipped state — it drops exactly once, no body runs, and the caller
// resumes with the refusal status.`
