#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#define RT_CARRIER_BENCH_IMPLEMENTATION
#include "rt_carrier_bench.h"
#include "rt_carrier_bench_internal.h"

#include <inttypes.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define RECORD_PREFIX "SURGE_CARRIER_COUNTERS "

static volatile sig_atomic_t signal_enabled = 0;
static volatile sig_atomic_t signal_emitted = 0;
static char signal_record[512];
static size_t signal_record_len = 0;

static bool is_token(const char* value, size_t max_length) {
    if (value == NULL || value[0] == '\0') {
        return false;
    }
    size_t length = 0;
    for (const char* cursor = value; *cursor != '\0'; cursor++) {
        char ch = *cursor;
        if (!(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') &&
            ch != '-' && ch != '_' && ch != '.') {
            return false;
        }
        length++;
        if (length > max_length) {
            return false;
        }
    }
    return true;
}

static bool is_lower_hex(const char* value, size_t expected_length) {
    if (value == NULL || strlen(value) != expected_length) {
        return false;
    }
    for (size_t i = 0; i < expected_length; i++) {
        if (!((value[i] >= '0' && value[i] <= '9') || (value[i] >= 'a' && value[i] <= 'f'))) {
            return false;
        }
    }
    return true;
}

const char* rt_carrier_bench_error_name(enum rt_carrier_bench_error error) {
    switch (error) {
        case RT_CARRIER_BENCH_ERROR_ENVIRONMENT:
            return "invalid_environment";
        case RT_CARRIER_BENCH_ERROR_MISSING_MARKER:
            return "missing_or_unclosed_marker";
        case RT_CARRIER_BENCH_ERROR_EXTRA_MARKER:
            return "extra_marker";
        case RT_CARRIER_BENCH_ERROR_CONCURRENT_MARKER:
            return "concurrent_marker";
        case RT_CARRIER_BENCH_ERROR_LATE_EVENT:
            return "late_carrier_event";
        case RT_CARRIER_BENCH_ERROR_COUNTER_OVERFLOW:
            return "counter_overflow";
        case RT_CARRIER_BENCH_ERROR_TRANSPORT_UNDERFLOW:
            return "transport_underflow";
        case RT_CARRIER_BENCH_ERROR_TRANSPORT_BALANCE:
            return "transport_balance_not_restored";
        case RT_CARRIER_BENCH_ERROR_NONE:
            return "none";
    }
    return "unknown";
}

static int emit_error(const char* error) {
    return fprintf(stderr,
                   RECORD_PREFIX "{\"schema_version\":1,\"status\":\"error\",\"probe\":\"%s\","
                                 "\"nonce\":\"%s\",\"protocol_sha256\":\"%s\","
                                 "\"metrics\":null,\"error\":\"%s\"}\n",
                   rt_carrier_bench_state.probe,
                   rt_carrier_bench_state.nonce,
                   rt_carrier_bench_state.protocol_sha256,
                   error) < 0
               ? 1
               : 0;
}

static void at_exit(void) {
    if (atomic_load_explicit(&rt_carrier_bench_fast_phase, memory_order_acquire) !=
            RT_CARRIER_BENCH_DISABLED &&
        !rt_carrier_bench_state.emitted) {
        (void)rt_carrier_bench_finish();
        _Exit(1);
    }
}

static void on_signal(int number) {
    if (signal_enabled && !signal_emitted) {
        signal_emitted = 1;
        (void)write(STDERR_FILENO, signal_record, signal_record_len);
    }
    _exit(128 + number);
}

static int install_exit_guards(void) {
    int length = snprintf(signal_record,
                          sizeof(signal_record),
                          RECORD_PREFIX "{\"schema_version\":1,\"status\":\"error\","
                                        "\"probe\":\"%s\",\"nonce\":\"%s\","
                                        "\"protocol_sha256\":\"%s\",\"metrics\":null,"
                                        "\"error\":\"abnormal_exit\"}\n",
                          rt_carrier_bench_state.probe,
                          rt_carrier_bench_state.nonce,
                          rt_carrier_bench_state.protocol_sha256);
    if (length <= 0 || (size_t)length >= sizeof(signal_record) || atexit(at_exit) != 0) {
        return 1;
    }
    signal_record_len = (size_t)length;
    struct sigaction action = {0};
    action.sa_handler = on_signal;
    sigfillset(&action.sa_mask);
    for (size_t i = 0; i < 6; i++) {
        const int signals[] = {SIGABRT, SIGBUS, SIGFPE, SIGILL, SIGSEGV, SIGTERM};
        if (sigaction(signals[i], &action, NULL) != 0) {
            return 1;
        }
    }
    signal_enabled = 1;
    return 0;
}

int rt_carrier_bench_init(void) {
    const char* enabled = getenv("SURGE_CARRIER_BENCH_COUNTERS");
    if (enabled == NULL || strcmp(enabled, "1") != 0) {
        return 0;
    }
    const char* probe = getenv("SURGE_CARRIER_BENCH_PROBE");
    const char* nonce = getenv("SURGE_CARRIER_BENCH_NONCE");
    const char* protocol = getenv("SURGE_CARRIER_BENCH_PROTOCOL_SHA256");
    if (!is_token(probe, 64) || !is_lower_hex(nonce, 32) || !is_lower_hex(protocol, 64)) {
        pthread_mutex_lock(&rt_carrier_bench_state.lock);
        rt_carrier_bench_state.phase = RT_CARRIER_BENCH_INVALID;
        rt_carrier_bench_state.error = RT_CARRIER_BENCH_ERROR_ENVIRONMENT;
        rt_carrier_bench_state.emitted = true;
        atomic_store_explicit(
            &rt_carrier_bench_fast_phase, RT_CARRIER_BENCH_INVALID, memory_order_release);
        pthread_mutex_unlock(&rt_carrier_bench_state.lock);
        (void)emit_error("invalid_environment");
        return 1;
    }
    (void)snprintf(rt_carrier_bench_state.probe, sizeof(rt_carrier_bench_state.probe), "%s", probe);
    (void)snprintf(rt_carrier_bench_state.nonce, sizeof(rt_carrier_bench_state.nonce), "%s", nonce);
    (void)snprintf(rt_carrier_bench_state.protocol_sha256,
                   sizeof(rt_carrier_bench_state.protocol_sha256),
                   "%s",
                   protocol);
    if (install_exit_guards() != 0) {
        rt_carrier_bench_state.emitted = true;
        (void)emit_error("exit_guard_install_failed");
        return 1;
    }
    rt_carrier_bench_state.phase = RT_CARRIER_BENCH_EXPECT_OPEN;
    atomic_store_explicit(
        &rt_carrier_bench_fast_phase, RT_CARRIER_BENCH_EXPECT_OPEN, memory_order_release);
    return 0;
}

int rt_carrier_bench_finish(void) {
    if (atomic_load_explicit(&rt_carrier_bench_fast_phase, memory_order_relaxed) ==
        RT_CARRIER_BENCH_DISABLED) {
        return 0;
    }
    pthread_mutex_lock(&rt_carrier_bench_state.lock);
    if (rt_carrier_bench_state.emitted) {
        pthread_mutex_unlock(&rt_carrier_bench_state.lock);
        return 1;
    }
    if (rt_carrier_bench_state.phase != RT_CARRIER_BENCH_CLOSED ||
        rt_carrier_bench_state.active_hooks != 0) {
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_MISSING_MARKER);
    }
    if (rt_carrier_bench_state.error == RT_CARRIER_BENCH_ERROR_NONE &&
        rt_carrier_bench_state.counters.transport_bytes != 0) {
        rt_carrier_bench_fail_locked(RT_CARRIER_BENCH_ERROR_TRANSPORT_BALANCE);
    }
    rt_carrier_bench_state.emitted = true;
    enum rt_carrier_bench_error error = rt_carrier_bench_state.error;
    struct rt_carrier_bench_counters counters = rt_carrier_bench_state.counters;
    pthread_mutex_unlock(&rt_carrier_bench_state.lock);
    if (error != RT_CARRIER_BENCH_ERROR_NONE) {
        (void)emit_error(rt_carrier_bench_error_name(error));
        return 1;
    }
    int result =
        fprintf(stderr,
                RECORD_PREFIX "{\"schema_version\":1,\"status\":\"ok\",\"probe\":\"%s\","
                              "\"nonce\":\"%s\",\"protocol_sha256\":\"%s\",\"metrics\":{"
                              "\"bytes_copied\":%" PRIu64 ",\"bytes_moved\":%" PRIu64
                              ",\"callback_count\":%" PRIu64 ",\"data_slot_stalls\":%" PRIu64
                              ",\"peak_transport_bytes\":%" PRIu64 "},\"error\":null}\n",
                rt_carrier_bench_state.probe,
                rt_carrier_bench_state.nonce,
                rt_carrier_bench_state.protocol_sha256,
                counters.bytes_copied,
                counters.bytes_moved,
                counters.callback_count,
                counters.data_slot_stalls,
                counters.peak_transport_bytes);
    return result < 0 ? 1 : 0;
}
