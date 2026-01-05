#ifndef SURGE_RUNTIME_NATIVE_BIGNUM_INTERNAL_H
#define SURGE_RUNTIME_NATIVE_BIGNUM_INTERNAL_H

#include "rt.h"

#include <stdalign.h>
#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// Limb representation is little-endian 32-bit words.
#define SURGE_BIGNUM_LIMB_BITS 32
#define SURGE_BIGNUM_LIMB_BASE ((uint64_t)1u << SURGE_BIGNUM_LIMB_BITS)

// Hard limit to avoid unbounded allocation in runtime operations.
#define SURGE_BIGNUM_MAX_LIMBS 1000000u

// Bigfloat mantissa size in bits (normalized, base-2).
#define SURGE_BIGNUM_MANTISSA_BITS 256

// Decimal chunk base for formatting: 1e9 fits in uint32_t.
#define SURGE_BIGNUM_DEC_BASE 1000000000u

// Clamp for parsing exponent to keep intermediate sizes bounded.
#define SURGE_BIGNUM_MAX_EXP10 1000000

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

static inline const SurgeBigUint* bi_as_uint(const SurgeBigInt* i) {
    if (i == NULL) {
        return NULL;
    }
    return (const SurgeBigUint*)((const uint8_t*)i + offsetof(SurgeBigInt, len));
}

static inline uint32_t trim_len(const uint32_t* limbs, uint32_t len) {
    while (len > 0 && limbs[len - 1] == 0) {
        len--;
    }
    return len;
}

void bignum_panic(const char* msg);
void bignum_panic_err(bn_err err);

// BigUint helpers.
SurgeBigUint* bu_alloc(uint32_t len, bn_err* err);
SurgeBigUint* bu_clone(const SurgeBigUint* u, bn_err* err);
static inline void bu_free(SurgeBigUint* u) {
    if (u == NULL) {
        return;
    }
    size_t size = sizeof(SurgeBigUint) + (size_t)u->len * sizeof(uint32_t);
    rt_free((uint8_t*)u, (uint64_t)size, (uint64_t)alignof(SurgeBigUint));
}
uint32_t bu_bitlen(const SurgeBigUint* u);
bool bu_is_zero(const SurgeBigUint* u);
bool bu_is_odd(const SurgeBigUint* u);
int bu_cmp_limbs(const uint32_t* a, uint32_t alen, const uint32_t* b, uint32_t blen);
int bu_cmp(const SurgeBigUint* a, const SurgeBigUint* b);
bool bu_limbs_to_u64(const uint32_t* limbs, uint32_t len, uint64_t* out);
bool bu_to_u64(const SurgeBigUint* u, uint64_t* out);
SurgeBigUint* bu_from_u64(uint64_t v);
SurgeBigUint* bu_add(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err);
SurgeBigUint* bu_add_small(const SurgeBigUint* u, uint32_t v, bn_err* err);
void bu_sub_in_place(uint32_t* dst, uint32_t dst_len, const uint32_t* sub, uint32_t sub_len);
SurgeBigUint* bu_sub(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err);
SurgeBigUint* bu_mul(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err);
SurgeBigUint* bu_mul_small(const SurgeBigUint* u, uint32_t m, bn_err* err);
SurgeBigUint* bu_div_mod_small(const SurgeBigUint* u, uint32_t d, uint32_t* rem, bn_err* err);
SurgeBigUint* bu_shl(const SurgeBigUint* u, int bits, bn_err* err);
SurgeBigUint* bu_shr(const SurgeBigUint* u, int bits, bn_err* err);
SurgeBigUint*
bu_div_mod(const SurgeBigUint* a, const SurgeBigUint* b, SurgeBigUint** out_rem, bn_err* err);
SurgeBigUint* bu_and(const SurgeBigUint* a, const SurgeBigUint* b);
SurgeBigUint* bu_or(const SurgeBigUint* a, const SurgeBigUint* b);
SurgeBigUint* bu_xor(const SurgeBigUint* a, const SurgeBigUint* b);
bool bu_bit_set(const SurgeBigUint* u, int bit);
SurgeBigUint* bu_shift_right_round_even(const SurgeBigUint* u, int bits, bn_err* err);
SurgeBigUint* bu_round_quotient_even(const SurgeBigUint* q,
                                     const SurgeBigUint* r,
                                     const SurgeBigUint* denom,
                                     bn_err* err);
