// A stand for the scheduler trace's ownership protocol, with no executor and no
// tasks: the point is the cells, not the scheduler that fills them.
//
// Each thread takes one carrier's identity and records a known, exact number of
// pops into that carrier's own cell, while a reader thread reads every cell
// through the public dump. The final dump is then checked by the caller against
// numbers it can compute in advance, which is what makes this stand say
// something rather than merely run:
//
//   * plain shared words lose increments under this load, so the totals come
//     back SHORT and there is only one row to look at;
//   * one shared word made atomic loses nothing and still comes back as one row
//     belonging to nobody;
//   * a cell per owner comes back as one exact row per owner.
//
// Under -fsanitize=thread the same run answers the other half: whether the
// concurrent writes and the concurrent reads are ordered by anything at all.

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define _POSIX_C_SOURCE 200809L

#include "rt_async_internal.h"
#include "rt_sched_trace.h"

#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define SCHED_TRACE_STAND_CARRIERS 4u
// Divisible by three so every source bucket lands on an exact, precomputable
// count: a lost increment cannot hide inside a rounded expectation.
#define SCHED_TRACE_STAND_POPS 30000u
#define SCHED_TRACE_STAND_DUMPS 200u

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

// The emitter's dispatch spellings. No task is ever created here, so reaching
// any of them is a defect in the stand, not a result. Declared before defined
// because the stand is compiled with -Wmissing-prototypes, and exempted by name
// because the emitter owns the spelling.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    (void)id;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

static void* stand_carrier_main(void* arg) {
    rt_worker_ctx ctx;
    memset(&ctx, 0, sizeof(ctx));
    ctx.worker_index = (uint32_t)(uintptr_t)arg;
    ctx.worker_id = ctx.worker_index;
    ctx.shard_id = 0;
    tls_worker_ctx = &ctx;
    for (uint64_t i = 0; i < SCHED_TRACE_STAND_POPS; i++) {
        rt_trace_sched_record((rt_trace_sched_source)(i % 3u), i);
    }
    // The ctx is this frame's, so it must stop being reachable before the frame
    // goes away.
    tls_worker_ctx = NULL;
    return NULL;
}

static void* stand_reader_main(void* arg) {
    (void)arg;
    // Reads every owner's cell while every owner is still writing it. This is
    // the pairing that had no ordering at all before the cells got owners.
    for (unsigned i = 0; i < SCHED_TRACE_STAND_DUMPS; i++) {
        rt_sched_trace_dump();
    }
    return NULL;
}

int main(void) {
    if (setenv("SURGE_SCHED_TRACE", "1", 1) != 0) {
        fprintf(stderr, "STAND_FAIL setenv\n");
        return 1;
    }
    rt_sched_trace_init(SCHED_TRACE_STAND_CARRIERS);

    pthread_t carriers[SCHED_TRACE_STAND_CARRIERS];
    pthread_t reader;
    if (pthread_create(&reader, NULL, stand_reader_main, NULL) != 0) {
        fprintf(stderr, "STAND_FAIL reader\n");
        return 1;
    }
    for (uint32_t i = 0; i < SCHED_TRACE_STAND_CARRIERS; i++) {
        if (pthread_create(&carriers[i], NULL, stand_carrier_main, (void*)(uintptr_t)i) != 0) {
            fprintf(stderr, "STAND_FAIL carrier %u\n", i);
            return 1;
        }
    }
    for (uint32_t i = 0; i < SCHED_TRACE_STAND_CARRIERS; i++) {
        (void)pthread_join(carriers[i], NULL);
    }
    (void)pthread_join(reader, NULL);

    // The dump the caller checks: every owner has stopped writing, so this one
    // is a cut and not a sample.
    fprintf(stderr,
            "STAND_FINAL carriers=%u pops=%u\n",
            SCHED_TRACE_STAND_CARRIERS,
            SCHED_TRACE_STAND_POPS);
    rt_sched_trace_dump();
    return 0;
}
