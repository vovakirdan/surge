#include "rt.h"

#include <ctype.h>
#include <errno.h>
#include <inttypes.h>
#include <limits.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdalign.h>
#include <unistd.h>

#define SURGE_BIGNUM_MAX_LIMBS 1000000u
#define SURGE_BIGNUM_MANTISSA_BITS 256

typedef struct SurgeBigUint {
    uint32_t len;
    uint32_t limbs[];
} SurgeBigUint;

typedef struct SurgeBigInt {
    uint8_t neg;
    uint8_t _pad[3];
    uint32_t len;
    uint32_t limbs[];
} SurgeBigInt;

typedef struct SurgeBigFloat {
    uint8_t neg;
    int32_t exp;
    SurgeBigUint* mant;
} SurgeBigFloat;

typedef enum {
    BN_OK = 0,
    BN_ERR_MAX_LIMBS,
    BN_ERR_DIV_ZERO,
    BN_ERR_UNDERFLOW,
    BN_ERR_NEG_SHIFT,
} bn_err;

static const SurgeBigUint* bi_as_uint(const SurgeBigInt* i) {
    if (i == NULL) {
        return NULL;
    }
    return (const SurgeBigUint*)((const uint8_t*)i + offsetof(SurgeBigInt, len));
}

static uint32_t trim_len(const uint32_t* limbs, uint32_t len) {
    while (len > 0 && limbs[len - 1] == 0) {
        len--;
    }
    return len;
}

static void panic_with_code(int code, const char* msg) {
    if (msg == NULL) {
        msg = "invalid numeric conversion";
    }
    char buf[256];
    int n = snprintf(buf, sizeof(buf), "panic VM%d: %s\n", code, msg);
    if (n < 0) {
        const uint8_t fallback[] = "panic VM3202: invalid numeric conversion\n";
        rt_write_stderr(fallback, (uint64_t)(sizeof(fallback) - 1));
        _exit(1);
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    rt_write_stderr((const uint8_t*)buf, (uint64_t)n);
    _exit(1);
}

static void bignum_panic(const char* msg) {
    if (msg == NULL) {
        msg = "invalid numeric conversion";
    }
    int code = 3202;
    if (strcmp(msg, "numeric size limit exceeded") == 0) {
        code = 3201;
    } else if (strcmp(msg, "division by zero") == 0) {
        code = 3203;
    } else if (strcmp(msg, "integer overflow") == 0) {
        code = 1101;
    }
    panic_with_code(code, msg);
}

static void bignum_panic_err(bn_err err) {
    switch (err) {
    case BN_OK:
        return;
    case BN_ERR_MAX_LIMBS:
        bignum_panic("numeric size limit exceeded");
        return;
    case BN_ERR_DIV_ZERO:
        bignum_panic("division by zero");
        return;
    case BN_ERR_UNDERFLOW:
        bignum_panic("unsigned underflow");
        return;
    case BN_ERR_NEG_SHIFT:
        bignum_panic("negative shift");
        return;
    default:
        bignum_panic("invalid numeric conversion");
        return;
    }
}

static SurgeBigUint* bu_alloc(uint32_t len, bn_err* err) {
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
    return out;
}

static SurgeBigUint* bu_clone(const SurgeBigUint* u, bn_err* err) {
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

static uint32_t bu_bitlen(const SurgeBigUint* u) {
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
    return (len - 1) * 32 + bits;
}

static bool bu_is_zero(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        return true;
    }
    return trim_len(u->limbs, u->len) == 0;
}

static bool bu_is_odd(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        return false;
    }
    return (u->limbs[0] & 1u) == 1u;
}

static int bu_cmp_limbs(const uint32_t* a, uint32_t alen, const uint32_t* b, uint32_t blen) {
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

static int bu_cmp(const SurgeBigUint* a, const SurgeBigUint* b) {
    const uint32_t* al = a ? a->limbs : NULL;
    const uint32_t* bl = b ? b->limbs : NULL;
    uint32_t alen = a ? a->len : 0;
    uint32_t blen = b ? b->len : 0;
    return bu_cmp_limbs(al, alen, bl, blen);
}

static bool bu_limbs_to_u64(const uint32_t* limbs, uint32_t len, uint64_t* out) {
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
        *out = lo | (hi << 32);
    }
    return true;
}

static bool bu_to_u64(const SurgeBigUint* u, uint64_t* out) {
    if (out != NULL) {
        *out = 0;
    }
    if (u == NULL || u->len == 0) {
        return true;
    }
    return bu_limbs_to_u64(u->limbs, u->len, out);
}

static SurgeBigUint* bu_from_u64(uint64_t v) {
    if (v == 0) {
        return NULL;
    }
    uint32_t lo = (uint32_t)(v & 0xFFFFFFFFu);
    uint32_t hi = (uint32_t)(v >> 32);
    bn_err err = BN_OK;
    if (hi == 0) {
        SurgeBigUint* out = bu_alloc(1, &err);
        if (out == NULL) {
            return NULL;
        }
        out->limbs[0] = lo;
        return out;
    }
    SurgeBigUint* out = bu_alloc(2, &err);
    if (out == NULL) {
        return NULL;
    }
    out->limbs[0] = lo;
    out->limbs[1] = hi;
    return out;
}

