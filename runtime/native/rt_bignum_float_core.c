#include "rt_bignum_internal.h"

#include <limits.h>
#include <string.h>

// BigFloat core arithmetic and normalization helpers.
static SurgeBigFloat* bf_alloc(bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    size_t size = sizeof(SurgeBigFloat);
    SurgeBigFloat* out = (SurgeBigFloat*)rt_alloc((uint64_t)size, (uint64_t)alignof(SurgeBigFloat));
    if (out == NULL) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    out->neg = 0;
    out->exp = 0;
    out->mant = NULL;
    return out;
}

bool bf_is_zero(const SurgeBigFloat* f) {
    if (f == NULL || f->mant == NULL) {
        return true;
    }
    return bu_is_zero(f->mant);
}

static SurgeBigFloat* bf_clone(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || f->mant == NULL) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = f->neg;
    out->exp = f->exp;
    out->mant = bu_clone(f->mant, err);
    if (out->mant == NULL) {
        return NULL;
    }
    return out;
}

int bf_cmp(const SurgeBigFloat* a, const SurgeBigFloat* b) {
    if (bf_is_zero(a) && bf_is_zero(b)) {
        return 0;
    }
    uint8_t a_neg = a ? a->neg : 0;
    uint8_t b_neg = b ? b->neg : 0;
    if (a_neg != b_neg) {
        return a_neg ? -1 : 1;
    }
    int32_t a_exp = a ? a->exp : 0;
    int32_t b_exp = b ? b->exp : 0;
    if (a_exp < b_exp) {
        return a_neg ? 1 : -1;
    }
    if (a_exp > b_exp) {
        return a_neg ? -1 : 1;
    }
    int cmp = bu_cmp(a ? a->mant : NULL, b ? b->mant : NULL);
    if (a_neg) {
        return -cmp;
    }
    return cmp;
}

SurgeBigFloat* bf_neg(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || bf_is_zero(f)) {
        return NULL;
    }
    SurgeBigFloat* out = bf_clone(f, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = out->neg ? 0 : 1;
    return out;
}

SurgeBigFloat* bf_abs(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || bf_is_zero(f)) {
        return NULL;
    }
    SurgeBigFloat* out = bf_clone(f, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = 0;
    return out;
}

