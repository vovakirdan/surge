#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

typedef struct SurgeHeapStats {
    void* alloc_count;
    void* free_count;
    void* live_blocks;
    void* live_bytes;
    void* rc_increments;
    void* rc_decrements;
} SurgeHeapStats;

static _Atomic uint64_t heap_alloc_count;
static _Atomic uint64_t heap_free_count;
static _Atomic uint64_t heap_live_blocks;
static _Atomic uint64_t heap_live_bytes;

static uint64_t min_u64(uint64_t a, uint64_t b) {
    return a < b ? a : b;
}

static uint64_t alloc_size(uint64_t size) {
    return size == 0 ? 1 : size;
}

static void record_alloc(uint64_t size) {
    (void)atomic_fetch_add_explicit(&heap_alloc_count, 1, memory_order_relaxed);
    (void)atomic_fetch_add_explicit(&heap_live_blocks, 1, memory_order_relaxed);
    (void)atomic_fetch_add_explicit(&heap_live_bytes, alloc_size(size), memory_order_relaxed);
}

static void record_free(uint64_t size) {
    (void)atomic_fetch_add_explicit(&heap_free_count, 1, memory_order_relaxed);
    (void)atomic_fetch_sub_explicit(&heap_live_blocks, 1, memory_order_relaxed);
    (void)atomic_fetch_sub_explicit(&heap_live_bytes, alloc_size(size), memory_order_relaxed);
}

static void record_realloc(uint64_t old_size, uint64_t new_size) {
    uint64_t old_actual = alloc_size(old_size);
    uint64_t new_actual = alloc_size(new_size);
    (void)atomic_fetch_add_explicit(&heap_alloc_count, 1, memory_order_relaxed);
    (void)atomic_fetch_add_explicit(&heap_free_count, 1, memory_order_relaxed);
    if (new_actual >= old_actual) {
        (void)atomic_fetch_add_explicit(
            &heap_live_bytes, new_actual - old_actual, memory_order_relaxed);
    } else {
        (void)atomic_fetch_sub_explicit(
            &heap_live_bytes, old_actual - new_actual, memory_order_relaxed);
    }
}

void* rt_alloc(uint64_t size, uint64_t align) {
    size = alloc_size(size);
    void* ptr = NULL;
    if (align <= sizeof(void*)) {
        ptr = malloc((size_t)size);
        if (ptr != NULL) {
            record_alloc(size);
        }
        return ptr;
    }
    if (posix_memalign(&ptr, (size_t)align, (size_t)size) != 0) {
        return NULL;
    }
    record_alloc(size);
    return ptr;
}

void rt_free(uint8_t* ptr, uint64_t size, uint64_t align) {
    (void)align;
    if (ptr != NULL) {
        rt_array_forget_allocation(ptr);
        record_free(size);
    }
    free(ptr);
}

void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align) {
    if (new_size == 0) {
        rt_free(ptr, old_size, align);
        return NULL;
    }
    if (align <= sizeof(void*)) {
        void* next = realloc(ptr, (size_t)alloc_size(new_size));
        if (next != NULL) {
            if (ptr == NULL) {
                record_alloc(new_size);
            } else {
                record_realloc(old_size, new_size);
            }
        }
        return next;
    }
    void* next = rt_alloc(new_size, align);
    if (next == NULL) {
        return NULL;
    }
    if (ptr != NULL && old_size > 0) {
        rt_memcpy((uint8_t*)next, ptr, min_u64(old_size, new_size));
        rt_free(ptr, old_size, align);
    }
    return next;
}

void* rt_heap_stats(void) {
    uint64_t alloc_count = atomic_load_explicit(&heap_alloc_count, memory_order_relaxed);
    uint64_t free_count = atomic_load_explicit(&heap_free_count, memory_order_relaxed);
    uint64_t live_blocks = atomic_load_explicit(&heap_live_blocks, memory_order_relaxed);
    uint64_t live_bytes = atomic_load_explicit(&heap_live_bytes, memory_order_relaxed);

    SurgeHeapStats* stats = (SurgeHeapStats*)rt_alloc((uint64_t)sizeof(SurgeHeapStats),
                                                      (uint64_t) _Alignof(SurgeHeapStats));
    if (stats == NULL) {
        return NULL;
    }
    stats->alloc_count = rt_biguint_from_u64(alloc_count);
    stats->free_count = rt_biguint_from_u64(free_count);
    stats->live_blocks = rt_biguint_from_u64(live_blocks);
    stats->live_bytes = rt_biguint_from_u64(live_bytes);
    stats->rc_increments = rt_biguint_from_u64(0);
    stats->rc_decrements = rt_biguint_from_u64(0);
    return stats;
}

void rt_memcpy(uint8_t* dst, const uint8_t* src, uint64_t n) {
    if (n == 0) {
        return;
    }
    (void)memcpy(dst, src, (size_t)n);
}

void rt_memmove(uint8_t* dst, const uint8_t* src, uint64_t n) {
    if (n == 0) {
        return;
    }
    (void)memmove(dst, src, (size_t)n);
}
