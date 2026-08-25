#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_transport.h"

#include <string.h>

// Anchored-op audit rows: data-lane saturation with bounded per-attempt
// failure and control-lane progress, and the leak census after a full
// lifecycle churn.

// Caller poll that saturates the destination's DATA lane before submitting
// the anchored execute, mirroring the control-lane fill of the lifecycle
// queue-failure row: the enqueue must answer QUEUE_FULL synchronously, and
// the failed attempt must leave no listed pending behind.
static void poll_anchored_flooded_caller(rtb_anchored_state* state) {
    rt_executor* ex = ensure_exec();
    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), state->anchor.owner_shard_id);
    rt_shard_lock(owner);
    memset(owner->transport.data, 0, sizeof(owner->transport.data));
    owner->transport.data_head = 0;
    owner->transport.data_len = RT_TRANSPORT_DATA_QUEUE_CAP;
    rt_shard_unlock(owner);
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute_anchored(
        &state->anchor, 0, 0, (int64_t)state->body_poll_id, state, &state->pending, &kind, &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

void rtb_anchored_audit_poll_dispatch(uint64_t id) {
    if (id == POLL_RTB_ANCHORED_FLOODED_CALLER) {
        poll_anchored_flooded_caller((rtb_anchored_state*)__task_state());
    }
}

static void drain_data_lane(rt_executor* ex, uint32_t shard_id) {
    rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), shard_id);
    rt_shard_lock(owner);
    memset(owner->transport.data, 0, sizeof(owner->transport.data));
    owner->transport.data_head = 0;
    owner->transport.data_len = 0;
    rt_shard_unlock(owner);
}

static size_t listed_pending_count(rt_executor* ex) {
    rt_remote_task_state* state = rt_remote_task_state_get(ex);
    if (state == NULL) {
        return 0;
    }
    size_t count = 0;
    pthread_mutex_lock(&state->lock);
    for (const rt_remote_task_pending* it = state->pending_head; it != NULL; it = it->next) {
        count++;
    }
    pthread_mutex_unlock(&state->lock);
    return count;
}

