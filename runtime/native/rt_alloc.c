#ifndef _POSIX_C_SOURCE
#define _POSIX_C_SOURCE 200809L // NOLINT(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
#endif

#include "rt.h"
#include "rt_heap_accounting.h"

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

static uint64_t min_u64(uint64_t a, uint64_t b) {
    return a < b ? a : b;
}

static uint64_t alloc_size(uint64_t size) {
    return size == 0 ? 1 : size;
}

static void record_alloc(uint64_t size) {
    rt_heap_accounting_record_alloc(rt_heap_accounting_current_cell(), size);
}

static void record_free(uint64_t size) {
    rt_heap_accounting_record_free(rt_heap_accounting_current_cell(), size);
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

// A reallocation RELEASES the old block, and this runtime has exactly one
// release: rt_free. That is where a block stops being reachable through the
// registries that recorded its address -- the array view registry is the one
// that exists today, and rt_free is the only thing that tells it to forget.
//
// Growing a block in place is still a release of the old block, and it is safe
// only for the lane that OWNS the block. No owning lane is recorded in page or
// span metadata yet, so no caller can claim to be one, and no reallocation may
// grow in place. libc realloc grows in place when it can and releases the old
// block itself, telling no registry: a block grown through it kept its
// registrations while its address went back to the allocator. So there is no
// alignment-conditional fast path here. Every reallocation allocates, copies,
// and releases through rt_free.
//
// The consequence for callers: the returned pointer is always a new address, so
// a pointer held INTO the old block does not survive this call. That was
// already true of every over-aligned reallocation; it is now true of all of
// them, rather than true only when the allocator happened to move the block.
//
// The counters are unchanged by the merge: rt_alloc + rt_free record exactly
// what the single realloc record did, one allocation and one release against
// the issuing lane's cell.
void* rt_realloc(uint8_t* ptr, uint64_t old_size, uint64_t new_size, uint64_t align) {
    if (new_size == 0) {
        rt_free(ptr, old_size, align);
        return NULL;
    }
    void* next = rt_alloc(new_size, align);
    if (next == NULL) {
        // Nothing was released: a failed reallocation leaves the caller owning
        // the block it came in with.
        return NULL;
    }
    if (ptr != NULL) {
        // An empty copy is still a release. A live pointer whose old size is
        // zero has a block behind it, and skipping the release here is how it
        // used to leak on the over-aligned path.
        rt_memcpy((uint8_t*)next, ptr, min_u64(old_size, new_size));
        rt_free(ptr, old_size, align);
    }
    return next;
}

void* rt_heap_stats(void) {
    struct rt_heap_accounting_snapshot snapshot;
    rt_heap_accounting_status status =
        rt_heap_accounting_snapshot(rt_runtime_global_heap_accounting(), &snapshot);
    if (status != RT_HEAP_ACCOUNTING_OK) {
        return NULL;
    }

    SurgeHeapStats* stats = (SurgeHeapStats*)rt_alloc((uint64_t)sizeof(SurgeHeapStats),
                                                      (uint64_t) _Alignof(SurgeHeapStats));
    if (stats == NULL) {
        return NULL;
    }
    stats->alloc_count = rt_biguint_from_u64(snapshot.alloc_count);
    stats->free_count = rt_biguint_from_u64(snapshot.free_count);
    stats->live_blocks = rt_biguint_from_u64(snapshot.live_blocks);
    stats->live_bytes = rt_biguint_from_u64(snapshot.live_bytes);
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