static SurgeBigUint* bu_add(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
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

static SurgeBigUint* bu_add_small(const SurgeBigUint* u, uint32_t v, bn_err* err) {
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

static void bu_sub_in_place(uint32_t* dst, uint32_t dst_len, const uint32_t* sub, uint32_t sub_len) {
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

static SurgeBigUint* bu_sub(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
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

static SurgeBigUint* bu_mul(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err) {
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

static SurgeBigUint* bu_mul_small(const SurgeBigUint* u, uint32_t m, bn_err* err) {
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

static SurgeBigUint* bu_div_mod_small(const SurgeBigUint* u, uint32_t d, uint32_t* rem, bn_err* err) {
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

static SurgeBigUint* bu_shl(const SurgeBigUint* u, int bits, bn_err* err) {
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
            return NULL;
        }
        if (out->len == 0) {
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
        return NULL;
    }
    if (out->len == 0) {
        return NULL;
    }
    return out;
}

static SurgeBigUint* bu_shr(const SurgeBigUint* u, int bits, bn_err* err) {
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
        return NULL;
    }
    return out;
}

static SurgeBigUint* bu_div_mod(const SurgeBigUint* a, const SurgeBigUint* b, SurgeBigUint** out_rem, bn_err* err) {
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
            return NULL;
        }
        memcpy(denom, denom_shifted->limbs, (size_t)denom_len * sizeof(uint32_t));
    }

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
        return NULL;
    }
    return quot;
}

static SurgeBigUint* bu_and(const SurgeBigUint* a, const SurgeBigUint* b) {
    if (a == NULL || b == NULL) {
        return NULL;
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    uint32_t n = alen < blen ? alen : blen;
    if (n == 0) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_alloc(n, &err);
    if (out == NULL) {
        return NULL;
    }
    for (uint32_t i = 0; i < n; i++) {
        out->limbs[i] = a->limbs[i] & b->limbs[i];
    }
    out->len = trim_len(out->limbs, out->len);
    if (out->len == 0) {
        return NULL;
    }
    return out;
}

static SurgeBigUint* bu_or(const SurgeBigUint* a, const SurgeBigUint* b) {
    if ((a == NULL || a->len == 0) && (b == NULL || b->len == 0)) {
        return NULL;
    }
    if (a == NULL || a->len == 0) {
        return bu_clone(b, NULL);
    }
    if (b == NULL || b->len == 0) {
        return bu_clone(a, NULL);
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    uint32_t n = alen > blen ? alen : blen;
    if (n == 0) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_alloc(n, &err);
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
        return NULL;
    }
    return out;
}

static SurgeBigUint* bu_xor(const SurgeBigUint* a, const SurgeBigUint* b) {
    if ((a == NULL || a->len == 0) && (b == NULL || b->len == 0)) {
        return NULL;
    }
    if (a == NULL || a->len == 0) {
        return bu_clone(b, NULL);
    }
    if (b == NULL || b->len == 0) {
        return bu_clone(a, NULL);
    }
    uint32_t alen = trim_len(a->limbs, a->len);
    uint32_t blen = trim_len(b->limbs, b->len);
    uint32_t n = alen > blen ? alen : blen;
    if (n == 0) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_alloc(n, &err);
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
        return NULL;
    }
    return out;
}

static bool bu_bit_set(const SurgeBigUint* u, int bit) {
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

static SurgeBigUint* bu_shift_right_round_even(const SurgeBigUint* u, int bits, bn_err* err) {
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
        return bu_add_small(shifted, 1, err);
    }
    if (bu_is_odd(shifted)) {
        return bu_add_small(shifted, 1, err);
    }
    return shifted;
}

static SurgeBigUint* bu_round_quotient_even(const SurgeBigUint* q, const SurgeBigUint* r, const SurgeBigUint* denom, bn_err* err) {
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
        return bu_clone(q, err);
    }
    if (cmp > 0) {
        return bu_add_small(q, 1, err);
    }
    if (bu_is_odd(q)) {
        return bu_add_small(q, 1, err);
    }
    return bu_clone(q, err);
}

static SurgeBigUint* bu_pow10(int n, bn_err* err) {
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
        return bu_from_u64(1);
    }
    SurgeBigUint* result = bu_from_u64(1);
    SurgeBigUint* base = bu_from_u64(10);
    int exp = n;
    while (exp > 0) {
        if (exp & 1) {
            bn_err tmp_err = BN_OK;
            SurgeBigUint* next = bu_mul(result, base, &tmp_err);
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                return NULL;
            }
            result = next;
        }
        exp >>= 1;
        if (exp == 0) {
            break;
        }
        bn_err tmp_err = BN_OK;
        SurgeBigUint* next_base = bu_mul(base, base, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        base = next_base;
    }
    return result;
}

static SurgeBigUint* bu_pow5(int n, bn_err* err) {
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
        return bu_from_u64(1);
    }
    SurgeBigUint* result = bu_from_u64(1);
    SurgeBigUint* base = bu_from_u64(5);
    int exp = n;
    while (exp > 0) {
        if (exp & 1) {
            bn_err tmp_err = BN_OK;
            SurgeBigUint* next = bu_mul(result, base, &tmp_err);
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                return NULL;
            }
            result = next;
        }
        exp >>= 1;
        if (exp == 0) {
            break;
        }
        bn_err tmp_err = BN_OK;
        SurgeBigUint* next_base = bu_mul(base, base, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        base = next_base;
    }
    return result;
}

static SurgeBigUint* bu_low_bits(const SurgeBigUint* u, int bits) {
    if (u == NULL || u->len == 0 || bits <= 0) {
        return NULL;
    }
    uint32_t len = trim_len(u->limbs, u->len);
    if (len == 0) {
        return NULL;
    }
    int word_count = bits / 32;
    int rem_bits = bits % 32;
    if (word_count >= (int)len) {
        return bu_clone(u, NULL);
    }
    uint32_t out_len = (uint32_t)word_count;
    if (rem_bits != 0) {
        out_len++;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_alloc(out_len, &err);
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
        return NULL;
    }
    return out;
}

