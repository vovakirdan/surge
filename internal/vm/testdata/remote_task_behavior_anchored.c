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

// Body poll: resolve the anchor and receive one value; a false-ish recv
// status of 0 means parked (yield and re-enter), 1 delivers the value, and
// 2 is the closed outcome — returned through the single reply as-is.
static void poll_anchored_recv(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    void* channel = rt_far_channel_resolve(ex, &state->anchor);
    if (channel == NULL) {
        rt_async_return(state, 0);
    }
    atomic_store_explicit(&state->body_ran, 1, memory_order_release);
    uint64_t received = 0;
    uint8_t status = rt_channel_recv(channel, &received);
    if (status == 0) {
        rt_async_yield(state);
    }
    rt_async_return(state, (uint64_t)status);
}

// Body poll: resolve the anchor once, remember the raw channel, then
// busy-yield (no waiter registered, so the task stays runnable and the
// shard never looks quiescent) until the harness flips `proceed`. The send
// after the gate runs on the remembered pointer — if the token was released
// while this block was in flight, the dispatch-time pin is what keeps that
// pointer alive.
static void poll_anchored_pinned_send(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    if (state->channel == NULL) {
        void* channel = rt_far_channel_resolve(ex, &state->anchor);
        if (channel == NULL) {
            rt_async_return(state, 0);
        }
        state->channel = channel;
        atomic_store_explicit(&state->body_ran, 1, memory_order_release);
    }
    if (atomic_load_explicit(&state->proceed, memory_order_acquire) == 0) {
        rt_async_yield(state);
    }
    // The dispatch-cached pointer must match the body's own resolve and must
    // still be served after the release (the pin holds it to the reply edge).
    if (rt_remote_task_anchored_channel_current() != state->channel) {
        rt_async_return(state, 0);
    }
    if (!rt_channel_send(state->channel, state->value)) {
        rt_async_yield(state);
    }
    rt_async_return(state, 1);
}

// Body polls shaped exactly like compiled vertical-1 output: the anchored
// operation is the first statement and the helper parks/re-enters inside.
static void poll_anchored_helper_send(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    atomic_store_explicit(&state->body_ran, 1, memory_order_release);
    rt_anchored_channel_send(state, state->value);
    rt_async_return(state, 1);
}

static void poll_anchored_helper_recv(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    uint64_t bits = 0;
    uint8_t status = rt_anchored_channel_recv(state, &bits);
    rt_async_return(state, ((uint64_t)status << 32) | (bits & 0xffffffffu));
}

static void poll_anchored_helper_close(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    if (current_task_cancelled(ex)) {
        rt_async_return_cancelled(state);
    }
    rt_anchored_channel_close();
    rt_async_return(state, 1);
}

// Caller poll: one anchored execute, retry until the reply.
static void poll_anchored_caller(rtb_anchored_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute_anchored(
        &state->anchor, (int64_t)state->body_poll_id, state, &state->pending, &kind, &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, (uint64_t)state->status);
}

