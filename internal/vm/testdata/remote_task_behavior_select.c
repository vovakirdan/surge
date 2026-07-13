#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_transport.h"

#include <string.h>

// Remote-select rows: the owner-side proxy selector over pinned arm
// channels. The reply is the winner index — the same value local select's
// lowering stores — and recv arms follow the local readiness contract
// (the probe consumes without surfacing the value).

static void poll_rtb_select_caller(rtb_select_state* state) {
    state->status = rt_far_channel_select(state->anchors,
                                          state->kinds,
                                          state->bits,
                                          state->count,
                                          POLL_RTB_SELECT_BODY,
                                          state,
                                          &state->pending,
                                          &state->result_kind,
                                          &state->result_bits);
    atomic_store_explicit(&state->visible_pending, state->pending, memory_order_release);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state);
    }
    rt_async_return(state, (uint64_t)state->status);
}

// The shape the compiled remote select lowers to in the vertical: one
// select operation as the whole body.
static void poll_rtb_select_body(void* state) {
    uint64_t winner = rt_anchored_channel_select();
    rt_async_return(state, winner);
}

void rtb_select_poll_dispatch(uint64_t id) {
    if (id == POLL_RTB_SELECT_CALLER) {
        poll_rtb_select_caller((rtb_select_state*)__task_state());
    }
    if (id == POLL_RTB_SELECT_BODY) {
        poll_rtb_select_body(__task_state());
    }
}

static int rtb_select_counters_ok(rt_executor* ex, uint64_t expected_requests) {
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (destination.far_channel_select_requests != expected_requests ||
        source.far_channel_select_replies != expected_requests) {
        return 0;
    }
    return source.unsupported_fallback_attempts == 0 &&
           destination.unsupported_fallback_attempts == 0;
}

// Row 1 (ready-before-execute): an arm is already ready when the request
// dispatches, so the body's first poll decides without parking.
int rtb_mode_select_ready_first(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 2) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 2)) {
        return rtb_fail("ready-first mint failed");
    }
    void* raw_b = rt_far_channel_resolve(ex, &chan_b.handle);
    if (raw_b == NULL) {
        return rtb_fail("ready-first resolve failed");
    }
    rt_channel_send_blocking(raw_b, 33);

    rtb_select_state state;
    memset(&state, 0, sizeof(state));
    state.anchors[0] = chan_a.handle;
    state.anchors[1] = chan_b.handle;
    state.kinds[0] = SELECT_CHAN_RECV;
    state.kinds[1] = SELECT_CHAN_RECV;
    state.count = 2;
    void* caller = __task_create(POLL_RTB_SELECT_CALLER, &state);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("ready-first select did not complete");
    }
    if (state.result_kind != 1 || state.result_bits != 1) {
        return rtb_fail("ready-first winner is not the ready arm");
    }
    if (!rtb_select_counters_ok(ex, 1)) {
        return rtb_fail("ready-first trace counters mismatch");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("ready-first release failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return rtb_fail("ready-first census found residue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 2 (park-vs-send): the selector parks with no arm ready; a send to arm
// zero wakes it exactly once and the reply names that arm.
int rtb_mode_select_park_then_send(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("park-then-send mint failed");
    }
    rtb_select_state state;
    memset(&state, 0, sizeof(state));
    state.anchors[0] = chan_a.handle;
    state.anchors[1] = chan_b.handle;
    state.kinds[0] = SELECT_CHAN_RECV;
    state.kinds[1] = SELECT_CHAN_RECV;
    state.count = 2;
    void* caller = __task_create(POLL_RTB_SELECT_CALLER, &state);

    rt_remote_task_pending* pending = NULL;
    for (uint32_t i = 0; i < 4000 && pending == NULL; i++) {
        pending = atomic_load_explicit(&state.visible_pending, memory_order_acquire);
        if (pending == NULL) {
            rtb_sleep_us(1000);
        }
    }
    if (pending == NULL) {
        return rtb_fail("park-then-send request never became visible");
    }
    uint64_t body_id = 0;
    for (uint32_t i = 0; i < 4000 && body_id == 0; i++) {
        body_id = pending->handle.task_id;
        if (body_id == 0) {
            rtb_sleep_us(1000);
        }
    }
    if (body_id == 0) {
        return rtb_fail("park-then-send body was never bound");
    }
    const rt_task* body = get_task(ex, body_id);
    int parked = 0;
    for (uint32_t i = 0; i < 4000 && !parked; i++) {
        parked = body != NULL && task_status_load(body) == TASK_WAITING;
        if (!parked) {
            rtb_sleep_us(1000);
        }
    }
    if (!parked) {
        return rtb_fail("park-then-send selector never parked");
    }

    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("park-then-send resolve failed");
    }
    rt_channel_send_blocking(raw_a, 42);

    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("park-then-send select did not complete");
    }
    if (state.result_kind != 1 || state.result_bits != 0) {
        return rtb_fail("park-then-send winner is not the sent arm");
    }
    if (!rtb_select_counters_ok(ex, 1)) {
        return rtb_fail("park-then-send trace counters mismatch");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("park-then-send release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 3 (ready-vs-ready tie-break): both arms ready; the winner matches the
// local rt_select_poll scan order (lowest index), exactly one arm is
// consumed, and the loser's value stays put.
int rtb_mode_select_tie_break(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 2) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 2)) {
        return rtb_fail("tie-break mint failed");
    }
    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    void* raw_b = rt_far_channel_resolve(ex, &chan_b.handle);
    if (raw_a == NULL || raw_b == NULL) {
        return rtb_fail("tie-break resolve failed");
    }
    rt_channel_send_blocking(raw_a, 7);
    rt_channel_send_blocking(raw_b, 9);

    rtb_select_state state;
    memset(&state, 0, sizeof(state));
    state.anchors[0] = chan_a.handle;
    state.anchors[1] = chan_b.handle;
    state.kinds[0] = SELECT_CHAN_RECV;
    state.kinds[1] = SELECT_CHAN_RECV;
    state.count = 2;
    void* caller = __task_create(POLL_RTB_SELECT_CALLER, &state);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("tie-break select did not complete");
    }
    if (state.result_kind != 1 || state.result_bits != 0) {
        return rtb_fail("tie-break winner does not match the local scan order");
    }
    uint64_t leftover = 0;
    if (rt_channel_try_recv(raw_a, &leftover)) {
        return rtb_fail("tie-break consumed arm still holds a value");
    }
    if (!rt_channel_try_recv(raw_b, &leftover) || leftover != 9) {
        return rtb_fail("tie-break loser arm lost its value");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("tie-break release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
