#include "rt_task_result.h"

#include "rt.h"
#include "rt_value_ops.h"

#include <string.h>

// One canonical result slot per task. See rt_task_result.h for why the task
// owns it and why small results stay inline.

static int result_fits_inline(const rt_value_ops* operations) {
    size_t align = operations->layout.align == 0 ? 1 : operations->layout.align;
    return operations->layout.size <= RT_TASK_RESULT_INLINE_BYTES &&
           align <= _Alignof(max_align_t) && RT_TASK_RESULT_INLINE_BYTES % align == 0;
}

rt_slot_control_status rt_task_result_bind(rt_task_result* slot, const rt_value_ops* operations) {
    if (slot == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    memset(slot, 0, sizeof(*slot));
    slot->state = RT_SLOT_EMPTY;
    if (operations == NULL) {
        // A task that produces no value. Nothing to size, nothing to destroy,
        // and every operation below answers "there is no result".
        return RT_SLOT_CONTROL_OK;
    }
    slot->operations = operations;
    if (operations->layout.size == 0) {
        // A zero-sized result has no bytes, but it still has a lifecycle: the
        // inline run gives it a legal address to hand a move that writes none.
        slot->storage = slot->inline_storage;
        return RT_SLOT_CONTROL_OK;
    }
    if (result_fits_inline(operations)) {
        slot->storage = slot->inline_storage;
        return RT_SLOT_CONTROL_OK;
    }
    size_t align = operations->layout.align == 0 ? 1 : operations->layout.align;
    slot->storage = rt_alloc(operations->layout.size, align);
    if (slot->storage == NULL) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    slot->owns_block = 1;
    return RT_SLOT_CONTROL_OK;
}

// cppcheck-suppress constParameterPointer
// The slot is not written HERE, but what this hands back is storage the caller
// writes a value into. A const parameter would say the opposite of what the
// return value is for.
void* rt_task_result_publish_storage(rt_task_result* slot) {
    if (slot == NULL || slot->operations == NULL || slot->state != RT_SLOT_EMPTY) {
        return NULL;
    }
    return slot->storage;
}

rt_slot_control_status rt_task_result_commit(rt_task_result* slot) {
    if (slot == NULL || slot->operations == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (slot->state != RT_SLOT_EMPTY) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    slot->state = RT_SLOT_INITIALIZED;
    return RT_SLOT_CONTROL_OK;
}

int rt_task_result_is_ready(const rt_task_result* slot) {
    return slot != NULL && slot->operations != NULL && slot->state == RT_SLOT_INITIALIZED;
}

void* rt_task_result_value(const rt_task_result* slot) {
    return rt_task_result_is_ready(slot) ? slot->storage : NULL;
}

rt_slot_control_status rt_task_result_commit_move(rt_task_result* slot) {
    if (!rt_task_result_is_ready(slot)) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    slot->state = RT_SLOT_MOVED;
    return RT_SLOT_CONTROL_OK;
}

void rt_task_result_dispose(rt_task_result* slot) {
    if (slot == NULL) {
        return;
    }
    if (slot->state == RT_SLOT_INITIALIZED && slot->operations != NULL) {
        // The header is cleared BEFORE the callback so a re-entrant caller
        // cannot find the same value to destroy a second time -- the rule every
        // typed owner in this runtime follows.
        slot->state = RT_SLOT_DROPPED;
        rt_value_drop_in_place_detached(slot->operations, slot->storage);
    }
    if (slot->owns_block && slot->storage != NULL && slot->operations != NULL) {
        size_t align = slot->operations->layout.align == 0 ? 1 : slot->operations->layout.align;
        rt_free((uint8_t*)slot->storage, slot->operations->layout.size, align);
    }
    slot->storage = NULL;
    slot->owns_block = 0;
    slot->operations = NULL;
    slot->state = RT_SLOT_EMPTY;
}