void rtb_anchored_poll_dispatch(uint64_t id) {
    if (id == POLL_RTB_ANCHORED_HELPER_SEND) {
        poll_anchored_helper_send((rtb_anchored_state*)__task_state());
    }
    if (id == POLL_RTB_ANCHORED_HELPER_RECV) {
        poll_anchored_helper_recv((rtb_anchored_state*)__task_state());
    }
    if (id == POLL_RTB_ANCHORED_HELPER_CLOSE) {
        poll_anchored_helper_close((rtb_anchored_state*)__task_state());
    }
    if (id == POLL_RTB_ANCHORED_BODY) {
        poll_anchored_send((rtb_anchored_state*)__task_state());
    }
    if (id == POLL_RTB_ANCHORED_RECV_BODY) {
        poll_anchored_recv((rtb_anchored_state*)__task_state());
    }
    if (id == POLL_RTB_ANCHORED_PINNED_BODY) {
        poll_anchored_pinned_send((rtb_anchored_state*)__task_state());
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
    state->body_poll_id = POLL_RTB_ANCHORED_BODY;
    return __task_create(POLL_RTB_ANCHORED_CALLER, state);
}

static void* rtb_start_anchored_recv(rtb_anchored_state* state, const rt_far_task_handle* anchor) {
    memset(state, 0, sizeof(*state));
    state->anchor = *anchor;
    state->body_poll_id = POLL_RTB_ANCHORED_RECV_BODY;
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

// Close-vs-recv: an empty-channel recv body parks; the owner-side close
// wakes it with the closed outcome, which travels through the single reply.
// Never a success-after-close.
int rtb_mode_anchored_close_vs_recv(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("close-vs-recv mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    rtb_anchored_state state;
    void* caller = rtb_start_anchored_recv(&state, &minted.handle);
    if (!rtb_wait_u32(&state.body_ran, 1, 5000)) {
        return rtb_fail("recv body did not start");
    }
    rt_channel_close(channel);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 2) {
        return rtb_fail("closed channel did not deliver the closed outcome through the reply");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Caller cancelled while the body is parked on a full channel: the caller
// resumes exactly once through the cancel path, the routed cancel reaches
// the parked body (which observes cancellation on its re-poll), and the
// orphaned reply edge is consumed exactly once; a later capacity wake
// cannot resurrect the cancelled body.
int rtb_mode_anchored_cancel_parked_body(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("cancel-parked mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    rtb_anchored_state prefill;
    void* prefill_caller = rtb_start_anchored(&prefill, &minted.handle, 5);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("cancel-parked prefill failed");
    }
    rtb_anchored_state state;
    void* caller = rtb_start_anchored(&state, &minted.handle, 6);
    uint64_t caller_id = ((rt_task*)caller)->id;
    if (!rtb_wait_u32(&state.body_ran, 1, 5000)) {
        return rtb_fail("cancel-parked body did not start");
    }
    rt_control_lock(ex);
    cancel_task(ex, caller_id);
    rt_control_unlock(ex);
    (void)rtb_await(caller, &kind, &bits);
    if (kind != 2) {
        return rtb_fail("cancelled caller did not resume through the cancel path");
    }
    // The orphaned reply edge must resolve exactly once; give it time, then
    // try a late wake — draining a slot must not resurrect the body.
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source = {0};
    for (uint32_t i = 0; i < 5000; i++) {
        source = rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
        if (source.immediate_on_replies >= 2) {
            break;
        }
        rtb_sleep_us(1000);
    }
    if (source.immediate_on_replies != 2 || source.remote_task_stale_drops != 0) {
        return rtb_fail("cancel-parked reply edges were not each consumed exactly once");
    }
    uint64_t drained = 0;
    (void)rt_channel_try_recv(channel, &drained);
    rtb_sleep_us(20000);
    struct rt_transport_debug_snapshot after =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    if (after.immediate_on_replies != 2) {
        return rtb_fail("a late capacity wake resurrected the cancelled body");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Owner teardown mid-flight: shutdown fails the suspended caller's pending
// deterministically; no vanished reply.
int rtb_mode_anchored_owner_teardown(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("teardown mint failed");
    }
    rtb_anchored_state prefill;
    void* prefill_caller = rtb_start_anchored(&prefill, &minted.handle, 9);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    rtb_anchored_state state;
    void* caller = rtb_start_anchored(&state, &minted.handle, 10);
    (void)caller;
    if (!rtb_wait_u32(&state.body_ran, 1, 5000)) {
        return rtb_fail("teardown body did not start");
    }
    rt_remote_task_pending* pending = NULL;
    for (uint32_t i = 0; i < 5000 && pending == NULL; i++) {
        pending = state.pending;
        rtb_sleep_us(1000);
    }
    if (pending == NULL) {
        return rtb_fail("teardown caller had no pending");
    }
    if (rt_executor_request_shutdown(ex) != RT_RUNTIME_STATUS_OK) {
        return rtb_fail("executor shutdown failed");
    }
    if (rt_remote_task_pending_snapshot(pending, NULL, NULL) !=
        RT_REMOTE_TASK_STATUS_DESTINATION_SHUTDOWN) {
        return rtb_fail("teardown left the caller without a deterministic failure");
    }
    return 0;
}

// Self-deadlock reproducer: a full channel whose only consumer is the
// initiating caller — the shipped send body parks, the caller sleeps on the
// reply, and once every shard is idle the runtime must panic with the
// actionable remote-channel-deadlock message rather than hang. The harness
// process dies; the Go row asserts the exit and the message.
int rtb_mode_anchored_self_deadlock(void) {
    (void)ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(0), 1)) {
        return rtb_fail("self-deadlock mint failed");
    }
    rtb_anchored_state prefill;
    void* prefill_caller = rtb_start_anchored(&prefill, &minted.handle, 1);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("self-deadlock prefill failed");
    }
    rtb_anchored_state state;
    (void)rtb_start_anchored(&state, &minted.handle, 2);
    // Do not await and do not drain: the only path that could free the
    // channel is this thread, which now just sleeps. The detection must
    // fire from a worker's idle-park edge and abort the process.
    for (uint32_t i = 0; i < 10000; i++) {
        rtb_sleep_us(1000);
    }
    return rtb_fail("self-deadlock was not detected within the window");
}

// Release during an active block: the dispatch-time pin refuses
// reclamation under the body's feet — the token stops resolving the moment
// the release lands (stop-new), but the channel pointer the body already
// holds stays valid until the block's reply edge unpins the entry.
int rtb_mode_anchored_pin_vs_release(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("pin-vs-release mint failed");
    }
    rtb_anchored_state state;
    memset(&state, 0, sizeof(state));
    state.anchor = minted.handle;
    state.value = 4;
    state.body_poll_id = POLL_RTB_ANCHORED_PINNED_BODY;
    void* caller = __task_create(POLL_RTB_ANCHORED_CALLER, &state);
    if (!rtb_wait_u32(&state.body_ran, 1, 5000)) {
        return rtb_fail("pin-vs-release body did not resolve the channel");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("release of a pinned entry did not succeed logically");
    }
    if (rt_far_channel_resolve(ex, &minted.handle) != NULL) {
        return rtb_fail("released token still resolves during the block");
    }
    atomic_store_explicit(&state.proceed, 1, memory_order_release);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_bits != 1) {
        return rtb_fail("pinned block did not complete after the release");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// The helper protocol end to end, in the exact shape compiled vertical-1
// bodies take: a send that parks on capacity and completes after the drain,
// a recv that delivers the parked value, a close, and a recv that reports
// the closed outcome. The send's counterparty is the harness main thread,
// so the row runs with the self-deadlock detector opted out.
int rtb_mode_anchored_helper_protocol(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("helper-protocol mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    rtb_anchored_state prefill;
    memset(&prefill, 0, sizeof(prefill));
    prefill.anchor = minted.handle;
    prefill.value = 7;
    prefill.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* prefill_caller = __task_create(POLL_RTB_ANCHORED_CALLER, &prefill);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK || prefill.result_bits != 1) {
        return rtb_fail("helper send did not complete on free capacity");
    }
    rtb_anchored_state parked;
    memset(&parked, 0, sizeof(parked));
    parked.anchor = minted.handle;
    parked.value = 8;
    parked.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* parked_caller = __task_create(POLL_RTB_ANCHORED_CALLER, &parked);
    if (!rtb_wait_u32(&parked.body_ran, 1, 5000)) {
        return rtb_fail("helper send body did not start");
    }
    uint64_t drained = 0;
    if (!rt_channel_try_recv(channel, &drained) || drained != 7) {
        return rtb_fail("owner-side drain did not observe the first value");
    }
    (void)rtb_await(parked_caller, &kind, &bits);
    if (parked.status != RT_REMOTE_TASK_STATUS_OK || parked.result_bits != 1) {
        return rtb_fail("parked helper send did not complete after the drain");
    }
    rtb_anchored_state receiver;
    memset(&receiver, 0, sizeof(receiver));
    receiver.anchor = minted.handle;
    receiver.body_poll_id = POLL_RTB_ANCHORED_HELPER_RECV;
    void* recv_caller = __task_create(POLL_RTB_ANCHORED_CALLER, &receiver);
    (void)rtb_await(recv_caller, &kind, &bits);
    if (receiver.status != RT_REMOTE_TASK_STATUS_OK || receiver.result_bits != ((1ull << 32) | 8)) {
        return rtb_fail("helper recv did not deliver the parked send's value");
    }
    rtb_anchored_state closer;
    memset(&closer, 0, sizeof(closer));
    closer.anchor = minted.handle;
    closer.body_poll_id = POLL_RTB_ANCHORED_HELPER_CLOSE;
    void* close_caller = __task_create(POLL_RTB_ANCHORED_CALLER, &closer);
    (void)rtb_await(close_caller, &kind, &bits);
    if (closer.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("helper close failed");
    }
    rtb_anchored_state after;
    memset(&after, 0, sizeof(after));
    after.anchor = minted.handle;
    after.body_poll_id = POLL_RTB_ANCHORED_HELPER_RECV;
    void* after_caller = __task_create(POLL_RTB_ANCHORED_CALLER, &after);
    (void)rtb_await(after_caller, &kind, &bits);
    if (after.status != RT_REMOTE_TASK_STATUS_OK || after.result_bits != (2ull << 32)) {
        return rtb_fail("helper recv after close did not report the closed outcome");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
