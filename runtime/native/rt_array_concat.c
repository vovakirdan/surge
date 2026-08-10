#include "rt.h"
#include "rt_array_internal.h"

#include <stdalign.h>
#include <stdint.h>
#include <string.h>

// Array concatenation. `a + b` builds a THIRD array; the two operands are
// borrows and are handed back untouched.
//
// The new array owns its buffer outright, which is the whole difficulty: an
// element that owns heap cannot simply have its word copied, or the result's
// drop and the source's drop would reach the same bytes. The compiler knows
// what an element owns and this function does not, so it is handed a
// per-element clone the same way rt_array_free_elems is handed a per-element
// drop. A null clone means the element's bits ARE its value, and one memcpy of
// the whole run does what a call per element would have.
//
// A VIEW is a legal source: its len and data describe its own window into a
// base, and reading them needs no registry lookup. The RESULT is never a view
// — it is a fresh base, so its cap counts elements and its drop frees the
// buffer it just allocated.

typedef void (*SurgeArrayCloneElem)(void*, const void*);

static void concat_panic(const char* msg) {
    // NULL span: a panic raised inside the runtime has no source location to
    // give, and the reporter prints no location line for one.
    rt_panic_numeric((const uint8_t*)msg, (uint64_t)strlen(msg), NULL, 0);
}

static const SurgeArrayHeader* concat_source(void* slot) {
    if (slot == NULL) {
        concat_panic("array concat received null pointer");
        return NULL;
    }
    const SurgeArrayHeader* header = *(const SurgeArrayHeader**)slot;
    if (header == NULL) {
        concat_panic("array concat received null array");
        return NULL;
    }
    return header;
}

// Fills one run of the destination buffer from one source, element by element
// when the elements own something and in one move when they do not.
static void concat_copy_run(uint8_t* dst,
                            const SurgeArrayHeader* src,
                            uint64_t elem_stride,
                            SurgeArrayCloneElem clone_elem) {
    if (src == NULL || src->len == 0) {
        return;
    }
    if (src->data == NULL) {
        concat_panic("array concat received null data");
        return;
    }
    const uint8_t* bytes = (const uint8_t*)src->data;
    if (clone_elem == NULL) {
        rt_memcpy(dst, bytes, src->len * elem_stride);
        return;
    }
    for (uint64_t i = 0; i < src->len; i++) {
        clone_elem(dst + i * elem_stride, bytes + i * elem_stride);
    }
}

void* rt_array_concat(void* left_slot,
                      void* right_slot,
                      uint64_t elem_stride,
                      uint64_t elem_align,
                      void (*clone_elem)(void*, const void*)) {
    const SurgeArrayHeader* left = concat_source(left_slot);
    const SurgeArrayHeader* right = concat_source(right_slot);
    if (left == NULL || right == NULL) {
        return NULL;
    }
    if (left->len > UINT64_MAX - right->len) {
        concat_panic("array length out of range");
        return NULL;
    }
    uint64_t total = left->len + right->len;
    // A base may never claim the capacity that marks a view, or its own drop
    // would take the view path and leave the buffer behind.
    if (total >= SURGE_ARRAY_VIEW_CAP) {
        concat_panic("array length out of range");
        return NULL;
    }
    if (elem_stride != 0 && total > UINT64_MAX / elem_stride) {
        concat_panic("array length out of range");
        return NULL;
    }
    if (elem_align == 0) {
        elem_align = 1;
    }

    uint64_t size = total * elem_stride;
    uint8_t* data = NULL;
    if (size > 0) {
        data = (uint8_t*)rt_alloc(size, elem_align);
        if (data == NULL) {
            concat_panic("array allocation failed");
            return NULL;
        }
        concat_copy_run(data, left, elem_stride, clone_elem);
        concat_copy_run(data + left->len * elem_stride, right, elem_stride, clone_elem);
    }

    SurgeArrayHeader* header = (SurgeArrayHeader*)rt_alloc((uint64_t)sizeof(SurgeArrayHeader),
                                                           (uint64_t)alignof(SurgeArrayHeader));
    if (header == NULL) {
        concat_panic("array allocation failed");
        return NULL;
    }
    header->len = total;
    // cap counts ELEMENTS for a typed base: the drop multiplies it by the
    // stride it is given to recover the size this allocation was made with.
    header->cap = total;
    header->data = data;
    return header;
}
