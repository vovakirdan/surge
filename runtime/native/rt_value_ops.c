#include "rt_value_ops.h"

#include "rt_carrier_bench.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// rt_value_copy_init_unbound_trap is an aborting trap, not a copy.
//
// Its job in the copy_init slot is to be a named non-null value, so that a
// descriptor which sets RT_VALUE_FLAG_COPY without a per-type copy still
// satisfies the runtime's flag/callback biconditional, and so that the slot's
// identity says "the width of this copy is rt_value_layout.size".
//
// It is never dispatched. The frozen rt_value_copy_init_fn signature is
// (void* dst, const void* src): there is no width in it, and the width of an
// ordinary copy is the descriptor's rt_value_layout.size. The byte copy is
// therefore performed by rt_value_copy_init below, which still holds the
// descriptor and branches away from this symbol. Reaching this body means
// somebody dispatched `operations->copy_init(dst, src)` directly.
//
// It fails closed instead of copying zero bytes. A silent no-op would leave the
// destination uninitialized while its owner published the slot as INITIALIZED,
// which is a use of uninitialized storage that no later check can see.
//
// It is not marked _Noreturn: the generated ABI header declares this symbol from
// the frozen manifest, which has no spelling for the specifier, and the
// definition matches that declaration exactly.
void rt_value_copy_init_unbound_trap(void* dst, const void* src) {
    (void)dst;
    (void)src;
    fprintf(stderr,
            "rt_value_ops: rt_value_copy_init_unbound_trap was dispatched through "
            "rt_value_ops.copy_init without its descriptor; the byte copy needs "
            "rt_value_layout.size, so copy through rt_value_copy_init(operations, dst, src)\n");
    abort();
}

// _Noreturn is what makes the refusals above real control flow: without it,
// every rt_value_copy_init check formally falls through into the next
// dereference, and a static analyzer reads a null dereference on paper.
static _Noreturn void rt_value_copy_refuse(const char* reason) {
    fprintf(stderr, "rt_value_ops: rt_value_copy_init %s\n", reason);
    abort();
}

int rt_value_copy_uses_runtime_width(const rt_value_ops* operations) {
    return operations != NULL && (operations->layout.flags & RT_VALUE_FLAG_COPY) != 0 &&
           operations->copy_init == rt_value_copy_init_unbound_trap;
}

void rt_value_copy_init(const rt_value_ops* operations, void* dst, const void* src) {
    if (operations == NULL) {
        rt_value_copy_refuse("needs a descriptor");
    }
    if ((operations->layout.flags & RT_VALUE_FLAG_COPY) == 0) {
        rt_value_copy_refuse("needs a descriptor whose RT_VALUE_FLAG_COPY is set");
    }
    if (operations->copy_init == NULL) {
        rt_value_copy_refuse("needs a descriptor whose copy_init slot is bound");
    }
    if (dst == NULL || src == NULL) {
        rt_value_copy_refuse("needs non-null destination and source storage");
    }
    if (operations->copy_init != rt_value_copy_init_unbound_trap) {
        // A backend that emitted its own copy for this exact type. The slot is a
        // real function pointer and is dispatched as one.
        operations->copy_init(dst, src);
        return;
    }
    // The trap-bound case: this helper is the width. A zero-sized Copy value
    // takes the same path, because memcpy of nothing between two non-null
    // pointers is exactly what a zero-sized value's initialization is.
    memcpy(dst, src, operations->layout.size);
}

// The lane predicates are declared WEAK rather than included, because this file
// is the descriptor layer and the lane record belongs to the executor. Pulling
// rt_async_internal.h in here would make every link that wants a typed copy
// drag the scheduler with it, and the slot-control harness — which links the
// descriptor layer alone — would stop linking at all.
//
// An absent symbol is not a skipped check. It means no executor is linked into
// this program, so there is no owner lock for a callback to run under and the
// property is vacuous rather than unverified. A PRESENT symbol answering
// "locked" is the violation, and it aborts.
extern int rt_lane_holds_control(void) __attribute__((weak));
extern int rt_lane_holds_any_shard(void) __attribute__((weak));

