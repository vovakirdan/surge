#include "rt_bignum_internal.h"

#include <limits.h>
#include <string.h>

// Signed integers are stored as sign + magnitude. Bitwise ops use two's complement.
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
    memset(out->limbs, 0, (size_t)len * sizeof(uint32_t));
    return out;
}

static SurgeBigInt* bi_clone(const SurgeBigInt* i, bn_err* err) {
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

bool bi_is_zero(const SurgeBigInt* i) {
    if (i == NULL || i->len == 0) {
        return true;
    }
    return trim_len(i->limbs, i->len) == 0;
}

SurgeBigUint* bi_abs(const SurgeBigInt* i, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (i == NULL || i->len == 0) {
        return NULL;
    }
    uint32_t len = trim_len(i->limbs, i->len);
    if (len == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(len, err);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out->limbs, i->limbs, (size_t)len * sizeof(uint32_t));
    out->len = len;
    return out;
}

bool bi_to_i64(const SurgeBigInt* i, int64_t* out) {
    if (out != NULL) {
        *out = 0;
    }
    if (i == NULL || i->len == 0) {
        return true;
    }
    uint64_t mag = 0;
    if (!bu_limbs_to_u64(i->limbs, i->len, &mag)) {
        return false;
    }
    if (mag == 0) {
        return true;
    }
    if (!i->neg) {
        if (mag > (uint64_t)(INT64_MAX)) {
            return false;
        }
        if (out != NULL) {
            *out = (int64_t)mag;
        }
        return true;
    }
    if (mag > (uint64_t)INT64_MAX + 1u) {
        return false;
    }
    if (mag == (uint64_t)INT64_MAX + 1u) {
        if (out != NULL) {
            *out = INT64_MIN;
        }
        return true;
    }
    if (out != NULL) {
        *out = -(int64_t)mag;
    }
    return true;
}

SurgeBigInt* bi_from_i64(int64_t v) {
    if (v == 0) {
        return NULL;
    }
    uint64_t mag = 0;
    uint8_t neg = 0;
    if (v < 0) {
        neg = 1;
        mag = (uint64_t)(-(v + 1));
        mag++;
    } else {
        mag = (uint64_t)v;
    }
    SurgeBigUint* abs = bu_from_u64(mag);
    if (abs == NULL) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_alloc(abs->len, &err);
    if (out == NULL) {
        bu_free(abs);
        return NULL;
    }
    out->neg = neg;
    memcpy(out->limbs, abs->limbs, (size_t)abs->len * sizeof(uint32_t));
    out->len = abs->len;
    bu_free(abs);
    return out;
}

SurgeBigInt* bi_from_u64(uint64_t v) {
    if (v == 0) {
        return NULL;
    }
    SurgeBigUint* abs = bu_from_u64(v);
    if (abs == NULL) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_alloc(abs->len, &err);
    if (out == NULL) {
        bu_free(abs);
        return NULL;
    }
    out->neg = 0;
    memcpy(out->limbs, abs->limbs, (size_t)abs->len * sizeof(uint32_t));
    out->len = abs->len;
    bu_free(abs);
    return out;
}

int bi_cmp(const SurgeBigInt* a, const SurgeBigInt* b) {
    bool a_zero = bi_is_zero(a);
    bool b_zero = bi_is_zero(b);
    if (a_zero && b_zero) {
        return 0;
    }
    uint8_t a_neg = a ? a->neg : 0;
    uint8_t b_neg = b ? b->neg : 0;
    if (a_neg != b_neg) {
        return a_neg ? -1 : 1;
    }
    int cmp = bu_cmp(bi_as_uint(a), bi_as_uint(b));
    if (a_neg) {
        return -cmp;
    }
    return cmp;
}

SurgeBigInt* bi_neg(const SurgeBigInt* a, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (a == NULL || a->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_clone(a, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = out->neg ? 0 : 1;
    if (bi_is_zero(out)) {
        return NULL;
    }
    return out;
}

SurgeBigInt* bi_abs_val(const SurgeBigInt* a, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (a == NULL || a->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_clone(a, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = 0;
    if (bi_is_zero(out)) {
        return NULL;
    }
    return out;
}

SurgeBigInt* bi_add(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (a == NULL || a->len == 0) {
        return bi_clone(b, err);
    }
    if (b == NULL || b->len == 0) {
        return bi_clone(a, err);
    }
    if (a->neg == b->neg) {
        bn_err tmp_err = BN_OK;
        SurgeBigUint* sum = bu_add(bi_as_uint(a), bi_as_uint(b), &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            bu_free(sum);
            return NULL;
        }
        if (sum == NULL || sum->len == 0) {
            bu_free(sum);
            return NULL;
        }
        SurgeBigInt* out = bi_alloc(sum->len, err);
        if (out == NULL) {
            bu_free(sum);
            return NULL;
        }
        out->neg = a->neg;
        memcpy(out->limbs, sum->limbs, (size_t)sum->len * sizeof(uint32_t));
        out->len = sum->len;
        bu_free(sum);
        return out;
    }

    int cmp = bu_cmp(bi_as_uint(a), bi_as_uint(b));
    if (cmp == 0) {
        return NULL;
    }
    if (cmp > 0) {
        bn_err tmp_err = BN_OK;
        SurgeBigUint* diff = bu_sub(bi_as_uint(a), bi_as_uint(b), &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            bu_free(diff);
            return NULL;
        }
        if (diff == NULL || diff->len == 0) {
            bu_free(diff);
            return NULL;
        }
        SurgeBigInt* out = bi_alloc(diff->len, err);
        if (out == NULL) {
            bu_free(diff);
            return NULL;
        }
        out->neg = a->neg;
        memcpy(out->limbs, diff->limbs, (size_t)diff->len * sizeof(uint32_t));
        out->len = diff->len;
        bu_free(diff);
        return out;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* diff = bu_sub(bi_as_uint(b), bi_as_uint(a), &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(diff);
        return NULL;
    }
    if (diff == NULL || diff->len == 0) {
        bu_free(diff);
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(diff->len, err);
    if (out == NULL) {
        bu_free(diff);
        return NULL;
    }
    out->neg = b->neg;
    memcpy(out->limbs, diff->limbs, (size_t)diff->len * sizeof(uint32_t));
    out->len = diff->len;
    bu_free(diff);
    return out;
}

SurgeBigInt* bi_sub(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
    if (b == NULL || b->len == 0) {
        return bi_clone(a, err);
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* neg = bi_neg(b, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigInt* res = bi_add(a, neg, err);
    bi_free(neg);
    return res;
}

SurgeBigInt* bi_mul(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (a == NULL || b == NULL) {
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* prod = bu_mul(bi_as_uint(a), bi_as_uint(b), &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(prod);
        return NULL;
    }
    if (prod == NULL || prod->len == 0) {
        bu_free(prod);
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(prod->len, err);
    if (out == NULL) {
        bu_free(prod);
        return NULL;
    }
    out->neg = (a->neg != b->neg) ? 1 : 0;
    memcpy(out->limbs, prod->limbs, (size_t)prod->len * sizeof(uint32_t));
    out->len = prod->len;
    bu_free(prod);
    return out;
}

SurgeBigInt*
bi_div_mod(const SurgeBigInt* a, const SurgeBigInt* b, SurgeBigInt** out_rem, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (out_rem != NULL) {
        *out_rem = NULL;
    }
    if (b == NULL || b->len == 0) {
        if (err != NULL) {
            *err = BN_ERR_DIV_ZERO;
        }
        return NULL;
    }
    if (a == NULL || a->len == 0) {
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* rem_u = NULL;
    SurgeBigUint* q_u = bu_div_mod(bi_as_uint(a), bi_as_uint(b), &rem_u, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(q_u);
        bu_free(rem_u);
        return NULL;
    }
    SurgeBigInt* q = NULL;
    if (q_u != NULL && q_u->len > 0) {
        q = bi_alloc(q_u->len, err);
        if (q == NULL) {
            bu_free(q_u);
            bu_free(rem_u);
            return NULL;
        }
        q->neg = (a->neg != b->neg) ? 1 : 0;
        memcpy(q->limbs, q_u->limbs, (size_t)q_u->len * sizeof(uint32_t));
        q->len = q_u->len;
    }
    if (out_rem != NULL) {
        if (rem_u != NULL && rem_u->len > 0) {
            SurgeBigInt* r = bi_alloc(rem_u->len, err);
            if (r == NULL) {
                bu_free(q_u);
                bu_free(rem_u);
                bi_free(q);
                return NULL;
            }
            r->neg = a->neg;
            memcpy(r->limbs, rem_u->limbs, (size_t)rem_u->len * sizeof(uint32_t));
            r->len = rem_u->len;
            *out_rem = r;
        }
    }
    bu_free(q_u);
    bu_free(rem_u);
    return q;
}

static SurgeBigUint*
bi_twos_complement(const SurgeBigUint* mag, bool neg, const SurgeBigUint* pow2, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (!neg || mag == NULL || mag->len == 0) {
        return bu_clone(mag, err);
    }
    return bu_sub(pow2, mag, err);
}

// Bitwise ops are defined via two's-complement over a fixed width.
SurgeBigInt* bi_bit_op(const SurgeBigInt* a,
                       const SurgeBigInt* b,
                       SurgeBigUint* (*op)(const SurgeBigUint*, const SurgeBigUint*),
                       bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if ((a == NULL || a->len == 0) && (b == NULL || b->len == 0)) {
        return NULL;
    }
    uint32_t width = 0;
    uint32_t a_bits = bu_bitlen(bi_as_uint(a));
    uint32_t b_bits = bu_bitlen(bi_as_uint(b));
    width = a_bits > b_bits ? a_bits : b_bits;
    width += 1;
    bn_err tmp_err = BN_OK;
    SurgeBigUint* one = bu_from_u64(1);
    if (one == NULL) {
        return NULL;
    }
    SurgeBigUint* pow2 = bu_shl(one, (int)width, &tmp_err);
    bu_free(one);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        return NULL;
    }
    if (pow2 == NULL || pow2->len == 0) {
        bu_free(pow2);
        return NULL;
    }
    SurgeBigUint* rep_a = bi_twos_complement(bi_as_uint(a), a != NULL && a->neg, pow2, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        bu_free(rep_a);
        return NULL;
    }
    SurgeBigUint* rep_b = bi_twos_complement(bi_as_uint(b), b != NULL && b->neg, pow2, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        bu_free(rep_a);
        bu_free(rep_b);
        return NULL;
    }
    SurgeBigUint* res = op(rep_a, rep_b);
    if (res == NULL || res->len == 0) {
        bu_free(pow2);
        bu_free(rep_a);
        bu_free(rep_b);
        bu_free(res);
        return NULL;
    }
    if (!bu_bit_set(res, (int)width - 1)) {
        SurgeBigInt* out = bi_alloc(res->len, err);
        if (out == NULL) {
            bu_free(pow2);
            bu_free(rep_a);
            bu_free(rep_b);
            bu_free(res);
            return NULL;
        }
        out->neg = 0;
        memcpy(out->limbs, res->limbs, (size_t)res->len * sizeof(uint32_t));
        out->len = res->len;
        bu_free(pow2);
        bu_free(rep_a);
        bu_free(rep_b);
        bu_free(res);
        return out;
    }
    SurgeBigUint* mag = bu_sub(pow2, res, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        bu_free(rep_a);
        bu_free(rep_b);
        bu_free(res);
        bu_free(mag);
        return NULL;
    }
    if (mag == NULL || mag->len == 0) {
        bu_free(pow2);
        bu_free(rep_a);
        bu_free(rep_b);
        bu_free(res);
        bu_free(mag);
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(mag->len, err);
    if (out == NULL) {
        bu_free(pow2);
        bu_free(rep_a);
        bu_free(rep_b);
        bu_free(res);
        bu_free(mag);
        return NULL;
    }
    out->neg = 1;
    memcpy(out->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    out->len = mag->len;
    bu_free(pow2);
    bu_free(rep_a);
    bu_free(rep_b);
    bu_free(res);
    bu_free(mag);
    return out;
}

static bool shift_count_from_bigint(const SurgeBigInt* i, int* out) {
    if (out != NULL) {
        *out = 0;
    }
    if (i != NULL && i->neg && !bi_is_zero(i)) {
        return false;
    }
    return shift_count_from_biguint(bi_as_uint(i), out);
}

SurgeBigInt* bi_shl(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    int shift = 0;
    if (!shift_count_from_bigint(b, &shift)) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    if (shift == 0 || a == NULL || a->len == 0) {
        return bi_clone(a, err);
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* shifted = bu_shl(bi_as_uint(a), shift, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (shifted == NULL || shifted->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(shifted->len, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = a->neg;
    memcpy(out->limbs, shifted->limbs, (size_t)shifted->len * sizeof(uint32_t));
    out->len = shifted->len;
    return out;
}

SurgeBigInt* bi_shr(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    int shift = 0;
    if (!shift_count_from_bigint(b, &shift)) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    if (shift == 0 || a == NULL || a->len == 0) {
        return bi_clone(a, err);
    }
    bn_err tmp_err = BN_OK;
    if (!a->neg) {
        SurgeBigUint* shifted = bu_shr(bi_as_uint(a), shift, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            bu_free(shifted);
            return NULL;
        }
        if (shifted == NULL || shifted->len == 0) {
            bu_free(shifted);
            return NULL;
        }
        SurgeBigInt* out = bi_alloc(shifted->len, err);
        if (out == NULL) {
            bu_free(shifted);
            return NULL;
        }
        out->neg = 0;
        memcpy(out->limbs, shifted->limbs, (size_t)shifted->len * sizeof(uint32_t));
        out->len = shifted->len;
        bu_free(shifted);
        return out;
    }
    SurgeBigUint* one = bu_from_u64(1);
    if (one == NULL) {
        return NULL;
    }
    SurgeBigUint* pow2 = bu_shl(one, shift, &tmp_err);
    bu_free(one);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        return NULL;
    }
    SurgeBigUint* one_again = bu_from_u64(1);
    if (one_again == NULL) {
        bu_free(pow2);
        return NULL;
    }
    SurgeBigUint* pow2m1 = bu_sub(pow2, one_again, &tmp_err);
    bu_free(one_again);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        bu_free(pow2m1);
        return NULL;
    }
    SurgeBigUint* sum = bu_add(bi_as_uint(a), pow2m1, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(pow2);
        bu_free(pow2m1);
        bu_free(sum);
        return NULL;
    }
    bu_free(pow2);
    bu_free(pow2m1);
    SurgeBigUint* shifted = bu_shr(sum, shift, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        bu_free(sum);
        bu_free(shifted);
        return NULL;
    }
    bu_free(sum);
    if (shifted == NULL || shifted->len == 0) {
        bu_free(shifted);
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(shifted->len, err);
    if (out == NULL) {
        bu_free(shifted);
        return NULL;
    }
    out->neg = 1;
    memcpy(out->limbs, shifted->limbs, (size_t)shifted->len * sizeof(uint32_t));
    out->len = shifted->len;
    bu_free(shifted);
    return out;
}
