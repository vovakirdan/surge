#include "rt_async_internal.h"

// Segmented scope table growth (S5-Q7). Mirror of
// rt_task_table.c (Global Rule 5 - reuse the proven never-moved-slot pattern):
// this file owns lazily allocating the fixed-size directory's segments.
// get_scope / rt_scope_slot_store (the lock-free-read half of the protocol)
// stay in rt_async_state.c - the P9 lifecycle static gate pins get_scope's
// acquire-load shape there, and get_scope cannot be a delegating wrapper
// without losing that proof.
//
// Scope enter reuses ensure_scope_cap under its own inline control bracket only
// when rt_scope_table_segment_missing says the fast, lock-free path cannot
// proceed; checkpoint/sleep/blocking creators never enter scopes, so unlike the
// task table this growth entry point has a single caller lane.

static int scope_table_segment_index(uint64_t id, size_t* out_idx) {
    size_t seg_idx = (size_t)(id >> RT_SCOPE_TABLE_SEGMENT_SHIFT);
    if (seg_idx >= RT_SCOPE_TABLE_MAX_SEGMENTS) {
        return 0;
    }
    *out_idx = seg_idx;
    return 1;
}

// Caller holds the control lock. Double-checked: another scope enter may have
// published this segment between the caller's lock-free peek and this call.
static rt_scope_segment* ensure_scope_segment_locked(rt_executor* ex, size_t seg_idx) {
    rt_scope_segment* segment =
        atomic_load_explicit(&ex->scopes_table.segments[seg_idx], memory_order_acquire);
    if (segment != NULL) {
        return segment;
    }
    segment = (rt_scope_segment*)rt_alloc(sizeof(rt_scope_segment), _Alignof(rt_scope_segment));
    if (segment == NULL) {
        panic_msg("async: scope segment allocation failed");
        return NULL;
    }
    for (size_t i = 0; i < RT_SCOPE_TABLE_SEGMENT_SIZE; i++) {
        atomic_store_explicit(&segment->slots[i], NULL, memory_order_relaxed);
    }
    // Publish with release: a lock-free get_scope that observes this pointer is
    // guaranteed to see the zeroed slots above.
    atomic_store_explicit(&ex->scopes_table.segments[seg_idx], segment, memory_order_release);
    return segment;
}

void ensure_scope_cap(rt_executor* ex, uint64_t id) {
    // Caller holds the control lock (rt_scope_enter's rare growth branch).
    size_t seg_idx;
    if (ex == NULL || !scope_table_segment_index(id, &seg_idx)) {
        panic_msg("async: scope capacity overflow");
        return;
    }
    (void)ensure_scope_segment_locked(ex, seg_idx);
}

int rt_scope_table_segment_missing(rt_executor* ex, uint64_t id) {
    size_t seg_idx;
    if (ex == NULL || !scope_table_segment_index(id, &seg_idx)) {
        // Out of range reads as "missing" so the caller's slow path takes
        // control and lets ensure_scope_cap raise the real overflow panic.
        return 1;
    }
    return atomic_load_explicit(&ex->scopes_table.segments[seg_idx], memory_order_acquire) == NULL;
}
