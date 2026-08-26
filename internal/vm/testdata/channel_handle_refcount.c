// nanosleep, which this stand uses to let contending threads interleave.
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#define _POSIX_C_SOURCE 199309L

#include "rt_async_internal.h"

#include "rt_channel_lane.h"

#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

// What a channel's handle count buys, asked of a channel that OWNS what it
// holds (RV2-DEBT-155).
//
// The object behind a Channel<T> handle is shared and deterministically
// reference counted: copying a handle retains, dropping a copy releases, and
// the last release destroys the object -- which drops every payload the
// channel still owns. Three questions follow, and each is a row here:
//
//   - ONE handle. The single release is the reclaim, and the values still in
//     the ring are destroyed there. Nothing else destroys them, so the count
//     of drops is the count of values that never reached a receiver.
//   - TWO handles. The FIRST release must destroy nothing: a second copy can
//     still send and receive, and a channel torn down under it would either
//     lose its buffered values or hand back freed storage. This is the row a
//     tree with no retain fails, and it fails it as a use-after-free rather
//     than as a wrong number.
//   - MANY handles, from many threads, while the channel is in use. The count
//     is shared state on the hot path; a lost update either frees the object
//     under a live holder or frees it never, and the invariant that catches
//     both is that every value sent was either received or destroyed by the
//     reclaim -- never twice, and never neither.
//
// The element owns a heap allocation for the same reason the owned-element
// stand's does: a value delivered twice frees one block twice, a value read
// after its move reads storage that was emptied, and a value destroyed instead
// of delivered shows up as a number rather than as a wrong answer later.

#define HANDLE_CAPACITY 4u
#define HANDLE_THREADS 4u
#define HANDLE_ROUNDS 64u
#define HANDLE_TEXT_BYTES 24

typedef struct {
    uint64_t marker;
    char* text;
} owned_text;

// The entry point's argv, which this stand has none of and rt_io.c requires.
int rt_argc = 0;
char** rt_argv_raw = NULL;

// Destroyed BY THE CHANNEL: a value the reclaim found still in the ring.
static _Atomic uint32_t g_reclaimed_drops;
// Sent into the channel, and taken back out by this stand.
static _Atomic uint32_t g_sent;
static _Atomic uint32_t g_received;
// A value that came back out of storage that had already been moved from.
static _Atomic uint32_t g_bad_markers;
static _Atomic(void*) g_channel;

static void owned_text_move(void* destination, void* source) {
    owned_text* to = (owned_text*)destination;
    owned_text* from = (owned_text*)source;
    to->marker = from->marker;
    to->text = from->text;
    // A move CONSUMES its source: the obligation is the destination's now, and
    // a second read of the source must not find one.
    from->marker = 0;
    from->text = NULL;
}

static void owned_text_drop(void* value) {
    owned_text* typed = (owned_text*)value;
    free(typed->text);
    typed->text = NULL;
    typed->marker = 0;
    atomic_fetch_add_explicit(&g_reclaimed_drops, 1, memory_order_acq_rel);
}

