#ifndef SURGE_RUNTIME_NATIVE_RT_SCOPE_PROVENANCE_TRACE_H
#define SURGE_RUNTIME_NATIVE_RT_SCOPE_PROVENANCE_TRACE_H

#include <stddef.h>
#include <stdint.h>

void rt_scope_trace_identity_rewritten(void);
void rt_scope_trace_failfast_after_drained_answer(void);
uint64_t rt_scope_identity_rewritten_total(void);
uint64_t rt_scope_failfast_after_drained_answer_total(void);
void rt_scope_provenance_trace_reset(void);
void rt_scope_provenance_trace_append(char* buf, size_t* pos, size_t cap);

#endif // SURGE_RUNTIME_NATIVE_RT_SCOPE_PROVENANCE_TRACE_H
