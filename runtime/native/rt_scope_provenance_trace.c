#include "rt_async_trace.h"

#include <stdatomic.h>

static _Atomic uint64_t scope_identity_rewritten;
static _Atomic uint64_t scope_failfast_after_drained_answer;

void rt_scope_trace_identity_rewritten(void) {
    (void)atomic_fetch_add_explicit(&scope_identity_rewritten, 1, memory_order_relaxed);
}

void rt_scope_trace_failfast_after_drained_answer(void) {
    (void)atomic_fetch_add_explicit(&scope_failfast_after_drained_answer, 1, memory_order_relaxed);
}

uint64_t rt_scope_identity_rewritten_total(void) {
    return atomic_load_explicit(&scope_identity_rewritten, memory_order_relaxed);
}

uint64_t rt_scope_failfast_after_drained_answer_total(void) {
    return atomic_load_explicit(&scope_failfast_after_drained_answer, memory_order_relaxed);
}

void rt_scope_provenance_trace_reset(void) {
    atomic_store_explicit(&scope_identity_rewritten, 0, memory_order_relaxed);
    atomic_store_explicit(&scope_failfast_after_drained_answer, 0, memory_order_relaxed);
}

void rt_scope_provenance_trace_append(char* buf, size_t* pos, size_t cap) {
    trace_append_kv_u64(
        buf, pos, cap, "scope_identity_rewritten", rt_scope_identity_rewritten_total());
    trace_append_kv_u64(buf,
                        pos,
                        cap,
                        "scope_failfast_after_drained_answer",
                        rt_scope_failfast_after_drained_answer_total());
}
