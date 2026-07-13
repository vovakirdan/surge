#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_transport.h"

#include <string.h>

// Remote-select race rows: stale-wake absorption, lease invalidation under
// pins, sibling-lease selector isolation, caller teardown with an orphaned
// reply, and owner teardown with a parked selector.

// Shared with remote_task_behavior_select.c through the header: the park
// driver below repeats its shape for two-channel selects whose state the
// row owns directly.
static int rtb_select_park2(rt_executor* ex,
                            const rt_far_task_handle* arm0,
                            const rt_far_task_handle* arm1,
                            rtb_select_state* state,
                            void** out_caller) {
    memset(state, 0, sizeof(*state));
    state->anchors[0] = *arm0;
    state->anchors[1] = *arm1;
    state->kinds[0] = SELECT_CHAN_RECV;
    state->kinds[1] = SELECT_CHAN_RECV;
    state->count = 2;
    void* caller = __task_create(POLL_RTB_SELECT_CALLER, state);
    if (out_caller != NULL) {
        *out_caller = caller;
    }
    rt_remote_task_pending* pending = NULL;
    for (uint32_t i = 0; i < 4000 && pending == NULL; i++) {
        pending = atomic_load_explicit(&state->visible_pending, memory_order_acquire);
        if (pending == NULL) {
            rtb_sleep_us(1000);
        }
    }
    if (pending == NULL) {
        return rtb_fail("select race request never became visible");
    }
    uint64_t body_id = 0;
    for (uint32_t i = 0; i < 4000 && body_id == 0; i++) {
        body_id = pending->handle.task_id;
        if (body_id == 0) {
            rtb_sleep_us(1000);
        }
    }
    if (body_id == 0) {
        return rtb_fail("select race body was never bound");
    }
    const rt_task* body = get_task(ex, body_id);
    for (uint32_t i = 0; i < 4000; i++) {
        if (body != NULL && task_status_load(body) == TASK_WAITING) {
            return 0;
        }
        rtb_sleep_us(1000);
    }
    return rtb_fail("select race selector never parked");
}