SurgeBigUint* bu_pow10(int n, bn_err* err);
SurgeBigUint* bu_pow5(int n, bn_err* err);
SurgeBigUint* bu_low_bits(const SurgeBigUint* u, int bits);
bool shift_count_from_biguint(const SurgeBigUint* u, int* out);

// BigInt helpers.
SurgeBigInt* bi_alloc(uint32_t len, bn_err* err);
static inline void bi_free(SurgeBigInt* i) {
    if (i == NULL) {
        return;
    }
    size_t size = sizeof(SurgeBigInt) + (size_t)i->len * sizeof(uint32_t);
    rt_free((uint8_t*)i, (uint64_t)size, (uint64_t)alignof(SurgeBigInt));
}
bool bi_is_zero(const SurgeBigInt* i);
SurgeBigUint* bi_abs(const SurgeBigInt* i, bn_err* err);
bool bi_to_i64(const SurgeBigInt* i, int64_t* out);
SurgeBigInt* bi_from_i64(int64_t v);
SurgeBigInt* bi_from_u64(uint64_t v);
int bi_cmp(const SurgeBigInt* a, const SurgeBigInt* b);
SurgeBigInt* bi_neg(const SurgeBigInt* a, bn_err* err);
SurgeBigInt* bi_abs_val(const SurgeBigInt* a, bn_err* err);
SurgeBigInt* bi_add(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);
SurgeBigInt* bi_sub(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);
SurgeBigInt* bi_mul(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);
SurgeBigInt*
bi_div_mod(const SurgeBigInt* a, const SurgeBigInt* b, SurgeBigInt** out_rem, bn_err* err);
SurgeBigInt* bi_bit_op(const SurgeBigInt* a,
                       const SurgeBigInt* b,
                       SurgeBigUint* (*op)(const SurgeBigUint*, const SurgeBigUint*),
                       bn_err* err);
SurgeBigInt* bi_shl(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);
SurgeBigInt* bi_shr(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);

// BigFloat helpers.
bool bf_is_zero(const SurgeBigFloat* f);
SurgeBigFloat* bf_from_uint(const SurgeBigUint* u, bn_err* err);
SurgeBigFloat* bf_from_int(const SurgeBigInt* i, bn_err* err);
SurgeBigFloat* bf_add(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_sub(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_mul(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_div(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_mod(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_neg(const SurgeBigFloat* f, bn_err* err);
SurgeBigFloat* bf_abs(const SurgeBigFloat* f, bn_err* err);
int bf_cmp(const SurgeBigFloat* a, const SurgeBigFloat* b);
SurgeBigInt* bf_to_int_trunc(const SurgeBigFloat* f, bn_err* err);
SurgeBigUint* bf_to_uint_trunc(const SurgeBigFloat* f, bn_err* err);
SurgeBigFloat*
bf_from_ratio(bool neg, const SurgeBigUint* num, const SurgeBigUint* den, bn_err* err);

// Parsing/formatting helpers.
bn_err parse_uint_string(
    const uint8_t* data, size_t len, bool allow_plus, bool allow_prefix, SurgeBigUint** out);
bn_err parse_int_string(const uint8_t* data, size_t len, SurgeBigInt** out);
bn_err parse_float_string(const uint8_t* data, size_t len, SurgeBigFloat** out);
char* format_uint(const SurgeBigUint* u);
char* format_int(const SurgeBigInt* i);
char* format_float(const SurgeBigFloat* f, bn_err* err);

#endif
