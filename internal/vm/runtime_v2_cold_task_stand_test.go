package vm_test

// The C translation unit of the cold task stand (runtime_v2_cold_task_test.go, RV2-DEBT-370):
// compiled against every runtime/native source but rt_entry.c, with a start frame whose
// descriptor counts its own destruction and a poll function per role.

const coldTaskStand = `
#define _POSIX_C_SOURCE 199309L
#include "rt_async_internal.h"
#include "rt_bignum_tag.h"
#include "rt_frame.h"
#include "rt_sync_point.h"
#include "rt_task_cold.h"

#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

int rt_argc = 0;
char** rt_argv_raw = NULL;

void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

#define POLL_CHILD 7001
#define POLL_OWNER_DROP 7002
#define POLL_OWNER_ESCAPE 7003
#define POLL_OWNER_AFFINE_INLINE 7004
#define POLL_OWNER_AFFINE_HANDOFF 7005
#define POLL_OWNER_FOREIGN 7006
#define POLL_OWNER_WALK_DROP 7007
#define POLL_OWNER_INLINE_SCOPE 7008
#define POLL_CHILD_OLDER 7009
#define POLL_MEMBER 7010
#define POLL_OWNER_FAILFAST_COLD 7011

// The start frame: field 0 is the lifecycle word a compiled frame opens with,
// PACKED as the constructor builds it, and one owned member.
typedef struct {
    void* lifecycle;
    char* text;
} cold_frame;

static _Atomic uint32_t g_frame_drops;
static _Atomic uint32_t g_child_polls;
static _Atomic uint32_t g_child_worker;
static _Atomic uint32_t g_owner_phase;
static _Atomic(void*) g_escaped;
static _Atomic uint32_t g_carrier;
static _Atomic uint64_t g_scope_id;
#ifdef RT_TEST_SYNC_POINTS
// The join-walk mode's state: that mode exists only in the armed build.
static _Atomic(void*) g_walk_child;
static _Atomic uint32_t g_walk_timeout;
static _Atomic uint64_t g_walk_child_id;
static _Atomic uint32_t g_walk_freed_early;
#endif
static _Atomic uint32_t g_owner_awaiting;
static pthread_t g_owner_thread;
static _Atomic uint32_t g_nested_latest;
static _Atomic uint32_t g_nested_older;
static _Atomic uint32_t g_older_cold_at_call;
static _Atomic uint32_t g_owner_verdict;
static _Atomic(void*) g_older;

static void cold_frame_move(void* dst, void* src) {
    memcpy(dst, src, sizeof(cold_frame));
}

static void cold_frame_drop(void* value) {
    cold_frame* frame = (cold_frame*)value;
    free(frame->text);
    frame->text = NULL;
    atomic_fetch_add_explicit(&g_frame_drops, 1, memory_order_acq_rel);
}

static rt_carrier_status
cold_frame_plan_cross(const void* src, rt_cross_mode mode, rt_cross_plan* out) {
    (void)src;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops cold_frame_ops = {
    .layout = {.size = sizeof(cold_frame),
               .align = _Alignof(cold_frame),
               .stride = sizeof(cold_frame),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = cold_frame_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = cold_frame_drop,
    .trace = NULL,
    .plan_cross = cold_frame_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static void* word(int64_t value) {
    bool ok = false;
    void* boxed = fixi_box(value, &ok);
    if (!ok) {
        fputs("stand: lifecycle word does not fit a fixnum\n", stderr);
        abort();
    }
    return boxed;
}

static cold_frame* new_frame(void) {
    cold_frame* frame = (cold_frame*)rt_frame_alloc(&cold_frame_ops);
    if (frame == NULL) {
        abort();
    }
    frame->lifecycle = word(SURGE_FRAME_STATE_PACKED);
    frame->text = (char*)malloc(7);
    if (frame->text == NULL) {
        abort();
    }
    memcpy(frame->text, "abcdef", 7);
    return frame;
}

static void* create_child(int affine) {
    cold_frame* frame = new_frame();
    return affine ? __task_create_cold_affine(
                        POLL_CHILD, frame, rt_channel_opaque_word_ops(), &cold_frame_ops)
                  : __task_create_cold(
                        POLL_CHILD, frame, rt_channel_opaque_word_ops(), &cold_frame_ops);
}

static int is_cold(const void* handle) {
    const rt_task* task = (const rt_task*)handle;
    return rt_task_publication_load(task) == RT_TASK_COLD && task_status_load(task) == TASK_READY &&
           task_enqueued_load(task) == 0;
}

// What a compiled body does: take the frame, own its members, give the storage
// back SPENT, and publish a result -- or unwind if it finds itself cancelled.
// Whether this child is being polled inside its owner's own await, on the
// owner's thread: the inline poll, as opposed to a turn of its own.
static uint32_t polled_inside_owner(void) {
    return atomic_load_explicit(&g_owner_awaiting, memory_order_acquire) != 0 &&
                   pthread_equal(pthread_self(), g_owner_thread)
               ? 1u
               : 0u;
}

static void poll_child(void);
// The fail-fast member and its owner: coldTaskStandFailfast, appended after
// this unit and before the driver.
static void poll_member(void);
static void poll_owner_failfast_cold(void);

// Only the cold claim can poll the older child inline while it is still cold:
// once the owner's await has published it, the base's local-tail claim may
// poll it inline too, when it happens to be the top of this worker's queue.
static void poll_child_older(void) {
    uint32_t claimed_cold =
        polled_inside_owner() && atomic_load_explicit(&g_older_cold_at_call, memory_order_acquire) != 0;
    atomic_store_explicit(&g_nested_older, claimed_cold, memory_order_release);
    poll_child();
}

static void poll_child(void) {
    atomic_fetch_add_explicit(&g_child_polls, 1, memory_order_acq_rel);
    if (tls_worker_ctx != NULL) {
        atomic_store_explicit(&g_child_worker, tls_worker_ctx->worker_id + 1, memory_order_release);
    }
    cold_frame* frame = (cold_frame*)__task_state();
    if (frame != NULL) {
        cold_frame_drop(frame);
        frame->lifecycle = word(SURGE_FRAME_STATE_SPENT);
        rt_frame_release(&cold_frame_ops, frame);
    }
    if (current_task_cancelled(ensure_exec())) {
        rt_async_return_cancelled(NULL, 0);
        return;
    }
    rt_async_return(NULL, &(uint64_t){7});
}

static void finish(uint32_t verdict) {
    rt_async_return(NULL, &(uint64_t){verdict});
}

// A fail-fast scope that creates a member cold and drops it: the join does not
// wait, the scope is not fail-fast, and the member never ran.
static void poll_owner_drop(void) {
    uint64_t scope = rt_scope_enter(true);
    void* child = create_child(0);
    const rt_scope* record = get_scope(ensure_exec(), scope);
    if (record == NULL || record->active_children != 1 || record->cold_children != 1 ||
        !is_cold(child)) {
        finish(11);
        return;
    }
    rt_task_handle_drop(child);
    if (record->active_children != 0 || record->cold_children != 0 || record->failfast_triggered) {
        finish(12);
        return;
    }
    uint64_t pending = 0;
    bool failfast = true;
    if (!rt_scope_join_all(scope, &pending, &failfast) || failfast) {
        finish(13);
        return;
    }
    rt_scope_exit(scope);
    finish(1);
}

// Whether this stand runs one worker: then nothing but the owner can poll a
// member before the owner yields. With more, the join's publication may be
// taken by another worker at once, and "not run yet" is no longer a promise.
static int single_worker(void) {
    const char* threads = getenv("SURGE_THREADS");
    return threads != NULL && strcmp(threads, "1") == 0;
}

// A member whose handle outlives the body: the join publishes it and waits.
static void poll_owner_escape(void) {
    uint64_t pending = 0;
    bool failfast = false;
    if (atomic_load_explicit(&g_owner_phase, memory_order_acquire) == 0) {
        uint64_t scope = rt_scope_enter(false);
        void* child = create_child(0);
        atomic_store_explicit(&g_escaped, child, memory_order_release);
        atomic_store_explicit(&g_owner_phase, (uint32_t)scope, memory_order_release);
        if (rt_scope_join_all(scope, &pending, &failfast)) {
            finish(21);
            return;
        }
        if (rt_task_publication_load((const rt_task*)child) != RT_TASK_PUBLISHED ||
            (single_worker() && atomic_load_explicit(&g_child_polls, memory_order_acquire) != 0)) {
            finish(22);
            return;
        }
        rt_async_yield(NULL, 0);
        return;
    }
    uint64_t scope = atomic_load_explicit(&g_owner_phase, memory_order_acquire);
    if (!rt_scope_join_all(scope, &pending, &failfast)) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_scope_exit(scope);
    finish(atomic_load_explicit(&g_child_polls, memory_order_acquire) == 1 ? 1 : 23);
}

// The creator's own await claims an affine child inline, on its carrier.
static void poll_owner_affine_inline(void) {
    void* child = create_child(1);
    const rt_task* task = (const rt_task*)child;
    if (!is_cold(child) || task->carrier_valid == 0 || tls_worker_ctx == NULL ||
        task->carrier_worker_id != tls_worker_ctx->worker_id) {
        finish(31);
        return;
    }
    atomic_store_explicit(&g_carrier, task->carrier_worker_id + 1, memory_order_release);
    uint64_t out = 0;
    if (rt_task_poll(child, &out) != 1 || out != 7) {
        finish(32);
        return;
    }
    finish(1);
}

// The creator records the pin and hands the handle out; a thread that is no
// worker publishes it later.
static void poll_owner_affine_handoff(void) {
    void* child = create_child(1);
    const rt_task* task = (const rt_task*)child;
    if (!is_cold(child) || task->carrier_valid == 0 || tls_worker_ctx == NULL ||
        task->carrier_worker_id != tls_worker_ctx->worker_id) {
        finish(41);
        return;
    }
    atomic_store_explicit(&g_carrier, task->carrier_worker_id + 1, memory_order_release);
    atomic_store_explicit(&g_escaped, child, memory_order_release);
    finish(1);
}

// How many scope events shard 0's transport has carried: the only road a lane
// that is not the scope's owner lane has to the scope's hint and count (Р6).
static uint64_t scope_events(void) {
    rt_executor* ex = ensure_exec();
    rt_shard* shard = rt_runtime_shard(rt_executor_runtime(ex), 0);
    struct rt_transport_debug_snapshot snap = rt_transport_debug_snapshot(shard);
    return snap.scope_child_done_events;
}

// A scope whose cold member the driver -- a thread running no task, so not the
// scope's owner lane -- publishes or drops between the owner's two turns. By
// the owner's second turn the event has been applied on the owner lane: the
// hint is back to zero, and the join waits only for a member that runs.
static void poll_owner_foreign(void) {
    uint32_t phase = atomic_load_explicit(&g_owner_phase, memory_order_acquire);
    uint64_t pending = 0;
    bool failfast = false;
    if (phase == 0) {
        uint64_t scope = rt_scope_enter(false);
        atomic_store_explicit(&g_scope_id, scope, memory_order_release);
        atomic_store_explicit(&g_escaped, create_child(0), memory_order_release);
        atomic_store_explicit(&g_owner_phase, 1, memory_order_release);
        rt_async_yield(NULL, 0);
        return;
    }
    uint64_t scope = atomic_load_explicit(&g_scope_id, memory_order_acquire);
    if (phase == 1) {
        atomic_store_explicit(&g_owner_phase, 2, memory_order_release);
        const rt_scope* record = get_scope(ensure_exec(), scope);
        if (record == NULL || record->cold_children != 0) {
            finish(51);
            return;
        }
    }
    if (!rt_scope_join_all(scope, &pending, &failfast)) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_scope_exit(scope);
    finish(1);
}

// The base polled a child inline only when it was the top of this worker's
// local queue; the cold claim takes only the awaiter's most recent creation.
// The owner creates an older child and then the latest one: awaiting the latest
// polls it inline, on this thread; awaiting the older one, while it is still
// cold, does not claim it. Only the owner holds the older handle, so whether
// it is cold when an await begins cannot change under that await's feet.
static void poll_owner_inline_scope(void) {
    uint64_t out = 0;
    if (atomic_load_explicit(&g_owner_phase, memory_order_acquire) == 0) {
        g_owner_thread = pthread_self();
        void* older = __task_create_cold(
            POLL_CHILD_OLDER, new_frame(), rt_channel_opaque_word_ops(), &cold_frame_ops);
        void* latest = create_child(0);
        atomic_store_explicit(&g_older, older, memory_order_release);
        atomic_store_explicit(&g_owner_phase, 1, memory_order_release);
        atomic_store_explicit(&g_owner_awaiting, 1, memory_order_release);
        uint8_t latest_done = rt_task_poll(latest, &out);
        atomic_store_explicit(&g_owner_awaiting, 0, memory_order_release);
        if (latest_done != 1 || atomic_load_explicit(&g_nested_latest, memory_order_acquire) != 1) {
            finish(61);
            return;
        }
    }
    void* older = atomic_load_explicit(&g_older, memory_order_acquire);
    atomic_store_explicit(&g_older_cold_at_call, is_cold(older) ? 1u : 0u, memory_order_release);
    atomic_store_explicit(&g_owner_awaiting, 1, memory_order_release);
    uint8_t older_done = rt_task_poll(older, &out);
    atomic_store_explicit(&g_owner_awaiting, 0, memory_order_release);
    if (older_done == 0) {
        rt_async_yield(NULL, 0);
        return;
    }
    finish(atomic_load_explicit(&g_nested_older, memory_order_acquire) == 0 ? 1 : 62);
}

// A join whose walk is held after get_task while another thread drops the
// escaped member's last handle: the drop discards it inside the window, the
// walk then finds it DISCARDED and wakes nothing, and the member's free waits
// for the walk to let the control lane go (RV2-DEBT-370, packet section 00).
static void poll_owner_walk_drop(void) {
    uint64_t pending = 0;
    bool failfast = false;
    if (atomic_load_explicit(&g_owner_phase, memory_order_acquire) == 0) {
        uint64_t scope = rt_scope_enter(false);
        atomic_store_explicit(&g_scope_id, scope, memory_order_release);
        atomic_store_explicit(&g_escaped, create_child(0), memory_order_release);
        atomic_store_explicit(&g_owner_phase, 1, memory_order_release);
    }
    uint64_t scope = atomic_load_explicit(&g_scope_id, memory_order_acquire);
    if (!rt_scope_join_all(scope, &pending, &failfast)) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_scope_exit(scope);
    finish(1);
}

void __surge_poll_call(uint64_t id) {
    switch (id) {
        case POLL_CHILD:
            atomic_store_explicit(&g_nested_latest, polled_inside_owner(), memory_order_release);
            poll_child();
            return;
        case POLL_CHILD_OLDER:
            poll_child_older();
            return;
        case POLL_OWNER_INLINE_SCOPE:
            poll_owner_inline_scope();
            return;
        case POLL_OWNER_DROP:
            poll_owner_drop();
            return;
        case POLL_OWNER_ESCAPE:
            poll_owner_escape();
            return;
        case POLL_OWNER_AFFINE_INLINE:
            poll_owner_affine_inline();
            return;
        case POLL_OWNER_WALK_DROP:
            poll_owner_walk_drop();
            return;
        case POLL_OWNER_FOREIGN:
            poll_owner_foreign();
            return;
        case POLL_OWNER_AFFINE_HANDOFF:
            poll_owner_affine_handoff();
            return;
        case POLL_MEMBER:
            poll_member();
            return;
        case POLL_OWNER_FAILFAST_COLD:
            poll_owner_failfast_cold();
            return;
        default:
            fprintf(stderr, "stand: unknown poll id %llu\n", (unsigned long long)id);
            abort();
    }
}

static int fail(const char* mode, const char* why) {
    fprintf(stderr,
            "COLD_STAND_FAIL %s: %s (frame drops %u, child polls %u, owner verdict %u)\n",
            mode,
            why,
            atomic_load(&g_frame_drops),
            atomic_load(&g_child_polls),
            atomic_load(&g_owner_verdict));
    return 1;
}
`
