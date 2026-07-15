#include "rt_bignum_internal.h"

// Tagged int/uint arithmetic entry points. Each takes the allocation-free
// fast path when its operands are inline-or-zero (see rt_bignum_tag.h) and
// otherwise promotes to the heap bi_*/bu_* implementation via the bridging
// helpers declared in rt_bignum_internal.h and defined in rt_bignum_api.c.

// ---- signed int arithmetic ----------------------------------------------

void* rt_bigint_add(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    int64_t r = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv) && !__builtin_add_overflow(av, bv, &r)) {
        return bi_from_i64_tagged(r);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_add(ao.p, bo.p, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_sub(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    int64_t r = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv) && !__builtin_sub_overflow(av, bv, &r)) {
        return bi_from_i64_tagged(r);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_sub(ao.p, bo.p, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_mul(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    int64_t r = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv) && !__builtin_mul_overflow(av, bv, &r)) {
        return bi_from_i64_tagged(r);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_mul(ao.p, bo.p, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_div(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    // Inline range excludes INT64_MIN, so av / bv never overflows.
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv) && bv != 0) {
        return bi_from_i64_tagged(av / bv);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_div_mod(ao.p, bo.p, NULL, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_mod(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv) && bv != 0) {
        return bi_from_i64_tagged(av % bv);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* rem = NULL;
    bi_div_mod(ao.p, bo.p, &rem, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(rem);
}

void* rt_bigint_neg(const void* a) {
    int64_t av = 0;
    if (fixi_as_i64(a, &av)) {
        return bi_from_i64_tagged(-av);
    }
    bi_operand ao = bi_promote(a);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_neg(ao.p, &err);
    bi_operand_release(&ao);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_abs(const void* a) {
    int64_t av = 0;
    if (fixi_as_i64(a, &av)) {
        return bi_from_i64_tagged(av < 0 ? -av : av);
    }
    bi_operand ao = bi_promote(a);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_abs_val(ao.p, &err);
    bi_operand_release(&ao);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

int32_t rt_bigint_cmp(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv)) {
        return (int32_t)((av > bv) - (av < bv));
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    int32_t c = (int32_t)bi_cmp(ao.p, bo.p);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    return c;
}

void* rt_bigint_bit_and(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv)) {
        return bi_from_i64_tagged(av & bv);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op(ao.p, bo.p, bu_and, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_bit_or(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv)) {
        return bi_from_i64_tagged(av | bv);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op(ao.p, bo.p, bu_or, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_bit_xor(const void* a, const void* b) {
    int64_t av = 0;
    int64_t bv = 0;
    if (fixi_as_i64(a, &av) && fixi_as_i64(b, &bv)) {
        return bi_from_i64_tagged(av ^ bv);
    }
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_bit_op(ao.p, bo.p, bu_xor, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bi_finish(out);
}

void* rt_bigint_shl(const void* a, const void* b) {
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_shl(ao.p, bo.p, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic("integer overflow");
    }
    return bi_finish(out);
}

void* rt_bigint_shr(const void* a, const void* b) {
    bi_operand ao = bi_promote(a);
    bi_operand bo = bi_promote(b);
    bn_err err = BN_OK;
    SurgeBigInt* out = bi_shr(ao.p, bo.p, &err);
    bi_operand_release(&ao);
    bi_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic("integer overflow");
    }
    return bi_finish(out);
}

// ---- unsigned int arithmetic --------------------------------------------

void* rt_biguint_add(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    uint64_t r = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv) && !__builtin_add_overflow(av, bv, &r)) {
        return bu_from_u64_tagged(r);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_add(ao.p, bo.p, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_sub(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv) && av >= bv) {
        return bu_from_u64_tagged(av - bv);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_sub(ao.p, bo.p, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_mul(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    uint64_t r = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv) && !__builtin_mul_overflow(av, bv, &r)) {
        return bu_from_u64_tagged(r);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_mul(ao.p, bo.p, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_div(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv) && bv != 0) {
        return bu_from_u64_tagged(av / bv);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_div_mod(ao.p, bo.p, NULL, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_mod(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv) && bv != 0) {
        return bu_from_u64_tagged(av % bv);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* rem = NULL;
    bu_div_mod(ao.p, bo.p, &rem, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(rem);
}

int32_t rt_biguint_cmp(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv)) {
        return (int32_t)((av > bv) - (av < bv));
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    int32_t c = (int32_t)bu_cmp(ao.p, bo.p);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    return c;
}

void* rt_biguint_bit_and(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv)) {
        return bu_from_u64_tagged(av & bv);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_and(ao.p, bo.p, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_bit_or(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv)) {
        return bu_from_u64_tagged(av | bv);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_or(ao.p, bo.p, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_bit_xor(const void* a, const void* b) {
    uint64_t av = 0;
    uint64_t bv = 0;
    if (fixu_as_u64(a, &av) && fixu_as_u64(b, &bv)) {
        return bu_from_u64_tagged(av ^ bv);
    }
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_xor(ao.p, bo.p, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_shl(const void* a, const void* b) {
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    int shift = 0;
    if (!shift_count_from_biguint(bo.p, &shift)) {
        bu_operand_release(&ao);
        bu_operand_release(&bo);
        bignum_panic("integer overflow");
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_shl(ao.p, shift, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}

void* rt_biguint_shr(const void* a, const void* b) {
    bu_operand ao = bu_promote(a);
    bu_operand bo = bu_promote(b);
    int shift = 0;
    if (!shift_count_from_biguint(bo.p, &shift)) {
        bu_operand_release(&ao);
        bu_operand_release(&bo);
        bignum_panic("integer overflow");
        return NULL;
    }
    bn_err err = BN_OK;
    SurgeBigUint* out = bu_shr(ao.p, shift, &err);
    bu_operand_release(&ao);
    bu_operand_release(&bo);
    if (err != BN_OK) {
        bignum_panic_err(err);
    }
    return bu_finish(out);
}
