#ifndef SURGE_RT_ASYNC_TRACE_H
#define SURGE_RT_ASYNC_TRACE_H

#include <signal.h>
#include <stddef.h>
#include <stdint.h>

#include "rt_sched_trace.h"
#include "rt_scope_provenance_trace.h"

// Whether SURGE_TRACE_EXEC asked for the execution trace. Set once at init
// (rt_async_trace.c), read at every trace point; the read is inline because
// forty trace points sit on the send/receive/select paths and a call per
// point was a measurable share of a local select's cost (2026-09-04,
// select-send-scalar: rt_exec_trace_enabled +18 instructions per operation).
// always_inline because the runtime is compiled without -O
// (internal/buildpipeline: `clang -c -std=c11 -g`), where a plain inline is
// still a call.
extern volatile sig_atomic_t rt_exec_trace_enabled_flag;

static inline __attribute__((always_inline)) int rt_exec_trace_enabled(void) {
    return rt_exec_trace_enabled_flag != 0;
}

// What the scheduler REPORTS about itself.
//
// These say nothing about what the runtime does and everything about what an
// observer is told, which is why they are filed apart from the executor's own
// declarations: a trace point can be added or removed without any scheduling
// contract changing, and a reader looking for the contract should not have to
// step over them.

void rt_net_trace_dump(const char* reason);
void rt_trace_drain_signal_dump(void);

// Shared record formatting. A trace record is assembled into a caller-owned
// buffer and written once, so these never allocate, never touch errno, and
// truncate rather than overrun: a record that did not fit is short, and the
// writer counts it as dropped rather than growing the buffer under a signal.
size_t trace_append_literal(char* buf, size_t pos, size_t cap, const char* lit);
size_t trace_append_u64(char* buf, size_t pos, size_t cap, uint64_t value);
void trace_append_kv_u64(char* buf, size_t* pos, size_t cap, const char* name, uint64_t value);

#endif // SURGE_RT_ASYNC_TRACE_H