// The lane refusal the header promises, shared by both detached helpers.
//
// It is fail-CLOSED where the question can be asked at all. A generated
// callback invoked under the owner lock can call back into the runtime,
// allocate, or run user code, and the resulting deadlock or reentry surfaces
// far from here — so the refusal happens at the dispatch rather than being left
// to a reviewer to notice.
static void rt_value_refuse_if_locked(const char* operation) {
    int control = rt_lane_holds_control != NULL && rt_lane_holds_control();
    int shard = rt_lane_holds_any_shard != NULL && rt_lane_holds_any_shard();
    if (control || shard) {
        fprintf(stderr,
                "rt_value_ops: %s was dispatched while a runtime lock is held; every "
                "generated operation runs detached from the owner lock\n",
                operation);
        abort();
    }
}

static _Noreturn void rt_value_dispatch_refuse(const char* operation, const char* reason) {
    fprintf(stderr, "rt_value_ops: %s %s\n", operation, reason);
    abort();
}

void rt_value_move_init_detached(const rt_value_ops* operations, void* dst, void* src) {
    if (operations == NULL) {
        rt_value_dispatch_refuse("rt_value_move_init_detached", "needs a descriptor");
    }
    if (operations->move_init == NULL) {
        rt_value_dispatch_refuse("rt_value_move_init_detached",
                                 "needs a descriptor whose move_init slot is bound; the "
                                 "manifest declares it always present");
    }
    if (dst == NULL || src == NULL) {
        rt_value_dispatch_refuse("rt_value_move_init_detached",
                                 "needs non-null destination and source storage");
    }
    rt_value_refuse_if_locked("rt_value_move_init_detached");
    // The physical move, at the descriptor's width: what the carrier bench
    // counts as bytes moved, wherever the move is dispatched from.
    rt_carrier_bench_record_move(operations->layout.size);
    operations->move_init(dst, src);
}

void rt_value_clone_init_detached(const rt_value_ops* operations, void* dst, const void* src) {
    if (operations == NULL) {
        rt_value_dispatch_refuse("rt_value_clone_init_detached", "needs a descriptor");
    }
    if ((operations->layout.flags & RT_VALUE_FLAG_CLONABLE) == 0 ||
        operations->clone_init == NULL) {
        rt_value_dispatch_refuse("rt_value_clone_init_detached",
                                 "needs a descriptor that carries a clone; the flag and the "
                                 "callback are one statement in the manifest");
    }
    if (dst == NULL || src == NULL) {
        rt_value_dispatch_refuse("rt_value_clone_init_detached",
                                 "needs non-null destination and source storage");
    }
    rt_value_refuse_if_locked("rt_value_clone_init_detached");
    operations->clone_init(dst, src);
}

void rt_value_duplicate_detached(rt_value_clone_init_fn duplicate, void* dst, const void* src) {
    if (duplicate == NULL) {
        rt_value_dispatch_refuse("rt_value_duplicate_detached", "needs a duplication to dispatch");
    }
    if (dst == NULL || src == NULL) {
        rt_value_dispatch_refuse("rt_value_duplicate_detached",
                                 "needs non-null destination and source storage");
    }
    rt_value_refuse_if_locked("rt_value_duplicate_detached");
    duplicate(dst, src);
}

void rt_value_drop_in_place_detached(const rt_value_ops* operations, void* value) {
    if (operations == NULL) {
        rt_value_dispatch_refuse("rt_value_drop_in_place_detached", "needs a descriptor");
    }
    if ((operations->layout.flags & RT_VALUE_FLAG_DROPPABLE) == 0) {
        // No obligation exists. The biconditional makes this the same statement
        // as "drop_in_place is null", so there is nothing to dispatch and
        // nothing to refuse.
        return;
    }
    if (operations->drop_in_place == NULL) {
        rt_value_dispatch_refuse("rt_value_drop_in_place_detached",
                                 "has RT_VALUE_FLAG_DROPPABLE set with an unbound "
                                 "drop_in_place slot, so the obligation names no destructor");
    }
    if (value == NULL) {
        rt_value_dispatch_refuse("rt_value_drop_in_place_detached", "needs non-null storage");
    }
    rt_value_refuse_if_locked("rt_value_drop_in_place_detached");
    operations->drop_in_place(value);
}

