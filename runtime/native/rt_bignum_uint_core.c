#include "rt_bignum_internal.h"

#include <string.h>

// BigUint core ops: allocation, basic arithmetic, shifts, and bitwise helpers.
SurgeBigUint* bu_alloc(uint32_t len, bn_err* err) {
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
    size_t size = sizeof(SurgeBigUint) + (size_t)len * sizeof(uint32_t);
    SurgeBigUint* out = (SurgeBigUint*)rt_alloc((uint64_t)size, (uint64_t)alignof(SurgeBigUint));
    if (out == NULL) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    out->len = len;
    memset(out->limbs, 0, (size_t)len * sizeof(uint32_t));
    return out;
}

SurgeBigUint* bu_clone(const SurgeBigUint* u, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (u == NULL || u->len == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(u->len, err);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out->limbs, u->limbs, (size_t)u->len * sizeof(uint32_t));
    return out;
}

uint32_t bu_bitlen(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        return 0;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return 0;
    }
    uint32_t ms = u->limbs[len - 1];
    uint32_t bits = 0;
    while (ms != 0) {
        ms >>= 1;
        bits++;
    }
    return (len - 1) * SURGE_BIGNUM_LIMB_BITS + bits;
}

bool bu_is_zero(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        return true;
    }
    return trim_len(u->limbs, u->len) == 0;
}

bool bu_is_odd(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        return false;
    }
    return (u->limbs[0] & 1u) == 1u;
}

int bu_cmp_limbs(const uint32_t* a, uint32_t alen, const uint32_t* b, uint32_t blen) {
    if (a == NULL || alen == 0) {
        return (b == NULL || blen == 0) ? 0 : -1;
    }
    if (b == NULL || blen == 0) {
        return 1;
    }
    alen = trim_len(a, alen);
    blen = trim_len(b, blen);
    if (alen < blen) {
        return -1;
    }
    if (alen > blen) {
        return 1;
    }
    for (uint32_t i = alen; i-- > 0;) {
        uint32_t av = a[i];
        uint32_t bv = b[i];
        if (av < bv) {
            return -1;
        }
        if (av > bv) {
            return 1;
        }
        if (i == 0) {
            break;
        }
    }
    return 0;
}

int bu_cmp(const SurgeBigUint* a, const SurgeBigUint* b) {
    const uint32_t* al = a ? a->limbs : NULL;
    const uint32_t* bl = b ? b->limbs : NULL;
    uint32_t alen = a ? a->len : 0;
    uint32_t blen = b ? b->len : 0;
    return bu_cmp_limbs(al, alen, bl, blen);
}

bool bu_limbs_to_u64(const uint32_t* limbs, uint32_t len, uint64_t* out) {
    if (out != NULL) {
        *out = 0;
    }
    if (limbs == NULL || len == 0) {
        return true;
    }
    len = trim_len(limbs, len);
    if (len == 0) {
        return true;
    }
    if (len > 2) {
        return false;
    }
    uint64_t lo = limbs[0];
    uint64_t hi = len > 1 ? (uint64_t)limbs[1] : 0;
    if (out != NULL) {
        *out = lo | (hi << SURGE_BIGNUM_LIMB_BITS);
    }
    return true;
}

bool bu_to_u64(const SurgeBigUint* u, uint64_t* out) {
    if (out != NULL) {
        *out = 0;
    }
    if (u == NULL || u->len == 0) {
        return true;
    }
    return bu_limbs_to_u64(u->limbs, u->len, out);
}

SurgeBigUint* bu_from_u64(uint64_t v, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (v == 0) {
        return NULL;
    }
    uint32_t lo = (uint32_t)(v & 0xFFFFFFFFu);
    uint32_t hi = (uint32_t)(v >> 32);
    if (hi == 0) {
        SurgeBigUint* out = bu_alloc(1, err);
        if (out == NULL) {
            return NULL;
        }
        out->limbs[0] = lo;
        return out;
    }
    SurgeBigUint* out = bu_alloc(2, err);
    if (out == NULL) {
        return NULL;
    }
    out->limbs[0] = lo;
    out->limbs[1] = hi;
    return out;
}

SurgeBigUint* bu_add(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
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
    SurgeBigUint* out = bu_alloc(n + 1, err);
    if (out == NULL) {
        return NULL;
    }
    uint64_t carry = 0;
    for (uint32_t i = 0; i < n; i++) {
        uint64_t av = i < alen ? (uint64_t)a->limbs[i] : 0;
        uint64_t bv = i < blen ? (uint64_t)b->limbs[i] : 0;
        uint64_t sum = av + bv + carry;
        out->limbs[i] = (uint32_t)sum;
        carry = sum >> 32;
    }
    out->limbs[n] = (uint32_t)carry;
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        return NULL;
    }
    if (out->len > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    return out;
}

