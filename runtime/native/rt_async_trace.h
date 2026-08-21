#ifndef SURGE_RT_ASYNC_TRACE_H
#define SURGE_RT_ASYNC_TRACE_H

#include <stdint.h>

// What the scheduler REPORTS about itself.
//
// These say nothing about what the runtime does and everything about what an
// observer is told, which is why they are filed apart from the executor's own
// declarations: a trace point can be added or removed without any scheduling
// contract changing, and a reader looking for the contract should not have to
// step over them.

void rt_net_trace_dump(const char* reason);
void rt_trace_sched_tier1_steal_denied(void);
void rt_trace_sched_connection_owner_placed(void);
void rt_trace_sched_connection_owner_run(uint32_t owner_shard_id, uint32_t worker_shard_id);
void rt_trace_drain_signal_dump(void);

#endif // SURGE_RT_ASYNC_TRACE_H