static rt_carrier_status
owned_text_plan_cross(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    // A local channel never crosses; the slot refuses rather than inventing a
    // plan, which is what the mandatory-but-vacuous slot is for.
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

static const rt_value_ops owned_text_ops = {
    .layout = {.size = sizeof(owned_text),
               .align = _Alignof(owned_text),
               .stride = sizeof(owned_text),
               .flags = RT_VALUE_FLAG_DROPPABLE},
    .move_init = owned_text_move,
    .copy_init = NULL,
    .clone_init = NULL,
    .drop_in_place = owned_text_drop,
    .trace = NULL,
    .plan_cross = owned_text_plan_cross,
    .cross_move_init = NULL,
    .cross_clone_init = NULL,
};

static int handle_fail(const char* message) {
    if (message != NULL) {
        fputs(message, stderr);
        fputc('\n', stderr);
    }
    return 1;
}

// Builds one value with its obligation attached.
static int owned_text_make(owned_text* out, uint64_t marker) {
    out->marker = marker;
    out->text = (char*)malloc(HANDLE_TEXT_BYTES);
    if (out->text == NULL) {
        return 0;
    }
    snprintf(out->text, HANDLE_TEXT_BYTES, "%llu", (unsigned long long)marker);
    return 1;
}

// Sends one freshly built value, and keeps the obligation when the buffer had
// no room: a refused send delivered nothing, so this side still owns it.
static int handle_send_one(void* channel, uint64_t marker) {
    owned_text value;
    if (!owned_text_make(&value, marker)) {
        return 0;
    }
    if (!rt_channel_try_send(channel, &value)) {
        free(value.text);
        return 0;
    }
    atomic_fetch_add_explicit(&g_sent, 1, memory_order_acq_rel);
    return 1;
}

// Takes one value back out and ends its obligation here, which is what makes a
// value destroyed BY THE CHANNEL a different, separately counted outcome.
static int handle_recv_one(void* channel) {
    owned_text got = {.marker = 0, .text = NULL};
    if (!rt_channel_try_recv(channel, &got)) {
        return 0;
    }
    char expected[HANDLE_TEXT_BYTES];
    snprintf(expected, sizeof(expected), "%llu", (unsigned long long)got.marker);
    if (got.marker == 0 || got.text == NULL || strcmp(got.text, expected) != 0) {
        atomic_fetch_add_explicit(&g_bad_markers, 1, memory_order_acq_rel);
    }
    free(got.text);
    atomic_fetch_add_explicit(&g_received, 1, memory_order_acq_rel);
    return 1;
}

static void* handle_make_channel(uint64_t capacity) {
    void* channel = rt_channel_new(capacity, &owned_text_ops, 0);
    atomic_store_explicit(&g_channel, channel, memory_order_release);
    return channel;
}

// One handle, three values nobody receives. The single release IS the reclaim,
// and the ring's contents are destroyed there or nowhere.
static int mode_one_handle(void) {
    void* channel = handle_make_channel(HANDLE_CAPACITY);
    if (channel == NULL) {
        return handle_fail("channel was not created");
    }
    for (uint64_t marker = 1; marker <= 3; marker++) {
        if (!handle_send_one(channel, marker)) {
            return handle_fail("a send the buffer had room for was refused");
        }
    }
    rt_channel_handle_drop(channel);
    printf("one handle: sent=%u received=%u reclaimed_drops=%u bad=%u\n",
           (unsigned)atomic_load_explicit(&g_sent, memory_order_acquire),
           (unsigned)atomic_load_explicit(&g_received, memory_order_acquire),
           (unsigned)atomic_load_explicit(&g_reclaimed_drops, memory_order_acquire),
           (unsigned)atomic_load_explicit(&g_bad_markers, memory_order_acquire));
    return 0;
}

// Two handles on one object. The first release must be inert -- the second
// copy still sends and receives through the same buffer -- and the second must
// destroy exactly what is left.
static int mode_two_handles(void) {
    void* channel = handle_make_channel(HANDLE_CAPACITY);
    if (channel == NULL) {
        return handle_fail("channel was not created");
    }
    rt_channel_handle_retain(channel);
    for (uint64_t marker = 1; marker <= 3; marker++) {
        if (!handle_send_one(channel, marker)) {
            return handle_fail("a send the buffer had room for was refused");
        }
    }
    rt_channel_handle_drop(channel);
    unsigned after_first =
        (unsigned)atomic_load_explicit(&g_reclaimed_drops, memory_order_acquire);
    // Everything below this line reaches the object through the surviving
    // copy. On a tree with no retain the release above already freed it, and
    // these three lines are the use-after-free that says so.
    if (!handle_recv_one(channel)) {
        return handle_fail("the surviving handle could not receive a buffered value");
    }
    if (!handle_send_one(channel, 4)) {
        return handle_fail("the surviving handle could not send");
    }
    rt_channel_handle_drop(channel);
    printf("two handles: sent=%u received=%u drops_after_first_release=%u reclaimed_drops=%u "
           "bad=%u\n",
           (unsigned)atomic_load_explicit(&g_sent, memory_order_acquire),
           (unsigned)atomic_load_explicit(&g_received, memory_order_acquire),
           after_first,
           (unsigned)atomic_load_explicit(&g_reclaimed_drops, memory_order_acquire),
           (unsigned)atomic_load_explicit(&g_bad_markers, memory_order_acquire));
    return 0;
}

static void handle_sleep_us(unsigned long micros) {
    struct timespec ts;
    ts.tv_sec = (time_t)(micros / 1000000UL);
    ts.tv_nsec = (long)((micros % 1000000UL) * 1000UL);
    while (nanosleep(&ts, &ts) != 0) {
    }
}

typedef struct {
    uint32_t index;
    // Written by the worker: how many of its rounds got a value in. A round
    // that found the buffer full is not a failure, and this is what says how
    // much of the row actually pressed on the channel rather than bouncing.
    uint32_t admitted;
} handle_worker;

// One contending copy: retain, use the channel, release -- over and over,
// while every other thread does the same and the creator's own handle keeps
// the object alive underneath them all. The retain and the release are the
// contended instructions; the send and the recv are what makes an object freed
// early observable rather than merely accounted wrong.
static void* handle_worker_main(void* arg) {
    handle_worker* worker = (handle_worker*)arg;
    void* channel = atomic_load_explicit(&g_channel, memory_order_acquire);
    for (uint32_t round = 0; round < HANDLE_ROUNDS; round++) {
        rt_channel_handle_retain(channel);
        if (handle_send_one(channel, (uint64_t)worker->index * HANDLE_ROUNDS + round + 1u)) {
            worker->admitted++;
        }
        (void)handle_recv_one(channel);
        rt_channel_handle_drop(channel);
        if ((round & 7u) == 7u) {
            handle_sleep_us(1);
        }
    }
    return NULL;
}

// The census that answers for all of it: every value that entered the channel
// left it exactly once, either into a receiver or through the reclaim.
static int mode_contended_handles(void) {
    void* channel = handle_make_channel(HANDLE_CAPACITY);
    if (channel == NULL) {
        return handle_fail("channel was not created");
    }
    pthread_t threads[HANDLE_THREADS];
    handle_worker workers[HANDLE_THREADS];
    for (uint32_t i = 0; i < HANDLE_THREADS; i++) {
        workers[i].index = i;
        workers[i].admitted = 0;
        if (pthread_create(&threads[i], NULL, handle_worker_main, &workers[i]) != 0) {
            return handle_fail("a contending thread failed to start");
        }
    }
    unsigned admitted = 0;
    for (uint32_t i = 0; i < HANDLE_THREADS; i++) {
        (void)pthread_join(threads[i], NULL);
        admitted += workers[i].admitted;
    }
    // The creator's handle is the last one, so the reclaim happens here and
    // the ring's leftovers are destroyed with it.
    rt_channel_handle_drop(channel);
    unsigned sent = (unsigned)atomic_load_explicit(&g_sent, memory_order_acquire);
    unsigned received = (unsigned)atomic_load_explicit(&g_received, memory_order_acquire);
    unsigned drops = (unsigned)atomic_load_explicit(&g_reclaimed_drops, memory_order_acquire);
    unsigned bad = (unsigned)atomic_load_explicit(&g_bad_markers, memory_order_acquire);
    printf("contended handles: threads=%u sent=%u received=%u reclaimed_drops=%u bad=%u "
           "accounted=%u\n",
           (unsigned)HANDLE_THREADS,
           sent,
           received,
           drops,
           bad,
           sent == received + drops ? 1u : 0u);
    if (sent == 0 || admitted != sent) {
        return handle_fail("no value ever entered the channel");
    }
    return 0;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id) {
    // This stand drives the channel from ordinary threads, so no task body is
    // ever polled: reaching this is a defect in the stand, not a result.
    (void)id;
    rt_async_return(NULL, &(uint64_t){0});
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value) {
    (void)id;
    (void)value;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_abandoned_state_call(uint64_t id, void* state) {
    (void)id;
    (void)state;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

// The row is named by the environment and not by argv, so that every harness
// -- including the one that runs this binary under valgrind, which passes it
// no arguments of its own -- selects a row the same way.
int main(void) {
    const char* mode = getenv("SURGE_CHANNEL_HANDLE_MODE");
    if (mode == NULL) {
        return handle_fail("SURGE_CHANNEL_HANDLE_MODE names the row to run");
    }
    rt_executor* ex = ensure_exec();
    if (ex == NULL) {
        return handle_fail("missing executor");
    }
    int status;
    if (strcmp(mode, "one-handle") == 0) {
        status = mode_one_handle();
    } else if (strcmp(mode, "two-handles") == 0) {
        status = mode_two_handles();
    } else if (strcmp(mode, "contended-handles") == 0) {
        status = mode_contended_handles();
    } else {
        return handle_fail("unknown mode");
    }
    atomic_store_explicit(&g_channel, NULL, memory_order_release);
    (void)rt_executor_request_shutdown(ex);
    if (atomic_load_explicit(&g_bad_markers, memory_order_acquire) != 0) {
        return handle_fail("a value arrived out of storage that had been moved from");
    }
    return status;
}
