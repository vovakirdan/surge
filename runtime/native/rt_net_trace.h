#ifndef SURGE_RUNTIME_NATIVE_RT_NET_TRACE_H
#define SURGE_RUNTIME_NATIVE_RT_NET_TRACE_H

#include <stdint.h>

int rt_exec_trace_enabled(void);
void rt_net_trace_dump(const char* reason);
void rt_net_trace_direct_wait_enabled(void);
void rt_net_trace_poll_alloc_enabled(void);
void rt_net_trace_poll_start_enabled(int timeout_ms, uint64_t waiter_count);
void rt_net_trace_poll_error_enabled(void);
void rt_net_trace_poll_timeout_enabled(void);
void rt_net_trace_poll_wake_fd_enabled(void);
void rt_net_trace_poll_ready_enabled(void);
void rt_net_trace_waiter_completion_enabled(uint64_t calls, uint64_t woken);

static inline void rt_net_trace_direct_wait(void) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_direct_wait_enabled();
}

static inline void rt_net_trace_poll_alloc(void) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_poll_alloc_enabled();
}

static inline void rt_net_trace_poll_start(int timeout_ms, uint64_t waiter_count) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_poll_start_enabled(timeout_ms, waiter_count);
}

static inline void rt_net_trace_poll_error(void) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_poll_error_enabled();
}

static inline void rt_net_trace_poll_timeout(void) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_poll_timeout_enabled();
}

static inline void rt_net_trace_poll_wake_fd(void) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_poll_wake_fd_enabled();
}

static inline void rt_net_trace_poll_ready(void) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_poll_ready_enabled();
}

static inline void rt_net_trace_waiter_completion(uint64_t calls, uint64_t woken) {
    if (!rt_exec_trace_enabled()) {
        return;
    }
    rt_net_trace_waiter_completion_enabled(calls, woken);
}

#endif
