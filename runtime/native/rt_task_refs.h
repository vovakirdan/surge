#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_REFS_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_REFS_H
// How many handles a task has, and whether it has completed — in one word.
//
// The two live together because they are one question. A drop may free the task
// only when its own decrement BOTH emptied the count AND found the completion
// flag already set, and that has to be answered by the decrement's return value
// with nothing read afterwards.
//
// Asking it as a decrement plus a separate load of task->status was a double
// free. A poller holds the raw task pointer rather than a reference and takes
// one of its own, so a count that reached zero can be raised back to one,
// completed, and freed by that reference's drop — all inside the window between
// the first thread's decrement and its status load. That thread then read a
// freed task, saw TASK_DONE in the bytes, and freed it a second time; the free
// after that read the task's id out of reused memory, which is why the task
// table's store panicked with a heap pointer where an id belonged.
//
// A fresh task starts at plain 1, so a count of one and no flag is exactly what
// zeroed-then-initialised memory means.

#include <stdint.h>

typedef struct rt_task rt_task;

#define RT_TASK_REFS_COMPLETED 0x80000000u
#define RT_TASK_REFS_COUNT_MASK 0x7fffffffu

// Records that the task has completed, so a later drop can decide the free from
// its own decrement alone. Called beside the TASK_DONE store, while the caller
// still holds its own reference.
void task_mark_completed(rt_task* task);

#endif // SURGE_RUNTIME_NATIVE_RT_TASK_REFS_H
