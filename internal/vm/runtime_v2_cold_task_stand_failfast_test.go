package vm_test

// The fail-fast half of the cold task stand's translation unit: a member cancelled while cold
// by the cancel-all its cancelled sibling raises (runtime_v2_cold_task_test.go). Appended after
// coldTaskStand, whose dispatch names these poll functions, and before coldTaskStandMain.
const coldTaskStandFailfast = `
static _Atomic uint32_t g_member_polls;
static _Atomic uint32_t g_member_after_release;
static _Atomic uint32_t g_local_alive;
static _Atomic(void*) g_member;
static _Atomic(void*) g_sibling;
static _Atomic uint32_t g_gate_seen;
static _Atomic uint32_t g_road_drop;
static _Atomic uint32_t g_road_wake;
static _Atomic uint32_t g_road_answered;
static _Atomic uint64_t g_reach_start;

// A member that borrows its owner's frame, as worker(&l) does. Polled at all,
// it says so, and whether the owner had already released the local it borrows:
// a read of freed storage in a compiled program.
static void poll_member(void) {
    atomic_fetch_add_explicit(&g_member_polls, 1, memory_order_acq_rel);
    if (atomic_load_explicit(&g_local_alive, memory_order_acquire) == 0) {
        atomic_fetch_add_explicit(&g_member_after_release, 1, memory_order_acq_rel);
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

static uint64_t monotonic_ns(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return (uint64_t)now.tv_sec * 1000000000u + (uint64_t)now.tv_nsec;
}

// The longest the owner waits for the cancel-all to reach its member: far
// below the package timeout, so a runtime that stops delivering it fails
// here, by name.
#define FAILFAST_REACH_NS (10ull * 1000000000ull)

// A fail-fast scope with two cold members. The owner cancels the sibling, which
// ends Cancelled and so cancels every member of the scope -- landing on the
// affine member while it is still cold and its handle still held. Only after
// the member's gate shows that cancel does the owner drop the handle, release
// the local the member borrows (the order compiled code has: a return's drops,
// then the scope's join) and join. Which road publishes the member -- the
// cancel's own wake, or the drop that finds the cancel in the gate -- is the
// scheduler's; it is recorded, not required, and both must leave the body
// unentered.
static void poll_owner_failfast_cold(void) {
    uint64_t pending = 0;
    bool failfast = false;
    uint32_t phase = atomic_load_explicit(&g_owner_phase, memory_order_acquire);
    if (phase == 0) {
        uint64_t scope = rt_scope_enter(true);
        atomic_store_explicit(&g_scope_id, scope, memory_order_release);
        void* sibling = create_child(0);
        void* member = __task_create_cold_affine(
            POLL_MEMBER, new_frame(), rt_channel_opaque_word_ops(), &cold_frame_ops);
        atomic_store_explicit(&g_sibling, sibling, memory_order_release);
        atomic_store_explicit(&g_member, member, memory_order_release);
        atomic_store_explicit(&g_local_alive, 1, memory_order_release);
        atomic_store_explicit(&g_reach_start, monotonic_ns(), memory_order_release);
        rt_task_cancel(sibling);
        atomic_store_explicit(&g_owner_phase, 1, memory_order_release);
        rt_async_yield(NULL, 0);
        return;
    }
    uint64_t scope = atomic_load_explicit(&g_scope_id, memory_order_acquire);
    if (phase == 1) {
        void* member = atomic_load_explicit(&g_member, memory_order_acquire);
        const rt_task* record = (const rt_task*)member;
        if (task_cancelled_load(record) == 0) {
            uint64_t started = atomic_load_explicit(&g_reach_start, memory_order_acquire);
            if (monotonic_ns() - started > FAILFAST_REACH_NS) {
                finish(82);
                return;
            }
            rt_async_yield(NULL, 0);
            return;
        }
        // The window, witnessed: the cancel is in the gate and the handle is
        // still held. The publication word says which road will publish it.
        atomic_fetch_add_explicit(&g_gate_seen, 1, memory_order_acq_rel);
        uint8_t road = rt_task_publication_load(record);
        if (road == RT_TASK_COLD) {
            atomic_fetch_add_explicit(&g_road_drop, 1, memory_order_acq_rel);
        } else if (road == RT_TASK_CANCELLED_COLD) {
            atomic_fetch_add_explicit(&g_road_wake, 1, memory_order_acq_rel);
        } else {
            atomic_fetch_add_explicit(&g_road_answered, 1, memory_order_acq_rel);
        }
        rt_task_handle_drop(member);
        atomic_store_explicit(&g_local_alive, 0, memory_order_release);
        rt_task_handle_drop(atomic_load_explicit(&g_sibling, memory_order_acquire));
        atomic_store_explicit(&g_owner_phase, 2, memory_order_release);
    }
    if (!rt_scope_join_all(scope, &pending, &failfast)) {
        rt_async_yield(NULL, 0);
        return;
    }
    rt_scope_exit(scope);
    finish(failfast ? 1 : 81);
}

`
