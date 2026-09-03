#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt_resident_bytes.h"

#include "rt_carrier_bench.h"
#include "rt_value_ops.h"

#include <stdatomic.h>
#include <stdio.h>
#include <unistd.h>

// Relaxed atomics, like the net trace: the counters are read at a dump or by
// a stand after the work it measures has drained, never as a synchronization
// edge. A peak is the value right after this thread's own add, which is exact
// for that add because the add and the read are one fetch_add.

static _Atomic uint64_t resident_live[RT_RESIDENT_KIND_COUNT];
static _Atomic uint64_t resident_peak[RT_RESIDENT_KIND_COUNT];
static _Atomic uint64_t resident_acquired[RT_RESIDENT_KIND_COUNT];
static _Atomic uint64_t resident_live_total;
static _Atomic uint64_t resident_peak_total;
static _Atomic uint64_t resident_crossing_clone_bytes;
static _Atomic uint64_t resident_crossing_clones;
static _Atomic uint64_t resident_underflows;

static void raise_peak(_Atomic uint64_t* peak, uint64_t value) {
    uint64_t current = atomic_load_explicit(peak, memory_order_relaxed);
    while (current < value &&
           !atomic_compare_exchange_weak_explicit(
               peak, &current, value, memory_order_relaxed, memory_order_relaxed)) {
    }
}

// Gives back at most what is held, and says whether it had to clamp: a
// release that outruns its acquire is a bookkeeping defect, counted as one
// by the caller instead of wrapping the balance into a number nobody could
// read.
static int lower_live(_Atomic uint64_t* live, uint64_t bytes) {
    uint64_t current = atomic_load_explicit(live, memory_order_relaxed);
    for (;;) {
        uint64_t next = current >= bytes ? current - bytes : 0;
        if (atomic_compare_exchange_weak_explicit(
                live, &current, next, memory_order_relaxed, memory_order_relaxed)) {
            return current < bytes;
        }
    }
}

void rt_resident_bytes_acquire(rt_resident_kind kind, uint64_t bytes) {
    if (bytes == 0 || kind >= RT_RESIDENT_KIND_COUNT) {
        return;
    }
    uint64_t live =
        atomic_fetch_add_explicit(&resident_live[kind], bytes, memory_order_relaxed) + bytes;
    raise_peak(&resident_peak[kind], live);
    (void)atomic_fetch_add_explicit(&resident_acquired[kind], bytes, memory_order_relaxed);
    uint64_t total =
        atomic_fetch_add_explicit(&resident_live_total, bytes, memory_order_relaxed) + bytes;
    raise_peak(&resident_peak_total, total);
    rt_carrier_bench_transport_acquire(bytes);
}

void rt_resident_bytes_release(rt_resident_kind kind, uint64_t bytes) {
    if (bytes == 0 || kind >= RT_RESIDENT_KIND_COUNT) {
        return;
    }
    int clamped = lower_live(&resident_live[kind], bytes);
    (void)lower_live(&resident_live_total, bytes);
    if (clamped) {
        (void)atomic_fetch_add_explicit(&resident_underflows, 1, memory_order_relaxed);
    }
    rt_carrier_bench_transport_release(bytes);
}

static uint64_t payload_width(const rt_value_ops* operations) {
    return operations == NULL ? 0 : (uint64_t)operations->layout.size;
}

void rt_resident_payload_acquire(const rt_value_ops* operations) {
    rt_resident_bytes_acquire(RT_RESIDENT_PAYLOAD, payload_width(operations));
}

void rt_resident_payload_release(const rt_value_ops* operations) {
    rt_resident_bytes_release(RT_RESIDENT_PAYLOAD, payload_width(operations));
}

void rt_resident_bytes_record_crossing_clone(uint64_t bytes) {
    if (bytes == 0) {
        return;
    }
    (void)atomic_fetch_add_explicit(&resident_crossing_clone_bytes, bytes, memory_order_relaxed);
    (void)atomic_fetch_add_explicit(&resident_crossing_clones, 1, memory_order_relaxed);
}

struct rt_resident_bytes_snapshot rt_resident_bytes_snapshot(void) {
    struct rt_resident_bytes_snapshot snapshot = {0};
    for (int kind = 0; kind < RT_RESIDENT_KIND_COUNT; kind++) {
        snapshot.live[kind] = atomic_load_explicit(&resident_live[kind], memory_order_relaxed);
        snapshot.peak[kind] = atomic_load_explicit(&resident_peak[kind], memory_order_relaxed);
        snapshot.acquired[kind] =
            atomic_load_explicit(&resident_acquired[kind], memory_order_relaxed);
    }
    snapshot.live_total = atomic_load_explicit(&resident_live_total, memory_order_relaxed);
    snapshot.peak_total = atomic_load_explicit(&resident_peak_total, memory_order_relaxed);
    snapshot.crossing_clone_bytes =
        atomic_load_explicit(&resident_crossing_clone_bytes, memory_order_relaxed);
    snapshot.crossing_clones =
        atomic_load_explicit(&resident_crossing_clones, memory_order_relaxed);
    snapshot.underflows = atomic_load_explicit(&resident_underflows, memory_order_relaxed);
    return snapshot;
}

const char* rt_resident_kind_name(rt_resident_kind kind) {
    switch (kind) {
        case RT_RESIDENT_ENVELOPE:
            return "envelope";
        case RT_RESIDENT_PADDING:
            return "padding";
        case RT_RESIDENT_RECORD:
            return "record";
        case RT_RESIDENT_PAYLOAD:
            return "payload";
        case RT_RESIDENT_SIDECAR:
            return "sidecar";
        case RT_RESIDENT_KIND_COUNT:
            break;
    }
    return "unknown";
}

void rt_resident_bytes_dump(const char* reason) {
    if (reason == NULL || reason[0] == '\0') {
        reason = "unknown";
    }
    struct rt_resident_bytes_snapshot snapshot = rt_resident_bytes_snapshot();
    char buf[1024];
    int pos = snprintf(buf, sizeof(buf), "TRACE_RESIDENT reason=%s", reason);
    if (pos < 0) {
        return;
    }
    for (int kind = 0; kind < RT_RESIDENT_KIND_COUNT && (size_t)pos < sizeof(buf); kind++) {
        const char* name = rt_resident_kind_name((rt_resident_kind)kind);
        int written = snprintf(&buf[pos],
                               sizeof(buf) - (size_t)pos,
                               " %s_live=%llu %s_peak=%llu %s_acquired=%llu",
                               name,
                               (unsigned long long)snapshot.live[kind],
                               name,
                               (unsigned long long)snapshot.peak[kind],
                               name,
                               (unsigned long long)snapshot.acquired[kind]);
        if (written < 0) {
            return;
        }
        pos += written;
    }
    if ((size_t)pos < sizeof(buf)) {
        int written = snprintf(&buf[pos],
                               sizeof(buf) - (size_t)pos,
                               " live_total=%llu peak_total=%llu crossing_clone_bytes=%llu "
                               "crossing_clones=%llu underflows=%llu\n",
                               (unsigned long long)snapshot.live_total,
                               (unsigned long long)snapshot.peak_total,
                               (unsigned long long)snapshot.crossing_clone_bytes,
                               (unsigned long long)snapshot.crossing_clones,
                               (unsigned long long)snapshot.underflows);
        if (written < 0) {
            return;
        }
        pos += written;
    }
    if ((size_t)pos >= sizeof(buf)) {
        pos = (int)sizeof(buf) - 1;
    }
    (void)write(STDERR_FILENO, buf, (size_t)pos);
}
