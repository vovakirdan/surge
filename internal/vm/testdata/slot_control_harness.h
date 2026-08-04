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

#define REQUIRE(expression)                                                                        \
    do {                                                                                           \
        if (!(expression)) {                                                                       \
            return harness_fail(#expression, __FILE__, __LINE__);                                  \
        }                                                                                          \
    } while (0)

#endif