// A saturated data lane answers QUEUE_FULL per attempt without wedging
// anything: the failed attempt rolls its pending back, a block already
// parked on the owner completes through the control-lane reply, and a
// fresh attempt after the drain succeeds. The gate parks the in-flight
// body on channel capacity (a genuinely idle owner cannot drain the
// saturation before the flooded enqueue observes it), so the row runs
// with the self-deadlock detector opted out — the counterparty is the
// harness main thread.
int rtb_mode_anchored_queue_full(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("queue-full mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    rtb_anchored_state prefill;
    memset(&prefill, 0, sizeof(prefill));
    prefill.anchor = minted.handle;
    prefill.value = 5;
    prefill.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* prefill_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &prefill, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(prefill_caller, &kind, &bits);
    if (prefill.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("queue-full prefill failed");
    }
    rtb_anchored_state gated;
    memset(&gated, 0, sizeof(gated));
    gated.anchor = minted.handle;
    gated.value = 6;
    gated.body_poll_id = POLL_RTB_ANCHORED_HELPER_SEND;
    void* gated_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &gated, rt_channel_opaque_word_ops());
    if (!rtb_wait_u32(&gated.body_ran, 1, 5000)) {
        return rtb_fail("queue-full gated body did not start");
    }
    // Give the parked body's owner shard time to reach its idle park; a
    // parked owner cannot drain the saturation the flooded caller is about
    // to install.
    rtb_sleep_us(50000);
    rtb_anchored_state flooded;
    memset(&flooded, 0, sizeof(flooded));
    flooded.anchor = minted.handle;
    flooded.value = 7;
    flooded.body_poll_id = POLL_RTB_ANCHORED_BODY;
    void* flooded_caller =
        __task_create(POLL_RTB_ANCHORED_FLOODED_CALLER, &flooded, rt_channel_opaque_word_ops());
    (void)rtb_await(flooded_caller, &kind, &bits);
    if (flooded.status != RT_REMOTE_TASK_STATUS_QUEUE_FULL) {
        return rtb_fail("saturated data lane did not answer queue-full");
    }
    // Draining the channel wakes the parked body; its reply rides the
    // control lane and completes the in-flight block.
    uint64_t drained = 0;
    if (!rt_channel_try_recv(channel, &drained) || drained != 5) {
        return rtb_fail("queue-full drain did not observe the prefill value");
    }
    (void)rtb_await(gated_caller, &kind, &bits);
    if (gated.status != RT_REMOTE_TASK_STATUS_OK || gated.result_bits != 1) {
        return rtb_fail("parked block did not complete after the drain");
    }
    drain_data_lane(ex, minted.handle.owner_shard_id);
    // The completed block refilled the capacity-1 channel; free the slot so
    // the post-drain attempt can run to completion.
    if (!rt_channel_try_recv(channel, &drained) || drained != 6) {
        return rtb_fail("queue-full second drain did not observe the parked send's value");
    }
    rtb_anchored_state retried;
    memset(&retried, 0, sizeof(retried));
    retried.anchor = minted.handle;
    retried.value = 7;
    retried.body_poll_id = POLL_RTB_ANCHORED_BODY;
    void* retried_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &retried, rt_channel_opaque_word_ops());
    (void)rtb_await(retried_caller, &kind, &bits);
    if (retried.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("post-drain attempt did not succeed");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (source.credit_stalls != 0 || destination.credit_stalls != 0) {
        return rtb_fail("credit stalls must stay structurally zero without a credit protocol");
    }
    if (source.unsupported_fallback_attempts != 0 ||
        destination.unsupported_fallback_attempts != 0) {
        return rtb_fail("queue-full path must not attempt a local fallback");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Lifecycle churn leaves no residue: after mint/send/recv/close/release
// cycles (including release racing an active pinned block), the pending
// list and the channel registry are empty and the fallback tripwire is
// still zero.
int rtb_mode_anchored_leak_audit(void) {
    rt_executor* ex = ensure_exec();
    for (uint32_t round = 0; round < 48; round++) {
        rtb_create_state minted;
        uint32_t owner = 1u - (round & 1u);
        if (!rtb_mint_channel(&minted, rt_placement_shard(owner), 2)) {
            return rtb_fail("leak-audit mint failed");
        }
        uint8_t kind = 0;
        uint64_t bits = 0;
        if ((round & 7u) == 7u) {
            // Release while a gated block holds the pin: the entry must be
            // reclaimed at the reply edge, not leaked and not double-freed.
            rtb_anchored_state pinned;
            memset(&pinned, 0, sizeof(pinned));
            pinned.anchor = minted.handle;
            pinned.value = round;
            pinned.body_poll_id = POLL_RTB_ANCHORED_PINNED_BODY;
            void* pinned_caller =
                __task_create(POLL_RTB_ANCHORED_CALLER, &pinned, rt_channel_opaque_word_ops());
            if (!rtb_wait_u32(&pinned.body_ran, 1, 5000)) {
                return rtb_fail("leak-audit pinned body did not start");
            }
            if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
                return rtb_fail("leak-audit release failed");
            }
            atomic_store_explicit(&pinned.proceed, 1, memory_order_release);
            (void)rtb_await(pinned_caller, &kind, &bits);
            if (pinned.status != RT_REMOTE_TASK_STATUS_OK) {
                return rtb_fail("leak-audit pinned block failed");
            }
            continue;
        }
        rtb_anchored_state sender;
        memset(&sender, 0, sizeof(sender));
        sender.anchor = minted.handle;
        sender.value = round;
        sender.body_poll_id = POLL_RTB_ANCHORED_BODY;
        void* send_caller =
            __task_create(POLL_RTB_ANCHORED_CALLER, &sender, rt_channel_opaque_word_ops());
        (void)rtb_await(send_caller, &kind, &bits);
        if (sender.status != RT_REMOTE_TASK_STATUS_OK) {
            return rtb_fail("leak-audit send failed");
        }
        rtb_anchored_state receiver;
        memset(&receiver, 0, sizeof(receiver));
        receiver.anchor = minted.handle;
        receiver.body_poll_id = POLL_RTB_ANCHORED_RECV_BODY;
        void* recv_caller =
            __task_create(POLL_RTB_ANCHORED_CALLER, &receiver, rt_channel_opaque_word_ops());
        (void)rtb_await(recv_caller, &kind, &bits);
        if (receiver.status != RT_REMOTE_TASK_STATUS_OK) {
            return rtb_fail("leak-audit recv failed");
        }
        if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
            return rtb_fail("leak-audit final release failed");
        }
    }
    // Replies consume pendings on the await edge; give stragglers a bounded
    // window before the census.
    for (uint32_t attempt = 0; attempt < 5000; attempt++) {
        if (listed_pending_count(ex) == 0) {
            break;
        }
        rtb_sleep_us(1000);
    }
    if (listed_pending_count(ex) != 0) {
        return rtb_fail("leak audit found listed pendings after the churn");
    }
    if (rt_far_channel_debug_live_count(ex) != 0) {
        return rtb_fail("leak audit found live registry entries after the churn");
    }
    rt_runtime* runtime = rt_executor_runtime(ex);
    struct rt_transport_debug_snapshot source =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 0));
    struct rt_transport_debug_snapshot destination =
        rt_transport_debug_snapshot(rt_runtime_shard(runtime, 1));
    if (source.unsupported_fallback_attempts != 0 ||
        destination.unsupported_fallback_attempts != 0) {
        return rtb_fail("leak-audit churn must not attempt a local fallback");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// The cross-producer negative observation: values land in the owner's
// local-lane execution order, NOT in block-start order. Producer one's
// block starts first but its body holds at a gate; producer two's block
// sends and completes; releasing the gate lands producer one's value
// second. Per-producer FIFO is pinned elsewhere (the round-trip rows and
// the source e2e); this row pins that no cross-producer promise exists.
int rtb_mode_anchored_cross_producer_order(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 4)) {
        return rtb_fail("cross-producer mint failed");
    }
    void* channel = rt_far_channel_resolve(ex, &minted.handle);
    rtb_anchored_state first;
    memset(&first, 0, sizeof(first));
    first.anchor = minted.handle;
    first.value = 11;
    first.body_poll_id = POLL_RTB_ANCHORED_PINNED_BODY;
    void* first_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &first, rt_channel_opaque_word_ops());
    if (!rtb_wait_u32(&first.body_ran, 1, 5000)) {
        return rtb_fail("first producer's body did not start");
    }
    rtb_anchored_state second;
    memset(&second, 0, sizeof(second));
    second.anchor = minted.handle;
    second.value = 22;
    second.body_poll_id = POLL_RTB_ANCHORED_BODY;
    void* second_caller =
        __task_create(POLL_RTB_ANCHORED_CALLER, &second, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(second_caller, &kind, &bits);
    if (second.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("second producer failed");
    }
    atomic_store_explicit(&first.proceed, 1, memory_order_release);
    (void)rtb_await(first_caller, &kind, &bits);
    if (first.status != RT_REMOTE_TASK_STATUS_OK || first.result_bits != 1) {
        return rtb_fail("first producer failed after the gate");
    }
    uint64_t drained = 0;
    if (!rt_channel_try_recv(channel, &drained) || drained != 22) {
        return rtb_fail("cross-producer order followed block-start order (expected the "
                        "second producer's value first)");
    }
    if (!rt_channel_try_recv(channel, &drained) || drained != 11) {
        return rtb_fail("first producer's value was lost");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
