#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_net_trace.h"

#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <unistd.h>

static _Atomic uint64_t net_poll_calls_total;
static _Atomic uint64_t net_poll_timeouts_total;
static _Atomic uint64_t net_poll_wake_fd_total;
static _Atomic uint64_t net_poll_ready_total;
static _Atomic uint64_t net_poll_errors_total;
static _Atomic uint64_t net_poll_timeout_last_ms;
static _Atomic uint64_t net_poll_timeout_max_ms;
static _Atomic uint64_t net_poll_waiters_last;
static _Atomic uint64_t net_poll_waiters_max;
static _Atomic uint64_t net_poll_waiters_total;
static _Atomic uint64_t net_direct_wait_total;
static _Atomic uint64_t net_waiter_scan_entries_total;
static _Atomic uint64_t net_waiter_net_entries_total;
static _Atomic uint64_t net_poll_rebuilds_total;
static _Atomic uint64_t net_poll_allocs_total;
static _Atomic uint64_t net_poll_dedup_checks_total;
static _Atomic uint64_t net_waiter_complete_calls_total;
static _Atomic uint64_t net_waiter_completed_total;

#define NET_TRACE_DUMP_FORMAT                                                                      \
    "TRACE_NET reason=%s io_poll_calls=%llu io_poll_timeouts=%llu "                                \
    "io_poll_wake_fd=%llu io_poll_net_ready=%llu io_poll_errors=%llu "                             \
    "io_poll_timeout_last_ms=%llu io_poll_timeout_max_ms=%llu "                                    \
    "io_poll_waiters_last=%llu io_poll_waiters_max=%llu "                                          \
    "io_poll_waiters_total=%llu io_direct_waits=%llu "                                             \
    "io_waiter_scan_entries=%llu io_waiter_net_entries=%llu "                                      \
    "io_poll_rebuilds=%llu io_poll_allocs=%llu io_poll_dedup_checks=%llu "                         \
    "io_waiter_complete_calls=%llu io_waiter_completed=%llu\n"
#define NET_TRACE_DUMP_ARGS(reason)                                                                \
    (reason), net_trace_load(&net_poll_calls_total), net_trace_load(&net_poll_timeouts_total),     \
        net_trace_load(&net_poll_wake_fd_total), net_trace_load(&net_poll_ready_total),            \
        net_trace_load(&net_poll_errors_total), net_trace_load(&net_poll_timeout_last_ms),         \
        net_trace_load(&net_poll_timeout_max_ms), net_trace_load(&net_poll_waiters_last),          \
        net_trace_load(&net_poll_waiters_max), net_trace_load(&net_poll_waiters_total),            \
        net_trace_load(&net_direct_wait_total), net_trace_load(&net_waiter_scan_entries_total),    \
        net_trace_load(&net_waiter_net_entries_total), net_trace_load(&net_poll_rebuilds_total),   \
        net_trace_load(&net_poll_allocs_total), net_trace_load(&net_poll_dedup_checks_total),      \
        net_trace_load(&net_waiter_complete_calls_total),                                          \
        net_trace_load(&net_waiter_completed_total)

static unsigned long long net_trace_load(const _Atomic uint64_t* counter) {
    return (unsigned long long)atomic_load_explicit(counter, memory_order_relaxed);
}

static void net_trace_inc(_Atomic uint64_t* counter) {
    (void)atomic_fetch_add_explicit(counter, 1, memory_order_relaxed);
}

static void net_trace_add(_Atomic uint64_t* counter, uint64_t value) {
    (void)atomic_fetch_add_explicit(counter, value, memory_order_relaxed);
}

static void net_trace_store(_Atomic uint64_t* counter, uint64_t value) {
    atomic_store_explicit(counter, value, memory_order_relaxed);
}

static uint64_t net_trace_timeout_ms(int timeout_ms) {
    if (timeout_ms <= 0) {
        return 0;
    }
    return (uint64_t)timeout_ms;
}

static void net_trace_max(_Atomic uint64_t* counter, uint64_t value) {
    uint64_t current = atomic_load_explicit(counter, memory_order_relaxed);
    while (current < value &&
           !atomic_compare_exchange_weak_explicit(
               counter, &current, value, memory_order_relaxed, memory_order_relaxed)) {
    }
}

void rt_net_trace_dump(const char* reason) {
    if (reason == NULL || reason[0] == '\0') {
        reason = "unknown";
    }
    (void)dprintf(STDERR_FILENO, NET_TRACE_DUMP_FORMAT, NET_TRACE_DUMP_ARGS(reason));
}

void rt_net_trace_direct_wait_enabled(void) {
    net_trace_inc(&net_direct_wait_total);
}

void rt_net_trace_poll_alloc_enabled(void) {
    net_trace_inc(&net_poll_allocs_total);
}

void rt_net_trace_poll_start_enabled(int timeout_ms, uint64_t waiter_count) {
    net_trace_inc(&net_poll_rebuilds_total);
    net_trace_inc(&net_poll_calls_total);
    uint64_t requested_timeout_ms = net_trace_timeout_ms(timeout_ms);
    net_trace_store(&net_poll_timeout_last_ms, requested_timeout_ms);
    net_trace_max(&net_poll_timeout_max_ms, requested_timeout_ms);
    net_trace_store(&net_poll_waiters_last, waiter_count);
    net_trace_max(&net_poll_waiters_max, waiter_count);
    net_trace_add(&net_poll_waiters_total, waiter_count);
}

void rt_net_trace_poll_error_enabled(void) {
    net_trace_inc(&net_poll_errors_total);
}

void rt_net_trace_poll_timeout_enabled(void) {
    net_trace_inc(&net_poll_timeouts_total);
}

void rt_net_trace_poll_wake_fd_enabled(void) {
    net_trace_inc(&net_poll_wake_fd_total);
}

void rt_net_trace_poll_ready_enabled(void) {
    net_trace_inc(&net_poll_ready_total);
}

void rt_net_trace_waiter_completion_enabled(uint64_t calls, uint64_t woken) {
    net_trace_add(&net_waiter_complete_calls_total, calls);
    net_trace_add(&net_waiter_completed_total, woken);
}