rt_carrier_status rt_value_cross_move_init_detached(const rt_value_ops* operations,
                                                    void* dst,
                                                    void* src,
                                                    const rt_cross_plan* plan,
                                                    rt_cross_allocator* allocator) {
    if (operations == NULL) {
        rt_value_dispatch_refuse("rt_value_cross_move_init_detached", "needs a descriptor");
    }
    if ((operations->layout.flags & RT_VALUE_FLAG_SHARD_MOVABLE) == 0 ||
        operations->cross_move_init == NULL) {
        rt_value_dispatch_refuse("rt_value_cross_move_init_detached",
                                 "needs a descriptor that admits a shard move; the flag and the "
                                 "callback are one statement in the manifest");
    }
    if (dst == NULL || src == NULL || plan == NULL || allocator == NULL) {
        rt_value_dispatch_refuse("rt_value_cross_move_init_detached",
                                 "needs non-null destination, source, plan and allocator");
    }
    rt_value_refuse_if_locked("rt_value_cross_move_init_detached");
    return operations->cross_move_init(dst, src, plan, allocator);
}

rt_carrier_status rt_value_cross_clone_init_detached(const rt_value_ops* operations,
                                                     void* dst,
                                                     const void* src,
                                                     const rt_cross_plan* plan,
                                                     rt_cross_allocator* allocator) {
    if (operations == NULL) {
        rt_value_dispatch_refuse("rt_value_cross_clone_init_detached", "needs a descriptor");
    }
    if ((operations->layout.flags & RT_VALUE_FLAG_CROSS_CLONABLE) == 0 ||
        operations->cross_clone_init == NULL) {
        rt_value_dispatch_refuse("rt_value_cross_clone_init_detached",
                                 "needs a descriptor that admits a crossing clone; the flag and "
                                 "the callback are one statement in the manifest");
    }
    if (dst == NULL || src == NULL || plan == NULL || allocator == NULL) {
        rt_value_dispatch_refuse("rt_value_cross_clone_init_detached",
                                 "needs non-null destination, source, plan and allocator");
    }
    rt_value_refuse_if_locked("rt_value_cross_clone_init_detached");
    return operations->cross_clone_init(dst, src, plan, allocator);
}

static void rt_value_opaque_word_move(void* destination, void* source) {
    memcpy(destination, source, sizeof(uint64_t));
    *(uint64_t*)source = 0;
}

static rt_carrier_status
rt_value_opaque_word_plan(const void* source, rt_cross_mode mode, rt_cross_plan* out) {
    (void)source;
    (void)mode;
    (void)out;
    return RT_CARRIER_STATUS_INVALID_STATE;
}

// The descriptor for a carrier whose element is an opaque machine word.
//
// It lives HERE rather than beside the channel for the same reason this file is
// the one place exempt from the slot-dispatch scan: naming a slot is how a
// descriptor is BUILT, and a scan that cannot tell construction from dispatch
// would have to be weakened to allow it. Keeping every hand-written descriptor
// in the descriptor file keeps that scan strict everywhere else.
//
// This is the FALLBACK a far channel holds when no descriptor is available, not
// what a far channel holds in general: rt_far_channel_crossing.c resolves the
// element descriptor from the payload type id that crossed with the message
// ("the payload type arrived as a number ... the descriptor is looked up on this
// side"), and only an absent lookup or a zero id lands here. It is also what a C
// stand builds when it exercises the scheduler with no compiled Surge code to
// supply descriptors. This paragraph used to say a crossed payload arrives as a
// word because crossing has no descriptor support; the typed-carrier cutover
// (Epic 23b) made that false, and it was corrected 2026-09-04.
//
// It claims no capability beyond the two the ABI makes mandatory: the move is a
// word copy that empties its source, and plan_cross refuses. Nothing here can
// be mistaken for an element that owns something.
//
// KNOWN DEVIATION, recorded rather than papered over: the storage model says a
// plan_cross for a type admitting neither cross mode must NOT RETURN, because a
// status would assert the call was legal and merely declined (section 5). This
// one returns RT_CARRIER_STATUS_INVALID_STATE. It is unreachable today -- no
// caller dispatches plan_cross anywhere in the runtime -- and it is fixed with
// the dispatcher that gives it its first caller. RV2-DEBT-038.
const rt_value_ops* rt_channel_opaque_word_ops(void) {
    static const rt_value_ops ops = {
        .layout = {.size = sizeof(uint64_t),
                   .align = _Alignof(uint64_t),
                   .stride = sizeof(uint64_t),
                   .flags = 0},
        .move_init = rt_value_opaque_word_move,
        .copy_init = NULL,
        .clone_init = NULL,
        .drop_in_place = NULL,
        .trace = NULL,
        .plan_cross = rt_value_opaque_word_plan,
        .cross_move_init = NULL,
        .cross_clone_init = NULL,
    };
    return &ops;
}
