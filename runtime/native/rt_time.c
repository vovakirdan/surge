#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <pthread.h>
#include <stdint.h>
#include <time.h>

static pthread_once_t monotonic_once = PTHREAD_ONCE_INIT;
static struct timespec monotonic_start;

static void monotonic_init(void) {
    if (clock_gettime(CLOCK_MONOTONIC, &monotonic_start) != 0) {
        monotonic_start.tv_sec = 0;
        monotonic_start.tv_nsec = 0;
    }
}

int64_t rt_monotonic_now(void) {
    struct timespec now = {0};
    if (pthread_once(&monotonic_once, monotonic_init) != 0) {
        return 0;
    }
    if (clock_gettime(CLOCK_MONOTONIC, &now) != 0) {
        return 0;
    }

    int64_t sec = (int64_t)now.tv_sec - (int64_t)monotonic_start.tv_sec;
    int64_t nsec = (int64_t)now.tv_nsec - (int64_t)monotonic_start.tv_nsec;
    if (nsec < 0) {
        sec -= 1;
        nsec += 1000000000LL;
    }
    return sec * 1000000000LL + nsec;
}
