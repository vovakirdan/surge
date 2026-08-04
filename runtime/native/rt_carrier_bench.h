#ifndef SURGE_RUNTIME_NATIVE_RT_CARRIER_BENCH_H
#define SURGE_RUNTIME_NATIVE_RT_CARRIER_BENCH_H

#include <stdint.h>

// The benchmark bridge is inert unless SURGE_CARRIER_BENCH_COUNTERS is exactly
// "1". Initialization happens once at process entry; carrier hot paths never
// inspect the environment.
int rt_carrier_bench_init(void);
int rt_carrier_bench_finish(void);
void rt_carrier_bench_marker(void);

// Wave B+ typed carriers call these at the physical operation, not at source-
// language syntax. Zero-byte operations are intentionally not events.
void rt_carrier_bench_record_copy(uint64_t bytes);
void rt_carrier_bench_record_move(uint64_t bytes);
void rt_carrier_bench_record_callback(void);
void rt_carrier_bench_record_credit_stall(void);
void rt_carrier_bench_transport_acquire(uint64_t bytes);
void rt_carrier_bench_transport_release(uint64_t bytes);

#ifdef RT_CARRIER_BENCH_TESTING
int rt_carrier_bench_test_hook_enter(void);
void rt_carrier_bench_test_hook_leave(void);
uint8_t rt_carrier_bench_test_state(void);
#endif

#endif