// Row 10 (stale-generation wake): a spurious wake of the parked selector
// body re-arms the same registrations and is absorbed — the next real send
// still answers the right arm through one request/reply pair.
int rtb_mode_select_stale_wake(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("stale-wake mint failed");
    }
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &state, &caller) != 0) {
        return 1;
    }
    rt_remote_task_pending* pending =
        atomic_load_explicit(&state.visible_pending, memory_order_acquire);
    rtb_wake(ex, pending->handle.task_id);
    rtb_sleep_us(20000);
    const rt_task* body = get_task(ex, pending->handle.task_id);
    if (body == NULL || task_status_load(body) != TASK_WAITING) {
        return rtb_fail("stale-wake selector did not re-park after the spurious wake");
    }
    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("stale-wake resolve failed");
    }
    rt_channel_send_blocking(raw_a, 42);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 0) {
        return rtb_fail("stale-wake select did not answer the sent arm");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    if (destination.far_channel_select_requests != 1 || source.far_channel_select_replies != 1) {
        return rtb_fail("stale-wake produced extra transport traffic");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("stale-wake release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 11 (lease invalidation while parked): every lease is released while
// the selector is parked; the dispatch-time pins keep the channels alive,
// the op still completes, and the reply-edge unpin lets the entries
// reclaim — the census reaches zero with no further releases.
int rtb_mode_select_release_while_parked(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("release-while-parked mint failed");
    }
    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("release-while-parked resolve failed");
    }
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &state, &caller) != 0) {
        return 1;
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("release-while-parked release failed");
    }
    if (rt_far_channel_debug_live_count(ex) == 0) {
        return rtb_fail("release-while-parked reclaimed pinned entries early");
    }
    rt_channel_send_blocking(raw_a, 42);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 0) {
        return rtb_fail("release-while-parked select did not complete");
    }
    int clean = 0;
    for (uint32_t i = 0; i < 4000 && !clean; i++) {
        clean = rt_far_channel_debug_live_count(ex) == 0;
        if (!clean) {
            rtb_sleep_us(1000);
        }
    }
    if (!clean) {
        return rtb_fail("release-while-parked entries never reclaimed after the reply");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 12 (sibling-lease concurrency): two selectors park over sibling
// leases of one channel; one send completes exactly one selector, and the
// close completes the other through its closed arm.
int rtb_mode_select_sibling_isolation(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    rtb_create_state chan_c;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_c, rt_placement_shard(1), 1)) {
        return rtb_fail("sibling mint failed");
    }
    rt_far_task_handle sibling = {0};
    if (rt_far_channel_mint_sibling(ex, &chan_a.handle, &sibling) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("sibling mint-sibling failed");
    }
    rtb_select_state first_state;
    void* first_caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &first_state, &first_caller) != 0) {
        return 1;
    }
    rtb_select_state second_state;
    void* second_caller = NULL;
    if (rtb_select_park2(ex, &sibling, &chan_c.handle, &second_state, &second_caller) != 0) {
        return 1;
    }
    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("sibling resolve failed");
    }
    rt_channel_send_blocking(raw_a, 42);
    rt_task* first_task = (rt_task*)first_caller;
    rt_task* second_task = (rt_task*)second_caller;
    int first_done = 0;
    int second_done = 0;
    for (uint32_t i = 0; i < 4000; i++) {
        first_done = task_status_load(first_task) == TASK_DONE;
        second_done = task_status_load(second_task) == TASK_DONE;
        if (first_done || second_done) {
            break;
        }
        rtb_sleep_us(1000);
    }
    rtb_sleep_us(20000);
    first_done = task_status_load(first_task) == TASK_DONE;
    second_done = task_status_load(second_task) == TASK_DONE;
    if (first_done == second_done) {
        return rtb_fail("sibling send completed zero or both selectors");
    }
    rt_channel_close(raw_a);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(first_caller, &kind, &bits);
    (void)rtb_await(second_caller, &kind, &bits);
    if (first_state.status != RT_REMOTE_TASK_STATUS_OK ||
        second_state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("sibling selectors did not both complete");
    }
    if (first_state.result_bits != 0 || second_state.result_bits != 0) {
        return rtb_fail("sibling winner was not the shared-channel arm on both selectors");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &sibling) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_c.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("sibling release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 13 (caller teardown vs reply): the caller is cancelled while the
// selector is parked; the reply produced by the cancelled body is orphaned
// and consumed autonomously — one request, one reply, zero residue.
int rtb_mode_select_caller_teardown(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("caller-teardown mint failed");
    }
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &state, &caller) != 0) {
        return 1;
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind != 2) {
        return rtb_fail("caller-teardown caller did not resume as Cancelled");
    }
    int replied = 0;
    rt_runtime* runtime = rt_executor_runtime(ex);
    for (uint32_t i = 0; i < 4000 && !replied; i++) {
        struct rt_transport_debug_snapshot source =
            rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
        replied = source.far_channel_select_replies == 1;
        if (!replied) {
            rtb_sleep_us(1000);
        }
    }
    if (!replied) {
        return rtb_fail("caller-teardown reply never arrived to be consumed");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("caller-teardown release failed");
    }
    int clean = 0;
    for (uint32_t i = 0; i < 4000 && !clean; i++) {
        clean = rt_far_channel_debug_live_count(ex) == 0;
        if (!clean) {
            rtb_sleep_us(1000);
        }
    }
    if (!clean) {
        return rtb_fail("caller-teardown census found residue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 14 (owner teardown): shutdown lands while the selector is parked; the
// caller resumes with the deterministic shutdown status and the dispatcher
// never blocks (the mode returning at all is the liveness assert).
int rtb_mode_select_owner_teardown(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("owner-teardown mint failed");
    }
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &state, &caller) != 0) {
        return 1;
    }
    rt_remote_task_pending* pending =
        atomic_load_explicit(&state.visible_pending, memory_order_acquire);
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        return rtb_fail("owner-teardown shutdown failed");
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) !=
        RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN) {
        return rtb_fail("owner-teardown left the caller without a deterministic failure");
    }
    return 0;
}

// Row 15 (detector false-positive guard): a runnable spinner keeps the
// executor non-quiescent, so a parked selector must NOT trip the deadlock
// panic; the send then completes the select and the row exits cleanly.
int rtb_mode_select_no_deadlock_when_runnable(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("select-runnable mint failed");
    }
    static rtb_share_state spin;
    memset(&spin, 0, sizeof(spin));
    void* spinner = __task_create(POLL_RTB_SPINNER, &spin);
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &state, &caller) != 0) {
        return 1;
    }
    // The would-be false-positive window: shards idle except the spinner.
    rtb_sleep_us(300000);
    // Complete the select BEFORE parking the spinner: the main thread is
    // invisible to quiescence, so releasing the spinner first would open a
    // real (momentary) deadlock window ahead of this send.
    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("select-runnable resolve failed");
    }
    rt_channel_send_blocking(raw_a, 42);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_bits != 0) {
        return rtb_fail("select-runnable select did not complete");
    }
    atomic_store_explicit(&spin.spin_gate, 1, memory_order_release);
    (void)rtb_await(spinner, &kind, &bits);
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("select-runnable release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row 16 (detector true positive): the selector's arm channels have no
// producer anywhere — the only other task is the caller suspended on the
// select's own reply. Once every shard is idle the runtime must panic with
// the select-shaped deadlock report instead of hanging. The process dies;
// the Go row asserts the exit and the message.
int rtb_mode_select_self_deadlock(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("select-deadlock mint failed");
    }
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park2(ex, &chan_a.handle, &chan_b.handle, &state, &caller) != 0) {
        return 1;
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    return rtb_fail("select-deadlock select unexpectedly completed");
}
