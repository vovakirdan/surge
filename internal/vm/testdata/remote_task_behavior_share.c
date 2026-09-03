#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_transport.h"

#include <string.h>

// Shared-handle rows: sibling leases minted through the share execute/reply
// discipline, per-lease stale detection, pin-vs-release with multiple
// holders, and the lease census after teardown.

// A plain local spinner: busy-yields (stays RUNNABLE, never parks) until
// its gate opens. Its runnable-ness is what keeps the executor
// non-quiescent — the false-negative guard for the deadlock detector.
static void poll_rtb_spinner(rtb_share_state* state) {
    if (atomic_load_explicit(&state->spin_gate, memory_order_acquire) == 0) {
        rt_async_yield(state, 0);
    }
    rt_async_return(state, rtb_word(1));
}

static void poll_rtb_channel_share(rtb_share_state* state) {
    uint8_t kind = 0;
    state->status = rt_far_channel_share(&state->source, &state->pending, &state->sibling, &kind);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

void rtb_share_poll_dispatch(uint64_t id) {
    if (id == POLL_RTB_CHANNEL_SHARE) {
        poll_rtb_channel_share((rtb_share_state*)__task_state());
    }
    if (id == POLL_RTB_SPINNER) {
        poll_rtb_spinner((rtb_share_state*)__task_state());
    }
}

static int rtb_share_sibling(rt_executor* ex,
                             const rt_far_task_handle* source,
                             rt_far_task_handle* out,
                             rt_remote_task_status* out_status) {
    (void)ex;
    rtb_share_state state;
    memset(&state, 0, sizeof(state));
    state.source = *source;
    void* caller = __task_create(POLL_RTB_CHANNEL_SHARE, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (out_status != NULL) {
        *out_status = state.status;
    }
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return 0;
    }
    *out = state.sibling;
    return 1;
}

// A sibling lease reaches the same owner-side channel, carries its own
// generation, and the share request/reply pair rides the transport with
// counters and no fallback.
int rtb_mode_share_round_trip(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 2)) {
        return rtb_fail("share round-trip mint failed");
    }
    rt_far_task_handle sibling = {0};
    rt_remote_task_status share_status = 0;
    if (!rtb_share_sibling(ex, &minted.handle, &sibling, &share_status)) {
        return rtb_fail("share did not complete");
    }
    if (sibling.task_id != minted.handle.task_id ||
        sibling.generation == minted.handle.generation ||
        sibling.owner_shard_id != minted.handle.owner_shard_id ||
        sibling.kind != RT_FAR_HANDLE_KIND_CHANNEL) {
        return rtb_fail("sibling token fields are wrong");
    }
    void* through_original = rt_far_channel_resolve(ex, &minted.handle);
    const void* through_sibling = rt_far_channel_resolve(ex, &sibling);
    if (through_original == NULL || through_original != through_sibling) {
        return rtb_fail("sibling does not resolve to the same channel");
    }
    if (rt_far_channel_debug_lease_count(ex) != 2) {
        return rtb_fail("expected exactly two lease rows after one share");
    }
    // An anchored block through the SIBLING lands on the shared channel.
    rtb_anchored_state sender;
    memset(&sender, 0, sizeof(sender));
    sender.anchor = sibling;
    sender.value = 17;
    sender.body_poll_id = POLL_RTB_ANCHORED_BODY;
    void* send_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &sender, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(send_caller, &kind, &bits);
    if (sender.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("anchored send through the sibling failed");
    }
    uint64_t drained = 0;
    if (!rt_channel_try_recv(through_original, &drained) || drained != 17) {
        return rtb_fail("value sent through the sibling did not land on the shared channel");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (destination.far_channel_share_requests != 1 || source.far_channel_share_replies != 1) {
        return rtb_fail("share trace counters mismatch");
    }
    if (source.unsupported_fallback_attempts != 0 ||
        destination.unsupported_fallback_attempts != 0) {
        return rtb_fail("share attempted a local fallback");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &sibling) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("releasing both leases failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0 || rt_far_channel_debug_lease_count(ex) != 0) {
        return rtb_fail("lease census found residue after both releases");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Releasing one lease neither disturbs the other nor tolerates a second
// release of itself: per-lease stale detection is exact.
int rtb_mode_share_release_independence(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 2)) {
        return rtb_fail("independence mint failed");
    }
    rt_far_task_handle sibling = {0};
    if (!rtb_share_sibling(ex, &minted.handle, &sibling, NULL)) {
        return rtb_fail("independence share failed");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("releasing the original lease failed");
    }
    if (rt_far_channel_resolve(ex, &minted.handle) != NULL) {
        return rtb_fail("released original still resolves");
    }
    if (rt_far_channel_resolve(ex, &sibling) == NULL) {
        return rtb_fail("sibling stopped resolving after the original's release");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("double release of the original was not exactly stale");
    }
    if (rt_far_channel_resolve(ex, &sibling) == NULL) {
        return rtb_fail("a stale double-release disturbed the sibling");
    }
    if (rt_far_channel_release(ex, &sibling) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("releasing the sibling failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 0 || rt_far_channel_debug_lease_count(ex) != 0) {
        return rtb_fail("census found residue after all releases");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A released holder cannot propagate access: share through a released
// lease answers stale without minting anything.
int rtb_mode_share_from_released_lease(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 2)) {
        return rtb_fail("released-lease mint failed");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("release failed");
    }
    rt_far_task_handle sibling = {0};
    rt_remote_task_status share_status = 0;
    if (rtb_share_sibling(ex, &minted.handle, &sibling, &share_status)) {
        return rtb_fail("share through a released lease unexpectedly succeeded");
    }
    if (share_status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("share through a released lease was not stale");
    }
    if (rt_far_channel_debug_live_count(ex) != 0 || rt_far_channel_debug_lease_count(ex) != 0) {
        return rtb_fail("failed share left registry residue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Both leases released while an anchored block holds the pin: the entry
// survives to the reply edge on the pin alone, then reclaims fully.
int rtb_mode_share_pin_outlives_leases(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 2)) {
        return rtb_fail("pin-outlives mint failed");
    }
    rt_far_task_handle sibling = {0};
    if (!rtb_share_sibling(ex, &minted.handle, &sibling, NULL)) {
        return rtb_fail("pin-outlives share failed");
    }
    rtb_anchored_state pinned;
    memset(&pinned, 0, sizeof(pinned));
    pinned.anchor = sibling;
    pinned.value = 23;
    pinned.body_poll_id = POLL_RTB_ANCHORED_PINNED_BODY;
    void* pinned_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &pinned, rt_channel_opaque_word_ops());
    if (!rtb_wait_u32(&pinned.body_ran, 1, 5000)) {
        return rtb_fail("pinned body did not start");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &sibling) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("releasing both leases during the block failed");
    }
    if (rt_far_channel_debug_live_count(ex) != 1) {
        return rtb_fail("pinned entry was reclaimed under the block");
    }
    atomic_store_explicit(&pinned.proceed, 1, memory_order_release);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(pinned_caller, &kind, &bits);
    if (pinned.status != RT_REMOTE_TASK_STATUS_OK || pinned.result_bits != 1) {
        return rtb_fail("pinned block did not complete after both releases");
    }
    if (rt_far_channel_debug_live_count(ex) != 0 || rt_far_channel_debug_lease_count(ex) != 0) {
        return rtb_fail("census found residue after the reply-edge unpin");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Owner teardown with a sibling still held remotely: every lease goes
// stale deterministically and the census drains.
int rtb_mode_share_teardown(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 2)) {
        return rtb_fail("teardown mint failed");
    }
    rt_far_task_handle sibling = {0};
    if (!rtb_share_sibling(ex, &minted.handle, &sibling, NULL)) {
        return rtb_fail("teardown share failed");
    }
    rt_far_channel_release_all(ex);
    if (rt_far_channel_resolve(ex, &minted.handle) != NULL ||
        rt_far_channel_resolve(ex, &sibling) != NULL) {
        return rtb_fail("a lease survived the teardown sweep");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_STALE_TOKEN ||
        rt_far_channel_release(ex, &sibling) != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("post-teardown releases were not exactly stale");
    }
    if (rt_far_channel_debug_live_count(ex) != 0 || rt_far_channel_debug_lease_count(ex) != 0) {
        return rtb_fail("census found residue after teardown");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Two holders, both parked as producers on a full channel, nobody left to
// drain: a true multi-holder deadlock. The panic must name the lease
// topology instead of implying a sole holder.
int rtb_mode_share_deadlock_two_holders(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("two-holder deadlock mint failed");
    }
    rt_far_task_handle sibling = {0};
    if (!rtb_share_sibling(ex, &minted.handle, &sibling, NULL)) {
        return rtb_fail("two-holder deadlock share failed");
    }
    rtb_anchored_state prefill;
    memset(&prefill, 0, sizeof(prefill));
    prefill.anchor = minted.handle;
    prefill.value = 1;
    prefill.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* prefill_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &prefill, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("two-holder prefill failed");
    }
    rtb_anchored_state first;
    memset(&first, 0, sizeof(first));
    first.anchor = minted.handle;
    first.value = 2;
    first.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    (void)__task_create(POLL_RTB_ANCHORED_CALLER, &first, rt_channel_opaque_word_ops());
    rtb_anchored_state second;
    memset(&second, 0, sizeof(second));
    second.anchor = sibling;
    second.value = 3;
    second.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    (void)__task_create(POLL_RTB_ANCHORED_CALLER, &second, rt_channel_opaque_word_ops());
    // Both producers park; every holder is now idle. The detector must
    // abort the process from a worker's idle-park edge.
    for (uint32_t i = 0; i < 10000; i++) {
        rtb_sleep_us(1000);
    }
    return rtb_fail("two-holder deadlock was not detected within the window");
}

