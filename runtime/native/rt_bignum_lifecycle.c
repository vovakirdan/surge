#include "rt_bignum_internal.h"
#include "rt_resident_bytes.h"

// The exported lifecycle of the heap halves of `int` and `uint`, and the leaf
// of the crossing barrier for both. The contracts are on the declarations in
// rt.h; the bodies are here, on their own, because rt_bignum_api.c -- where the
// bigfloat twins of these functions live -- is a legacy-size file the size gate
// forbids growing.
//
// Two shapes carry over from the bigfloat originals unchanged. Each entry point
// is a thin wrapper over the `static inline` bodies in rt_bignum_internal.h
// rather than an export of them, so bi_free/bu_free stay internal and keep
// their existing callers; and free is unconditional while release is the
// ownership operation, which is the distinction rt.h spells out.
//
// One shape is new, and it is why none of these could be the bigfloat bodies
// with a different type name. An `int` or `uint` reaches the runtime as a
// tagged word: low bit set means the value rode inline and there is no block at
// all, low bit clear means a pointer or NULL. So every entry point asks
// fix_is_heap FIRST, before any load. The bigfloat form tests only for NULL,
// because a float is always a pointer or nothing; that same test applied here
// would read -- and retain, release or free -- through an inline word that is
// neither aligned nor mapped.

// Rule 13: the tag guard's negative control replaces it with exactly the
// NULL-only test the bigfloat entry points use. That is the guard this pair
// would have inherited had nobody noticed the tag, so a row run under it shows
// what asking the tag first is worth: an inline word stops being answered and
// starts being dereferenced. It cuts ALL EIGHT lifecycle entry points and both
// barrier leaves, so no guard here is out of the control's reach.
#ifdef RV2_BIGNUM_TAG_GUARD_NEGATIVE_CONTROL
#define BIGNUM_OWNS_A_BLOCK(w) ((w) != NULL)
#else
#define BIGNUM_OWNS_A_BLOCK(w) fix_is_heap(w)
#endif

// ---- bigint -------------------------------------------------------------

void* rt_bigint_clone(const void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        // An inline fixnum is already its own copy and NULL is the canonical
        // zero. Neither owns a block, so the word IS the duplicate.
        return (void*)(uintptr_t)a;
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_clone((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    // No demotion back to an inline fixnum: a clone is a block, so that the
    // caller of a deep copy gets a leaf it can hold, retain and release.
    return (void*)out;
}

void rt_bigint_free(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return;
    }
    bi_free((SurgeBigInt*)a);
}

void rt_bigint_retain(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return;
    }
    SurgeBigInt* i = (SurgeBigInt*)a;
    if (i->rc == UINT32_MAX) {
        // Saturating at the ceiling would leak the block instead of freeing it
        // early; a program reaching 2^32 live references to one int has a
        // defect worth naming rather than papering over.
        bignum_panic("bigint reference count overflow");
    }
    i->rc++;
}

void rt_bigint_release(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return;
    }
    SurgeBigInt* i = (SurgeBigInt*)a;
    if (i->rc == 0) {
        bignum_panic("bigint release below zero");
    }
    i->rc--;
#ifndef RV2_BIGNUM_RELEASE_FREE_NEGATIVE_CONTROL
    if (i->rc == 0) {
        bi_free(i);
    }
#endif
}

// The crossing barrier's leaf for an int. See rt_bigfloat_unshare.c for the
// same four arms on a float, and rt.h for what the caller must already be true
// of when it calls this.
void* rt_bigint_unshare(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return a; // An inline word owns nothing and is already private.
    }
    SurgeBigInt* i = (SurgeBigInt*)a;
    if (i->rc == 1) {
        // The caller holds the only reference, so relinquishing the value
        // relinquishes the block with it. Cloning here would allocate a
        // duplicate and free the original for nothing.
        return a;
    }
    if (i->rc == 0) {
        bignum_panic("bigint unshare below zero");
    }
#ifdef RV2_BIGINT_UNSHARE_NEGATIVE_CONTROL
    return a;
#else
    // Somebody else on this shard still holds the block. The caller gets a
    // duplicate at count one and gives up the reference it held, which is what
    // leaves each side with a block of its own -- the crossing's precondition,
    // since the count is not atomic and the two sides are about to be on
    // different threads.
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_clone(i, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    i->rc--;
    rt_resident_bytes_record_unshare_clone();
    return (void*)out;
#endif
}

// ---- biguint ------------------------------------------------------------

void* rt_biguint_clone(const void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return (void*)(uintptr_t)a;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_clone((const SurgeBigUint*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void rt_biguint_free(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return;
    }
    bu_free((SurgeBigUint*)a);
}

void rt_biguint_retain(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return;
    }
    SurgeBigUint* u = (SurgeBigUint*)a;
    if (u->rc == UINT32_MAX) {
        bignum_panic("biguint reference count overflow");
    }
    u->rc++;
}

void rt_biguint_release(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return;
    }
    SurgeBigUint* u = (SurgeBigUint*)a;
    if (u->rc == 0) {
        bignum_panic("biguint release below zero");
    }
    u->rc--;
#ifndef RV2_BIGNUM_RELEASE_FREE_NEGATIVE_CONTROL
    if (u->rc == 0) {
        bu_free(u);
    }
#endif
}

void* rt_biguint_unshare(void* a) {
    if (!BIGNUM_OWNS_A_BLOCK(a)) {
        return a;
    }
    SurgeBigUint* u = (SurgeBigUint*)a;
    if (u->rc == 1) {
        return a;
    }
    if (u->rc == 0) {
        bignum_panic("biguint unshare below zero");
    }
#ifdef RV2_BIGUINT_UNSHARE_NEGATIVE_CONTROL
    return a;
#else
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_clone(u, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    u->rc--;
    rt_resident_bytes_record_unshare_clone();
    return (void*)out;
#endif
}
