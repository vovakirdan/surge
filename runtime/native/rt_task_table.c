#include "rt_async_internal.h"

// Segmented task table growth (Epic 8 Task 6, S5-Q1 realization B). This
// file owns the one concept the escalation added: lazily allocating the
// fixed-size directory's segments. get_task / rt_task_slot_store /
// rt_task_table_snapshot (the lock-free-read half of this protocol) stay in
// rt_async_state.c - G3 (runtime_v2_lifecycle_static_test.go) pins their
// exact acquire-load shape there, and get_task cannot be a delegating
// wrapper without losing that proof.
//
// Reused, not duplicated (Global Rule 5): ensure_task_cap is the same
// growth entry point checkpoint/sleep spawn and blocking submit already
// call while holding control (unchanged lane, unchanged contract - the
// caller holds control before calling it). __task_create's owner-lane path
// reuses it too, just wrapped in its own inline rt_control_lock/unlock
// bracket only when rt_task_table_segment_missing says the fast, lock-free
// path cannot proceed.

static int task_table_segment_index(uint64_t id, size_t* out_idx) {
    size_t seg_idx = (size_t)(id >> RT_TASK_TABLE_SEGMENT_SHIFT);
    if (seg_idx >= RT_TASK_TABLE_MAX_SEGMENTS) {
        return 0;
    }
    *out_idx = seg_idx;
    return 1;
}

// Caller holds the control lock. Double-checked: another creator may have
// published this segment between the caller's lock-free peek and this call.
static rt_task_segment* ensure_segment_locked(rt_executor* ex, size_t seg_idx) {
    rt_task_segment* segment =
        atomic_load_explicit(&ex->tasks_table.segments[seg_idx], memory_order_acquire);
    if (segment != NULL) {
        return segment;
    }
    segment = (rt_task_segment*)rt_alloc(sizeof(rt_task_segment), _Alignof(rt_task_segment));
    if (segment == NULL) {
        panic_msg("async: task segment allocation failed");
        return NULL;
    }
    for (size_t i = 0; i < RT_TASK_TABLE_SEGMENT_SIZE; i++) {
        atomic_store_explicit(&segment->slots[i], NULL, memory_order_relaxed);
    }
    // Publish with release: a lock-free get_task that observes this pointer
    // is guaranteed to see the zeroed slots above.
    atomic_store_explicit(&ex->tasks_table.segments[seg_idx], segment, memory_order_release);
    return segment;
}

void ensure_task_cap(rt_executor* ex, uint64_t id) {
    // Caller holds the control lock (checkpoint/sleep/blocking-submit
    // creators; unchanged lane, unchanged contract from before this task).
    size_t seg_idx;
    if (ex == NULL || !task_table_segment_index(id, &seg_idx)) {
        panic_msg("async: task capacity overflow");
        return;
    }
    (void)ensure_segment_locked(ex, seg_idx);
}

int rt_task_table_segment_missing(rt_executor* ex, uint64_t id) {
    size_t seg_idx;
    if (ex == NULL || !task_table_segment_index(id, &seg_idx)) {
        // Out of range reads as "missing" so the caller's slow path takes
        // control and lets ensure_task_cap raise the real overflow panic.
        return 1;
    }
    return atomic_load_explicit(&ex->tasks_table.segments[seg_idx], memory_order_acquire) == NULL;
}
