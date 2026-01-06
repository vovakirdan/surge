#include "rt_bignum_internal.h"

#include <limits.h>
#include <string.h>

// BigFloat conversions, modulus, and ratio construction.
SurgeBigInt* bf_to_int_trunc(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || bf_is_zero(f)) {
        return NULL;
    }
    SurgeBigUint* mag = f->mant;
    bool mag_owned = false;
    bn_err tmp_err = BN_OK;
    if (f->exp > 0) {
        mag = bu_shl(mag, (int)f->exp, &tmp_err);
        mag_owned = true;
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            if (mag_owned) {
                bu_free(mag);
            }
            return NULL;
        }
    } else if (f->exp < 0) {
        int64_t shift = -(int64_t)f->exp;
        if (shift > (int64_t)INT_MAX) {
            return NULL;
        }
        mag = bu_shr(mag, (int)shift, &tmp_err);
        mag_owned = true;
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            if (mag_owned) {
                bu_free(mag);
            }
            return NULL;
        }
    }
    if (mag == NULL || mag->len == 0) {
        if (mag_owned) {
            bu_free(mag);
        }
        return NULL;
    }
    tmp_err = BN_OK;
    SurgeBigInt* out = bi_alloc(mag->len, &tmp_err);
    if (tmp_err != BN_OK || out == NULL) {
        if (err != NULL) {
            *err = tmp_err;
        }
        if (mag_owned) {
            bu_free(mag);
        }
        return NULL;
    }
    out->neg = f->neg;
    memcpy(out->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    out->len = mag->len;
    if (mag_owned) {
        bu_free(mag);
    }
    return out;
}

SurgeBigUint* bf_to_uint_trunc(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f != NULL && f->neg && !bf_is_zero(f)) {
        if (err != NULL) {
            *err = BN_ERR_UNDERFLOW;
        }
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* i = bf_to_int_trunc(f, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (i == NULL || i->len == 0) {
        bi_free(i);
        return NULL;
    }
    if (i->neg) {
        if (err != NULL) {
            *err = BN_ERR_UNDERFLOW;
        }
        bi_free(i);
        return NULL;
    }
    SurgeBigUint* out = bu_clone(bi_as_uint(i), err);
    bi_free(i);
    return out;
}

SurgeBigFloat* bf_mod(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (b == NULL || bf_is_zero(b)) {
        if (err != NULL) {
            *err = BN_ERR_DIV_ZERO;
        }
        return NULL;
    }
    if (a == NULL || bf_is_zero(a)) {
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigFloat* q = NULL;
    SurgeBigInt* qi = NULL;
    SurgeBigFloat* qf = NULL;
    SurgeBigFloat* prod = NULL;
    SurgeBigFloat* res = NULL;

    q = bf_div(a, b, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    qi = bf_to_int_trunc(q, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    qf = bf_from_int(qi, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    prod = bf_mul(qf, b, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
    res = bf_sub(a, prod, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        goto cleanup;
    }
cleanup:
    bf_free(q);
    bi_free(qi);
    bf_free(qf);
    bf_free(prod);
    if (tmp_err != BN_OK) {
        bf_free(res);
        return NULL;
    }
    return res;
}
