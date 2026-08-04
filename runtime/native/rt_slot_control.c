#include "rt_slot_control.h"

#include <limits.h>

static int rt_slot_alignment_valid(size_t alignment) {
    return alignment != 0 && (alignment & (alignment - 1)) == 0 && alignment <= (size_t)UINTPTR_MAX;
}

static int
rt_slot_flag_matches_callback(rt_value_flags flags, rt_value_flags flag, int callback_present) {
    return ((flags & flag) != 0) == (callback_present != 0);
}

static rt_slot_control_status rt_slot_operations_preflight(const rt_value_ops* operations) {
    const rt_value_flags known_flags = RT_VALUE_FLAG_COPY | RT_VALUE_FLAG_CLONABLE |
                                       RT_VALUE_FLAG_DROPPABLE | RT_VALUE_FLAG_TRACEABLE |
                                       RT_VALUE_FLAG_SHARD_MOVABLE | RT_VALUE_FLAG_CROSS_CLONABLE;
    if ((operations->layout.flags & ~known_flags) != 0 ||
        !rt_slot_alignment_valid(operations->layout.align)) {
        return RT_SLOT_CONTROL_INVALID_OPERATIONS;
    }
    if (operations->layout.size == 0) {
        if (operations->layout.stride != 0) {
            return RT_SLOT_CONTROL_INVALID_OPERATIONS;
        }
    } else {
        size_t padding = operations->layout.align - 1;
        if (operations->layout.size > SIZE_MAX - padding) {
            return RT_SLOT_CONTROL_INVALID_OPERATIONS;
        }
        size_t stride = (operations->layout.size + padding) & ~padding;
        if (operations->layout.stride != stride) {
            return RT_SLOT_CONTROL_INVALID_OPERATIONS;
        }
    }
#if SIZE_MAX > UINTPTR_MAX
    if (operations->layout.size > (size_t)UINTPTR_MAX ||
        operations->layout.stride > (size_t)UINTPTR_MAX) {
        return RT_SLOT_CONTROL_INVALID_OPERATIONS;
    }
#endif
    if (operations->move_init == NULL || operations->plan_cross == NULL ||
        !rt_slot_flag_matches_callback(
            operations->layout.flags, RT_VALUE_FLAG_COPY, operations->copy_init != NULL) ||
        !rt_slot_flag_matches_callback(
            operations->layout.flags, RT_VALUE_FLAG_CLONABLE, operations->clone_init != NULL) ||
        !rt_slot_flag_matches_callback(
            operations->layout.flags, RT_VALUE_FLAG_DROPPABLE, operations->drop_in_place != NULL) ||
        !rt_slot_flag_matches_callback(
            operations->layout.flags, RT_VALUE_FLAG_TRACEABLE, operations->trace != NULL) ||
        !rt_slot_flag_matches_callback(operations->layout.flags,
                                       RT_VALUE_FLAG_SHARD_MOVABLE,
                                       operations->cross_move_init != NULL) ||
        !rt_slot_flag_matches_callback(operations->layout.flags,
                                       RT_VALUE_FLAG_CROSS_CLONABLE,
                                       operations->cross_clone_init != NULL)) {
        return RT_SLOT_CONTROL_INVALID_OPERATIONS;
    }
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_storage_preflight(const rt_value_ops* operations,
                                                 uintptr_t address,
                                                 size_t size,
                                                 size_t alignment,
                                                 uintptr_t* out_end) {
    if (operations == NULL || address == 0 || out_end == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    rt_slot_control_status operations_status = rt_slot_operations_preflight(operations);
    if (operations_status != RT_SLOT_CONTROL_OK) {
        return operations_status;
    }
    if (size != operations->layout.size || alignment < operations->layout.align) {
        return RT_SLOT_CONTROL_LAYOUT_MISMATCH;
    }
    if (!rt_slot_alignment_valid(alignment) || address % (uintptr_t)alignment != 0) {
        return RT_SLOT_CONTROL_STORAGE_MISALIGNED;
    }
#if SIZE_MAX > UINTPTR_MAX
    if (operations->layout.size > (size_t)UINTPTR_MAX) {
        return RT_SLOT_CONTROL_STORAGE_OVERFLOW;
    }
#endif
    uintptr_t canonical_size = (uintptr_t)operations->layout.size;
    if (canonical_size > UINTPTR_MAX - address) {
        return RT_SLOT_CONTROL_STORAGE_OVERFLOW;
    }
    *out_end = address + canonical_size;
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_pair_preflight(const rt_slot_control* source,
                                              const rt_slot_control* destination) {
    if (source == NULL || destination == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (source->logical_identity == destination->logical_identity) {
        return RT_SLOT_CONTROL_LOGICAL_SELF;
    }
    if (source->operations == NULL || source->operations != destination->operations) {
        return RT_SLOT_CONTROL_OPERATIONS_MISMATCH;
    }

    uintptr_t source_end = 0;
    uintptr_t destination_end = 0;
    rt_slot_control_status status = rt_slot_storage_preflight(source->operations,
                                                              source->storage_address,
                                                              source->storage_size,
                                                              source->storage_alignment,
                                                              &source_end);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    status = rt_slot_storage_preflight(destination->operations,
                                       destination->storage_address,
                                       destination->storage_size,
                                       destination->storage_alignment,
                                       &destination_end);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    if (source->operations->layout.size != 0 && destination->operations->layout.size != 0 &&
        source->storage_address < destination_end && destination->storage_address < source_end) {
        return RT_SLOT_CONTROL_STORAGE_OVERLAP;
    }
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_control_init(rt_slot_control* control,
                                            uint64_t logical_identity,
                                            const rt_value_ops* operations,
                                            uint64_t generation,
                                            uintptr_t storage_address,
                                            size_t storage_size,
                                            size_t storage_alignment) {
    if (control == NULL || logical_identity == 0 || operations == NULL || generation == 0) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    uintptr_t end = 0;
    rt_slot_control_status status = rt_slot_storage_preflight(
        operations, storage_address, storage_size, storage_alignment, &end);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    (void)end;

    *control = (rt_slot_control){0};
    control->slot.state = RT_SLOT_EMPTY;
    control->operations = operations;
    control->storage_address = storage_address;
    control->storage_size = storage_size;
    control->storage_alignment = storage_alignment;
    control->logical_identity = logical_identity;
    control->generation = generation;
    return RT_SLOT_CONTROL_OK;
}

void rt_slot_read_claim_init(rt_slot_read_claim* claim) {
    if (claim != NULL) {
        *claim = (rt_slot_read_claim){0};
    }
}

rt_slot_control_status rt_slot_publish_initial_locked(rt_slot_control* control,
                                                      uint64_t generation) {
    if (control == NULL) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (control->generation != generation) {
        return RT_SLOT_CONTROL_STALE;
    }
    if (control->slot.state != RT_SLOT_EMPTY ||
        control->reservation_phase != RT_SLOT_RESERVATION_NONE || control->reader_pins != 0 ||
        control->readers != NULL || control->exclusive_epoch != 0) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    control->slot.state = RT_SLOT_INITIALIZED;
    return RT_SLOT_CONTROL_OK;
}

rt_slot_control_status rt_slot_begin_generation_locked(rt_slot_control* control,
                                                       uint64_t generation,
                                                       uintptr_t storage_address,
                                                       size_t storage_size,
                                                       size_t storage_alignment) {
    if (control == NULL || generation == 0) {
        return RT_SLOT_CONTROL_INVALID_ARGUMENT;
    }
    if (control->reader_pins != 0 || control->readers != NULL || control->exclusive_epoch != 0 ||
        control->reservation_phase != RT_SLOT_RESERVATION_NONE) {
        return RT_SLOT_CONTROL_BUSY;
    }
    if (control->slot.state != RT_SLOT_MOVED && control->slot.state != RT_SLOT_DROPPED) {
        return RT_SLOT_CONTROL_INVALID_STATE;
    }
    if (generation <= control->generation) {
        return RT_SLOT_CONTROL_STALE;
    }

    uintptr_t end = 0;
    rt_slot_control_status status = rt_slot_storage_preflight(
        control->operations, storage_address, storage_size, storage_alignment, &end);
    if (status != RT_SLOT_CONTROL_OK) {
        return status;
    }
    (void)end;
    control->slot.state = RT_SLOT_EMPTY;
    control->storage_address = storage_address;
    control->storage_size = storage_size;
    control->storage_alignment = storage_alignment;
    control->generation = generation;
    control->reservation_source_control = NULL;
    control->reservation_destination_control = NULL;
    return RT_SLOT_CONTROL_OK;
}
