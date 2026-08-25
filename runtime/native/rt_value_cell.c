#include "rt_value_cell.h"

#include "rt.h"
#include "rt_value_ops.h"

#include <string.h>

// One typed value and its storage. See rt_value_cell.h for what a cell is and
// why a small value stays inline.

static int cell_fits_inline(const rt_value_ops* operations) {
    size_t align = operations->layout.align == 0 ? 1 : operations->layout.align;
    return operations->layout.size <= RT_VALUE_CELL_INLINE_BYTES &&
           align <= _Alignof(max_align_t) && RT_VALUE_CELL_INLINE_BYTES % align == 0;
}

rt_slot_control_status rt_value_cell_bind(rt_value_cell* cell, const rt_value_ops* operations) {
    if (cell == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    uint64_t generation = cell->generation;
    memset(cell, 0, sizeof(*cell));
    cell->state = RT_SLOT_EMPTY;
    cell->generation = generation + 1;
    if (operations == NULL) {
        // An owner that produces no value. Nothing to size, nothing to destroy,
        // and every operation below answers "there is nothing here".
        return RT_SLOT_CONTROL_OK;
    }
    cell->operations = operations;
    if (operations->layout.size == 0) {
        // A zero-sized value has no bytes, but it still has a lifecycle: the
        // inline run gives it a legal address to hand a move that writes none.
        cell->storage = cell->inline_storage;
        return RT_SLOT_CONTROL_OK;
    }
    if (cell_fits_inline(operations)) {
        cell->storage = cell->inline_storage;
        return RT_SLOT_CONTROL_OK;
    }
    size_t align = operations->layout.align == 0 ? 1 : operations->layout.align;
    cell->storage = rt_alloc(operations->layout.size, align);
    if (cell->storage == NULL) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    cell->owns_block = 1;
    return RT_SLOT_CONTROL_OK;
}

// cppcheck-suppress constParameterPointer
// The cell is not written HERE, but what this hands back is storage the caller
// writes a value into. A const parameter would say the opposite of what the
// return value is for.
void* rt_value_cell_publish_storage(rt_value_cell* cell) {
    if (cell == NULL || cell->operations == NULL || cell->state != RT_SLOT_EMPTY) {
        return NULL;
    }
    return cell->storage;
}

rt_slot_control_status rt_value_cell_commit(rt_value_cell* cell) {
    if (cell == NULL || cell->operations == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (cell->state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    cell->state = RT_SLOT_INITIALIZED;
    return RT_SLOT_CONTROL_OK;
}

int rt_value_cell_is_ready(const rt_value_cell* cell) {
    return cell != NULL && cell->operations != NULL && cell->state == RT_SLOT_INITIALIZED;
}

void* rt_value_cell_value(const rt_value_cell* cell) {
    return rt_value_cell_is_ready(cell) ? cell->storage : NULL;
}

rt_slot_control_status rt_value_cell_commit_move(rt_value_cell* cell) {
    if (!rt_value_cell_is_ready(cell)) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    cell->state = RT_SLOT_MOVED;
    return RT_SLOT_CONTROL_OK;
}

int rt_value_cell_copy_value(const rt_value_cell* cell, void* dst) {
    if (!rt_value_cell_is_ready(cell) || dst == NULL) {
        return 0;
    }
    if ((cell->operations->layout.flags & RT_VALUE_FLAG_DROPPABLE) != 0) {
        // An owning value has exactly one owner. Copying its bytes would make a
        // second one silently, which is the double-free this whole layer exists
        // to prevent, so the answer is "no" rather than a copy.
        return 0;
    }
    memcpy(dst, cell->storage, (size_t)cell->operations->layout.size);
    return 1;
}

int rt_value_cell_was_taken(const rt_value_cell* cell) {
    return cell != NULL && cell->operations != NULL &&
           (cell->state == RT_SLOT_MOVED || cell->state == RT_SLOT_DROPPED);
}

uint64_t rt_value_cell_generation(const rt_value_cell* cell) {
    return cell == NULL ? 0 : cell->generation;
}

void rt_value_cell_dispose(rt_value_cell* cell) {
    if (cell == NULL) {
        return;
    }
    if (cell->state == RT_SLOT_INITIALIZED && cell->operations != NULL) {
        // The header is cleared BEFORE the callback so a re-entrant caller
        // cannot find the same value to destroy a second time -- the rule every
        // typed owner in this runtime follows.
        cell->state = RT_SLOT_DROPPED;
        rt_value_drop_in_place_detached(cell->operations, cell->storage);
    }
    if (cell->owns_block && cell->storage != NULL && cell->operations != NULL) {
        size_t align = cell->operations->layout.align == 0 ? 1 : cell->operations->layout.align;
        rt_free((uint8_t*)cell->storage, cell->operations->layout.size, align);
    }
    cell->storage = NULL;
    cell->owns_block = 0;
    cell->operations = NULL;
    cell->state = RT_SLOT_EMPTY;
}

void rt_value_release_owned_block(const rt_value_ops* operations, void* storage) {
    if (operations == NULL || storage == NULL) {
        return;
    }
    rt_value_drop_in_place_detached(operations, storage);
    size_t align = operations->layout.align == 0 ? 1 : operations->layout.align;
    rt_free((uint8_t*)storage, operations->layout.size, align);
}