SurgeBigUint* bu_add_small(const SurgeBigUint* u, uint32_t v, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (v == 0) {
        return bu_clone(u, err);
    }
    if (u == NULL || u->len == 0) {
        SurgeBigUint* out = bu_alloc(1, err);
        if (out == NULL) {
            return NULL;
        }
        out->limbs[0] = v;
        return out;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        SurgeBigUint* out = bu_alloc(1, err);
        if (out == NULL) {
            return NULL;
        }
        out->limbs[0] = v;
        return out;
    }
    SurgeBigUint* out = bu_alloc(len + 1, err);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out->limbs, u->limbs, (size_t)len * sizeof(uint32_t));
    uint64_t sum = (uint64_t)out->limbs[0] + (uint64_t)v;
    out->limbs[0] = (uint32_t)sum;
    uint64_t carry = sum >> 32;
    for (uint32_t i = 1; carry != 0 && i < out->len; i++) {
        uint64_t next = (uint64_t)out->limbs[i] + carry;
        out->limbs[i] = (uint32_t)next;
        carry = next >> 32;
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    if (out->len == 0) {
        return NULL;
    }
    return out;
}

void bu_sub_in_place(uint32_t* dst, uint32_t dst_len, const uint32_t* sub, uint32_t sub_len) {
    uint64_t borrow = 0;
    for (uint32_t i = 0; i < dst_len; i++) {
        uint64_t av = dst[i];
        uint64_t bv = i < sub_len ? sub[i] : 0;
        uint64_t tmp = av - bv - borrow;
        dst[i] = (uint32_t)tmp;
        if (av < bv + borrow) {
            borrow = 1;
        } else {
            borrow = 0;
        }
    }
}

SurgeBigUint* bu_sub(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (b == NULL || b->len == 0) {
        return bu_clone(a, err);
    }
    if (a == NULL || a->len == 0) {
        if (err != NULL) {
            *err = BN_ERR_UNDERFLOW;
        }
        return NULL;
    }
    if (bu_cmp(a, b) < 0) {
        if (err != NULL) {
            *err = BN_ERR_UNDERFLOW;
        }
        return NULL;
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    SurgeBigUint* out = bu_alloc(alen, err);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out->limbs, a->limbs, (size_t)alen * sizeof(uint32_t));
    bu_sub_in_place(out->limbs, out->len, b->limbs, blen);
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        return NULL;
    }
    return out;
}

SurgeBigUint* bu_mul(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (a == NULL || b == NULL) {
        return NULL;
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    if (alen == 0 || blen == 0) {
        return NULL;
    }
    if ((uint64_t)alen + (uint64_t)blen > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(alen + blen, err);
    if (out == NULL) {
        return NULL;
    }
    memset(out->limbs, 0, (size_t)out->len * sizeof(uint32_t));
    for (uint32_t i = 0; i < alen; i++) {
        uint64_t ai = a->limbs[i];
        uint64_t carry = 0;
        for (uint32_t j = 0; j < blen; j++) {
            uint32_t k = i + j;
            uint64_t sum = (uint64_t)out->limbs[k] + ai * (uint64_t)b->limbs[j] + carry;
            out->limbs[k] = (uint32_t)sum;
            carry = sum >> 32;
        }
        uint32_t k = i + blen;
        while (carry != 0) {
            if (k >= out->len) {
                if (err != NULL) {
                    *err = BN_ERR_MAX_LIMBS;
                }
                return NULL;
            }
            uint64_t sum = (uint64_t)out->limbs[k] + carry;
            out->limbs[k] = (uint32_t)sum;
            carry = sum >> 32;
            k++;
        }
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        return NULL;
    }
    return out;
}

SurgeBigUint* bu_mul_small(const SurgeBigUint* u, uint32_t m, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (m == 0 || u == NULL || u->len == 0) {
        return NULL;
    }
    if (m == 1) {
        return bu_clone(u, err);
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return NULL;
    }
    SurgeBigUint* out = bu_alloc(len + 1, err);
    if (out == NULL) {
        return NULL;
    }
    uint64_t carry = 0;
    for (uint32_t i = 0; i < len; i++) {
        uint64_t prod = (uint64_t)u->limbs[i] * (uint64_t)m + carry;
        out->limbs[i] = (uint32_t)prod;
        carry = prod >> 32;
    }
    out->limbs[len] = (uint32_t)carry;
    out->len = trim_len(out->limbs, out->len);
    if (out->len > SURGE_BIGNUM_MAX_LIMBS) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    if (out->len == 0) {
        return NULL;
    }
    return out;
}
