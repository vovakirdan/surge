#include "rt.h"
#include "rt_bignum_tag.h"

#include <stdalign.h>
#include <stddef.h>
#include <string.h>

// rt_alloc hands back uninitialized bytes, so every field a reader consults has
// to be written here. kind is one of those readers' questions: an iteration step
// asks it to tell a pair of bounds from an array cursor, and an unwritten byte
// would answer at random. bound is the other: a release asks it which of the
// three lifecycles a bound word belongs to.
static SurgeRange* alloc_range(uint8_t bound) {
    SurgeRange* r =
        (SurgeRange*)rt_alloc((uint64_t)sizeof(SurgeRange), (uint64_t)alignof(SurgeRange));
    if (r != NULL) {
        r->kind = SURGE_RANGE_KIND_BOUNDS;
        r->bound = bound;
    }
    return r;
}

// Inline integer bounds own no block: do not dispatch a numeric lifecycle
// call for them. Float bounds keep their existing pointer lifecycle.
static bool range_bound_owns_no_block(const void* word, uint8_t bound) {
    return bound != SURGE_RANGE_BOUND_FLOAT && !fix_is_heap(word);
}

static void range_bound_retain(void* word, uint8_t bound) {
    if (range_bound_owns_no_block(word, bound)) {
        return;
    }
    switch (bound) {
        case SURGE_RANGE_BOUND_FLOAT:
            rt_bigfloat_retain(word);
            return;
        case SURGE_RANGE_BOUND_UINT:
            rt_biguint_retain(word);
            return;
        default:
            rt_bigint_retain(word);
            return;
    }
}

static void range_bound_release(void* word, uint8_t bound) {
    if (bound != SURGE_RANGE_BOUND_FLOAT && !fix_is_heap(word)) {
        return;
    }
    switch (bound) {
        case SURGE_RANGE_BOUND_FLOAT:
            rt_bigfloat_release(word);
            return;
        case SURGE_RANGE_BOUND_UINT:
            rt_biguint_release(word);
            return;
        default:
            rt_bigint_release(word);
            return;
    }
}

static void* range_bound_unshare(void* word, uint8_t bound) {
    if (bound != SURGE_RANGE_BOUND_FLOAT && !fix_is_heap(word)) {
        return word;
    }
    switch (bound) {
        case SURGE_RANGE_BOUND_FLOAT:
            return rt_bigfloat_unshare(word);
        case SURGE_RANGE_BOUND_UINT:
            return rt_biguint_unshare(word);
        default:
            return rt_bigint_unshare(word);
    }
}

// See rt.h: a crossing refusal is reported under VM1003, the code both backends
// give a value that is not what its type promised, and with a NULL span because
// a panic raised inside this runtime has no source location of its own.
static void range_panic_cannot_cross(const char* msg) {
    static const char code[] = "VM1003";
    rt_panic_code((const uint8_t*)code,
                  (uint64_t)(sizeof(code) - 1),
                  (const uint8_t*)msg,
                  (uint64_t)strlen(msg),
                  NULL,
                  0);
}

// See rt.h: a null range is refused under VM1203, the code the VM gives a
// handle that names no object, in the words the VM uses.
void rt_range_require(const void* handle) {
    if (handle != NULL) {
        return;
    }
    static const char code[] = "VM1203";
    static const char msg[] = "null range handle";
    rt_panic_code((const uint8_t*)code,
                  (uint64_t)(sizeof(code) - 1),
                  (const uint8_t*)msg,
                  (uint64_t)(sizeof(msg) - 1),
                  NULL,
                  0);
}

// See rt.h for why each bound is released here, and for the four facts that had
// to be true first.
void rt_range_free(void* handle) {
    if (handle == NULL) {
        return;
    }
    const SurgeRange* r = (const SurgeRange*)handle;
    uint64_t size = (uint64_t)sizeof(SurgeRange);
    if (r->kind == SURGE_RANGE_KIND_ARRAY_ITER) {
        size = (uint64_t)sizeof(SurgeRangeArrayIter);
    } else {
        if (r->has_start) {
            range_bound_release(r->start, r->bound);
        }
        if (r->has_end) {
            range_bound_release(r->end, r->bound);
        }
    }
    rt_free((uint8_t*)handle, size, (uint64_t)alignof(SurgeRange));
}

void rt_range_bounds_retain(void* handle) {
    if (handle == NULL) {
        return;
    }
    SurgeRange* r = (SurgeRange*)handle;
    if (r->kind != SURGE_RANGE_KIND_BOUNDS) {
        return;
    }
    if (r->has_start) {
        range_bound_retain(r->start, r->bound);
    }
    if (r->has_end) {
        range_bound_retain(r->end, r->bound);
    }
}

void rt_range_unshare(void* range_slot) {
    if (range_slot == NULL) {
        return;
    }
    SurgeRange* r = *(SurgeRange**)range_slot;
    if (r == NULL) {
        return;
    }
    if (r->kind == SURGE_RANGE_KIND_ARRAY_ITER) {
        range_panic_cannot_cross("array cursor cannot cross a shard boundary: it reads elements "
                                 "out of the buffer the origin shard keeps; cross the array "
                                 "itself and walk it on the other side");
    }
    // In place, so the reference this range holds is the one given up: unshare
    // keeps the block when it has one holder and otherwise duplicates it.
    if (r->has_start) {
        r->start = range_bound_unshare(r->start, r->bound);
    }
    if (r->has_end) {
        r->end = range_bound_unshare(r->end, r->bound);
    }
}

void* rt_range_bounds_new(void* start, void* end, bool inclusive, uint8_t bound) {
    SurgeRange* r = alloc_range(bound);
    if (r == NULL) {
        return NULL;
    }
    range_bound_retain(start, bound);
    range_bound_retain(end, bound);
    r->start = start;
    r->end = end;
    r->has_start = 1;
    r->has_end = 1;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}

void* rt_range_int_new(void* start, void* end, bool inclusive) {
    return rt_range_bounds_new(start, end, inclusive, SURGE_RANGE_BOUND_INT);
}

void* rt_range_int_from_start(void* start, bool inclusive) {
    SurgeRange* r = alloc_range(SURGE_RANGE_BOUND_INT);
    if (r == NULL) {
        return NULL;
    }
    range_bound_retain(start, SURGE_RANGE_BOUND_INT);
    r->start = start;
    r->end = NULL;
    r->has_start = 1;
    r->has_end = 0;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}

void* rt_range_int_to_end(void* end, bool inclusive) {
    SurgeRange* r = alloc_range(SURGE_RANGE_BOUND_INT);
    if (r == NULL) {
        return NULL;
    }
    range_bound_retain(end, SURGE_RANGE_BOUND_INT);
    r->start = NULL;
    r->end = end;
    r->has_start = 0;
    r->has_end = 1;
    r->inclusive = inclusive ? 1 : 0;
    return (void*)r;
}

void* rt_range_int_full(bool inclusive) {
    SurgeRange* r = alloc_range(SURGE_RANGE_BOUND_INT);
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
