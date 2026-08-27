#ifndef SURGE_RUNTIME_NATIVE_RT_VALUE_CELL_H
#define SURGE_RUNTIME_NATIVE_RT_VALUE_CELL_H

#include <stddef.h>
#include <stdint.h>

#include "rt_slot_control.h"
#include "rt_typed_carrier_abi.generated.h"

// One typed value, owned by whatever declares the cell.
//
// It is the smallest complete owner in this runtime: a descriptor, storage
// sized from it, and a lifecycle of EMPTY -> INITIALIZED -> MOVED or DROPPED.
// It is not a queue, it holds no lock, and it decides nothing about WHO may
// read the value -- that belongs to the owner around it, which is why a task
// result and a remote-select arm can both be one of these without either of
// them meaning what the other means.
//
// WHY SMALL VALUES LIVE INLINE. The representation these cells replace is a
// machine word, and a word costs an `int` nothing: no allocation, no free. A
// cell that always allocated would make the cheap case worse than the thing it
// replaces, which is not a migration anybody should accept. A value that fits
// the inline run stays in the owner's own bytes; a wider one takes a single
// block, which the box it replaces already cost.
typedef struct rt_value_cell rt_value_cell;

// Enough bytes for every handle-shaped and scalar value, which is what the word
// this replaces could carry, plus a second word so a two-field composite does
// not fall off the cheap path. Wider than that is an allocation.
#define RT_VALUE_CELL_INLINE_BYTES 16

struct rt_value_cell {
    // The value's descriptor, or NULL when this cell holds nothing at all.
    const rt_value_ops* operations;
    // Bumped every time the cell is bound, so a capability minted for one
    // occupant cannot be spent on the next one in the same storage.
    uint64_t generation;
    // Where the value is: the inline run below, or one block this cell owns.
    void* storage;
    // Lifecycle, using the same states as every other typed slot.
    uint8_t state;
    // Whether `storage` names a block this cell allocated and must free.
    uint8_t owns_block;
    _Alignas(16) uint8_t inline_storage[RT_VALUE_CELL_INLINE_BYTES];
};

// Binds the descriptor and reserves storage for one value of that type.
//
// Called before a value can be published. A NULL descriptor is the no-value
// case and is not an error: the cell stays empty forever and every operation on
// it is a no-op.
rt_slot_control_status rt_value_cell_bind(rt_value_cell* cell, const rt_value_ops* operations);

// Adopts a block the caller already built a value in, as this cell's one value.
//
// The value is INITIALIZED on arrival -- there is nothing to publish, because
// the caller published it before the runtime ever saw the block -- and the cell
// owns the storage from here: disposing walks the members through the
// descriptor and frees the block at the width the descriptor states, exactly as
// a cell that reserved its own storage does.
//
// It exists because one kind of value is built where the runtime cannot reach:
// compiled code packs a shipped state into storage it reserves at the
// submission site, so `bind` -- which reserves -- would allocate a SECOND block
// and leave the first with no owner. A NULL block is the no-value case. A block
// with no descriptor is refused rather than adopted: a cell that cannot say how
// wide the value is cannot free it either.
rt_slot_control_status
rt_value_cell_adopt(rt_value_cell* cell, const rt_value_ops* operations, void* storage);

// Where to move a value INTO. NULL when the cell has no descriptor, or when it
// already holds a value -- a second publication is a defect in the caller
// rather than an overwrite this cell performs.
void* rt_value_cell_publish_storage(rt_value_cell* cell);

// Marks the value the caller move-initialized into that storage as present.
rt_slot_control_status rt_value_cell_commit(rt_value_cell* cell);

// Whether a value is there to be read.
int rt_value_cell_is_ready(const rt_value_cell* cell);

// Where the value IS, for a caller about to move or duplicate it out. NULL when
// nothing is there.
void* rt_value_cell_value(const rt_value_cell* cell);

// Marks the value as moved out. The storage stays; the obligation does not.
rt_slot_control_status rt_value_cell_commit_move(rt_value_cell* cell);

// Copies a value that owns NOTHING out to `dst`, leaving the cell holding it.
//
// This is the serving path for a value whose descriptor claims no drop: there
// is no obligation to transfer, so the bytes are the whole value and handing
// them to a second reader hands out nothing that was already given away. It
// refuses a droppable type outright rather than duplicating an obligation.
int rt_value_cell_copy_value(const rt_value_cell* cell, void* dst);

// Whether a value was published into this cell and then taken out of it.
//
// It is the difference between "there was never anything here" and "the one
// value this cell had is already somebody else's", which no later reader may be
// told is a value it can have.
int rt_value_cell_was_taken(const rt_value_cell* cell);

// This cell's generation, for a capability about to be minted from it.
uint64_t rt_value_cell_generation(const rt_value_cell* cell);

// Destroys whatever the cell still holds and releases any block it owns. Runs
// the element's drop, so no runtime lock may be held.
void rt_value_cell_dispose(rt_value_cell* cell);

// Hands the value to a caller that will destroy it LATER, and leaves the cell
// reading "already taken" straight away.
//
// It exists for the one caller that must separate those two moments: a
// completion that refuses the value its body produced (RV2-DEBT-263) may not
// run the drop where it makes that decision, because a scheduler lock may be
// held there (rule 8 P2) -- but it must empty the slot before it publishes
// TASK_DONE, or a reader downstream is offered a value by a task that answers
// Cancelled. `out_owns_block` says which kind of storage came back: a block the
// cell allocated, which the caller now owns whole and must drop AND free, or
// bytes inside the cell itself, which the caller drops in place and must never
// free, and which stay valid only as long as the cell's owner does. Returns
// NULL when there was no value, leaving the cell untouched.
void* rt_value_cell_hand_off(rt_value_cell* cell,
                             const rt_value_ops** out_operations,
                             int* out_owns_block);

// Destroys a heap allocation the runtime holds ONE of, and frees it: the
// members through the type's own drop, then the storage at the width and
// alignment that type asks for.
//
// This is what a numeric drop-fn id used to buy. The id named a generated
// function that did both halves; the descriptor names the same drop and states
// the layout, so the runtime performs the free itself and no dispatch table
// stands between a value and its own destructor. A NULL descriptor or pointer
// is a no-op -- an obligation that was never taken on.
void rt_value_release_owned_block(const rt_value_ops* operations, void* storage);

// The same release, deferred until this lane holds no scheduler lock.
//
// It is what a caller uses when it is holding one and cannot let go -- the
// completion path that clears a task's abandoned state runs under control, and
// the drop it owes runs generated code. The work is handed to the lane, which
// performs it at the moment it becomes free.
void rt_release_owned_block_when_unlocked(const rt_value_ops* operations, void* storage);

#endif
