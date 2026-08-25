#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_RESULT_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_RESULT_H

#include <stdint.h>

#include "rt_value_cell.h"

// A task's result is one rt_value_cell, and this file is what makes that cell a
// TASK's: how it is named from outside, and what proves the name still fits.
//
// WHY THE TASK OWNS THE CELL. A handle can ask for the result long after the
// poll frame that produced it is gone, so the value cannot live in the frame
// and it cannot live in the asker: there may be several askers and none of them
// exists yet when the task completes. The task outlives every handle on it, the
// same way a channel outlives every park on it.

// Names one task's result cell from outside the task, without naming the task's
// memory. A reply that travelled between shards must not carry an rt_task*: the
// task it points at can complete, be freed and have its id reused before the
// reply is read, and a pointer cannot say that happened. Four integers can --
// the id, the task's generation, the cell's own generation, and the shard that
// owns the lifecycle -- and every one of them is checked before the value is
// touched.
//
// Zeroed means "no result to fetch", which is what a cancelled or valueless
// outcome publishes.
typedef struct {
    uint64_t task_id;
    uint64_t task_generation;
    uint64_t result_generation;
    uint32_t owner_shard_id;
} rt_result_source;

// Whether a capability still names THIS result: same cell generation, and a
// value still in it. A capability that fails this names a result that has been
// taken, replaced, or never existed, and the caller must treat it as absent
// rather than as an error to retry.
int rt_task_result_matches(const rt_value_cell* cell, const rt_result_source* source);

#endif
