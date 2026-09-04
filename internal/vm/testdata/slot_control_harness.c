#include "slot_control_harness.h"

#include <errno.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>

_Static_assert(offsetof(rt_slot_control, slot) == 0,
               "the owner control must embed its sole slot header directly");

pthread_mutex_t harness_owner_lock = PTHREAD_MUTEX_INITIALIZER;
int harness_callback_error = 0;
int harness_callback_calls = 0;
int harness_payload_accesses = 0;

static void harness_observe_callback(void) {
    int status = pthread_mutex_trylock(&harness_owner_lock);
    if (status != 0) {
        harness_callback_error = status == EBUSY ? 1 : status;
        return;
    }
    if (pthread_mutex_unlock(&harness_owner_lock) != 0) {
        harness_callback_error = 2;
        return;
    }
    harness_callback_calls++;
}

static void harness_move_init(void* destination, void* source) {
    harness_observe_callback();
    mock_value* dst = destination;
    mock_value* src = source;
    harness_payload_accesses += 2;
    if (dst->initialized != 0 || src->initialized == 0) {
        harness_callback_error = 3;
        return;
    }
    *dst = *src;
    *src = (mock_value){0};
}

static void harness_copy_init(void* destination, const void* source) {
    harness_observe_callback();
    mock_value* dst = destination;
    const mock_value* src = source;
    harness_payload_accesses += 2;
    if (dst->initialized != 0 || src->initialized == 0) {
        harness_callback_error = 4;
        return;
    }
    *dst = *src;
}

static void harness_drop_in_place(void* value) {
    harness_observe_callback();
    mock_value* typed = value;
    harness_payload_accesses++;
    if (typed->initialized == 0) {
        harness_callback_error = 5;
        return;
    }
    *typed = (mock_value){0};
}

static void harness_trace(const void* value, const rt_trace_visitor* visitor) {
    (void)visitor;
    harness_observe_callback();
    const mock_value* typed = value;
    harness_payload_accesses++;
    if (typed->initialized == 0) {
        harness_callback_error = 6;
    }
}

static rt_carrier_status harness_cross_move(void* destination,
                                            void* source,
                                            const rt_cross_plan* plan,
                                            rt_cross_allocator* allocator) {
    (void)destination;
    (void)source;
    (void)plan;
    (void)allocator;
    harness_observe_callback();
    return RT_CARRIER_STATUS_CAPACITY;
}

void harness_noop_move(void* destination, void* source) {
    (void)destination;
    (void)source;
}

void harness_noop_copy(void* destination, const void* source) {
    (void)destination;
    (void)source;
}

void harness_noop_drop(void* value) {
    (void)value;
}

void harness_noop_trace(const void* value, const rt_trace_visitor* visitor) {
    (void)value;
    (void)visitor;
}

rt_carrier_status harness_noop_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

rt_carrier_status harness_noop_cross_move(void* destination,
                                          void* source,
                                          const rt_cross_plan* plan,
                                          rt_cross_allocator* allocator) {
    (void)destination;
    (void)source;
    (void)plan;
    (void)allocator;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

rt_carrier_status harness_noop_cross_clone(void* destination,
                                           const void* source,
                                           const rt_cross_plan* plan,
                                           rt_cross_allocator* allocator) {
    (void)destination;
    (void)source;
    (void)plan;
    (void)allocator;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

const rt_value_ops harness_value_ops = {
    .layout =
        {
            .size = sizeof(mock_value),
            .align = _Alignof(mock_value),
            .stride = sizeof(mock_value),
            .flags = RT_VALUE_FLAG_COPY | RT_VALUE_FLAG_CLONABLE | RT_VALUE_FLAG_DROPPABLE |
                     RT_VALUE_FLAG_TRACEABLE | RT_VALUE_FLAG_SHARD_MOVABLE,
        },
    .move_init = harness_move_init,
    .copy_init = harness_copy_init,
    .clone_init = harness_copy_init,
    .drop_in_place = harness_drop_in_place,
    .trace = harness_trace,
    .plan_cross = harness_noop_plan,
    .cross_move_init = harness_cross_move,
};

const rt_value_ops harness_other_ops = {
    .layout =
        {
            .size = sizeof(mock_value),
            .align = _Alignof(mock_value),
            .stride = sizeof(mock_value),
        },
    .move_init = harness_noop_move,
    .plan_cross = harness_noop_plan,
};

int harness_fail(const char* expression, const char* file, int line) {
    fprintf(stderr, "slot-control-check: %s:%d: %s\n", file, line, expression);
    return 1;
}

void harness_reset_callbacks(void) {
    harness_callback_error = 0;
    harness_callback_calls = 0;
    harness_payload_accesses = 0;
}

rt_slot_control_status harness_init_slot(rt_slot_control* control,
                                         uint64_t identity,
                                         uint64_t generation,
                                         mock_value* storage) {
    return rt_slot_control_init(control,
                                identity,
                                &harness_value_ops,
                                generation,
                                (uintptr_t)storage,
                                sizeof(*storage),
                                _Alignof(mock_value));
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return harness_fail("one mode argument required", __FILE__, __LINE__);
    }
    if (strcmp(argv[1], "read") == 0) {
        return harness_case_read();
    }
    if (strcmp(argv[1], "exclusive") == 0) {
        return harness_case_exclusive();
    }
    if (strcmp(argv[1], "stale") == 0) {
        return harness_case_stale();
    }
    if (strcmp(argv[1], "ordering") == 0) {
        return harness_case_ordering();
    }
    if (strcmp(argv[1], "storage") == 0) {
        return harness_case_storage();
    }
    if (strcmp(argv[1], "zst") == 0) {
        return harness_case_zst();
    }
    if (strcmp(argv[1], "descriptor") == 0) {
        return harness_case_descriptor();
    }
    if (strcmp(argv[1], "identity") == 0) {
        return harness_case_identity();
    }
    if (strcmp(argv[1], "copy") == 0) {
        return harness_case_copy();
    }
    if (strcmp(argv[1], "fifo") == 0) {
        return harness_case_fifo();
    }
    if (strcmp(argv[1], "park") == 0) {
        return harness_case_park();
    }
    if (strcmp(argv[1], "cross-clone") == 0) {
        return harness_case_cross_clone();
    }
    // Terminates the process on purpose; it is never in the passing mode list.
    if (strcmp(argv[1], "copy-direct") == 0) {
        return harness_case_copy_direct();
    }
    return harness_fail("unknown mode", __FILE__, __LINE__);
}
