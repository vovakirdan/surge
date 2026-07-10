#include "remote_task_behavior.h"
#include "rt_placement.h"

#include <string.h>

// Immediate `on placement` execute/reply behavior rows.

static int rtb_await_exec(rtb_execute_state* state, void* task) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task, &kind, &bits);
    return state->status == RT_REMOTE_TASK_STATUS_OK;
}

static void* rtb_start_exec(rtb_execute_state* state, uint64_t placement, rtb_child_state* child) {
    memset(state, 0, sizeof(*state));
    state->placement = placement;
    state->body_poll_id = POLL_RTB_CHILD;
    state->body_state = child;
    return __task_create(POLL_RTB_EXECUTE, state);
}

// Trace equivalence + owner proof: one execute request on the destination,
// one reply on the source, zero publication-ack pairs, body ran on the
// destination shard.
int rtb_mode_basic(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    atomic_store_explicit(&child.gate, 1, memory_order_relaxed);
    rtb_execute_state state;
    void* task = rtb_start_exec(&state, rt_placement_shard(1), &child);
    if (!rtb_await_exec(&state, task)) {
        return rtb_fail("immediate basic execute failed");
    }
    if (state.result_kind != 1 || state.result_bits != 91) {
        return rtb_fail("immediate basic result mismatch");
    }
    if (atomic_load_explicit(&child.owner, memory_order_acquire) != 1) {
        return rtb_fail("immediate basic body did not run on destination shard");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (destination.immediate_on_execute_requests != 1 || source.immediate_on_replies != 1) {
        return rtb_fail("immediate basic trace counters mismatch");
    }
    if (source.transport_spawn_requests != 0 || source.transport_spawn_acks != 0 ||
        destination.transport_spawn_requests != 0 || destination.transport_spawn_acks != 0) {
        return rtb_fail("immediate basic used a publication-ack pair");
    }
    if (source.unsupported_fallback_attempts != 0 ||
        destination.unsupported_fallback_attempts != 0) {
        return rtb_fail("a crossing attempted an unsupported local fallback");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Distributed policy: destination differs from the caller shard at shards>1.
int rtb_mode_distributed(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    atomic_store_explicit(&child.gate, 1, memory_order_relaxed);
    rtb_execute_state state;
    void* task = rtb_start_exec(&state, rt_placement_distributed(), &child);
    if (!rtb_await_exec(&state, task)) {
        return rtb_fail("immediate distributed execute failed");
    }
    if (state.result_kind != 1 || state.result_bits != 91) {
        return rtb_fail("immediate distributed result mismatch");
    }
    if (atomic_load_explicit(&child.owner, memory_order_acquire) == 0) {
        return rtb_fail("immediate distributed body ran on the caller shard");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Out-of-range shard(id): deterministic non-executing Cancelled resume, the
// body never runs, no execute request leaves the caller.
int rtb_mode_invalid_shard(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    atomic_store_explicit(&child.gate, 1, memory_order_relaxed);
    rtb_execute_state state;
    void* task = rtb_start_exec(&state, rt_placement_shard(4096), &child);
    if (!rtb_await_exec(&state, task)) {
        return rtb_fail("immediate invalid-shard execute failed");
    }
    if (state.result_kind != 2) {
        return rtb_fail("immediate invalid-shard did not resume Cancelled");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return rtb_fail("immediate invalid-shard body ran");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    if (source.immediate_on_execute_requests != 0 || source.immediate_on_replies != 0) {
        return rtb_fail("immediate invalid-shard sent transport messages");
    }
    if (rt_placement_debug_snapshot(runtime).invalid_shard_resolutions != 1) {
        return rtb_fail("immediate invalid-shard resolver counter missing");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Stale execute request: fabricated wrong-generation envelope is rejected
// with a stale drop; the pending resolves STALE_TOKEN without running a body.
int rtb_mode_immediate_stale(void) {
    rt_executor* ex = ensure_exec();
    rt_runtime* runtime = rt_executor_runtime(ex);
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rt_far_task_handle route = {.task_id = 0, .generation = 0, .owner_shard_id = 1, ._pad = 0};
    rt_remote_task_pending* bad =
        rt_remote_task_pending_new(ex, &route, 0, RT_REMOTE_TASK_OP_EXECUTE, 1);
    if (bad == NULL) {
        return rtb_fail("stale execute pending allocation failed");
    }
    bad->handle.generation = bad->request_id;
    bad->body_poll_fn_id = POLL_RTB_CHILD;
    bad->body_state = &child;
    rt_remote_task_pending_add_ref(bad);
    rt_transport_msg msg = {
        .kind = RT_TRANSPORT_MSG_IMMEDIATE_ON_EXECUTE_REQUEST,
        .source_shard_id = 0,
        .target_shard_id = 1,
        .route_id = bad->request_id,
        .generation = bad->handle.generation + 1,
        .payload = bad,
        .payload_len = 0,
    };
    (void)rt_remote_task_dispatch_message(ex, &msg);
    if (rt_remote_task_pending_snapshot(bad, NULL, NULL) != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("stale execute request was not rejected");
    }
    if (atomic_load_explicit(&child.ran, memory_order_acquire) != 0) {
        return rtb_fail("stale execute request ran a body");
    }
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (destination.remote_task_stale_drops != 1) {
        return rtb_fail("stale execute drop counter missing");
    }
    rt_remote_task_pending_consume(bad);
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Caller cancelled while the execute is in flight: the caller resumes
// exactly once through the cancel path (a cancelled task cannot re-park),
// the routed cancel reaches the destination body, and the orphaned reply
// edge is consumed exactly once without a waiter.
int rtb_mode_cancel_race(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rtb_execute_state state;
    void* task = rtb_start_exec(&state, rt_placement_shard(1), &child);
    uint64_t caller_id = ((rt_task*)task)->id;
    for (uint32_t i = 0; i < 5000; i++) {
        if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH) != 0) {
            break;
        }
        rtb_sleep_us(1000);
    }
    if (rt_sync_point_reached_count(RT_SYNC_POINT_SP_IMMEDIATE_ON_BEFORE_PUBLISH) != 1) {
        return rtb_fail("cancel race window not reached");
    }
    rt_control_lock(ex);
    cancel_task(ex, caller_id);
    rt_control_unlock(ex);
    rt_sync_point_open();
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task, &kind, &bits);
    if (kind != 2) {
        return rtb_fail("cancelled caller did not resume through the cancel path");
    }
    if (!rtb_wait_u32(&child.done, 1, 5000)) {
        return rtb_fail("destination body did not finish after routed cancel");
    }
    if (atomic_load_explicit(&child.cancelled, memory_order_acquire) != 1) {
        return rtb_fail("destination body did not observe cancellation");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source = {0};
    for (uint32_t i = 0; i < 5000; i++) {
        source = rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
        if (source.immediate_on_replies != 0) {
            break;
        }
        rtb_sleep_us(1000);
    }
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (source.immediate_on_replies != 1 || source.remote_task_stale_drops != 0 ||
        destination.remote_task_stale_drops != 0) {
        return rtb_fail("cancel race consumed more than one reply edge");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Self-crossing at one shard: the destination equals the caller shard and
// the executor has a single worker, yet the execute still travels the
// transport path — the counters prove there is no hidden local shortcut and
// the reply wait is a task suspend, not a shard park.
int rtb_mode_immediate_self_crossing(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    atomic_store_explicit(&child.gate, 1, memory_order_relaxed);
    rtb_execute_state state;
    void* task = rtb_start_exec(&state, rt_placement_shard(0), &child);
    if (!rtb_await_exec(&state, task)) {
        return rtb_fail("self-crossing execute failed");
    }
    if (state.result_kind != 1 || state.result_bits != 91) {
        return rtb_fail("self-crossing result mismatch");
    }
    if (atomic_load_explicit(&child.owner, memory_order_acquire) != 0) {
        return rtb_fail("self-crossing body did not run on the caller shard");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot shard0 =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    if (shard0.immediate_on_execute_requests != 1 || shard0.immediate_on_replies != 1) {
        return rtb_fail("self-crossing bypassed the transport path");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Shutdown wakes an execute reply waiter that will never get a reply.
int rtb_mode_shutdown(void) {
    rt_executor* ex = ensure_exec();
    rtb_child_state child;
    memset(&child, 0, sizeof(child));
    rtb_execute_state state;
    void* task = rtb_start_exec(&state, rt_placement_shard(1), &child);
    rt_remote_task_pending* pending = NULL;
    for (uint32_t i = 0; i < 5000 && pending == NULL; i++) {
        pending = atomic_load_explicit(&state.visible_pending, memory_order_acquire);
        rtb_sleep_us(1000);
    }
    if (pending == NULL) {
        return rtb_fail("shutdown execute did not become pending");
    }
    (void)task;
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        return rtb_fail("executor shutdown failed");
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) !=
        RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN) {
        return rtb_fail("shutdown did not fail the execute reply waiter");
    }
    return 0;
}
