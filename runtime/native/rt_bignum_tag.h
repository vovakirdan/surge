#ifndef SURGE_RUNTIME_NATIVE_BIGNUM_TAG_H
#define SURGE_RUNTIME_NATIVE_BIGNUM_TAG_H

// Small-integer inline representation ("fixnum") for Surge int/uint.
//
// An int/uint value crosses the runtime boundary as a uintptr_t-sized word
// carried in a void*. The low bit tags the word:
//
//   low bit 1  -> inline fixnum. The payload is the upper 63 bits.
//                 int  decodes with an arithmetic shift (sign-preserving);
//                 uint decodes with a logical shift.
//   low bit 0  -> an aligned heap SurgeBigInt*/SurgeBigUint* (alignof >= 2,
//                 so the low bits are always free), OR NULL.
//
// NULL is the one canonical representation of zero and is NEVER produced as an
// inline value: box(0) returns NULL. Every pre-existing "NULL means zero" check
// in the bignum runtime therefore stays valid, and inline payloads are always
// non-zero. Values outside the inline range stay on the heap.

#include <stdbool.h>
#include <stdint.h>

// The tagging scheme spends one pointer bit and assumes a 64-bit word. On a
// narrower target the inline range would shrink; fail loudly instead of
// silently mis-encoding. (Heap-pointer alignment is asserted next to the
// struct definitions, where alignof(SurgeBigInt) is visible.)
_Static_assert(sizeof(uintptr_t) == 8, "fixnum tagging assumes a 64-bit word");

// Inline payload is 63 bits. Signed int fits [-2^62, 2^62 - 1]; unsigned uint
// fits [0, 2^63 - 1]. These are the values for which (v << 1) | 1 round-trips.
#define SURGE_FIXI_MIN (-((int64_t)1 << 62))
#define SURGE_FIXI_MAX (((int64_t)1 << 62) - 1)
#define SURGE_FIXU_MAX (((uint64_t)1 << 63) - 1u)

static inline bool fix_is_inline(const void* w) {
    return ((uintptr_t)w & (uintptr_t)1) != 0;
}

static inline bool fix_is_heap(const void* w) {
    return w != NULL && ((uintptr_t)w & (uintptr_t)1) == 0;
}

// Signed inline decode: arithmetic shift preserves sign.
static inline int64_t fixi_value(const void* w) {
    return (int64_t)((intptr_t)(uintptr_t)w >> 1);
}

// Unsigned inline decode: logical shift.
static inline uint64_t fixu_value(const void* w) {
    return (uint64_t)((uintptr_t)w >> 1);
}

// Decode an int operand that is inline-or-zero into an int64. Returns false for
// heap operands (which the caller must promote). Zero (NULL) decodes to 0.
static inline bool fixi_as_i64(const void* w, int64_t* out) {
    if (w == NULL) {
        *out = 0;
        return true;
    }
    if (((uintptr_t)w & (uintptr_t)1) != 0) {
        *out = fixi_value(w);
        return true;
    }
    return false;
}

// Decode a uint operand that is inline-or-zero into a uint64. Returns false for
// heap operands. Zero (NULL) decodes to 0.
static inline bool fixu_as_u64(const void* w, uint64_t* out) {
    if (w == NULL) {
        *out = 0;
        return true;
    }
    if (((uintptr_t)w & (uintptr_t)1) != 0) {
        *out = fixu_value(w);
        return true;
    }
    return false;
}

// Box a signed value inline when it fits. Zero -> NULL (canonical). Sets *ok to
// false when the value must live on the heap instead (out of inline range).
static inline void* fixi_box(int64_t v, bool* ok) {
    if (v < SURGE_FIXI_MIN || v > SURGE_FIXI_MAX) {
        if (ok != NULL) {
            *ok = false;
        }
        return NULL;
    }
    if (ok != NULL) {
        *ok = true;
    }
    if (v == 0) {
        return NULL;
    }
    // Build the word with unsigned shifts to keep the encoding well-defined for
    // negative v; fixi_value decodes it back with an arithmetic shift.
    return (void*)(((uintptr_t)(uint64_t)v << 1) | (uintptr_t)1);
}

// Box an unsigned value inline when it fits. Zero -> NULL. Sets *ok to false
// when the value must live on the heap instead.
static inline void* fixu_box(uint64_t v, bool* ok) {
    if (v > SURGE_FIXU_MAX) {
        if (ok != NULL) {
            *ok = false;
        }
        return NULL;
    }
    if (ok != NULL) {
        *ok = true;
    }
    if (v == 0) {
        return NULL;
    }
    return (void*)(((uintptr_t)v << 1) | (uintptr_t)1);
}

#endif
