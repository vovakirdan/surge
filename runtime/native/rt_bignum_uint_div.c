#include "rt_bignum_internal.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

// BigUint division, rounding helpers, and power/bit-extraction utilities.
SurgeBigUint* bu_div_mod_small(const SurgeBigUint* u, uint32_t d, uint32_t* rem, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (rem != NULL) {
        *rem = 0;
    }
    if (d == 0) {
        if (err != NULL) {
            *err = BN_ERR_DIV_ZERO;
        }
        return NULL;
    }
    if (u == NULL || u->len == 0) {
        return NULL;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(len, err);
    if (out == NULL) {
        return NULL;
    }
    uint64_t r = 0;
    for (uint32_t i = len; i-- > 0;) {
        uint64_t cur = (r << 32) | (uint64_t)u->limbs[i];
        out->limbs[i] = (uint32_t)(cur / d);
        r = cur % d;
        if (i == 0) {
            break;
        }
    }
    out->len = trim_len(out->limbs, out->len);
    if (rem != NULL) {
        *rem = (uint32_t)r;
    }
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

static void bu_shr1_in_place(uint32_t* limbs, uint32_t len) {
    uint32_t carry = 0;
    for (uint32_t i = len; i-- > 0;) {
        uint32_t v = limbs[i];
        limbs[i] = (v >> 1) | (carry << 31);
        carry = v & 1u;
        if (i == 0) {
            break;
        }
    }
}

SurgeBigUint* bu_shl(const SurgeBigUint* u, int bits, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (bits < 0) {
        if (err != NULL) {
            *err = BN_ERR_NEG_SHIFT;
        }
        return NULL;
    }
    if (u == NULL || u->len == 0 || bits == 0) {
        return bu_clone(u, err);
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return NULL;
    }
    uint32_t word_shift = (uint32_t)(bits / 32);
    uint32_t bit_shift = (uint32_t)(bits % 32);
    uint32_t out_len = len + word_shift + 1;
    SurgeBigUint* out = bu_alloc(out_len, err);
    if (out == NULL) {
        return NULL;
    }
    memset(out->limbs, 0, (size_t)out->len * sizeof(uint32_t));
    if (bit_shift == 0) {
        memcpy(out->limbs + word_shift, u->limbs, (size_t)len * sizeof(uint32_t));
        out->len = trim_len(out->limbs, out->len);
        if (out->len > SURGE_BIGNUM_MAX_LIMBS) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            bu_free(out);
            return NULL;
        }
        if (out->len == 0) {
            bu_free(out);
            return NULL;
        }
        return out;
    }
    uint32_t carry = 0;
    for (uint32_t i = 0; i < len; i++) {
        uint32_t v = u->limbs[i];
        out->limbs[i + word_shift] = (v << bit_shift) | carry;
        carry = v >> (32 - bit_shift);
    }
    out->limbs[len + word_shift] = carry;
    out->len = trim_len(out->limbs, out->len);
    if (out->len > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        bu_free(out);
        return NULL;
    }
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

SurgeBigUint* bu_shr(const SurgeBigUint* u, int bits, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (bits < 0) {
        if (err != NULL) {
            *err = BN_ERR_NEG_SHIFT;
        }
        return NULL;
    }
    if (u == NULL || u->len == 0 || bits == 0) {
        return bu_clone(u, err);
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return NULL;
    }
    uint32_t word_shift = (uint32_t)(bits / 32);
    uint32_t bit_shift = (uint32_t)(bits % 32);
    if (word_shift >= len) {
        return NULL;
    }
    uint32_t out_len = len - word_shift;
    SurgeBigUint* out = bu_alloc(out_len, err);
    if (out == NULL) {
        return NULL;
    }
    if (bit_shift == 0) {
        memcpy(out->limbs, u->limbs + word_shift, (size_t)out_len * sizeof(uint32_t));
        out->len = trim_len(out->limbs, out->len);
        if (out->len == 0) {
            bu_free(out);
            return NULL;
        }
        return out;
    }
    uint32_t carry = 0;
    for (uint32_t i = len; i-- > word_shift;) {
        uint32_t v = u->limbs[i];
        out->limbs[i - word_shift] = (v >> bit_shift) | (carry << (32 - bit_shift));
        carry = v & ((1u << bit_shift) - 1u);
        if (i == word_shift) {
            break;
        }
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

// Long division in base 2^32 with normalization; returns quotient and remainder.
SurgeBigUint*
bu_div_mod(const SurgeBigUint* a, const SurgeBigUint* b, SurgeBigUint** out_rem, bn_err* err) {
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
    if (bu_cmp(a, b) < 0) {
        if (out_rem != NULL) {
            *out_rem = bu_clone(a, err);
        }
        return NULL;
    }
    int shift = (int)bu_bitlen(a) - (int)bu_bitlen(b);
    if (shift < 0) {
        if (out_rem != NULL) {
            *out_rem = bu_clone(a, err);
        }
        return NULL;
    }
    if ((uint32_t)(shift / 32 + 1) > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* denom_shifted = bu_shl(b, shift, &tmp_err);
    if (denom_shifted == NULL && tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    uint32_t denom_len = denom_shifted ? denom_shifted->len : 0;
    uint32_t* denom = NULL;
    if (denom_len > 0) {
        denom = (uint32_t*)malloc((size_t)denom_len * sizeof(uint32_t));
        if (denom == NULL) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            bu_free(denom_shifted);
            return NULL;
        }
        memcpy(denom, denom_shifted->limbs, (size_t)denom_len * sizeof(uint32_t));
    }
    bu_free(denom_shifted);
    denom_shifted = NULL;

    uint32_t rem_len = a->len;
    uint32_t* rem = (uint32_t*)malloc((size_t)rem_len * sizeof(uint32_t));
    if (rem == NULL) {
        free(denom);
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    memcpy(rem, a->limbs, (size_t)rem_len * sizeof(uint32_t));

    uint32_t quot_len = (uint32_t)(shift / 32 + 1);
    SurgeBigUint* quot = bu_alloc(quot_len, err);
    if (quot == NULL) {
        free(denom);
        free(rem);
        return NULL;
    }
    memset(quot->limbs, 0, (size_t)quot->len * sizeof(uint32_t));

    for (int i = shift; i >= 0; i--) {
        uint32_t denom_trim = trim_len(denom, denom_len);
        uint32_t rem_trim = trim_len(rem, rem_len);
        if (rem_trim > 0 && denom_trim > 0) {
            if (bu_cmp_limbs(rem, rem_trim, denom, denom_trim) >= 0) {
                bu_sub_in_place(rem, rem_len, denom, denom_len);
                quot->limbs[(uint32_t)i / 32] |= (uint32_t)1 << (i % 32);
            }
        }
        if (denom_len > 0) {
            bu_shr1_in_place(denom, denom_len);
        }
        if (i == 0) {
            break;
        }
    }

    quot->len = trim_len(quot->limbs, quot->len);
    if (quot->len > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        free(denom);
        free(rem);
        bu_free(quot);
        return NULL;
    }

    uint32_t rem_trim = trim_len(rem, rem_len);
    if (out_rem != NULL) {
        if (rem_trim == 0) {
            *out_rem = NULL;
        } else {
            SurgeBigUint* r = bu_alloc(rem_trim, err);
            if (r == NULL) {
                free(denom);
                free(rem);
                bu_free(quot);
                return NULL;
            }
            memcpy(r->limbs, rem, (size_t)rem_trim * sizeof(uint32_t));
            r->len = rem_trim;
            *out_rem = r;
        }
    }

    free(denom);
    free(rem);

    if (quot->len == 0) {
        bu_free(quot);
        return NULL;
    }
    return quot;
}

SurgeBigUint* bu_and(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (a == NULL || b == NULL) {
        return NULL;
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    uint32_t n = alen < blen ? alen : blen;
    if (n == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(n, err);
    if (out == NULL) {
        return NULL;
    }
    for (uint32_t i = 0; i < n; i++) {
        out->limbs[i] = a->limbs[i] & b->limbs[i];
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

SurgeBigUint* bu_or(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if ((a == NULL || a->len == 0) && (b == NULL || b->len == 0)) {
        return NULL;
    }
    if (a == NULL || a->len == 0) {
        return bu_clone(b, err);
    }
    if (b == NULL || b->len == 0) {
        return bu_clone(a, err);
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    uint32_t n = alen > blen ? alen : blen;
    if (n == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(n, err);
    if (out == NULL) {
        return NULL;
    }
    for (uint32_t i = 0; i < n; i++) {
        uint32_t av = i < alen ? a->limbs[i] : 0;
        uint32_t bv = i < blen ? b->limbs[i] : 0;
        out->limbs[i] = av | bv;
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

SurgeBigUint* bu_xor(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if ((a == NULL || a->len == 0) && (b == NULL || b->len == 0)) {
        return NULL;
    }
    if (a == NULL || a->len == 0) {
        return bu_clone(b, err);
    }
    if (b == NULL || b->len == 0) {
        return bu_clone(a, err);
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    uint32_t n = alen > blen ? alen : blen;
    if (n == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(n, err);
    if (out == NULL) {
        return NULL;
    }
    for (uint32_t i = 0; i < n; i++) {
        uint32_t av = i < alen ? a->limbs[i] : 0;
        uint32_t bv = i < blen ? b->limbs[i] : 0;
        out->limbs[i] = av ^ bv;
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

bool bu_bit_set(const SurgeBigUint* u, int bit) {
    if (u == NULL || u->len == 0 || bit < 0) {
        return false;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return false;
    }
    uint32_t word = (uint32_t)(bit / 32);
    if (word >= len) {
        return false;
    }
    uint32_t mask = (uint32_t)1 << (bit % 32);
    return (u->limbs[word] & mask) != 0;
}

static bool bu_any_low_bit_set(const SurgeBigUint* u, int bits) {
    if (u == NULL || u->len == 0 || bits <= 0) {
        return false;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return false;
    }
    int full_words = bits / 32;
    int rem_bits = bits % 32;
    for (int i = 0; i < full_words && i < (int)len; i++) {
        if (u->limbs[i] != 0) {
            return true;
        }
    }
    if (rem_bits == 0) {
        return false;
    }
    if (full_words >= (int)len) {
        return false;
    }
    uint32_t mask = ((uint32_t)1 << rem_bits) - 1u;
    return (u->limbs[full_words] & mask) != 0;
}

// Shift right with round-to-nearest-even ("banker's rounding").
SurgeBigUint* bu_shift_right_round_even(const SurgeBigUint* u, int bits, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (bits <= 0 || u == NULL || u->len == 0) {
        return bu_clone(u, err);
    }
    if (bits > (int)bu_bitlen(u)) {
        return NULL;
    }
    bool half_set = bu_bit_set(u, bits - 1);
    bool low_set = bu_any_low_bit_set(u, bits - 1);
    SurgeBigUint* shifted = bu_shr(u, bits, err);
    if (shifted == NULL) {
        return NULL;
    }
    if (!half_set) {
        return shifted;
    }
    if (low_set) {
        SurgeBigUint* rounded = bu_add_small(shifted, 1, err);
        bu_free(shifted);
        return rounded;
    }
    if (bu_is_odd(shifted)) {
        SurgeBigUint* rounded = bu_add_small(shifted, 1, err);
        bu_free(shifted);
        return rounded;
    }
    return shifted;
}

// Round quotient using remainder and denominator with half-even rule.
SurgeBigUint* bu_round_quotient_even(const SurgeBigUint* q,
                                     const SurgeBigUint* r,
                                     const SurgeBigUint* denom,
                                     bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (r == NULL || r->len == 0) {
        return bu_clone(q, err);
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* two_r = bu_shl(r, 1, &tmp_err);
    if (two_r == NULL && tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    int cmp = bu_cmp(two_r, denom);
    if (cmp < 0) {
        SurgeBigUint* out = bu_clone(q, err);
        bu_free(two_r);
        return out;
    }
    if (cmp > 0) {
        SurgeBigUint* out = bu_add_small(q, 1, err);
        bu_free(two_r);
        return out;
    }
    if (bu_is_odd(q)) {
        SurgeBigUint* out = bu_add_small(q, 1, err);
        bu_free(two_r);
        return out;
    }
    SurgeBigUint* out = bu_clone(q, err);
    bu_free(two_r);
    return out;
}

SurgeBigUint* bu_pow10(int n, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (n < 0) {
        if (err != NULL) {
            *err = BN_ERR_NEG_SHIFT;
        }
        return NULL;
    }
    if (n == 0) {
        return bu_from_u64(1, err);
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* result = bu_from_u64(1, &tmp_err);
    SurgeBigUint* base = bu_from_u64(10, &tmp_err);
    if (tmp_err != BN_OK || result == NULL || base == NULL) {
        if (err != NULL) {
            *err = (tmp_err != BN_OK) ? tmp_err : BN_ERR_MAX_LIMBS;
        }
        bu_free(result);
        bu_free(base);
        return NULL;
    }
    int exp = n;
    while (exp > 0) {
        if (exp & 1) {
            tmp_err = BN_OK;
            SurgeBigUint* next = bu_mul(result, base, &tmp_err);
            if (next == NULL && tmp_err == BN_OK) {
                tmp_err = BN_ERR_MAX_LIMBS;
            }
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                bu_free(result);
                bu_free(base);
                return NULL;
            }
            bu_free(result);
            result = next;
        }
        exp >>= 1;
        if (exp == 0) {
            break;
        }
        tmp_err = BN_OK;
        SurgeBigUint* next_base = bu_mul(base, base, &tmp_err);
        if (next_base == NULL && tmp_err == BN_OK) {
            tmp_err = BN_ERR_MAX_LIMBS;
        }
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            bu_free(result);
            bu_free(base);
            return NULL;
        }
        bu_free(base);
        base = next_base;
    }
    bu_free(base);
    return result;
}

SurgeBigUint* bu_pow5(int n, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (n < 0) {
        if (err != NULL) {
            *err = BN_ERR_NEG_SHIFT;
        }
        return NULL;
    }
    if (n == 0) {
        return bu_from_u64(1, err);
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* result = bu_from_u64(1, &tmp_err);
    SurgeBigUint* base = bu_from_u64(5, &tmp_err);
    if (tmp_err != BN_OK || result == NULL || base == NULL) {
        if (err != NULL) {
            *err = (tmp_err != BN_OK) ? tmp_err : BN_ERR_MAX_LIMBS;
        }
        bu_free(result);
        bu_free(base);
        return NULL;
    }
    int exp = n;
    while (exp > 0) {
        if (exp & 1) {
            tmp_err = BN_OK;
            SurgeBigUint* next = bu_mul(result, base, &tmp_err);
            if (next == NULL && tmp_err == BN_OK) {
                tmp_err = BN_ERR_MAX_LIMBS;
            }
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                bu_free(result);
                bu_free(base);
                return NULL;
            }
            bu_free(result);
            result = next;
        }
        exp >>= 1;
        if (exp == 0) {
            break;
        }
        tmp_err = BN_OK;
        SurgeBigUint* next_base = bu_mul(base, base, &tmp_err);
        if (next_base == NULL && tmp_err == BN_OK) {
            tmp_err = BN_ERR_MAX_LIMBS;
        }
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            bu_free(result);
            bu_free(base);
            return NULL;
        }
        bu_free(base);
        base = next_base;
    }
    bu_free(base);
    return result;
}

SurgeBigUint* bu_low_bits(const SurgeBigUint* u, int bits, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (u == NULL || u->len == 0 || bits <= 0) {
        return NULL;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return NULL;
    }
    int word_count = bits / SURGE_BIGNUM_LIMB_BITS;
    int rem_bits = bits % SURGE_BIGNUM_LIMB_BITS;
    if (word_count >= (int)len) {
        return bu_clone(u, err);
    }
    uint32_t out_len = (uint32_t)word_count;
    if (rem_bits != 0) {
        out_len++;
    }
    SurgeBigUint* out = bu_alloc(out_len, err);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out->limbs, u->limbs, (size_t)out_len * sizeof(uint32_t));
    if (rem_bits != 0) {
        uint32_t mask = ((uint32_t)1 << rem_bits) - 1u;
        out->limbs[out_len - 1] &= mask;
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        bu_free(out);
        return NULL;
    }
    return out;
}

bool shift_count_from_biguint(const SurgeBigUint* u, int* out) {
    if (out != NULL) {
        *out = 0;
    }
    uint64_t val = 0;
    if (!bu_to_u64(u, &val)) {
        return false;
    }
    uint64_t max_int = (uint64_t)(INT_MAX);
    if (val > max_int) {
        return false;
    }
    if (out != NULL) {
        *out = (int)val;
    }
    return true;
}
