#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <pthread.h>
#include <stdbool.h>
#include <stdint.h>
#include <time.h>

static pthread_once_t monotonic_once = PTHREAD_ONCE_INIT;
static struct timespec monotonic_baseline;
static bool monotonic_initialized_ok = false;

static void monotonic_init(void) {
    // The native runtime does not have a dedicated startup hook yet, so we
    // sample the baseline once on first use and fail closed if that sample is
    // unavailable. Later calls must not silently re-anchor against boot time.
    monotonic_initialized_ok = clock_gettime(CLOCK_MONOTONIC, &monotonic_baseline) == 0;
}

int64_t rt_monotonic_now(void) {
    struct timespec now = {0};
    if (pthread_once(&monotonic_once, monotonic_init) != 0 || !monotonic_initialized_ok) {
        return 0;
    }
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) {
        return 0;
    }

    int64_t sec = (int64_t)now.tv_sec - (int64_t)monotonic_baseline.tv_sec;
    int64_t nsec = (int64_t)now.tv_nsec - (int64_t)monotonic_baseline.tv_nsec;
    if (nsec < 0) {
        sec -= 1;
        nsec += 1000000000LL;
    }
    return sec * 1000000000LL + nsec;
}
