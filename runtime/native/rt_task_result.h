#ifndef SURGE_RUNTIME_NATIVE_RT_TASK_RESULT_H
#define SURGE_RUNTIME_NATIVE_RT_TASK_RESULT_H

#include <stddef.h>
#include <stdint.h>

#include "rt_slot_control.h"
#include "rt_typed_carrier_abi.generated.h"

// The one canonical result a task owns, at the width its type asks for.
//
// WHY THE TASK OWNS IT. A handle can ask for the result long after the poll
// frame that produced it is gone, so the value cannot live in the frame and it
// cannot live in the asker: there may be several askers and none of them exists
// yet when the task completes. The task outlives every handle on it, the same
// way a channel outlives every park on it.
//
// WHY SMALL RESULTS LIVE INLINE. The representation this replaces is a machine
// word, and a word costs a `Task<int>` nothing: no allocation, no free. An
// exact-sized slot that always allocated would make the cheap case worse than
// the thing it replaces, which is not a migration anybody should accept. A
// result that fits the inline run stays in the task's own bytes; a wider one
// takes a single block, which the boxed composite it replaces already paid for.
//
// WHAT IT IS NOT. It is not a general-purpose slot: there is exactly one per
// task, its lifecycle is EMPTY -> INITIALIZED -> MOVED or DROPPED, and it has
// no generation of its own because the task's own identity already answers
// "which result is this" -- a task completes once.
typedef struct rt_task_result rt_task_result;

// Enough bytes for every handle-shaped and scalar result, which is what the
// word this replaces could carry, plus a second word so a two-field composite
// does not fall off the cheap path. Wider than that is an allocation.
#define RT_TASK_RESULT_INLINE_BYTES 16

// Names one task's result slot from outside the task, without naming the task's
// memory. A reply that travelled between shards must not carry an rt_task*: the
// task it points at can complete, be freed and have its id reused before the
// reply is read, and a pointer cannot say that happened. Four integers can --
// the id, the task's generation, the slot's own generation, and the shard that
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

struct rt_task_result {
    // The result's descriptor, or NULL when the task produces no value.
    const rt_value_ops* operations;
    // Bumped every time this slot is bound, so a capability minted for one
    // result cannot be spent on the next occupant of the same task id.
    uint64_t generation;
    // Where the value is: the inline run below, or one block this slot owns.
    void* storage;
    // Lifecycle, using the same states as every other typed slot.
    uint8_t state;
    // Whether `storage` names a block this slot allocated and must free.
    uint8_t owns_block;
    _Alignas(16) uint8_t inline_storage[RT_TASK_RESULT_INLINE_BYTES];
};

// Binds the descriptor and reserves storage for one value of that type.
//
// Called once, before a result can be published. A NULL descriptor is the
// no-value case and is not an error: the slot stays empty forever and every
// operation on it is a no-op.
rt_slot_control_status rt_task_result_bind(rt_task_result* slot, const rt_value_ops* operations);

// Where to move a result INTO. NULL when the slot has no descriptor, or when it
// already holds a value -- a task completes once, and a second publication is a
// defect in the caller rather than an overwrite this slot performs.
void* rt_task_result_publish_storage(rt_task_result* slot);

// Marks the value the caller move-initialized into that storage as present.
rt_slot_control_status rt_task_result_commit(rt_task_result* slot);

// Whether a value is there to be read.
int rt_task_result_is_ready(const rt_task_result* slot);

// Where the value IS, for a caller about to move or clone it out. NULL when
// nothing is there.
void* rt_task_result_value(const rt_task_result* slot);

// Marks the value as moved out. The storage stays; the obligation does not.
rt_slot_control_status rt_task_result_commit_move(rt_task_result* slot);

// Copies a result that owns NOTHING out to `dst`, leaving the slot holding it.
//
// This is the serving path for a value whose descriptor claims no drop: there
// is no obligation to transfer, so the bytes are the whole value and handing
// them to a second asker hands out nothing that was already given away. It
// refuses a droppable type outright rather than duplicating an obligation.
int rt_task_result_copy_value(const rt_task_result* slot, void* dst);

// Whether a value was published into this slot and then served out of it.
//
// It is the difference between "this task answers with nothing" -- a valueless
// or cancelled outcome, which is a Success with no payload -- and "the one
// value this task had is already somebody else's", which no later asker may be
// told is a Success.
int rt_task_result_was_taken(const rt_task_result* slot);

// Destroys whatever the slot still holds and releases any block it owns. Runs
// the element's drop, so no runtime lock may be held.
void rt_task_result_dispose(rt_task_result* slot);

// Takes a WORD-shaped result out, for the one reply path that still carries a
// machine word: remote select, which D5 retypes. Answers 0 when the slot holds
// nothing, and refuses to truncate anything wider than a word.
uint64_t rt_task_result_take_word(rt_task_result* slot);

// This slot's generation, for a capability about to be minted from it.
uint64_t rt_task_result_generation(const rt_task_result* slot);

// Whether a capability still names THIS result: same slot generation, and a
// value still in it. A capability that fails this names a result that has been
// taken, replaced, or never existed, and the caller must treat it as absent
// rather than as an error to retry.
int rt_task_result_matches(const rt_task_result* slot, const rt_result_source* source);

#endif
