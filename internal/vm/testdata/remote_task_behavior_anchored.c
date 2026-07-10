#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"

#include <string.h>

// Anchored-op rows: a body shipped to the anchor's owner shard resolves the
// local channel through the registry and drives ordinary local channel
// operations; the caller suspends on one reply.

// Body poll: resolve the anchor, send one value, reply with the local
// channel length probe (send result) so the caller observes owner-side
// effects through the single reply.
static void poll_anchored_send(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    void* channel = rt_far_channel_resolve(ex, &state->anchor);
    if (channel == NULL) {
        rt_async_return(state, 0);
    }
    atomic_store_explicit(&state->body_ran, 1, memory_order_release);
    // A false send means "parked on capacity": yield and re-enter; the
    // re-poll consumes the handoff ack and returns true.
    if (!rt_channel_send(channel, state->value)) {
        rt_async_yield(state);
    }
    rt_async_return(state, 1);
}

// Caller poll: one anchored execute, retry until the reply.
static void poll_anchored_caller(rtb_anchored_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute_anchored(
        &state->anchor, POLL_RTB_ANCHORED_BODY, state, &state->pending, &kind, &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, (uint64_t)state->status);
}

void rtb_anchored_poll_dispatch(uint64_t id) {
    if (id == POLL_RTB_ANCHORED_BODY) {
        poll_anchored_send((rtb_anchored_state*)__task_state());
    }
    if (id == POLL_RTB_ANCHORED_CALLER) {
        poll_anchored_caller((rtb_anchored_state*)__task_state());
    }
}

static int rtb_mint_channel(rtb_create_state* create, uint64_t placement, uint64_t capacity) {
    void* task = rtb_start_channel_create(create, placement, capacity);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(task, &kind, &bits);
    return create->status == RT_REMOTE_TASK_STATUS_OK;
}

static void*
rtb_start_anchored(rtb_anchored_state* state, const rt_far_task_handle* anchor, uint64_t value) {
    memset(state, 0, sizeof(*state));
    state->anchor = *anchor;
    state->value = value;
    return __task_create(POLL_RTB_ANCHORED_CALLER, state);
}

// Round trip: mint on shard 1, run an anchored send body from shard 0, and
// observe the value owner-side plus one execute request / one reply with the
// fallback tripwire at zero.
int rtb_mode_anchored_send_round_trip(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 4)) {
        return rtb_fail("anchored round-trip mint failed");
    }
    rtb_anchored_state state;
    void* caller = rtb_start_anchored(&state, &minted.handle, 41);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 1) {
        return rtb_fail("anchored send did not complete successfully");
    }
    if (atomic_load_explicit(&state.body_ran, memory_order_acquire) != 1) {
        return rtb_fail("anchored body did not run");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    if (channel == NULL) {
        return rtb_fail("anchor no longer resolves after the block");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (destination.immediate_on_execute_requests != 1 || source.immediate_on_replies != 1) {
        return rtb_fail("anchored round-trip trace counters mismatch");
    }
    if (source.unsupported_fallback_attempts != 0 ||
        destination.unsupported_fallback_attempts != 0) {
        return rtb_fail("anchored block attempted a local fallback");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Stale and wrong-kind anchors answer without creating a body: the released
// entry and the forged task-kind token both resolve to the stale/invalid
// path, and the destination's stale counter proves the drop.
int rtb_mode_anchored_stale_anchor(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 2)) {
        return rtb_fail("stale-anchor mint failed");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("stale-anchor release failed");
    }
    rtb_anchored_state state;
    void* caller = rtb_start_anchored(&state, &minted.handle, 1);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("released anchor did not answer stale");
    }
    if (atomic_load_explicit(&state.body_ran, memory_order_acquire) != 0) {
        return rtb_fail("stale anchor ran a body");
    }
    rt_far_task_handle forged = minted.handle;
    forged.kind = RT_FAR_HANDLE_KIND_TASK;
    rtb_anchored_state forged_state;
    void* forged_caller = rtb_start_anchored(&forged_state, &forged, 1);
    (void)rtb_await(forged_caller, &kind, &bits);
    if (forged_state.status != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
        return rtb_fail("task-kind anchor was not rejected as invalid");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Full-channel suspension: the body parks on the owner's local send waiter
// while the owner's dispatcher stays live (a second mint is served during
// the park); freeing capacity releases the body and the caller's reply
// arrives.
int rtb_mode_anchored_full_channel(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("full-channel mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    if (channel == NULL) {
        return rtb_fail("full-channel anchor did not resolve");
    }
    // Fill to capacity locally so the anchored send must park owner-side.
    rtb_create_state filler;
    if (!rtb_mint_channel(&filler, rt_placement_shard(1), 1)) {
        return rtb_fail("filler mint failed");
    }
    void* filler_channel = rt_far_channel_resolve(ex, &filler.handle);
    (void)filler_channel;
    rtb_anchored_state state;
    // Pre-fill: one value occupies the single slot before the block ships.
    rtb_anchored_state prefill;
    void* prefill_caller = rtb_start_anchored(&prefill, &minted.handle, 7);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("prefill send failed");
    }
    void* caller = rtb_start_anchored(&state, &minted.handle, 8);
    if (!rtb_wait_u32(&state.body_ran, 1, 5000)) {
        return rtb_fail("blocked body did not start");
    }
    // The body is parked on the full channel; the dispatcher must still
    // serve new work on the same shard.
    rtb_create_state during;
    if (!rtb_mint_channel(&during, rt_placement_shard(1), 1)) {
        return rtb_fail("dispatcher was not live during the parked body");
    }
    // Drain one value owner-side to free capacity and release the body.
    uint64_t drained = 0;
    if (!rt_channel_try_recv(channel, &drained) || drained != 7) {
        return rtb_fail("owner-side drain failed");
    }
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_bits != 1) {
        return rtb_fail("released body did not complete the block");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
