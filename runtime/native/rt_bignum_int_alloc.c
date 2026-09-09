#include "rt_bignum_internal.h"

#include <string.h>

// The bigint allocator and its duplicate, held here rather than beside the rest
// of the bigint arithmetic. rt_bignum_int.c is over the size limit and may not
// grow by even one line, and bi_alloc has to gain one: the block it hands back
// now carries a reference count and rt_alloc does not zero what it returns.
//
// bi_clone came with it because the exported lifecycle needs a duplicate of a
// bigint and open-coding a second copy loop there would be this function
// written twice. It is no longer file-static for the same reason.

// Signed integers are stored as sign + magnitude; the limb run is the magnitude.
SurgeBigInt* bi_alloc(uint32_t len, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (len == 0) {
        return NULL;
    }
    if (len > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    size_t size = sizeof(SurgeBigInt) + (size_t)len * sizeof(uint32_t);
    SurgeBigInt* out = (SurgeBigInt*)rt_alloc((uint64_t)size, (uint64_t)alignof(SurgeBigInt));
    if (out == NULL) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    out->len = len;
    out->neg = 0;
    // rt_alloc hands back uninitialised bytes, so a fresh block's count is
    // whatever the allocator left there until it is written. One is the count
    // of a block whose only reference is the one being returned.
    out->rc = 1;
    memset(out->limbs, 0, (size_t)len * sizeof(uint32_t));
    return out;
}

// A duplicate that shares nothing with its source, at count one -- bi_alloc set
// that, and the source's count is deliberately NOT copied: a copy is held by
// whoever asked for it and by nobody else.
SurgeBigInt* bi_clone(const SurgeBigInt* i, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (i == NULL || i->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(i->len, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = i->neg;
    memcpy(out->limbs, i->limbs, (size_t)i->len * sizeof(uint32_t));
    return out;
}
