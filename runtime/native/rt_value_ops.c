#include "rt_value_ops.h"

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
