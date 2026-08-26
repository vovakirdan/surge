#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_ENTITLEMENT_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_ENTITLEMENT_H

#include <stdatomic.h>
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
// live          handles that have not been served or dropped: the one a spawn
//               returned plus every clone, less every take that finished and
//               every drop. A live handle that is not claimed could still ask
//               LATER, which is what obliges a take to leave the slot alone.
// claimed       live handles that are inside a take right now.
// mover         the claimed asker reserved for the final move, once no
//               unclaimed handle is left; every other claimed asker clones.
// clone_readers askers duplicating out of the canonical slot right now, with
//               no lock held. The value may not move while one is reading.
// move_waiting  the mover arrived while readers were still out; it is parked
//               and the reader that retires last wakes it.
// moved         the canonical value left the slot through the final move.
// duplicate     how a second asker is served when the type's own descriptor
//               publishes no clone: the recipe the first clone installed, or
//               NULL when the type owns nothing (the bytes are the value) or
//               cannot be duplicated (a second asker is refused).
typedef struct rt_task_entitlements {
    uint32_t live;
    uint32_t claimed;
    const void* mover;
    // Written under the owner lock, and also READ without it by an external
    // awaiter deciding whether to wait on done_cv, so it is atomic.
    _Atomic uint32_t clone_readers;
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
    // Another handle can still ask: build this one an independent value.
    RT_TASK_TAKE_CLONE,
    // The reserved mover, with nobody reading: move the value out.
    RT_TASK_TAKE_MOVE,
    // The reserved mover, but a reader is still out: park and ask again.
    RT_TASK_TAKE_WAIT,
    // Another handle can still ask and the value cannot be duplicated.
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

// Begins serving one asker, identified by `asker` (the task doing the take,
// or a per-thread token for an external awaiter) so that a WAIT answered
// earlier is recognised when the same asker comes back. Decides the mode from
// the counts and the value's capabilities and records what the mode needs: a
// CLONE registers a reader, a reservation names the mover, a MOVE closes the
// cohort. `has_value` describes the slot and `operations` the value; the
// caller passes them because the slot belongs to the task, not to this
// record.
rt_task_take_mode rt_task_entitlement_begin_take(struct rt_executor* ex,
                                                 struct rt_task* task,
                                                 const void* asker,
                                                 int has_value,
                                                 const rt_value_ops* operations);

// The recipe a CLONE is served by: the descriptor's own clone_init when the
// type carries one (a user __clone the compiler monomorphized), otherwise the
// recipe the handle clone installed (the language's duplication of a type
// whose descriptor publishes none, such as a string).
rt_value_clone_init_fn rt_task_entitlement_duplicate(const struct rt_task* task,
                                                     const rt_value_ops* operations);

// Ends the take that `begin` started: retires this asker's entitlement, and
// for a CLONE retires its reader and wakes a mover that was waiting for it.
// Not called after a WAIT: that asker is still claimed and comes back.
void rt_task_entitlement_finish_take(struct rt_executor* ex,
                                     struct rt_task* task,
                                     rt_task_take_mode mode);

// A handle dropped without asking: its entitlement retires and that is all.
void rt_task_entitlement_drop(struct rt_executor* ex, struct rt_task* task);

// Whether a mover that was told to WAIT may try again: no reader is out. An
// external awaiter checks this under the control lock before it waits on
// done_cv, which is what makes the reader's broadcast unable to slip past it.
int rt_task_entitlement_move_ready(const struct rt_task* task);

#endif
