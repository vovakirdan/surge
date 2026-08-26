#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_ENTITLEMENT_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_ENTITLEMENT_H

#include <stdint.h>

#include "rt_typed_carrier_abi.generated.h"

// Who may still ask a task for its result, counted by the task.
//
// A Task<T> handle is one machine word naming the task, so the handle itself
// carries no state; what §10 of the storage model calls an entitlement per
// handle lives here as COUNTS, and the counts are what decide the result's
// fate. They are deliberately not the handle refcount: pins held by
// completion, by a far reply and by a select also count references, and a
// value's ownership must never be inferred from those.
//
// live          handles the program can still ask through: the one a spawn
//               returned plus every clone, less every handle served or
//               dropped. While it is above one, a LATER asker can still come
//               and a take must leave the canonical value behind.
// clone_readers askers duplicating out of the canonical slot right now, with
//               no lock held. The value may not move while one is reading.
// move_waiting  the last asker arrived while readers were still out; it is
//               parked and the reader that retires last wakes it.
// moved         the canonical value left the slot through the final move.
// duplicate     how a second asker is served: the recipe the first clone
//               installed, or NULL when the type owns nothing (bytes are the
//               value) or cannot be duplicated (a second asker is refused).
typedef struct rt_task_entitlements {
    uint32_t live;
    uint32_t clone_readers;
    uint8_t move_waiting;
    uint8_t moved;
    rt_value_clone_init_fn duplicate;
} rt_task_entitlements;

// How one take serves its asker, decided under the owner lock.
typedef enum rt_task_take_mode {
    // No value to serve: the outcome is cancelled, or the slot is empty.
    RT_TASK_TAKE_NONE = 0,
    // The bytes are the value (nothing owned): copy them, leave the slot alone.
    RT_TASK_TAKE_COPY,
    // A later asker can still come: build this one an independent value.
    RT_TASK_TAKE_CLONE,
    // This is the last asker and nobody is reading: move the value out.
    RT_TASK_TAKE_MOVE,
    // This is the last asker but a reader is still out: park and retry.
    RT_TASK_TAKE_WAIT,
    // A later asker can still come and the value cannot be duplicated.
    RT_TASK_TAKE_REFUSED
} rt_task_take_mode;

struct rt_executor;
struct rt_task;

void rt_task_entitlements_init(rt_task_entitlements* entitlements);

// A clone: one more handle that can ask. The source handle must still be
// live; installs the duplication recipe the new asker will be served with.
void rt_task_entitlement_clone(struct rt_executor* ex,
                               struct rt_task* task,
                               rt_value_clone_init_fn duplicate);

// Begins serving one asker. Decides the mode from the counts and the value's
// own capabilities and records what the mode needs: a CLONE registers a
// reader, a MOVE closes the cohort. `has_value` and `droppable` describe the
// slot; the caller passes them because the slot belongs to the task, not to
// this record.
rt_task_take_mode rt_task_entitlement_begin_take(struct rt_executor* ex,
                                                 struct rt_task* task,
                                                 int has_value,
                                                 int droppable);

// Ends the take that `begin` started: retires this asker's entitlement, and
// for a CLONE retires its reader and wakes a mover that was waiting for it.
void rt_task_entitlement_finish_take(struct rt_executor* ex,
                                     struct rt_task* task,
                                     rt_task_take_mode mode);

// A handle dropped without asking: its entitlement retires and that is all.
void rt_task_entitlement_drop(struct rt_executor* ex, struct rt_task* task);

#endif
