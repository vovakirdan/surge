#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"
#include "rt_transport.h"

#include <pthread.h>
#include <stdio.h>
#include <string.h>

// Shared helpers live here so the original select fixture stays below the
// project file-size limit while the retry lifecycle has its own focused stand.
int rtb_select_counters_ok(rt_executor* ex, uint64_t expected_requests) {
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

// Mints two empty channels on shard 1, starts a recv selector, and waits until
// the owner-side body is parked behind both channel registrations.
int rtb_select_park(rt_executor* ex,
                    rtb_create_state* chan_a,
                    rtb_create_state* chan_b,
                    rtb_select_state* state,
                    void** out_caller) {
    if (!rtb_mint_channel(chan_a, rt_placement_shard(1), 1) ||
        !rtb_mint_channel(chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("select park mint failed");
    }
    memset(state, 0, sizeof(*state));
    state->anchor_slots[0] = chan_a->handle;
    state->anchor_slots[1] = chan_b->handle;
    state->anchors[0] = &state->anchor_slots[0];
    state->anchors[1] = &state->anchor_slots[1];
    state->kinds[0] = SELECT_CHAN_RECV;
    state->kinds[1] = SELECT_CHAN_RECV;
    state->count = 2;
    void* caller = __task_create(POLL_RTB_SELECT_CALLER, state, rt_channel_opaque_word_ops());
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
        return rtb_fail("select park request never became visible");
    }
    uint64_t body_id = 0;
    for (uint32_t i = 0; i < 4000 && body_id == 0; i++) {
        body_id = pending->handle.task_id;
        if (body_id == 0) {
            rtb_sleep_us(1000);
        }
    }
    if (body_id == 0) {
        return rtb_fail("select park body was never bound");
    }
    const rt_task* body = get_task(ex, body_id);
    for (uint32_t i = 0; i < 4000; i++) {
        if (body != NULL && task_status_load(body) == TASK_WAITING) {
            return 0;
        }
        rtb_sleep_us(1000);
    }
    return rtb_fail("select park selector never parked");
}

// Existing behavioural row: a spurious caller wake may re-enter the pending
// retry, but it must not mint another request/body/reply chain.
int rtb_mode_select_retry_single_body(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park(ex, &chan_a, &chan_b, &state, &caller) != 0) {
        return 1;
    }
    const rt_task* caller_task = (const rt_task*)caller;
    if (caller_task == NULL) {
        return rtb_fail("retry park produced no caller task");
    }
    rtb_wake(ex, caller_task->id);
    rtb_sleep_us(20000);
    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("retry resolve failed");
    }
    rt_channel_send_blocking(raw_a, rtb_word(42));
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 0) {
        return rtb_fail("retry select did not answer the sent arm");
    }
    if (!rtb_select_counters_ok(ex, 1)) {
        return rtb_fail("retry produced more than one request/reply pair");
    }
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("retry release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

typedef struct rtb_seq0_waker {
    rt_executor* ex;
    uint64_t task_id;
    _Atomic uint32_t done;
} rtb_seq0_waker;

int rtb_mode_seq0_retry_classification(void) {
    const waker_key terminal_drained[] = {
        {WAKER_JOIN, 1, 0},
        {WAKER_SCOPE, 1, 0},
        {WAKER_BLOCKING, 1, 0},
        {WAKER_REMOTE_SPAWN_REPLY, 1, 0},
        {WAKER_REMOTE_TASK_REPLY, 1, 0},
    };
    const waker_key independently_completed[] = {
        {WAKER_TIMER, 1, 0},
        {WAKER_CHAN_SEND, 1, 0},
        {WAKER_CHAN_RECV, 1, 0},
        {WAKER_NET_ACCEPT, 1, 0},
        {WAKER_NET_READ, 1, 0},
        {WAKER_NET_WRITE, 1, 0},
    };
    for (size_t i = 0; i < sizeof(terminal_drained) / sizeof(terminal_drained[0]); i++) {
        if (!waker_seq0_retry_is_terminal_drained(terminal_drained[i])) {
            return rtb_fail("seq0 classification omitted a terminal-drained retry key");
        }
    }
    for (size_t i = 0; i < sizeof(independently_completed) / sizeof(independently_completed[0]); i++) {
        if (waker_seq0_retry_is_terminal_drained(independently_completed[i])) {
            return rtb_fail("seq0 classification included a timer/net/channel key");
        }
    }
    fputs("seq0 classification: terminal=5 independent=6\n", stderr);
    return 0;
}

static void* rtb_seq0_wake_thread(void* arg) {
    rtb_seq0_waker* waker = (rtb_seq0_waker*)arg;
    rtb_wake(waker->ex, waker->task_id);
    atomic_store_explicit(&waker->done, 1, memory_order_release);
    return NULL;
}

static size_t rtb_seq0_reply_entries(rt_executor* ex,
                                     waker_key key,
                                     uint64_t task_id,
                                     int* all_seq0) {
    rt_shard* shard = rt_waiter_key_shard(ex, key);
    if (shard == NULL) {
        return 0;
    }
    size_t count = 0;
    int zero = 1;
    rt_shard_lock(shard);
    const rt_waiter_store* store = rt_waiter_store_for_key(ex, key);
    if (store != NULL) {
        for (size_t i = 0; i < store->len; i++) {
            const waiter* entry = &store->entries[i];
            if (entry->key.kind == key.kind && entry->key.id == key.id &&
                entry->key.owner_shard_id == key.owner_shard_id && entry->task_id == task_id) {
                count++;
                zero = zero && entry->seq == 0;
            }
        }
    }
    rt_shard_unlock(shard);
    if (all_seq0 != NULL) {
        *all_seq0 = zero;
    }
    return count;
}

// Remote-reply waiters and their caller are owned by the same shard.  Hold that
// one lane while proving the Rule-13 terminal state, so WAITING, !enqueued, and
// the exact-key registration count describe one snapshot rather than three
// independently sampled moments.
static int rtb_seq0_caller_is_stranded(rt_executor* ex,
                                       waker_key key,
                                       const rt_task* task,
                                       size_t* out_entries) {
    rt_shard* shard = rt_waiter_key_shard(ex, key);
    if (shard == NULL || rt_task_owner_shard(ex, task) != shard) {
        return 0;
    }
    rt_shard_lock(shard);
    size_t entries = 0;
    const rt_waiter_store* store = rt_waiter_store_for_key(ex, key);
    if (store != NULL) {
        for (size_t i = 0; i < store->len; i++) {
            const waiter* entry = &store->entries[i];
            if (entry->key.kind == key.kind && entry->key.id == key.id &&
                entry->key.owner_shard_id == key.owner_shard_id && entry->task_id == task->id) {
                entries++;
            }
        }
    }
    int stranded = task_status_load(task) == TASK_WAITING && task_enqueued_load(task) == 0 &&
                   entries == 0;
    rt_shard_unlock(shard);
    if (out_entries != NULL) {
        *out_entries = entries;
    }
    return stranded;
}

static int rtb_seq0_wait_entries(rt_executor* ex,
                                 waker_key key,
                                 const rt_task* task,
                                 size_t want) {
    for (uint32_t i = 0; i < 4000; i++) {
        int all_seq0 = 0;
        size_t count = rtb_seq0_reply_entries(ex, key, task->id, &all_seq0);
        if (count == want && all_seq0 && task_status_load(task) == TASK_WAITING &&
            task_enqueued_load(task) == 0) {
            return 1;
        }
        rtb_sleep_us(1000);
    }
    return 0;
}

// Deterministic RV2-DEBT-320 stand.  The existing stale-removal point holds
// the first wake after it has republished the caller but before it can remove
// the old entry.  A sibling carrier then re-polls and publishes the second
// seq-0 registration.  Only after observing both does the driver release the
// deferred removal and deliver the one terminal select reply.
int rtb_mode_select_seq0_retry_terminal_drain(void) {
    rt_executor* ex = ensure_exec();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    rtb_select_state state;
    void* caller = NULL;
    if (rtb_select_park(ex, &chan_a, &chan_b, &state, &caller) != 0) {
        return 1;
    }
    const rt_task* caller_task = (const rt_task*)caller;
    rt_remote_task_pending* pending =
        atomic_load_explicit(&state.visible_pending, memory_order_acquire);
    if (caller_task == NULL || pending == NULL) {
        return rtb_fail("seq0 stand did not publish caller and pending state");
    }
    waker_key key = rt_remote_task_reply_key(pending->request_id, pending->source_shard_id);
    if (!rtb_seq0_wait_entries(ex, key, caller_task, 1)) {
        return rtb_fail("seq0 stand did not observe the first reply registration");
    }
    if (pending->handle.task_id == 0) {
        return rtb_fail("seq0 stand pending state did not name its one body");
    }
    unsigned before =
        rt_sync_point_reached_count(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL);
    rtb_seq0_waker waker = {ex, caller_task->id, 0};
    pthread_t thread;
    if (pthread_create(&thread, NULL, rtb_seq0_wake_thread, &waker) != 0) {
        return rtb_fail("seq0 stand could not start the spurious waker");
    }
    if (!rt_sync_point_wait_until_after(RT_SYNC_POINT_SP_WAKE_BEFORE_STALE_REMOVAL, before)) {
        rt_sync_point_open();
        (void)pthread_join(thread, NULL);
        return rtb_fail("seq0 stand missed the stale-removal window");
    }
    if (!rtb_seq0_wait_entries(ex, key, caller_task, 2)) {
        rt_sync_point_open();
        (void)pthread_join(thread, NULL);
        return rtb_fail("seq0 stand did not observe the two-entry retry park");
    }
    rt_sync_point_open();
    (void)pthread_join(thread, NULL);
    if (atomic_load_explicit(&waker.done, memory_order_acquire) != 1) {
        return rtb_fail("seq0 stand spurious waker did not retire");
    }
    int all_seq0 = 0;
    size_t after_removal = rtb_seq0_reply_entries(ex, key, caller_task->id, &all_seq0);
    fprintf(stderr, "seq0 window: first=1 retry=2 after_removal=%zu all_seq0=%d\n",
            after_removal, all_seq0);

    void* raw_a = rt_far_channel_resolve(ex, &chan_a.handle);
    if (raw_a == NULL) {
        return rtb_fail("seq0 stand could not resolve the winning channel");
    }
    // Keep the review oracle's pending snapshot live even if the positive
    // caller consumes its own reference between the bounded wait and sampling.
    rt_remote_task_pending_add_ref(pending);
    rt_channel_send_blocking(raw_a, rtb_word(42));
    for (uint32_t i = 0; i < 4000; i++) {
        if (rtb_select_counters_ok(ex, 1)) {
            break;
        }
        rtb_sleep_us(1000);
    }
    if (!rtb_select_counters_ok(ex, 1)) {
        rt_remote_task_pending_release(pending);
        return rtb_fail("seq0 stand did not produce one request and enqueued reply");
    }
    if (!rtb_wait_task_done(ex, caller_task->id, 250)) {
        uint8_t pending_kind = 0;
        rt_remote_task_status pending_status = RT_REMOTE_TASK_STATUS_PENDING;
        for (uint32_t i = 0; i < 4000 && pending_status == RT_REMOTE_TASK_STATUS_PENDING; i++) {
            pending_status = rt_remote_task_pending_snapshot(pending, &pending_kind);
            if (pending_status == RT_REMOTE_TASK_STATUS_PENDING) {
                rtb_sleep_us(1000);
            }
        }
        if (pending_status != RT_REMOTE_TASK_STATUS_OK || pending_kind != 1) {
            rt_remote_task_pending_release(pending);
            return rtb_fail("seq0 stand did not observe the committed terminal pending snapshot");
        }
        size_t entries = 0;
        if (!rtb_seq0_caller_is_stranded(ex, key, caller_task, &entries)) {
            rt_remote_task_pending_release(pending);
            return rtb_fail("seq0 stand did not observe the exact stranded caller snapshot");
        }
        fprintf(stderr,
                "seq0 stranded: pending=ok caller=waiting enqueued=0 entries=%zu requests=1 "
                "replies=1\n",
                entries);
        rt_remote_task_pending_release(pending);
        (void)rt_executor_request_shutdown(ex);
        return rtb_fail("seq0 negative control swept the fresh remote-reply registration");
    }
    rt_remote_task_pending_release(pending);
    uint8_t kind = 0;
    uint64_t bits = 0;
    (void)rtb_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 1 ||
        state.result_bits != 0 || kind != 1) {
        return rtb_fail("seq0 stand terminal outcome named the wrong arm");
    }
    size_t entries = rtb_seq0_reply_entries(ex, key, caller_task->id, NULL);
    if (entries != 0) {
        return rtb_fail("seq0 stand terminal drain left reply registrations");
    }
    fprintf(stderr,
            "seq0 complete: caller=done entries=0 requests=1 bodies=1 replies=1 after_removal=%zu\n",
            after_removal);
    if (rt_far_channel_release(ex, &chan_a.handle) != RT_REMOTE_TASK_STATUS_OK ||
        rt_far_channel_release(ex, &chan_b.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("seq0 stand release failed");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
