#include "rt_carrier_bench.h"
#include "rt_carrier_bench_internal.h"

#include <limits.h>
#include <string.h>

struct rt_carrier_bench_state rt_carrier_bench_state = {
    .lock = PTHREAD_MUTEX_INITIALIZER,
    .drained = PTHREAD_COND_INITIALIZER,
    .phase = RT_CARRIER_BENCH_DISABLED,
};

_Atomic uint8_t rt_carrier_bench_fast_phase = RT_CARRIER_BENCH_DISABLED;
static atomic_flag marker_active = ATOMIC_FLAG_INIT;

static bool checked_add(uint64_t* value, uint64_t delta) {
    if (UINT64_MAX - *value < delta) {
        return false;
    }
    *value += delta;
    return true;
}

void rt_carrier_bench_fail_locked(enum rt_carrier_bench_error error) {
    if (rt_carrier_bench_state.error == RT_CARRIER_BENCH_ERROR_NONE) {
        rt_carrier_bench_state.error = error;
    }
    rt_carrier_bench_state.phase = RT_CARRIER_BENCH_INVALID;
    atomic_store_explicit(
        &rt_carrier_bench_fast_phase, RT_CARRIER_BENCH_INVALID, memory_order_release);
    pthread_cond_broadcast(&rt_carrier_bench_state.drained);
}

static bool event_lock(void) {
    uint8_t phase = atomic_load_explicit(&rt_carrier_bench_fast_phase, memory_order_relaxed);
    if (phase == RT_CARRIER_BENCH_DISABLED || phase == RT_CARRIER_BENCH_EXPECT_OPEN) {
        return false;
    }
    pthread_mutex_lock(&rt_carrier_bench_state.lock);
    if (rt_carrier_bench_state.phase != RT_CARRIER_BENCH_OPEN) {
        if (rt_carrier_bench_state.phase != RT_CARRIER_BENCH_INVALID) {
            rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_LATE_EVENT);
        }
        pthread_mutex_unlock(&rt_carrier_bench_state.lock);
        return false;
    }
    return true;
}

static void record_add(uint64_t* value, uint64_t delta) {
    if (delta == 0 || !event_lock()) {
        return;
    }
    if (!checked_add(value, delta)) {
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_COUNTER_OVERFLOW);
    }
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
}

void rt_carrier_bench_marker(void) {
    uint8_t phase = atomic_load_explicit(&rt_carrier_bench_fast_phase, memory_order_acquire);
    if (phase == RT_CARRIER_BENCH_DISABLED) {
        return;
    }
    if (atomic_flag_test_and_set_explicit(&marker_active, memory_order_acquire)) {
        pthread_mutex_lock(&rt_carrier_bench_state.lock);
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_CONCURRENT_MARKER);
        pthread_mutex_unlock(&rt_carrier_bench_state.lock);
        return;
    }

    pthread_mutex_lock(&rt_carrier_bench_state.lock);
    if (rt_carrier_bench_state.phase == RT_CARRIER_BENCH_EXPECT_OPEN) {
        memset(&rt_carrier_bench_state.counters, 0, sizeof(rt_carrier_bench_state.counters));
        rt_carrier_bench_state.phase = RT_CARRIER_BENCH_OPEN;
        atomic_store_explicit(
            &rt_carrier_bench_fast_phase, RT_CARRIER_BENCH_OPEN, memory_order_release);
    } else if (rt_carrier_bench_state.phase == RT_CARRIER_BENCH_OPEN) {
        rt_carrier_bench_state.phase = RT_CARRIER_BENCH_CLOSING;
        atomic_store_explicit(
            &rt_carrier_bench_fast_phase, RT_CARRIER_BENCH_CLOSING, memory_order_release);
        while (rt_carrier_bench_state.active_hooks != 0 &&
               rt_carrier_bench_state.error == RT_CARRIER_BENCH_ERROR_NONE) {
            pthread_cond_wait(&rt_carrier_bench_state.drained, &rt_carrier_bench_state.lock);
        }
        if (rt_carrier_bench_state.error == RT_CARRIER_BENCH_ERROR_NONE) {
            rt_carrier_bench_state.phase = RT_CARRIER_BENCH_CLOSED;
            atomic_store_explicit(
                &rt_carrier_bench_fast_phase, RT_CARRIER_BENCH_CLOSED, memory_order_release);
        }
    } else if (rt_carrier_bench_state.phase != RT_CARRIER_BENCH_INVALID) {
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_EXTRA_MARKER);
    }
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
    atomic_flag_clear_explicit(&marker_active, memory_order_release);
}

void rt_carrier_bench_record_copy(uint64_t bytes) {
    record_add(&rt_carrier_bench_state.counters.bytes_copied, bytes);
}

void rt_carrier_bench_record_move(uint64_t bytes) {
    record_add(&rt_carrier_bench_state.counters.bytes_moved, bytes);
}

void rt_carrier_bench_record_callback(void) {
    record_add(&rt_carrier_bench_state.counters.callback_count, 1);
}

void rt_carrier_bench_record_credit_stall(void) {
    record_add(&rt_carrier_bench_state.counters.credit_stalls, 1);
}

void rt_carrier_bench_transport_acquire(uint64_t bytes) {
    if (bytes == 0 || !event_lock()) {
        return;
    }
    struct rt_carrier_bench_counters* counters = &rt_carrier_bench_state.counters;
    if (!checked_add(&counters->transport_bytes, bytes)) {
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_COUNTER_OVERFLOW);
    } else if (counters->transport_bytes > counters->peak_transport_bytes) {
        counters->peak_transport_bytes = counters->transport_bytes;
    }
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
}

void rt_carrier_bench_transport_release(uint64_t bytes) {
    if (bytes == 0 || !event_lock()) {
        return;
    }
    struct rt_carrier_bench_counters* counters = &rt_carrier_bench_state.counters;
    if (counters->transport_bytes < bytes) {
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_TRANSPORT_UNDERFLOW);
    } else {
        counters->transport_bytes -= bytes;
    }
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
}

#ifdef RT_CARRIER_BENCH_TESTING
int rt_carrier_bench_test_hook_enter(void) {
    if (!event_lock()) {
        return 0;
    }
    rt_carrier_bench_state.active_hooks++;
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
    return 1;
}

void rt_carrier_bench_test_hook_leave(void) {
    pthread_mutex_lock(&rt_carrier_bench_state.lock);
    if (rt_carrier_bench_state.active_hooks > 0) {
        rt_carrier_bench_state.active_hooks--;
    }
    pthread_cond_broadcast(&rt_carrier_bench_state.drained);
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
}

uint8_t rt_carrier_bench_test_state(void) {
    return atomic_load_explicit(&rt_carrier_bench_fast_phase, memory_order_acquire);
}
#endif