static SurgeBigInt* bi_alloc(uint32_t len, bn_err* err) {
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

static bool bi_is_zero(const SurgeBigInt* i) {
    if (i == NULL || i->len == 0) {
        return true;
    }
    return trim_len(i->limbs, i->len) == 0;
}

static SurgeBigUint* bi_abs(const SurgeBigInt* i, bn_err* err) {
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

static bool bi_to_i64(const SurgeBigInt* i, int64_t* out) {
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

static SurgeBigInt* bi_from_i64(int64_t v) {
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
        return NULL;
    }
    out->neg = neg;
    memcpy(out->limbs, abs->limbs, (size_t)abs->len * sizeof(uint32_t));
    out->len = abs->len;
    return out;
}

static SurgeBigInt* bi_from_u64(uint64_t v) {
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
        return NULL;
    }
    out->neg = 0;
    memcpy(out->limbs, abs->limbs, (size_t)abs->len * sizeof(uint32_t));
    out->len = abs->len;
    return out;
}

static int bi_cmp(const SurgeBigInt* a, const SurgeBigInt* b) {
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

static SurgeBigInt* bi_neg(const SurgeBigInt* a, bn_err* err) {
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

static SurgeBigInt* bi_abs_val(const SurgeBigInt* a, bn_err* err) {
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

static SurgeBigInt* bi_add(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
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
            return NULL;
        }
        if (sum == NULL || sum->len == 0) {
            return NULL;
        }
        SurgeBigInt* out = bi_alloc(sum->len, err);
        if (out == NULL) {
            return NULL;
        }
        out->neg = a->neg;
        memcpy(out->limbs, sum->limbs, (size_t)sum->len * sizeof(uint32_t));
        out->len = sum->len;
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
            return NULL;
        }
        if (diff == NULL || diff->len == 0) {
            return NULL;
        }
        SurgeBigInt* out = bi_alloc(diff->len, err);
        if (out == NULL) {
            return NULL;
        }
        out->neg = a->neg;
        memcpy(out->limbs, diff->limbs, (size_t)diff->len * sizeof(uint32_t));
        out->len = diff->len;
        return out;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* diff = bu_sub(bi_as_uint(b), bi_as_uint(a), &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (diff == NULL || diff->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(diff->len, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = b->neg;
    memcpy(out->limbs, diff->limbs, (size_t)diff->len * sizeof(uint32_t));
    out->len = diff->len;
    return out;
}

static SurgeBigInt* bi_sub(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
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
    return bi_add(a, neg, err);
}

static SurgeBigInt* bi_mul(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
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
        return NULL;
    }
    if (prod == NULL || prod->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(prod->len, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = (a->neg != b->neg) ? 1 : 0;
    memcpy(out->limbs, prod->limbs, (size_t)prod->len * sizeof(uint32_t));
    out->len = prod->len;
    return out;
}

static SurgeBigInt* bi_div_mod(const SurgeBigInt* a, const SurgeBigInt* b, SurgeBigInt** out_rem, bn_err* err) {
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
        return NULL;
    }
    SurgeBigInt* q = NULL;
    if (q_u != NULL && q_u->len > 0) {
        q = bi_alloc(q_u->len, err);
        if (q == NULL) {
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
                return NULL;
            }
            r->neg = a->neg;
            memcpy(r->limbs, rem_u->limbs, (size_t)rem_u->len * sizeof(uint32_t));
            r->len = rem_u->len;
            *out_rem = r;
        }
    }
    return q;
}

static SurgeBigUint* bi_twos_complement(const SurgeBigUint* mag, bool neg, const SurgeBigUint* pow2, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (!neg || mag == NULL || mag->len == 0) {
        return bu_clone(mag, err);
    }
    return bu_sub(pow2, mag, err);
}

static SurgeBigInt* bi_bit_op(const SurgeBigInt* a, const SurgeBigInt* b,
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
    SurgeBigUint* pow2 = bu_shl(bu_from_u64(1), (int)width, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* rep_a = bi_twos_complement(bi_as_uint(a), a != NULL && a->neg, pow2, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* rep_b = bi_twos_complement(bi_as_uint(b), b != NULL && b->neg, pow2, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* res = op(rep_a, rep_b);
    if (res == NULL || res->len == 0) {
        return NULL;
    }
    if (!bu_bit_set(res, (int)width - 1)) {
        SurgeBigInt* out = bi_alloc(res->len, err);
        if (out == NULL) {
            return NULL;
        }
        out->neg = 0;
        memcpy(out->limbs, res->limbs, (size_t)res->len * sizeof(uint32_t));
        out->len = res->len;
        return out;
    }
    SurgeBigUint* mag = bu_sub(pow2, res, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    if (mag == NULL || mag->len == 0) {
        return NULL;
    }
    SurgeBigInt* out = bi_alloc(mag->len, err);
    if (out == NULL) {
        return NULL;
    }
    out->neg = 1;
    memcpy(out->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    out->len = mag->len;
    return out;
}

static bool shift_count_from_biguint(const SurgeBigUint* u, int* out) {
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

static bool shift_count_from_bigint(const SurgeBigInt* i, int* out) {
    if (out != NULL) {
        *out = 0;
    }
    if (i != NULL && i->neg && !bi_is_zero(i)) {
        return false;
    }
    return shift_count_from_biguint(bi_as_uint(i), out);
}

static SurgeBigInt* bi_shl(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
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

static SurgeBigInt* bi_shr(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err) {
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
            return NULL;
        }
        if (shifted == NULL || shifted->len == 0) {
            return NULL;
        }
        SurgeBigInt* out = bi_alloc(shifted->len, err);
        if (out == NULL) {
            return NULL;
        }
        out->neg = 0;
        memcpy(out->limbs, shifted->limbs, (size_t)shifted->len * sizeof(uint32_t));
        out->len = shifted->len;
        return out;
    }
    SurgeBigUint* pow2 = bu_shl(bu_from_u64(1), shift, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* pow2m1 = bu_sub(pow2, bu_from_u64(1), &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* sum = bu_add(bi_as_uint(a), pow2m1, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* shifted = bu_shr(sum, shift, &tmp_err);
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
    out->neg = 1;
    memcpy(out->limbs, shifted->limbs, (size_t)shifted->len * sizeof(uint32_t));
    out->len = shifted->len;
    return out;
}

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

static bool bf_is_zero(const SurgeBigFloat* f) {
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

static int bf_cmp(const SurgeBigFloat* a, const SurgeBigFloat* b) {
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

static SurgeBigFloat* bf_neg(const SurgeBigFloat* f, bn_err* err) {
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

static SurgeBigFloat* bf_abs(const SurgeBigFloat* f, bn_err* err) {
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

static SurgeBigFloat* bf_from_uint(const SurgeBigUint* u, bn_err* err) {
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

static SurgeBigFloat* bf_from_int(const SurgeBigInt* i, bn_err* err) {
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

static SurgeBigFloat* bf_add(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
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

static SurgeBigFloat* bf_sub(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
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

static SurgeBigFloat* bf_mul(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
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

static SurgeBigFloat* bf_div(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
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

static SurgeBigInt* bf_to_int_trunc(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || bf_is_zero(f)) {
        return NULL;
    }
    SurgeBigUint* mag = f->mant;
    if (f->exp > 0) {
        if ((int64_t)f->exp > (int64_t)INT_MAX) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        bn_err tmp_err = BN_OK;
        mag = bu_shl(mag, (int)f->exp, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
    } else if (f->exp < 0) {
        int64_t shift = -(int64_t)f->exp;
        if (shift > (int64_t)INT_MAX) {
            return NULL;
        }
        bn_err tmp_err = BN_OK;
        mag = bu_shr(mag, (int)shift, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
    }
    if (mag == NULL || mag->len == 0) {
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* out = bi_alloc(mag->len, &tmp_err);
    if (tmp_err != BN_OK || out == NULL) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    out->neg = f->neg;
    memcpy(out->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    out->len = mag->len;
    return out;
}

static SurgeBigUint* bf_to_uint_trunc(const SurgeBigFloat* f, bn_err* err) {
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
        return NULL;
    }
    if (i->neg) {
        if (err != NULL) {
            *err = BN_ERR_UNDERFLOW;
        }
        return NULL;
    }
    SurgeBigUint* out = bu_clone(bi_as_uint(i), err);
    return out;
}

static SurgeBigFloat* bf_mod(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err) {
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
    SurgeBigFloat* q = bf_div(a, b, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigInt* qi = bf_to_int_trunc(q, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigFloat* qf = bf_from_int(qi, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigFloat* prod = bf_mul(qf, b, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigFloat* res = bf_sub(a, prod, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    return res;
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

static SurgeBigFloat* bf_from_ratio(bool neg, const SurgeBigUint* num, const SurgeBigUint* den, bn_err* err) {
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
        int shift = -scale;
        if (shift > INT_MAX) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        scaled_den = bu_shl(den, shift, &tmp_err);
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

static int digit_value(char ch, uint32_t base, bool* ok) {
    if (ok != NULL) {
        *ok = true;
    }
    if (ch >= '0' && ch <= '9') {
        int d = ch - '0';
        if ((uint32_t)d >= base) {
            if (ok != NULL) {
                *ok = false;
            }
        }
        return d;
    }
    if (base == 16 && ch >= 'a' && ch <= 'f') {
        return 10 + (ch - 'a');
    }
    if (base == 16 && ch >= 'A' && ch <= 'F') {
        return 10 + (ch - 'A');
    }
    if (ok != NULL) {
        *ok = false;
    }
    return 0;
}

static bn_err parse_uint_string(const uint8_t* data, size_t len, bool allow_plus, bool allow_prefix, SurgeBigUint** out) {
    if (out != NULL) {
        *out = NULL;
    }
    if (data == NULL || len == 0) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t start = 0;
    size_t end = len;
    while (start < end && isspace(data[start])) {
        start++;
    }
    while (end > start && isspace(data[end - 1])) {
        end--;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    if (allow_plus && data[start] == '+') {
        start++;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    uint32_t base = 10;
    if (allow_prefix && end - start > 2 && data[start] == '0') {
        char c = (char)data[start + 1];
        if (c == 'x' || c == 'X') {
            base = 16;
            start += 2;
        } else if (c == 'b' || c == 'B') {
            base = 2;
            start += 2;
        } else if (c == 'o' || c == 'O') {
            base = 8;
            start += 2;
        }
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    SurgeBigUint* cur = NULL;
    for (size_t i = start; i < end; i++) {
        uint8_t ch = data[i];
        if (ch == '_') {
            continue;
        }
        bool ok = false;
        int d = digit_value((char)ch, base, &ok);
        if (!ok) {
            return BN_ERR_NEG_SHIFT;
        }
        bn_err tmp_err = BN_OK;
        cur = bu_mul_small(cur, base, &tmp_err);
        if (tmp_err != BN_OK) {
            return tmp_err;
        }
        cur = bu_add_small(cur, (uint32_t)d, &tmp_err);
        if (tmp_err != BN_OK) {
            return tmp_err;
        }
    }
    if (out != NULL) {
        *out = cur;
    }
    return BN_OK;
}

static bn_err parse_int_string(const uint8_t* data, size_t len, SurgeBigInt** out) {
    if (out != NULL) {
        *out = NULL;
    }
    if (data == NULL || len == 0) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t start = 0;
    size_t end = len;
    while (start < end && isspace(data[start])) {
        start++;
    }
    while (end > start && isspace(data[end - 1])) {
        end--;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    bool neg = false;
    if (data[start] == '+') {
        start++;
    } else if (data[start] == '-') {
        neg = true;
        start++;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(data + start, end - start, false, false, &mag);
    if (err != BN_OK) {
        return err;
    }
    if (mag == NULL || mag->len == 0) {
        return BN_OK;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* res = bi_alloc(mag->len, &tmp_err);
    if (tmp_err != BN_OK) {
        return tmp_err;
    }
    res->neg = neg ? 1 : 0;
    memcpy(res->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    res->len = mag->len;
    if (out != NULL) {
        *out = res;
    }
    return BN_OK;
}

static bn_err parse_float_string(const uint8_t* data, size_t len, SurgeBigFloat** out) {
    if (out != NULL) {
        *out = NULL;
    }
    if (data == NULL || len == 0) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t start = 0;
    size_t end = len;
    while (start < end && isspace(data[start])) {
        start++;
    }
    while (end > start && isspace(data[end - 1])) {
        end--;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    bool neg = false;
    if (data[start] == '+') {
        start++;
    } else if (data[start] == '-') {
        neg = true;
        start++;
    }
    if (start >= end) {
        return BN_ERR_NEG_SHIFT;
    }
    size_t i = start;
    uint8_t* digits = (uint8_t*)malloc(len + 1);
    size_t digits_len = 0;
    while (i < end && data[i] >= '0' && data[i] <= '9') {
        digits[digits_len++] = data[i];
        i++;
    }
    if (digits_len == 0) {
        free(digits);
        return BN_ERR_NEG_SHIFT;
    }
    int frac_digits = 0;
    if (i < end && data[i] == '.') {
        i++;
        while (i < end && data[i] >= '0' && data[i] <= '9') {
            digits[digits_len++] = data[i];
            frac_digits++;
            i++;
        }
    }
    int exp10 = 0;
    if (i < end && (data[i] == 'e' || data[i] == 'E')) {
        i++;
        if (i >= end) {
            free(digits);
            return BN_ERR_NEG_SHIFT;
        }
        bool exp_neg = false;
        if (data[i] == '+') {
            i++;
        } else if (data[i] == '-') {
            exp_neg = true;
            i++;
        }
        if (i >= end || data[i] < '0' || data[i] > '9') {
            free(digits);
            return BN_ERR_NEG_SHIFT;
        }
        int val = 0;
        const int max_exp = 1000000;
        while (i < end && data[i] >= '0' && data[i] <= '9') {
            if (val > max_exp) {
                free(digits);
                return BN_ERR_NEG_SHIFT;
            }
            val = val * 10 + (int)(data[i] - '0');
            i++;
        }
        exp10 = exp_neg ? -val : val;
    }
    if (i != end) {
        free(digits);
        return BN_ERR_NEG_SHIFT;
    }
    size_t leading = 0;
    while (leading < digits_len && digits[leading] == '0') {
        leading++;
    }
    if (leading == digits_len) {
        free(digits);
        return BN_OK;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigUint* n = NULL;
    tmp_err = parse_uint_string(digits + leading, digits_len - leading, false, false, &n);
    free(digits);
    if (tmp_err != BN_OK) {
        return tmp_err;
    }
    int k = exp10 - frac_digits;
    SurgeBigUint* num = n;
    SurgeBigUint* den = bu_from_u64(1);
    if (k >= 0) {
        SurgeBigUint* pow = bu_pow10(k, &tmp_err);
        if (tmp_err != BN_OK) {
            return tmp_err;
        }
        num = bu_mul(num, pow, &tmp_err);
        if (tmp_err != BN_OK) {
            return tmp_err;
        }
    } else {
        SurgeBigUint* pow = bu_pow10(-k, &tmp_err);
        if (tmp_err != BN_OK) {
            return tmp_err;
        }
        den = pow;
    }
    SurgeBigFloat* f = bf_from_ratio(neg, num, den, &tmp_err);
    if (tmp_err != BN_OK) {
        return tmp_err;
    }
    if (out != NULL) {
        *out = f;
    }
    return BN_OK;
}

static char* format_uint(const SurgeBigUint* u) {
    if (u == NULL || u->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    const uint32_t base = 1000000000u;
    SurgeBigUint* cur = bu_clone(u, NULL);
    if (cur == NULL) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    uint32_t* parts = NULL;
    size_t parts_len = 0;
    while (cur != NULL && cur->len != 0) {
        uint32_t rem = 0;
        bn_err err = BN_OK;
        SurgeBigUint* q = bu_div_mod_small(cur, base, &rem, &err);
        if (err != BN_OK) {
            break;
        }
        uint32_t* next = (uint32_t*)realloc(parts, (parts_len + 1) * sizeof(uint32_t));
        if (next == NULL) {
            break;
        }
        parts = next;
        parts[parts_len++] = rem;
        cur = q;
    }
    if (parts_len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    char first_buf[32];
    int first_len = snprintf(first_buf, sizeof(first_buf), "%u", parts[parts_len - 1]);
    if (first_len < 0) {
        return NULL;
    }
    size_t total_len = (size_t)first_len + (parts_len - 1) * 9;
    char* out = (char*)malloc(total_len + 1);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out, first_buf, (size_t)first_len);
    size_t offset = (size_t)first_len;
    for (size_t i = parts_len - 1; i-- > 0;) {
        char buf[16];
        int n = snprintf(buf, sizeof(buf), "%09u", parts[i]);
        if (n < 0) {
            break;
        }
        memcpy(out + offset, buf, (size_t)n);
        offset += (size_t)n;
        if (i == 0) {
            break;
        }
    }
    out[total_len] = 0;
    free(parts);
    return out;
}

static char* format_int(const SurgeBigInt* i) {
    if (i == NULL || i->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    SurgeBigUint* u = bu_clone(bi_as_uint(i), NULL);
    char* base = format_uint(u);
    if (base == NULL) {
        return NULL;
    }
    if (!i->neg) {
        return base;
    }
    size_t len = strlen(base);
    char* out = (char*)malloc(len + 2);
    if (out == NULL) {
        free(base);
        return NULL;
    }
    out[0] = '-';
    memcpy(out + 1, base, len + 1);
    free(base);
    return out;
}

static char* format_scientific(const char* int_str, const char* frac_str) {
    if (int_str == NULL || frac_str == NULL) {
        return NULL;
    }
    if (strcmp(int_str, "0") != 0) {
        int exp = (int)strlen(int_str) - 1;
        size_t digits_len = strlen(int_str) + strlen(frac_str);
        char* digits = (char*)malloc(digits_len + 1);
        if (digits == NULL) {
            return NULL;
        }
        strcpy(digits, int_str);
        strcat(digits, frac_str);
        char first = digits[0];
        const char* rest = digits + 1;
        size_t rest_len = strlen(rest);
        char exp_buf[32];
        if (exp >= 0) {
            snprintf(exp_buf, sizeof(exp_buf), "E+%d", exp);
        } else {
            snprintf(exp_buf, sizeof(exp_buf), "E-%d", -exp);
        }
        size_t out_len = 1 + (rest_len ? 1 + rest_len : 0) + strlen(exp_buf);
        char* out = (char*)malloc(out_len + 1);
        if (out == NULL) {
            free(digits);
            return NULL;
        }
        size_t off = 0;
        out[off++] = first;
        if (rest_len > 0) {
            out[off++] = '.';
            memcpy(out + off, rest, rest_len);
            off += rest_len;
        }
        memcpy(out + off, exp_buf, strlen(exp_buf));
        out[out_len] = 0;
        free(digits);
        return out;
    }
    size_t i = 0;
    size_t frac_len = strlen(frac_str);
    while (i < frac_len && frac_str[i] == '0') {
        i++;
    }
    if (i >= frac_len) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    int exp = -(int)(i + 1);
    const char* digits = frac_str + i;
    char exp_buf[32];
    if (exp >= 0) {
        snprintf(exp_buf, sizeof(exp_buf), "E+%d", exp);
    } else {
        snprintf(exp_buf, sizeof(exp_buf), "E-%d", -exp);
    }
    char first = digits[0];
    const char* rest = digits + 1;
    size_t rest_len = strlen(rest);
    size_t out_len = 1 + (rest_len ? 1 + rest_len : 0) + strlen(exp_buf);
    char* out = (char*)malloc(out_len + 1);
    if (out == NULL) {
        return NULL;
    }
    size_t off = 0;
    out[off++] = first;
    if (rest_len > 0) {
        out[off++] = '.';
        memcpy(out + off, rest, rest_len);
        off += rest_len;
    }
    memcpy(out + off, exp_buf, strlen(exp_buf));
    out[out_len] = 0;
    return out;
}

static char* format_float(const SurgeBigFloat* f, bn_err* err) {
    if (err != NULL) {
        *err = BN_OK;
    }
    if (f == NULL || bf_is_zero(f)) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    bool neg = f->neg;
    SurgeBigUint* mant = bu_clone(f->mant, NULL);
    if (mant == NULL || mant->len == 0) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    if (f->exp >= 0) {
        if ((int64_t)f->exp > (int64_t)INT_MAX) {
            if (err != NULL) {
                *err = BN_ERR_MAX_LIMBS;
            }
            return NULL;
        }
        bn_err tmp_err = BN_OK;
        SurgeBigUint* int_mag = bu_shl(mant, (int)f->exp, &tmp_err);
        if (tmp_err != BN_OK) {
            if (err != NULL) {
                *err = tmp_err;
            }
            return NULL;
        }
        char* s = format_uint(int_mag);
        if (s == NULL) {
            return NULL;
        }
        if (!neg) {
            return s;
        }
        size_t len = strlen(s);
        char* out = (char*)malloc(len + 2);
        if (out == NULL) {
            free(s);
            return NULL;
        }
        out[0] = '-';
        memcpy(out + 1, s, len + 1);
        free(s);
        return out;
    }
    int64_t n64 = -(int64_t)f->exp;
    if (n64 < 0) {
        n64 = 0;
    }
    if (n64 > (int64_t)INT_MAX) {
        if (err != NULL) {
            *err = BN_ERR_MAX_LIMBS;
        }
        return NULL;
    }
    int n = (int)n64;
    if (bu_is_zero(mant)) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    if (bu_bitlen(mant) <= (uint32_t)n) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    if (bu_is_zero(mant)) {
        char* z = (char*)malloc(2);
        if (z == NULL) {
            return NULL;
        }
        z[0] = '0';
        z[1] = 0;
        return z;
    }
    if (bu_bitlen(mant) >= (uint32_t)n && bu_is_odd(mant) == false) {
        int tz = 0;
        SurgeBigUint* tmp = mant;
        while (tmp != NULL && tmp->len > 0 && (tmp->limbs[0] & 1u) == 0u) {
            tz++;
            bn_err tmp_err = BN_OK;
            tmp = bu_shr(tmp, 1, &tmp_err);
            if (tmp_err != BN_OK) {
                break;
            }
        }
        if (tz >= n) {
            bn_err tmp_err = BN_OK;
            SurgeBigUint* int_mag = bu_shr(mant, n, &tmp_err);
            if (tmp_err != BN_OK) {
                if (err != NULL) {
                    *err = tmp_err;
                }
                return NULL;
            }
            char* s = format_uint(int_mag);
            if (s == NULL) {
                return NULL;
            }
            if (!neg) {
                return s;
            }
            size_t len = strlen(s);
            char* out = (char*)malloc(len + 2);
            if (out == NULL) {
                free(s);
                return NULL;
            }
            out[0] = '-';
            memcpy(out + 1, s, len + 1);
            free(s);
            return out;
        }
    }

    bn_err tmp_err = BN_OK;
    SurgeBigUint* int_part = bu_shr(mant, n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* frac_part = bu_low_bits(mant, n);
    SurgeBigUint* pow5 = bu_pow5(n, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }
    SurgeBigUint* frac_digits = bu_mul(frac_part, pow5, &tmp_err);
    if (tmp_err != BN_OK) {
        if (err != NULL) {
            *err = tmp_err;
        }
        return NULL;
    }

    char* int_str = format_uint(int_part);
    char* frac_str = format_uint(frac_digits);
    if (int_str == NULL || frac_str == NULL) {
        return NULL;
    }
    size_t frac_len = strlen(frac_str);
    if (frac_len < (size_t)n) {
        size_t pad = (size_t)n - frac_len;
        char* padded = (char*)malloc((size_t)n + 1);
        if (padded == NULL) {
            return NULL;
        }
        memset(padded, '0', pad);
        memcpy(padded + pad, frac_str, frac_len + 1);
        free(frac_str);
        frac_str = padded;
    }
    while (strlen(frac_str) > 0 && frac_str[strlen(frac_str) - 1] == '0') {
        frac_str[strlen(frac_str) - 1] = 0;
    }
    if (strlen(frac_str) == 0) {
        if (!neg) {
            return int_str;
        }
        size_t len = strlen(int_str);
        char* out = (char*)malloc(len + 2);
        if (out == NULL) {
            return NULL;
        }
        out[0] = '-';
        memcpy(out + 1, int_str, len + 1);
        free(int_str);
        return out;
    }
    char* sci = format_scientific(int_str, frac_str);
    if (sci == NULL) {
        return NULL;
    }
    if (!neg) {
        return sci;
    }
    size_t len = strlen(sci);
    char* out = (char*)malloc(len + 2);
    if (out == NULL) {
        free(sci);
        return NULL;
    }
    out[0] = '-';
    memcpy(out + 1, sci, len + 1);
    free(sci);
    return out;
}

static bool string_span(void* s, const uint8_t** out_ptr, uint64_t* out_len) {
    if (out_ptr != NULL) {
        *out_ptr = NULL;
    }
    if (out_len != NULL) {
        *out_len = 0;
    }
    if (s == NULL) {
        return false;
    }
    const uint8_t* ptr = rt_string_ptr(s);
    uint64_t len = rt_string_len_bytes(s);
    if (out_ptr != NULL) {
        *out_ptr = ptr;
    }
    if (out_len != NULL) {
        *out_len = len;
    }
    return true;
}

void* rt_bigint_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, false, true, &mag);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    if (mag == NULL || mag->len == 0) {
        return NULL;
    }
    bn_err tmp_err = BN_OK;
    SurgeBigInt* out = bi_alloc(mag->len, &tmp_err);
    if (tmp_err != BN_OK || out == NULL) {
        bignum_panic_err(tmp_err);
        return NULL;
    }
    out->neg = 0;
    memcpy(out->limbs, mag->limbs, (size_t)mag->len * sizeof(uint32_t));
    out->len = mag->len;
    return (void*)out;
}

void* rt_biguint_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigUint* mag = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, false, true, &mag);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    return (void*)mag;
}

void* rt_bigfloat_from_literal(const uint8_t* ptr, uint64_t len) {
    SurgeBigFloat* out = NULL;
    bn_err err = parse_float_string(ptr, (size_t)len, &out);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    return (void*)out;
}

bool rt_parse_bigint(void* s, void** out) {
    if (out != NULL) {
        *out = NULL;
    }
    const uint8_t* ptr = NULL;
    uint64_t len = 0;
    if (!string_span(s, &ptr, &len)) {
        return false;
    }
    SurgeBigInt* res = NULL;
    bn_err err = parse_int_string(ptr, (size_t)len, &res);
    if (err != BN_OK) {
        return false;
    }
    if (out != NULL) {
        *out = res;
    }
    return true;
}

bool rt_parse_biguint(void* s, void** out) {
    if (out != NULL) {
        *out = NULL;
    }
    const uint8_t* ptr = NULL;
    uint64_t len = 0;
    if (!string_span(s, &ptr, &len)) {
        return false;
    }
    SurgeBigUint* res = NULL;
    bn_err err = parse_uint_string(ptr, (size_t)len, true, false, &res);
    if (err != BN_OK) {
        return false;
    }
    if (out != NULL) {
        *out = res;
    }
    return true;
}

bool rt_parse_bigfloat(void* s, void** out) {
    if (out != NULL) {
        *out = NULL;
    }
    const uint8_t* ptr = NULL;
    uint64_t len = 0;
    if (!string_span(s, &ptr, &len)) {
        return false;
    }
    SurgeBigFloat* res = NULL;
    bn_err err = parse_float_string(ptr, (size_t)len, &res);
    if (err != BN_OK) {
        return false;
    }
    if (out != NULL) {
        *out = res;
    }
    return true;
}

void* rt_string_from_bigint(void* v) {
    char* s = format_int((const SurgeBigInt*)v);
    if (s == NULL) {
        bignum_panic("numeric size limit exceeded");
        return rt_string_from_bytes(NULL, 0);
    }
    size_t len = strlen(s);
    void* out = rt_string_from_bytes((const uint8_t*)s, (uint64_t)len);
    free(s);
    return out;
}

void* rt_string_from_biguint(void* v) {
    char* s = format_uint((const SurgeBigUint*)v);
    if (s == NULL) {
        bignum_panic("numeric size limit exceeded");
        return rt_string_from_bytes(NULL, 0);
    }
    size_t len = strlen(s);
    void* out = rt_string_from_bytes((const uint8_t*)s, (uint64_t)len);
    free(s);
    return out;
}

void* rt_string_from_bigfloat(void* v) {
    bn_err err = BN_OK;
    char* s = format_float((const SurgeBigFloat*)v, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return rt_string_from_bytes(NULL, 0);
    }
    if (s == NULL) {
        bignum_panic("numeric size limit exceeded");
        return rt_string_from_bytes(NULL, 0);
    }
    size_t len = strlen(s);
    void* out = rt_string_from_bytes((const uint8_t*)s, (uint64_t)len);
    free(s);
    return out;
}

void* rt_bigint_from_i64(int64_t value) {
    return (void*)bi_from_i64(value);
}

void* rt_bigint_from_u64(uint64_t value) {
    return (void*)bi_from_u64(value);
}

void* rt_biguint_from_u64(uint64_t value) {
    return (void*)bu_from_u64(value);
}

void* rt_bigfloat_from_i64(int64_t value) {
    bn_err err = BN_OK;
    SurgeBigInt* i = bi_from_i64(value);
    SurgeBigFloat* f = bf_from_int(i, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)f;
}

void* rt_bigfloat_from_u64(uint64_t value) {
    bn_err err = BN_OK;
    SurgeBigUint* u = bu_from_u64(value);
    SurgeBigFloat* f = bf_from_uint(u, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)f;
}

void* rt_bigfloat_from_f64(double value) {
    char buf[64];
    int n = snprintf(buf, sizeof(buf), "%.17g", value);
    if (n < 0) {
        return NULL;
    }
    if (n >= (int)sizeof(buf)) {
        n = (int)sizeof(buf) - 1;
    }
    return rt_bigfloat_from_literal((const uint8_t*)buf, (uint64_t)n);
}

bool rt_bigint_to_i64(void* v, int64_t* out) {
    return bi_to_i64((const SurgeBigInt*)v, out);
}

bool rt_biguint_to_u64(void* v, uint64_t* out) {
    return bu_to_u64((const SurgeBigUint*)v, out);
}

bool rt_bigfloat_to_f64(void* v, double* out) {
    if (out != NULL) {
        *out = 0.0;
    }
    if (v == NULL || bf_is_zero((const SurgeBigFloat*)v)) {
        return true;
    }
    bn_err err = BN_OK;
    char* s = format_float((const SurgeBigFloat*)v, &err);
    if (err != BN_OK || s == NULL) {
        return false;
    }
    errno = 0;
    char* endptr = NULL;
    double val = strtod(s, &endptr);
    bool ok = !(errno != 0 || endptr == s || *endptr != 0);
    free(s);
    if (ok && out != NULL) {
        *out = val;
    }
    return ok;
}

void* rt_bigint_add(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_add((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_sub(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_sub((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_mul(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_mul((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_div(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_div_mod((const SurgeBigInt*)a, (const SurgeBigInt*)b, NULL, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_mod(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* rem = NULL;
    bi_div_mod((const SurgeBigInt*)a, (const SurgeBigInt*)b, &rem, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)rem;
}

void* rt_bigint_neg(void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_neg((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_abs(void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_abs_val((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

int32_t rt_bigint_cmp(void* a, void* b) {
    return (int32_t)bi_cmp((const SurgeBigInt*)a, (const SurgeBigInt*)b);
}

void* rt_bigint_bit_and(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op((const SurgeBigInt*)a, (const SurgeBigInt*)b, bu_and, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_bit_or(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op((const SurgeBigInt*)a, (const SurgeBigInt*)b, bu_or, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_bit_xor(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op((const SurgeBigInt*)a, (const SurgeBigInt*)b, bu_xor, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigint_shl(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_shl((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic("integer overflow");
    }
    return (void*)out;
}

void* rt_bigint_shr(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_shr((const SurgeBigInt*)a, (const SurgeBigInt*)b, &err);
    if (err != BN_OK) {
        bignum_panic("integer overflow");
    }
    return (void*)out;
}

void* rt_biguint_add(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_add((const SurgeBigUint*)a, (const SurgeBigUint*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_sub(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_sub((const SurgeBigUint*)a, (const SurgeBigUint*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_mul(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_mul((const SurgeBigUint*)a, (const SurgeBigUint*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_div(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_div_mod((const SurgeBigUint*)a, (const SurgeBigUint*)b, NULL, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_mod(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigUint* rem = NULL;
    bu_div_mod((const SurgeBigUint*)a, (const SurgeBigUint*)b, &rem, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)rem;
}

int32_t rt_biguint_cmp(void* a, void* b) {
    return (int32_t)bu_cmp((const SurgeBigUint*)a, (const SurgeBigUint*)b);
}

void* rt_biguint_bit_and(void* a, void* b) {
    SurgeBigUint* out = bu_and((const SurgeBigUint*)a, (const SurgeBigUint*)b);
    return (void*)out;
}

void* rt_biguint_bit_or(void* a, void* b) {
    SurgeBigUint* out = bu_or((const SurgeBigUint*)a, (const SurgeBigUint*)b);
    return (void*)out;
}

void* rt_biguint_bit_xor(void* a, void* b) {
    SurgeBigUint* out = bu_xor((const SurgeBigUint*)a, (const SurgeBigUint*)b);
    return (void*)out;
}

void* rt_biguint_shl(void* a, void* b) {
    int shift = 0;
    if (!shift_count_from_biguint((const SurgeBigUint*)b, &shift)) {
        bignum_panic("integer overflow");
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_shl((const SurgeBigUint*)a, shift, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_shr(void* a, void* b) {
    int shift = 0;
    if (!shift_count_from_biguint((const SurgeBigUint*)b, &shift)) {
        bignum_panic("integer overflow");
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_shr((const SurgeBigUint*)a, shift, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_add(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_add((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_sub(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_sub((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_mul(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_mul((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_div(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_div((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_mod(void* a, void* b) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_mod((const SurgeBigFloat*)a, (const SurgeBigFloat*)b, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_neg(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_neg((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_abs(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_abs((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

int32_t rt_bigfloat_cmp(void* a, void* b) {
    return (int32_t)bf_cmp((const SurgeBigFloat*)a, (const SurgeBigFloat*)b);
}

void* rt_bigint_to_biguint(void* a) {
    const SurgeBigInt* src = (const SurgeBigInt*)a;
    if (src != NULL && src->neg && !bi_is_zero(src)) {
        bignum_panic("cannot convert negative int to uint");
        return NULL;
    }
    return (void*)bu_clone(bi_as_uint(src), NULL);
}

void* rt_biguint_to_bigint(void* a) {
    const SurgeBigUint* src = (const SurgeBigUint*)a;
    if (src == NULL || src->len == 0) {
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_alloc(src->len, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
        return NULL;
    }
    out->neg = 0;
    memcpy(out->limbs, src->limbs, (size_t)src->len * sizeof(uint32_t));
    out->len = src->len;
    return (void*)out;
}

void* rt_bigint_to_bigfloat(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_from_int((const SurgeBigInt*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_biguint_to_bigfloat(void* a) {
    bn_err err = BN_OK;
    SurgeBigFloat* out = bf_from_uint((const SurgeBigUint*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_to_bigint(void* a) {
    bn_err err = BN_OK;
    SurgeBigInt* out = bf_to_int_trunc((const SurgeBigFloat*)a, &err);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}

void* rt_bigfloat_to_biguint(void* a) {
    bn_err err = BN_OK;
    SurgeBigUint* out = bf_to_uint_trunc((const SurgeBigFloat*)a, &err);
    if (err == BN_ERR_UNDERFLOW) {
        bignum_panic("cannot convert negative float to uint");
        return NULL;
    }
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return (void*)out;
}