// The false-negative guard: one producer parks on the full channel while a
// RUNNABLE spinner keeps the executor non-quiescent — the detector must
// stay silent for the whole window, and the run must end cleanly once the
// channel is drained BEFORE the spinner exits.
int rtb_mode_share_no_deadlock_when_runnable(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("guard mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    rtb_anchored_state prefill;
    memset(&prefill, 0, sizeof(prefill));
    prefill.anchor = minted.handle;
    prefill.value = 4;
    prefill.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* prefill_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &prefill, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    rtb_share_state spinner;
    memset(&spinner, 0, sizeof(spinner));
    void* spinner_task = __task_create(POLL_RTB_SPINNER, &spinner, rt_channel_opaque_word_ops());
    rtb_anchored_state parked;
    memset(&parked, 0, sizeof(parked));
    parked.anchor = minted.handle;
    parked.value = 5;
    parked.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* parked_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &parked, rt_channel_opaque_word_ops());
    if (!rtb_wait_u32(&parked.body_ran, 1, 5000)) {
        return rtb_fail("guard body did not start");
    }
    // The parked producer plus the runnable spinner coexist for a long
    // window; a detector keyed on anything but real quiescence would
    // panic here.
    rtb_sleep_us(300000);
    uint64_t drained = 0;
    if (!rt_channel_try_recv(channel, &drained) || drained != 4) {
        return rtb_fail("guard drain did not observe the prefill");
    }
    (void)rtb_await(parked_caller, &kind, &bits);
    if (parked.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("parked producer did not complete after the drain");
    }
    atomic_store_explicit(&spinner.spin_gate, 1, memory_order_release);
    (void)rtb_await(spinner_task, &kind, &bits);
    uint64_t leftover = 0;
    if (!rt_channel_try_recv(channel, &leftover) || leftover != 5) {
        return rtb_fail("second value did not land after the drain");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("guard release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// A sibling holder releases and leaves while the peer's producer is
// parked: with the leaver gone, the parked producer is a true deadlock and
// the panic must still fire (one active lease remains).
int rtb_mode_share_deadlock_after_peer_release(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("peer-release mint failed");
    }
    rt_far_task_handle sibling = {0};
    if (!rtb_share_sibling(ex, &minted.handle, &sibling, NULL)) {
        return rtb_fail("peer-release share failed");
    }
    rtb_anchored_state prefill;
    memset(&prefill, 0, sizeof(prefill));
    prefill.anchor = minted.handle;
    prefill.value = 6;
    prefill.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* prefill_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &prefill, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    // The peer leaves FIRST (deterministic lease topology: one active
    // lease remains), then the surviving holder's producer parks with
    // nobody left to drain.
    if (rt_far_channel_release(ex, &sibling) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("peer release failed");
    }
    rtb_anchored_state parked;
    memset(&parked, 0, sizeof(parked));
    parked.anchor = minted.handle;
    parked.value = 7;
    parked.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    (void)__task_create(POLL_RTB_ANCHORED_CALLER, &parked, rt_channel_opaque_word_ops());
    for (uint32_t i = 0; i < 10000; i++) {
        rtb_sleep_us(1000);
    }
    return rtb_fail("post-peer-release deadlock was not detected within the window");
}
