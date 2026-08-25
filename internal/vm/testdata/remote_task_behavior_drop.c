#include "remote_task_behavior.h"
#include "rt_far_channel.h"
#include "rt_placement.h"

#include <string.h>

// Exactly-once-drop rows for shipped crossing states (the no-remote-owner
// half of the migration matrix): every terminal edge that abandons a
// request before a body exists destructs the shipped state exactly once
// through __surge_drop_call, and the successful handoff never does.

enum { RTB_DROP_MARK_ID = 42 };

// The shipped body: trivial and state-agnostic — the drop rows prove the
// PENDING lifecycle, and a body that runs treats the shipped state as
// borrowed bits.
static void poll_rtb_drop_body(void) {
    rt_async_return(NULL, rtb_word(5));
}

// A parked, state-agnostic body: the recv reaches the channel through the
// pending BINDING, so the shipped state stays untouched borrowed bits.
static void poll_rtb_drop_recv_body(void) {
    uint64_t bits = 0;
    (void)rt_anchored_channel_recv(&bits);
    rt_async_return(NULL, rtb_word(5));
}

typedef struct rtb_drop_state {
    rt_remote_task_pending* pending;
    _Atomic(rt_remote_task_pending*) visible_pending;
    uint64_t placement;
    uint32_t flood_destination;
    rt_far_task_handle anchor;
    rt_far_task_handle anchor_slots[2];
    const rt_far_task_handle* anchors[2];
    uint8_t kinds[2];
    uint64_t bits[2];
    // One address per arm, pointing into `bits`: a SEND payload travels by
    // address now, moved out of the caller's storage and back into it.
    void* addrs[2];
    uint64_t shipped_mark;
    rt_remote_task_status status;
    uint8_t result_kind;
    uint64_t result_bits;
} rtb_drop_state;

