#include "rt_async_internal.h"

#include "rt_frame.h"

#include <stdint.h>
#include <stdio.h>
#include <string.h>

// The one suspension-frame release, asked the same question three times with a
// different answer written into the frame each time.
//
// A frame reaches its release from two sides — compiled code gives back a frame
// a poll has emptied, and the completion path gives back one a yield packed and
// abandoned — and until the frame carried a lifecycle word the CALLER had to
// say which of the two it was holding. What this stand drives is the other
// arrangement: one entry point, one descriptor, and the answer read out of the
// frame.
//
// The two wrong answers are not symmetric, which is why both are pinned.
// Walking a SPENT frame destroys members the resumed locals already own, so its
// wrong answer is a double free; skipping a PACKED one leaks them. The third
// row is a frame nothing has written yet: rt_frame_alloc zero-fills, and zero
// is neither state, so a release must not walk it.

// The process arguments rt_io.c reads. This stand supplies its own main and
// links without rt_entry.c, which is where they otherwise live.
int rt_argc = 0;
char** rt_argv_raw = NULL;

// A frame with one owned member, so a walk is observable as a number.
typedef struct {
    // The lifecycle word, at offset zero, exactly where every generated frame
    // puts it.
    void* frame_state;
    void* member;
} stand_frame;

static int stand_drops;

static void stand_drop_in_place(void* value) {
    stand_frame* frame = (stand_frame*)value;
    frame->member = NULL;
    stand_drops++;
}

static void stand_move_init(void* dst, void* src) {
    memcpy(dst, src, sizeof(stand_frame));
    memset(src, 0, sizeof(stand_frame));
}

static const rt_value_ops stand_frame_ops = {
    {sizeof(stand_frame), _Alignof(stand_frame), sizeof(stand_frame), RT_VALUE_FLAG_DROPPABLE},
    stand_move_init,
    NULL,
    NULL,
    stand_drop_in_place,
    NULL,
    NULL,
    NULL,
    NULL,
};

// The lifecycle field is a Surge `int`, so it crosses as a fixnum-tagged word.
// The stand writes it the way generated code does rather than as a raw integer,
// because a release that compared raw words would pass a test that wrote raw
// words and fail on every real frame.
static void* frame_state_word(int64_t state) {
    return (void*)(uintptr_t)(((uint64_t)state << 1) | UINT64_C(1));
}

// Reserves one frame, writes `state` unless `leave_fresh`, releases it, and
// answers how many times the members were destroyed.
static int drops_for(int64_t state, int leave_fresh) {
    stand_drops = 0;
    stand_frame* frame = (stand_frame*)rt_frame_alloc(&stand_frame_ops);
    if (frame == NULL) {
        return -1;
    }
    if (leave_fresh == 0) {
        frame->frame_state = frame_state_word(state);
    }
    frame->member = frame;
    rt_frame_release(&stand_frame_ops, frame);
    return stand_drops;
}

// A fresh frame must read back as zero in every byte, which is what makes "not
// a lifecycle word" a state the release can recognize.
static int fresh_is_zeroed(void) {
    stand_frame* frame = (stand_frame*)rt_frame_alloc(&stand_frame_ops);
    if (frame == NULL) {
        return 0;
    }
    int zeroed = frame->frame_state == NULL && frame->member == NULL;
    rt_frame_release(&stand_frame_ops, frame);
    return zeroed;
}

// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_poll_call(uint64_t id);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_call(uint64_t id, void* state);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_drop_result_call(uint64_t id, void* value);
// NOLINTNEXTLINE(bugprone-reserved-identifier,cert-dcl37-c,cert-dcl51-cpp)
void __surge_blocking_call(uint64_t id, void* state, void* out_dst);

// This stand never starts the scheduler: it drives the frame's two ends
// directly, so reaching any of these is a defect in the stand, not a result.
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
void __surge_blocking_call(uint64_t id, void* state, void* out_dst) {
    (void)id;
    (void)state;
    if (out_dst != NULL) {
        *(uint64_t*)out_dst = 0;
    }
}

int main(void) {
    printf("packed=%d spent=%d fresh=%d zeroed=%d\n",
           drops_for(SURGE_FRAME_STATE_PACKED, 0),
           drops_for(SURGE_FRAME_STATE_SPENT, 0),
           drops_for(0, 1),
           fresh_is_zeroed());
    return 0;
}
