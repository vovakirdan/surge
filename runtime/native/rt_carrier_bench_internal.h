#ifndef SURGE_RUNTIME_NATIVE_RT_CARRIER_BENCH_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_RT_CARRIER_BENCH_INTERNAL_H

#include <pthread.h>
#include <stdatomic.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

enum rt_carrier_bench_phase {
    RT_CARRIER_BENCH_DISABLED = 0,
    RT_CARRIER_BENCH_EXPECT_OPEN = 1,
    RT_CARRIER_BENCH_OPEN = 2,
    RT_CARRIER_BENCH_CLOSING = 3,
    RT_CARRIER_BENCH_CLOSED = 4,
    RT_CARRIER_BENCH_INVALID = 5,
};

enum rt_carrier_bench_error {
    RT_CARRIER_BENCH_ERROR_NONE = 0,
    RT_CARRIER_BENCH_ERROR_ENVIRONMENT,
    RT_CARRIER_BENCH_ERROR_MISSING_MARKER,
    RT_CARRIER_BENCH_ERROR_EXTRA_MARKER,
    RT_CARRIER_BENCH_ERROR_CONCURRENT_MARKER,
    RT_CARRIER_BENCH_ERROR_LATE_EVENT,
    RT_CARRIER_BENCH_ERROR_COUNTER_OVERFLOW,
    RT_CARRIER_BENCH_ERROR_TRANSPORT_UNDERFLOW,
    RT_CARRIER_BENCH_ERROR_TRANSPORT_BALANCE,
};

struct rt_carrier_bench_counters {
    uint64_t bytes_copied;
    uint64_t bytes_moved;
    uint64_t callback_count;
    uint64_t credit_stalls;
    uint64_t transport_bytes;
    uint64_t peak_transport_bytes;
};

struct rt_carrier_bench_state {
    pthread_mutex_t lock;
    pthread_cond_t drained;
    enum rt_carrier_bench_phase phase;
    enum rt_carrier_bench_error error;
    uint64_t active_hooks;
    struct rt_carrier_bench_counters counters;
    char probe[65];
    char nonce[33];
    char protocol_sha256[65];
    bool emitted;
};

extern struct rt_carrier_bench_state rt_carrier_bench_state;
extern _Atomic uint8_t rt_carrier_bench_fast_phase;

void rt_carrier_bench_fail_locked(enum rt_carrier_bench_error error);
const char* rt_carrier_bench_error_name(enum rt_carrier_bench_error error);

#endif