static void poll_rtb_drop_execute(rtb_drop_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute(state->placement,
                                            RTB_DROP_MARK_ID,
                                            0,
                                            (int64_t)POLL_RTB_DROP_BODY,
                                            &state->shipped_mark,
                                            &state->pending,
                                            &kind,
                                            &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

// The parked-body variant: ships the anchored recv body so the block
// suspends owner-side until cancelled.
static void poll_rtb_drop_anchored_recv(rtb_drop_state* state) {
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute_anchored(&state->anchor,
                                                     RTB_DROP_MARK_ID,
                                                     0,
                                                     (int64_t)POLL_RTB_DROP_RECV_BODY,
                                                     &state->shipped_mark,
                                                     &state->pending,
                                                     &kind,
                                                     &bits);
    atomic_store_explicit(&state->visible_pending, state->pending, memory_order_release);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

static void poll_rtb_drop_anchored(rtb_drop_state* state) {
    if (state->flood_destination != 0 && state->pending == NULL) {
        rt_executor* ex = ensure_exec();
        rt_shard* owner = rt_runtime_shard(rt_executor_runtime(ex), state->anchor.owner_shard_id);
        rt_shard_lock(owner);
        memset(owner->transport.data, 0, sizeof(owner->transport.data));
        owner->transport.data_head = 0;
        owner->transport.data_len = RT_TRANSPORT_DATA_QUEUE_CAP;
        rt_shard_unlock(owner);
    }
    uint8_t kind = 0;
    uint64_t bits = 0;
    state->status = rt_immediate_on_execute_anchored(&state->anchor,
                                                     RTB_DROP_MARK_ID,
                                                     0,
                                                     (int64_t)POLL_RTB_DROP_BODY,
                                                     &state->shipped_mark,
                                                     &state->pending,
                                                     &kind,
                                                     &bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    state->result_kind = kind;
    state->result_bits = bits;
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

static void poll_rtb_drop_select(rtb_drop_state* state) {
    rtb_select_bind_addrs(state->addrs, state->bits, state->kinds, 2);
    state->status = rt_far_channel_select(state->anchors,
                                          state->kinds,
                                          state->addrs,
                                          NULL,
                                          2,
                                          RTB_DROP_MARK_ID,
                                          (int64_t)POLL_RTB_DROP_BODY,
                                          &state->shipped_mark,
                                          &state->pending,
                                          &state->result_kind,
                                          &state->result_bits);
    if (state->status == RT_REMOTE_TASK_STATUS_PENDING) {
        rt_async_yield(state, 0);
    }
    rt_async_return(state, rtb_word((uint64_t)state->status));
}

void rtb_drop_poll_dispatch(uint64_t id) {
    if (id == POLL_RTB_DROP_BODY) {
        poll_rtb_drop_body();
    }
    if (id == POLL_RTB_DROP_ANCHORED_RECV) {
        poll_rtb_drop_anchored_recv((rtb_drop_state*)__task_state());
    }
    if (id == POLL_RTB_DROP_RECV_BODY) {
        poll_rtb_drop_recv_body();
    }
    if (id == POLL_RTB_DROP_EXECUTE) {
        poll_rtb_drop_execute((rtb_drop_state*)__task_state());
    }
    if (id == POLL_RTB_DROP_ANCHORED) {
        poll_rtb_drop_anchored((rtb_drop_state*)__task_state());
    }
    if (id == POLL_RTB_DROP_SELECT) {
        poll_rtb_drop_select((rtb_drop_state*)__task_state());
    }
}

static void rtb_drop_reset(void) {
    atomic_store_explicit(&rtb_drop_calls, 0, memory_order_release);
    atomic_store_explicit(&rtb_drop_last_id, 0, memory_order_release);
    atomic_store_explicit(&rtb_drop_last_state, NULL, memory_order_release);
}

static int rtb_drop_expect_exactly_once(const rtb_drop_state* state, const char* label) {
    int settled = 0;
    for (uint32_t i = 0; i < 4000 && !settled; i++) {
        settled = atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) == 1;
        if (!settled) {
            rtb_sleep_us(1000);
        }
    }
    if (!settled) {
        rtb_fail(label);
        return rtb_fail("shipped state was not dropped exactly once");
    }
    rtb_sleep_us(20000);
    if (atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) != 1) {
        rtb_fail(label);
        return rtb_fail("shipped state was dropped more than once");
    }
    if (atomic_load_explicit(&rtb_drop_last_id, memory_order_acquire) != RTB_DROP_MARK_ID ||
        (uintptr_t)atomic_load_explicit(&rtb_drop_last_state, memory_order_acquire) !=
            (uintptr_t)&state->shipped_mark) {
        rtb_fail(label);
        return rtb_fail("drop carried the wrong id or state");
    }
    return 0;
}

// Row: invalid placement (out-of-range shard) resumes Cancelled without a
// body; the call site owns the drop.
int rtb_mode_drop_invalid_placement(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    static rtb_drop_state state;
    memset(&state, 0, sizeof(state));
    state.placement = rt_placement_shard(7);
    void* caller = __task_create(POLL_RTB_DROP_EXECUTE, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 2) {
        return rtb_fail("invalid-placement row did not resume Cancelled");
    }
    int rc = rtb_drop_expect_exactly_once(&state, "drop-invalid-placement");
    (void)rt_executor_request_shutdown(ex);
    return rc;
}

// Row: a stale anchor answers without a body; the pending's terminal
// cleanup owns the drop.
int rtb_mode_drop_stale_anchor(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("drop stale-anchor mint failed");
    }
    if (rt_far_channel_release(ex, &minted.handle) != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("drop stale-anchor release failed");
    }
    static rtb_drop_state state;
    memset(&state, 0, sizeof(state));
    state.anchor = minted.handle;
    void* caller = __task_create(POLL_RTB_DROP_ANCHORED, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_STALE_TOKEN) {
        return rtb_fail("stale-anchor row did not answer stale");
    }
    int rc = rtb_drop_expect_exactly_once(&state, "drop-stale-anchor");
    (void)rt_executor_request_shutdown(ex);
    return rc;
}

// Row: the initial request enqueue refuses synchronously with a saturated
// destination data lane (the corrected queue-full premise: no rollback —
// the pending's terminal cleanup owns the drop).
int rtb_mode_drop_queue_full(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("drop queue-full mint failed");
    }
    static rtb_drop_state state;
    memset(&state, 0, sizeof(state));
    state.anchor = minted.handle;
    state.flood_destination = 1;
    void* caller = __task_create(POLL_RTB_DROP_ANCHORED, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_QUEUE_FULL) {
        return rtb_fail("queue-full row did not refuse synchronously");
    }
    int rc = rtb_drop_expect_exactly_once(&state, "drop-queue-full");
    (void)rt_executor_request_shutdown(ex);
    return rc;
}

// Row: mixed-owner select arms refuse synchronously at the call site,
// which owns the drop.
int rtb_mode_drop_select_mixed_owners(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    rtb_create_state chan_a;
    rtb_create_state chan_b;
    if (!rtb_mint_channel(&chan_a, rt_placement_shard(0), 1) ||
        !rtb_mint_channel(&chan_b, rt_placement_shard(1), 1)) {
        return rtb_fail("drop mixed-owner mint failed");
    }
    static rtb_drop_state state;
    memset(&state, 0, sizeof(state));
    state.anchor_slots[0] = chan_a.handle;
    state.anchor_slots[1] = chan_b.handle;
    state.anchors[0] = &state.anchor_slots[0];
    state.anchors[1] = &state.anchor_slots[1];
    state.kinds[0] = SELECT_CHAN_RECV;
    state.kinds[1] = SELECT_CHAN_RECV;
    void* caller = __task_create(POLL_RTB_DROP_SELECT, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_INVALID_ARGUMENT) {
        return rtb_fail("mixed-owner row did not refuse");
    }
    int rc = rtb_drop_expect_exactly_once(&state, "drop-select-mixed-owners");
    (void)rt_far_channel_release(ex, &chan_a.handle);
    (void)rt_far_channel_release(ex, &chan_b.handle);
    (void)rt_executor_request_shutdown(ex);
    return rc;
}

// Negative control: a successful handoff must NOT drop through the pending
// — the published body owns the state from the handoff on (the body here
// is the harness poll fn, which treats the state as borrowed and returns).
int rtb_mode_drop_handoff_not_dropped(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    static rtb_drop_state state;
    memset(&state, 0, sizeof(state));
    state.placement = rt_placement_shard(1);
    uint8_t kind = 0;
    uint64_t bits = 0;
    void* caller = __task_create(POLL_RTB_DROP_EXECUTE, &state, rt_channel_opaque_word_ops());
    rt_task_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK) {
        return rtb_fail("handoff row did not complete");
    }
    rtb_sleep_us(50000);
    if (atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("handoff row dropped through the pending despite a published body");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Row (bound cancel): a droppable state handed off to a PUBLISHED body
// must never drop through the pending, even when the caller's cancel is
// routed and the body completes Cancelled — the reply edge and the
// orphaned-reply consumption leave the obligation with the body family.
int rtb_mode_drop_bound_cancel_no_pending_drop(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    rtb_create_state minted;
    if (!rtb_mint_channel(&minted, rt_placement_shard(1), 1)) {
        return rtb_fail("drop bound-cancel mint failed");
    }
    static rtb_drop_state state;
    memset(&state, 0, sizeof(state));
    state.anchor = minted.handle;
    // The anchored recv body parks on the empty channel, so the request is
    // provably bound and suspended when the cancel lands.
    void* caller = __task_create(POLL_RTB_DROP_ANCHORED_RECV, &state, rt_channel_opaque_word_ops());
    rt_remote_task_pending* pending = NULL;
    for (uint32_t i = 0; i < 4000 && pending == NULL; i++) {
        pending = atomic_load_explicit(&state.visible_pending, memory_order_acquire);
        if (pending == NULL) {
            rtb_sleep_us(1000);
        }
    }
    if (pending == NULL) {
        return rtb_fail("bound-cancel request never became visible");
    }
    uint64_t body_id = 0;
    for (uint32_t i = 0; i < 4000 && body_id == 0; i++) {
        body_id = pending->handle.task_id;
        if (body_id == 0) {
            rtb_sleep_us(1000);
        }
    }
    if (body_id == 0) {
        return rtb_fail("bound-cancel body was never bound");
    }
    rt_task_cancel(caller);
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (kind != 2) {
        return rtb_fail("bound-cancel caller did not resume as Cancelled");
    }
    rtb_sleep_us(50000);
    if (atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("bound-cancel dropped through the pending after handoff");
    }
    for (uint32_t i = 0; i < 4000; i++) {
        if (rt_far_channel_release(ex, &minted.handle) == RT_REMOTE_TASK_STATUS_OK) {
            break;
        }
        rtb_sleep_us(1000);
    }
    int clean = 0;
    for (uint32_t i = 0; i < 4000 && !clean; i++) {
        clean = rt_far_channel_debug_live_count(ex) == 0;
        if (!clean) {
            rtb_sleep_us(1000);
        }
    }
    if (!clean) {
        return rtb_fail("bound-cancel census found residue");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// RV2-DEBT-053a owner-side result reclamation rows. These bypass the async
// scheduler and drive free_task directly on a body task that completed with a
// heap-carried RESULT, which is the exact leak site: a completed owner task
// freed (release-while-DONE / cancel-after-done) before any consumer took its
// reply. A real far-task heap result is unreachable from compiled Surge today
// (the non-copy reply gate is still closed), so a direct free-path drive is
// the deterministic proof of the owner-side machinery.
enum { RTB_RESULT_DROP_MARK_ID = 53 };

// The result descriptor these rows drive free_task with: one machine word
// holding a heap block, whose drop frees it and is counted.
//
// It is what result_drop_fn_id used to say. The obligation travels with the
// value's TYPE now rather than in a number beside it, so the row that proves
// "an unconsumed result is destroyed exactly once" drives the same behaviour
// through the descriptor the task was created with.
static void rtb_result_block_move(void* destination, void* source) {
    *(void**)destination = *(void**)source;
    *(void**)source = NULL;
}

static void rtb_result_block_drop(void* value) {
    void* block = *(void**)value;
    *(void**)value = NULL;
    atomic_fetch_add_explicit(&rtb_result_drop_calls, 1, memory_order_acq_rel);
    atomic_store_explicit(&rtb_result_drop_last_id, RTB_RESULT_DROP_MARK_ID, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_value, block, memory_order_release);
    if (block != NULL) {
        rt_free((uint8_t*)block, RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    }
}

static rt_carrier_status
rtb_result_block_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops rtb_result_block_ops = {
    .layout = {.size = sizeof(void*),
               .align = _Alignof(void*),
               .stride = sizeof(void*),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = rtb_result_block_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = rtb_result_block_drop,
    .trace = NULL,
    .plan_cross = rtb_result_block_plan,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

// Publishes one heap block as the task's canonical result.
static int rtb_publish_result_block(rt_task* task, void* block) {
    if (rt_value_cell_bind(&task->result, &rtb_result_block_ops) != RT_SLOT_CONTROL_OK) {
        return 0;
    }
    void* destination = rt_value_cell_publish_storage(&task->result);
    if (destination == NULL) {
        return 0;
    }
    *(void**)destination = block;
    return rt_value_cell_commit(&task->result) == RT_SLOT_CONTROL_OK;
}

static void rtb_result_drop_reset(void) {
    atomic_store_explicit(&rtb_result_drop_calls, 0, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_id, 0, memory_order_release);
    atomic_store_explicit(&rtb_result_drop_last_value, NULL, memory_order_release);
}

// Row: a DONE owner task whose heap result nobody consumed is reclaimed by
// free_task exactly once, with the registered id and the actual result_bits
// pointer.
int rtb_mode_result_owner_release(void) {
    rt_executor* ex = ensure_exec();
    rtb_result_drop_reset();
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return rtb_fail("result-owner-release: body task creation failed");
    }
    void* block = rt_alloc(RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    if (block == NULL) {
        return rtb_fail("result-owner-release: block alloc failed");
    }
    task->result_kind = 1;
    if (!rtb_publish_result_block(task, block)) {
        return rtb_fail("result-owner-release: result publication failed");
    }
    task_status_store(task, TASK_DONE);
    // The create-time reference is the only one: releasing it frees the DONE
    // task through free_task, whose dispose owns the unconsumed-result drop.
    task_release_lane_aware(ex, task);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 1) {
        return rtb_fail("result-owner-release: result not dropped exactly once");
    }
    if (atomic_load_explicit(&rtb_result_drop_last_id, memory_order_acquire) !=
            RTB_RESULT_DROP_MARK_ID ||
        atomic_load_explicit(&rtb_result_drop_last_value, memory_order_acquire) != block) {
        return rtb_fail("result-owner-release: drop carried the wrong id or value");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: a Copy result keeps result_drop_fn_id 0, so inert bits are
// never handed to the result-drop dispatch.
int rtb_mode_result_copy_inert(void) {
    rt_executor* ex = ensure_exec();
    rtb_result_drop_reset();
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return rtb_fail("result-copy-inert: body task creation failed");
    }
    task->result_kind = 1;
    // Inert Copy bits (fixnum-shaped), not a heap pointer: the opaque-word
    // descriptor carries them and owns nothing, which is what "no obligation"
    // is now spelled as.
    (void)rt_value_cell_bind(&task->result, rt_channel_opaque_word_ops());
    void* inert = rt_value_cell_publish_storage(&task->result);
    if (inert == NULL) {
        return rtb_fail("result-copy-inert: result publication failed");
    }
    *(uint64_t*)inert = 42;
    (void)rt_value_cell_commit(&task->result);
    task_status_store(task, TASK_DONE);
    task_release_lane_aware(ex, task);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("result-copy-inert: inert Copy result reached the drop dispatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: a consumed result cleared the obligation when ownership
// transferred to the caller; free_task must NOT drop again (no double-free).
int rtb_mode_result_consumed_no_double_drop(void) {
    rt_executor* ex = ensure_exec();
    rtb_result_drop_reset();
    rt_task* task = NULL;
    if (rt_remote_spawn_create_body_task(ex, POLL_RTB_DROP_BODY, NULL, 0, 0, &task) !=
            RT_REMOTE_SPAWN_STATUS_OK ||
        task == NULL) {
        return rtb_fail("result-consumed: body task creation failed");
    }
    void* block = rt_alloc(RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    if (block == NULL) {
        return rtb_fail("result-consumed: block alloc failed");
    }
    task->result_kind = 1;
    if (!rtb_publish_result_block(task, block)) {
        return rtb_fail("result-consumed: result publication failed");
    }
    task_status_store(task, TASK_DONE);
    // Simulate the compiled consume path: the value MOVES to the caller, which
    // leaves the slot with nothing to destroy, and the caller frees it.
    void* taken = NULL;
    rtb_result_block_move((void*)&taken, rt_value_cell_value(&task->result));
    (void)rt_value_cell_commit_move(&task->result);
    rt_free((uint8_t*)taken, RTB_RESULT_BLOCK_SIZE, RTB_RESULT_BLOCK_ALIGN);
    task_release_lane_aware(ex, task);
    if (atomic_load_explicit(&rtb_result_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("result-consumed: free_task double-dropped a consumed result");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}

// Negative control: drop-fn id 0 never dispatches on any edge.
int rtb_mode_drop_zero_id_never_dispatches(void) {
    rt_executor* ex = ensure_exec();
    rtb_drop_reset();
    static rtb_execute_state state;
    memset(&state, 0, sizeof(state));
    state.placement = rt_placement_shard(7);
    state.body_poll_id = POLL_RTB_CHILD;
    static rtb_child_state child;
    memset(&child, 0, sizeof(child));
    atomic_store_explicit(&child.gate, 1, memory_order_release);
    state.body_state = &child;
    void* caller = __task_create(POLL_RTB_EXECUTE, &state, rt_channel_opaque_word_ops());
    uint8_t kind = 0;
    uint64_t bits = 0;
    rt_task_await(caller, &kind, &bits);
    if (state.status != RT_REMOTE_TASK_STATUS_OK || state.result_kind != 2) {
        return rtb_fail("zero-id row did not take the refusal edge");
    }
    rtb_sleep_us(20000);
    if (atomic_load_explicit(&rtb_drop_calls, memory_order_acquire) != 0) {
        return rtb_fail("zero-id state reached the drop dispatch");
    }
    (void)rt_executor_request_shutdown(ex);
    return 0;
}
