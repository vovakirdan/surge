#ifndef SURGE_RUNTIME_NATIVE_RT_CARRIER_BENCH_H
#define SURGE_RUNTIME_NATIVE_RT_CARRIER_BENCH_H

#include <stdint.h>

#if defined(RT_CARRIER_BENCH_ENABLED) || defined(RT_CARRIER_BENCH_TESTING) ||                      \
    defined(RT_CARRIER_BENCH_IMPLEMENTATION)
// Resource-capture builds define RT_CARRIER_BENCH_ENABLED. Timing builds use
// the no-op definitions below, and do not link the bridge implementation, so
// benchmark instrumentation cannot perturb their hot paths.
int rt_carrier_bench_init(void);
int rt_carrier_bench_finish(void);
void rt_carrier_bench_marker(void);

// Wave B+ typed carriers call these at the physical operation, not at source-
// language syntax. Zero-byte operations are intentionally not events.
void rt_carrier_bench_record_copy(uint64_t bytes);
void rt_carrier_bench_record_move(uint64_t bytes);
void rt_carrier_bench_record_callback(void);
void rt_carrier_bench_record_data_slot_stall(void);
void rt_carrier_bench_transport_acquire(uint64_t bytes);
void rt_carrier_bench_transport_release(uint64_t bytes);
#else
#define rt_carrier_bench_init() 0
#define rt_carrier_bench_finish() 0
#define rt_carrier_bench_marker() ((void)0)
#define rt_carrier_bench_record_copy(bytes) ((void)0)
#define rt_carrier_bench_record_move(bytes) ((void)0)
#define rt_carrier_bench_record_callback() ((void)0)
#define rt_carrier_bench_record_data_slot_stall() ((void)0)
#define rt_carrier_bench_transport_acquire(bytes) ((void)0)
#define rt_carrier_bench_transport_release(bytes) ((void)0)
#endif

#ifdef RT_CARRIER_BENCH_TESTING
int rt_carrier_bench_test_hook_enter(void);
void rt_carrier_bench_test_hook_leave(void);
uint8_t rt_carrier_bench_test_state(void);
#endif

#endif
