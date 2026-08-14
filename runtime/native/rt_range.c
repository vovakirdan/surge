#include "rt.h"

#include <stdalign.h>
#include <stddef.h>

// rt_alloc hands back uninitialized bytes, so every field a reader consults has
// to be written here. kind is one of those readers' questions: an iteration step
// asks it to tell a pair of bounds from an array cursor, and an unwritten byte
// would answer at random.
static SurgeRange* alloc_range(void) {
    SurgeRange* r =
        (SurgeRange*)rt_alloc((uint64_t)sizeof(SurgeRange), (uint64_t)alignof(SurgeRange));
    if (r != NULL) {
        r->kind = SURGE_RANGE_KIND_BOUNDS;
    }
    return r;
}

// See rt.h for why the bounds are not released here.
void rt_range_free(void* handle) {
    if (handle == NULL) {
        return;
    }
    const SurgeRange* r = (const SurgeRange*)handle;
    uint64_t size = r->kind == SURGE_RANGE_KIND_ARRAY_ITER ? (uint64_t)sizeof(SurgeRangeArrayIter)
                                                           : (uint64_t)sizeof(SurgeRange);
    rt_free((uint8_t*)handle, size, (uint64_t)alignof(SurgeRange));
}

void* rt_range_int_new(void* start, void* end, bool inclusive) {
    SurgeRange* r = alloc_range();
    if (r == NULL) {
        return NULL;
    }
    r->start = start;
    r->end = end;
    r->has_start = 1;
    r->has_end = 1;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}

void* rt_range_int_from_start(void* start, bool inclusive) {
    SurgeRange* r = alloc_range();
    if (r == NULL) {
        return NULL;
    }
    r->start = start;
    r->end = NULL;
    r->has_start = 1;
    r->has_end = 0;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}

void* rt_range_int_to_end(void* end, bool inclusive) {
    SurgeRange* r = alloc_range();
    if (r == NULL) {
        return NULL;
    }
    r->start = NULL;
    r->end = end;
    r->has_start = 0;
    r->has_end = 1;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}

void* rt_range_int_full(bool inclusive) {
    SurgeRange* r = alloc_range();
    if (r == NULL) {
        return NULL;
    }
    r->start = NULL;
    r->end = NULL;
    r->has_start = 0;
    r->has_end = 0;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}
