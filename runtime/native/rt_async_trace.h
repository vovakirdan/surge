#ifndef SURGE_RT_ASYNC_TRACE_H
#define SURGE_RT_ASYNC_TRACE_H

#include <stddef.h>
#include <stdint.h>

#include "rt_sched_trace.h"
#include "rt_scope_provenance_trace.h"

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
