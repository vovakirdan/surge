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

// A bigfloat block is reference counted: compiled code retains on every copy
// that outlives its source and releases at scope exit, and the block frees when
// the count reaches zero. `rc` sits FIRST and its offset is asserted below,
// because the LLVM backend emits the retain/release as inline IR at the use
// site rather than paying a call for every float copy — offset zero lets it
// address the counter without computing a field offset.
//
// Unlike the int/uint pair, nothing reinterprets a `SurgeBigFloat`'s tail as
// another struct (`bi_as_uint` aliases only `SurgeBigInt` -> `SurgeBigUint`),
// so a prefix field is safe here in a way it would not be there.
//
// The count is NON-ATOMIC. That is sound only while a block is never reachable
// from two shards at once. TWO things were meant to uphold that: the
// module-level `let` ban (shipped) and a deep copy at every crossing. The
// second is NOT BUILT -- `cross_move_init` and `cross_clone_init` are
// `filledNowhere` in internal/valueops/flags.go and NULL in every descriptor,
// and rt_bigfloat_clone has no caller. What upholds it today is a THIRD thing
// this comment used to hide by claiming the barrier existed: every crossing
// that would share a counted block is REFUSED at compile time. That is a
// narrowing, not a solution, and it is one `int`/`uint` cannot take. Epic 22
// Phase 2 builds the barrier and lifts the refusal -- RV2-DEBT-038. Corrected
// 2026-09-04.
typedef struct SurgeBigFloat {
    uint32_t rc;
    int32_t exp;
    uint8_t neg;
    uint8_t _pad[7];
    SurgeBigUint* mant;
} SurgeBigFloat;

// Heap bignums must be at least 2-byte aligned so the low pointer bit is free
// for the fixnum tag (see rt_bignum_tag.h). rt_alloc hands back >= 4-byte
// alignment in practice; assert the invariant the tagging depends on.
_Static_assert(alignof(SurgeBigInt) >= 2, "SurgeBigInt must leave the low bit free for tagging");
_Static_assert(alignof(SurgeBigUint) >= 2, "SurgeBigUint must leave the low bit free for tagging");
_Static_assert(alignof(SurgeBigFloat) >= 2,
               "SurgeBigFloat must leave the low bit free for tagging");

// The LLVM backend emits `retain` and `release` as inline IR that loads and
// stores the counter through the block pointer with no offset. Moving `rc`
// silently miscompiles every float copy, so pin it here rather than in a
// comment on the emitter.
_Static_assert(offsetof(SurgeBigFloat, rc) == 0, "bigfloat refcount must stay at offset 0");
_Static_assert(sizeof(((SurgeBigFloat*)0)->rc) == 4, "bigfloat refcount must stay a 32-bit word");

#include "rt_bignum_tag.h"

// fixnum <-> heap bridging, shared between the arithmetic entry points
// (rt_bignum_int_api.c) and the constructor/conversion entry points
// (rt_bignum_api.c, where these are defined). An operand is promoted to a
// heap bignum for the slow path; `owned` is non-NULL when a temp was
// allocated and must be released. Results run through *_finish, which
// demotes back to an inline fixnum when the value fits.
typedef struct {
    const SurgeBigInt* p;
    SurgeBigInt* owned;
} bi_operand;

typedef struct {
    const SurgeBigUint* p;
    SurgeBigUint* owned;
} bu_operand;

void* bi_from_i64_tagged(int64_t v);
void* bi_from_u64_tagged(uint64_t v);
void* bu_from_u64_tagged(uint64_t v);
bi_operand bi_promote(const void* w);
void bi_operand_release(bi_operand* o);
void* bi_finish(SurgeBigInt* r);
bu_operand bu_promote(const void* w);
void bu_operand_release(bu_operand* o);
void* bu_finish(SurgeBigUint* r);

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
    return (const SurgeBigUint*)&i->len;
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
SurgeBigUint* bu_from_u64(uint64_t v, bn_err* err);
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
SurgeBigUint* bu_and(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err);
SurgeBigUint* bu_or(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err);
SurgeBigUint* bu_xor(const SurgeBigUint* a, const SurgeBigUint* b, bn_err* err);
bool bu_bit_set(const SurgeBigUint* u, int bit);
SurgeBigUint* bu_shift_right_round_even(const SurgeBigUint* u, int bits, bn_err* err);
SurgeBigUint* bu_round_quotient_even(const SurgeBigUint* q,
                                     const SurgeBigUint* r,
                                     const SurgeBigUint* denom,
                                     bn_err* err);
SurgeBigUint* bu_pow10(int n, bn_err* err);
SurgeBigUint* bu_pow5(int n, bn_err* err);
SurgeBigUint* bu_low_bits(const SurgeBigUint* u, int bits, bn_err* err);
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
SurgeBigInt* bi_from_i64(int64_t v, bn_err* err);
SurgeBigInt* bi_from_u64(uint64_t v, bn_err* err);
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
                       SurgeBigUint* (*op)(const SurgeBigUint*, const SurgeBigUint*, bn_err* err),
                       bn_err* err);
SurgeBigInt* bi_shl(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);
SurgeBigInt* bi_shr(const SurgeBigInt* a, const SurgeBigInt* b, bn_err* err);

// BigFloat helpers.
bool bf_is_zero(const SurgeBigFloat* f);
SurgeBigFloat* bf_clone(const SurgeBigFloat* f, bn_err* err);
SurgeBigFloat* bf_from_uint(const SurgeBigUint* u, bn_err* err);
SurgeBigFloat* bf_from_int(const SurgeBigInt* i, bn_err* err);
SurgeBigFloat* bf_add(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_sub(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_mul(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_div(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_mod(const SurgeBigFloat* a, const SurgeBigFloat* b, bn_err* err);
SurgeBigFloat* bf_neg(const SurgeBigFloat* f, bn_err* err);
SurgeBigFloat* bf_abs(const SurgeBigFloat* f, bn_err* err);
static inline void bf_free(SurgeBigFloat* f) {
    if (f == NULL) {
        return;
    }
    bu_free(f->mant);
    rt_free((uint8_t*)f, (uint64_t)sizeof(SurgeBigFloat), (uint64_t)alignof(SurgeBigFloat));
}
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
char* format_uint(const SurgeBigUint* u, bn_err* err);
char* format_int(const SurgeBigInt* i, bn_err* err);
char* format_float(const SurgeBigFloat* f, bn_err* err);

#endif
