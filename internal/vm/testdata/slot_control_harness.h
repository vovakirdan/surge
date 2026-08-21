#ifndef SURGE_INTERNAL_VM_TESTDATA_SLOT_CONTROL_HARNESS_H
#define SURGE_INTERNAL_VM_TESTDATA_SLOT_CONTROL_HARNESS_H

#include "rt_slot_control.h"

#include <pthread.h>
#include <stdint.h>
#include <stdio.h>

typedef struct {
    int initialized;
    int payload;
} mock_value;

extern pthread_mutex_t harness_owner_lock;
extern const rt_value_ops harness_value_ops;
extern const rt_value_ops harness_other_ops;
extern int harness_callback_error;
extern int harness_callback_calls;
extern int harness_payload_accesses;

int harness_fail(const char* expression, const char* file, int line);
void harness_reset_callbacks(void);
void harness_noop_move(void* destination, void* source);
void harness_noop_copy(void* destination, const void* source);
void harness_noop_drop(void* value);
void harness_noop_trace(const void* value, const rt_trace_visitor* visitor);
rt_carrier_status harness_noop_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out);
rt_carrier_status harness_noop_cross_move(void* destination,
                                          void* source,
                                          const rt_cross_plan* plan,
                                          rt_cross_allocator* allocator);
rt_carrier_status harness_noop_cross_clone(void* destination,
                                           const void* source,
                                           const rt_cross_plan* plan,
                                           rt_cross_allocator* allocator);
rt_slot_control_status harness_init_slot(rt_slot_control* control,
                                         uint64_t identity,
                                         uint64_t generation,
                                         mock_value* storage);

int harness_case_read(void);
int harness_case_exclusive(void);
int harness_case_stale(void);
int harness_case_ordering(void);
int harness_case_storage(void);
int harness_case_zst(void);
int harness_case_descriptor(void);
int harness_case_identity(void);
int harness_case_copy(void);
int harness_case_copy_direct(void);
int harness_case_fifo(void);

#define REQUIRE(expression)                                                                        \
    do {                                                                                           \
        if (!(expression)) {                                                                       \
            return harness_fail(#expression, __FILE__, __LINE__);                                  \
        }                                                                                          \
    } while (0)

#endif