static SurgeBigUint* bf_normalize_mantissa(const SurgeBigUint* m, int32_t* exp, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (m == NULL || m->len == 0) {
        if (exp != NULL) {
            *exp = 0;
        }
        return NULL;
    }
    uint32_t bl = bu_bitlen(m);
    if (bl == SURGE_BIGNUM_MANTISSA_BITS) {
        return bu_clone(m, err);
    }
    if (bl > SURGE_BIGNUM_MANTISSA_BITS) {
        int shift = (int)bl - SURGE_BIGNUM_MANTISSA_BITS;
        bn_err tmp_err = BN_OK;
        SurgeBigUint* rounded = bu_shift_right_round_even(m, shift, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        if (exp != NULL) {
            *exp += (int32_t)shift;
        }
        if (rounded != NULL && bu_bitlen(rounded) > SURGE_BIGNUM_MANTISSA_BITS) {
            rounded = bu_shift_right_round_even(rounded, 1, &tmp_err);
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                return NULL;
            }
            if (exp != NULL) {
                *exp += 1;
            }
        }
        return rounded;
    }
    int shift = SURGE_BIGNUM_MANTISSA_BITS - (int)bl;
    bn_err tmp_err = BN_OK;
    SurgeBigUint* shifted = bu_shl(m, shift, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (exp != NULL) {
        *exp -= (int32_t)shift;
    }
    return shifted;
}

SurgeBigFloat* bf_from_uint(const SurgeBigUint* u, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (u == NULL || u->len == 0) {
        return NULL;
    }
    int32_t exp = 0;
    bn_err tmp_err = BN_OK;
    SurgeBigUint* mant = bf_normalize_mantissa(u, &exp, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mant == NULL || mant->len == 0) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = 0;
    out->exp = exp;
    out->mant = mant;
    return out;
}

static int bf_floor_log2_ratio(const SurgeBigUint* num, const SurgeBigUint* den, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (num == NULL || den == NULL || num->len == 0 || den->len == 0) {
        if (err != NULL) {
            *err = BN_ERR_DIV_ZERO;
        }
        return 0;
    }
    if (bu_cmp(num, den) >= 0) {
        int d = (int)bu_bitlen(num) - (int)bu_bitlen(den);
        int e = d;
        bn_err tmp_err = BN_OK;
        SurgeBigUint* shifted = bu_shl(den, e, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return 0;
        }
        if (bu_cmp(num, shifted) < 0) {
            e--;
        } else {
            SurgeBigUint* shifted2 = bu_shl(den, e + 1, &tmp_err);
            if (tmp_err == BN_OK && bu_cmp(num, shifted2) >= 0) {
                e++;
            }
        }
        return e;
    }
    int d = (int)bu_bitlen(den) - (int)bu_bitlen(num);
    int s = d;
    bn_err tmp_err = BN_OK;
    SurgeBigUint* shifted = bu_shl(num, s, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return 0;
    }
    if (bu_cmp(shifted, den) < 0) {
        s++;
    }
    return -s;
}

// Build a normalized float from num/den with mantissa rounding.
SurgeBigFloat* bf_from_ratio(bool neg, const SurgeBigUint* num, const SurgeBigUint* den, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (num == NULL || num->len == 0) {
        return NULL;
    }
    if (den == NULL || den->len == 0) {
        if (err != NULL) {
            *err = BN_ERR_DIV_ZERO;
        }
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    int e0 = bf_floor_log2_ratio(num, den, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    int scale = (SURGE_BIGNUM_MANTISSA_BITS - 1) - e0;
    SurgeBigUint* scaled_num = (SurgeBigUint*)num;
    SurgeBigUint* scaled_den = (SurgeBigUint*)den;
    if (scale >= 0) {
        if (scale > INT_MAX) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        scaled_num = bu_shl(num, scale, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
    } else {
        if (-scale > INT_MAX) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        scaled_den = bu_shl(den, -scale, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
    }
    SurgeBigUint* rem = NULL;
    SurgeBigUint* q = bu_div_mod(scaled_num, scaled_den, &rem, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    q = bu_round_quotient_even(q, rem, scaled_den, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    int64_t exp64 = (int64_t)e0 - (int64_t)(SURGE_BIGNUM_MANTISSA_BITS - 1);
    if (exp64 < INT32_MIN || exp64 > INT32_MAX) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    int32_t exp = (int32_t)exp64;
    SurgeBigUint* mant = bf_normalize_mantissa(q, &exp, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mant == NULL || mant->len == 0) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = neg ? 1 : 0;
    out->exp = exp;
    out->mant = mant;
    return out;
}

SurgeBigFloat* bf_from_int(const SurgeBigInt* i, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (i == NULL || i->len == 0) {
        return NULL;
    }
    int32_t exp = 0;
    bn_err tmp_err = BN_OK;
    SurgeBigUint* mant = bf_normalize_mantissa(bi_as_uint(i), &exp, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mant == NULL || mant->len == 0) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = i->neg;
    out->exp = exp;
    out->mant = mant;
    return out;
}

SurgeBigFloat* bf_add(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (bf_is_zero(a)) {
        return bf_clone(b, err);
    }
    if (bf_is_zero(b)) {
        return bf_clone(a, err);
    }
    SurgeBigFloat lhs = *a;
    SurgeBigFloat rhs = *b;
    if (lhs.exp < rhs.exp) {
        SurgeBigFloat tmp = lhs;
        lhs = rhs;
        rhs = tmp;
    }
    int64_t delta = (int64_t)lhs.exp - (int64_t)rhs.exp;
    if (delta > (int64_t)INT_MAX) {
        return bf_clone(&lhs, err);
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* rhs_mant = bu_shift_right_round_even(rhs.mant, (int)delta, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (lhs.neg == rhs.neg) {
        SurgeBigUint* sum = bu_add(lhs.mant, rhs_mant, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        int32_t exp = lhs.exp;
        SurgeBigUint* mant = bf_normalize_mantissa(sum, &exp, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        if (mant == NULL || mant->len == 0) {
            return NULL;
        }
        SurgeBigFloat* out = bf_alloc(err);
        if (out == NULL) {
            return NULL;
        }
        out->neg = lhs.neg;
        out->exp = exp;
        out->mant = mant;
        return out;
    }
    int cmp = bu_cmp(lhs.mant, rhs_mant);
    if (cmp == 0) {
        return NULL;
    }
    if (cmp > 0) {
        SurgeBigUint* diff = bu_sub(lhs.mant, rhs_mant, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        int32_t exp = lhs.exp;
        SurgeBigUint* mant = bf_normalize_mantissa(diff, &exp, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        if (mant == NULL || mant->len == 0) {
            return NULL;
        }
        SurgeBigFloat* out = bf_alloc(err);
        if (out == NULL) {
            return NULL;
        }
        out->neg = lhs.neg;
        out->exp = exp;
        out->mant = mant;
        return out;
    }
    SurgeBigUint* diff = bu_sub(rhs_mant, lhs.mant, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    int32_t exp = lhs.exp;
    SurgeBigUint* mant = bf_normalize_mantissa(diff, &exp, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mant == NULL || mant->len == 0) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = rhs.neg;
    out->exp = exp;
    out->mant = mant;
    return out;
}

SurgeBigFloat* bf_sub(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
    bn_err tmp_err = BN_OK;
    SurgeBigFloat* neg_b = bf_neg(b, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    return bf_add(a, neg_b, err);
}

SurgeBigFloat* bf_mul(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (bf_is_zero(a) || bf_is_zero(b)) {
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* prod = bu_mul(a->mant, b->mant, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    int32_t exp = a->exp + b->exp;
    SurgeBigUint* mant = bf_normalize_mantissa(prod, &exp, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mant == NULL || mant->len == 0) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = (a->neg != b->neg) ? 1 : 0;
    out->exp = exp;
    out->mant = mant;
    return out;
}

SurgeBigFloat* bf_div(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
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
    SurgeBigUint* scaled = bu_shl(a->mant, SURGE_BIGNUM_MANTISSA_BITS, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* rem = NULL;
    SurgeBigUint* q = bu_div_mod(scaled, b->mant, &rem, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    q = bu_round_quotient_even(q, rem, b->mant, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    int32_t exp = a->exp - b->exp - SURGE_BIGNUM_MANTISSA_BITS;
    SurgeBigUint* mant = bf_normalize_mantissa(q, &exp, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mant == NULL || mant->len == 0) {
        return NULL;
    }
    SurgeBigFloat* out = bf_alloc(err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = (a->neg != b->neg) ? 1 : 0;
    out->exp = exp;
    out->mant = mant;
    return out;
}
