#include "rt_frame.h"

#include "rt.h"
#include "rt_bignum_tag.h"
#include "rt_value_cell.h"

#include <string.h>

// One suspension frame's storage, from the descriptor at both ends. See
// rt_frame.h for what a frame is and why the width is read rather than written.

static uint64_t frame_align(const rt_value_ops* ops) {
    return ops->layout.align == 0 ? 1u : (uint64_t)ops->layout.align;
}

// Reads field 0 and reports whether the frame says it is still packed.
//
// ANYTHING ELSE READS AS SPENT, and the asymmetry is deliberate: walking a
// spent frame frees its members a second time, while skipping a packed one
// leaks them. A wrong answer in the leaking direction is recoverable and one in
// the other direction is not, so the walk happens only for a frame that says so
// in the one word written to say it.
//
// The word is copied out rather than read through a cast, because the frame's
// storage is a byte block the allocator returned and this is a different type
// than the one that wrote it.
static int frame_is_packed(const rt_value_ops* ops, const void* frame) {
    if (ops->layout.size < sizeof(void*)) {
        return 0;
    }
    const void* word = NULL;
    memcpy((void*)&word, frame, sizeof(word));
    int64_t state = 0;
    return fixi_as_i64(word, &state) != 0 && state == (int64_t)SURGE_FRAME_STATE_PACKED;
}

void* rt_frame_alloc(const rt_value_ops* ops) {
    if (ops == NULL) {
        return NULL;
    }
    void* frame = rt_alloc((uint64_t)ops->layout.size, frame_align(ops));
    if (frame == NULL) {
        return NULL;
    }
    // Zero-filled, so a frame read before anything packed it answers with a
    // word that is neither lifecycle state instead of whatever the allocator
    // last left in those bytes.
    memset(frame, 0, ops->layout.size);
    return frame;
}

void rt_frame_release(const rt_value_ops* ops, void* frame) {
    if (ops == NULL || frame == NULL) {
        return;
    }
    if (frame_is_packed(ops, frame) != 0) {
        // Destroying the members runs the type's GENERATED drop, which must not
        // run while this lane holds a scheduler lock. The deferral is immediate
        // on a lane holding none, so compiled code and the completion path that
        // clears an abandoned frame reach one entry point rather than two.
        rt_release_owned_block_when_unlocked(ops, frame);
        return;
    }
    rt_free((uint8_t*)frame, (uint64_t)ops->layout.size, frame_align(ops));
}
