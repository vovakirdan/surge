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

// The heap halves of `int` and `uint` are reference counted, the way a bigfloat
// block is: a copy that outlives its source retains, a scope exit releases, and
// the block frees when the count reaches zero. A value small enough to ride
// inline in the word (see rt_bignum_tag.h) owns no block and is never counted,
// which is why every exported entry point asks the tag before it touches
// memory -- an inline word is not a pointer and dereferencing one is a wild
// access, not a miscount.
//
// `rc` sits AFTER `len` rather than before it, and that placement is what keeps
// `bi_as_uint` below honest. That helper hands out a `SurgeBigUint` view of a
// `SurgeBigInt`'s TAIL; a count in the prefix position would sit at the base of
// a uint and at no matching place in an int, so the view would read a count
// where the int keeps its length. Suffixed, the tail matches the uint field for
// field -- asserted below, so the trap cannot come back silently. The price is
// that the three counts sit at three different offsets (float 0, uint 4,
// int 8), which the emitter prints per kind; that costs nothing, because it
// must already branch per kind to test the fixnum tag.
typedef struct SurgeBigUint {
    uint32_t len;
    uint32_t rc;
    uint32_t limbs[];
} SurgeBigUint;

typedef struct SurgeBigInt {
    uint8_t neg;
    uint8_t _pad[3];
    uint32_t len;
    uint32_t rc;
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
// from two shards at once. Three things uphold it: the module-level `let` ban
// (shipped); the barrier the compiler emits in the relinquishing operand of
// every capture, far-select SEND payload and crossing or blocking result,
// which makes the counted leaves of the value private before it travels
// (rt_bigfloat_unshare, whose duplicate is rt_bigfloat_clone; a dynamic
// array's elements are reached one by one through rt_array_unshare_walk) --
// an anchored body's send gives away a capture that barrier already made
// private, and makes nothing itself, because its prefix replays; and the
// compile-time REFUSAL of the shapes that walk cannot reach -- a map's table,
// a channel's ring -- at every gate: capture, channel element and reply.
//
// `int` and `uint` now take that same barrier through their own leaves
// (rt_bigint_unshare, rt_biguint_unshare), on the same non-atomic terms. Float
// keeps its count at offset zero because its emitter form was written before
// the pair existed and nothing aliases its tail; the pair pays a per-kind
// offset instead, for the aliasing reason spelled out above the two structs.
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

// The same pin for the pair, and it does two jobs.
//
// First the absolute offsets, which the LLVM backend prints as literals the way
// it prints float's zero: a uint's count is 4 bytes into the block and an int's
// is 8. Moving either one silently miscompiles every int or uint copy, so the
// numbers live here, where the field order that produces them is visible.
_Static_assert(offsetof(SurgeBigUint, len) == 0, "biguint length must stay at offset 0");
_Static_assert(offsetof(SurgeBigUint, rc) == 4, "biguint refcount must stay at offset 4");
_Static_assert(offsetof(SurgeBigUint, limbs) == 8, "biguint limbs must stay at offset 8");
_Static_assert(offsetof(SurgeBigInt, len) == 4, "bigint length must stay at offset 4");
_Static_assert(offsetof(SurgeBigInt, rc) == 8, "bigint refcount must stay at offset 8");
_Static_assert(offsetof(SurgeBigInt, limbs) == 12, "bigint limbs must stay at offset 12");
_Static_assert(sizeof(((SurgeBigUint*)0)->rc) == 4, "biguint refcount must stay a 32-bit word");
_Static_assert(sizeof(((SurgeBigInt*)0)->rc) == 4, "bigint refcount must stay a 32-bit word");

// Then the alias itself, stated as the relation rather than as three numbers
// that happen to line up. `bi_as_uint` below reinterprets a bigint's tail --
// everything from `len` onward -- as a biguint, so every field of the view must
// sit the same distance past `len` in both structs. Written this way the pair
// of asserts survives a future change to the absolute offsets and still refuses
// the one change that matters: a field added to, removed from, or reordered
// within either tail, which would make the view read one member where the int
// keeps another. That is the trap a prefix count would have introduced, and
// these two lines are what stop it coming back without a compiler error.
_Static_assert(offsetof(SurgeBigInt, rc) - offsetof(SurgeBigInt, len) ==
                   offsetof(SurgeBigUint, rc) - offsetof(SurgeBigUint, len),
               "a bigint's tail must place rc exactly where a biguint does");
_Static_assert(offsetof(SurgeBigInt, limbs) - offsetof(SurgeBigInt, len) ==
                   offsetof(SurgeBigUint, limbs) - offsetof(SurgeBigUint, len),
               "a bigint's tail must place limbs exactly where a biguint does");
_Static_assert(sizeof(SurgeBigInt) - offsetof(SurgeBigInt, len) == sizeof(SurgeBigUint),
               "a bigint's tail must be exactly one biguint header long");

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

// A biguint VIEW of a bigint's magnitude. The result is a pointer INTO the int's
// block, not a block of its own, and the assertions above are what make the
// reinterpretation legal field by field.
//
// A view is a READ and never an owner, and since the count moved into the tail
// that rule has teeth: the view's `rc` is the same address as the int's own
// count. Retaining or releasing through a view would mutate the int's count
// behind its back, and freeing one would hand the allocator an interior pointer
// four bytes past the block it was given. Every caller here passes the view
// straight to a magnitude helper that returns a fresh block; the `const` return
// discourages the rest but does not enforce it, so the rule is written down.
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

// BigInt helpers. bi_alloc and bi_clone are defined in rt_bignum_int_alloc.c
// rather than beside the rest of them: bi_alloc has to initialise the new count
// and rt_bignum_int.c is a legacy-size file the size gate forbids growing.
// bi_clone travelled with it because the exported lifecycle needs a duplicate
// and open-coding a second copy loop would be the same function twice.
SurgeBigInt* bi_alloc(uint32_t len, bn_err* err);
SurgeBigInt* bi_clone(const SurgeBigInt* i, bn_err* err);
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
