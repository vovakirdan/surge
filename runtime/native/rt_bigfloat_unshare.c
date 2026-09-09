#include "rt_bignum_internal.h"
#include "rt_resident_bytes.h"

// The leaf of the crossing barrier: a bigfloat reference made PRIVATE by the
// frame that is about to give it up across a shard or thread boundary. The
// contract is on the declaration in rt.h; this file holds the body on its own
// because rt_bignum_api.c is a legacy-size file the size gate forbids growing.
//
// Two things a reader of a program's trace needs to know about this body:
//
//   - the clone branch writes the `unshare_clones` field on the TRACE_RESIDENT
//     line, as the int and uint leaves in rt_bignum_lifecycle.c do for their
//     kinds, so a row that reads that field is counting the blocks the barrier
//     had to duplicate summed over every counted scalar kind the crossing
//     carried -- for a float-only crossing that is this branch and nothing
//     else;
//   - Rule 13: RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL cuts the clone branch out,
//     so a shared block travels shared and that same field reads 0 where the
//     row expects 1. The count-below-zero check stays in both configurations,
//     as it is an invariant and not the behaviour under test.
void* rt_bigfloat_unshare(void* a) {
    if (a == NULL) {
        return NULL; // NULL is the zero float: no block, nothing shared.
    }
    SurgeBigFloat* f = (SurgeBigFloat*)a;
    if (f->rc == 1) {
        // The caller holds the only reference, so relinquishing the value
        // relinquishes the block with it. Cloning here would allocate a
        // duplicate and free the original for nothing.
        return a;
    }
    if (f->rc == 0) {
        bignum_panic("bigfloat unshare below zero");
    }
#ifdef RV2_BIGFLOAT_UNSHARE_NEGATIVE_CONTROL
    return a;
#else
    // Somebody else on this shard still holds the block. The caller gets a
    // duplicate at count one and gives up the reference it held, which is what
    // leaves each side with a block of its own -- the crossing's precondition,
    // since the count is not atomic and the two sides are about to be on
    // different threads.
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_clone(f, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    f->rc--;
    rt_resident_bytes_record_unshare_clone();
    return (void*)out;
#endif
}
